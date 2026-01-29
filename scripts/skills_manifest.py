#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Dict, Iterable, List, Optional, Sequence, Set, Tuple


@dataclass
class ManifestConfig:
    codex_paths: List[str]
    claude_paths: List[str]
    must_have: Set[str]
    optional: Set[str]
    personal: Set[str]


def _utc_now_iso() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def _expand_path(path: str) -> Path:
    return Path(os.path.expanduser(path)).resolve()


def _display_path(path: Path) -> str:
    home = Path.home().resolve()
    try:
        resolved = path.resolve()
    except OSError:
        resolved = path
    if str(resolved).startswith(str(home) + os.sep):
        return str(Path("~") / resolved.relative_to(home))
    return str(resolved)


def _sha256(path: Path) -> Tuple[str, int]:
    data = path.read_bytes()
    return hashlib.sha256(data).hexdigest(), len(data)


def _parse_list(values: Sequence[str]) -> Set[str]:
    items: Set[str] = set()
    for value in values:
        for part in value.split(","):
            part = part.strip()
            if part:
                items.add(part)
    return items


def _load_config(path: Path) -> ManifestConfig:
    if not path.exists():
        return ManifestConfig(codex_paths=[], claude_paths=[], must_have=set(), optional=set(), personal=set())

    raw = json.loads(path.read_text())
    tiers = raw.get("tiers", {})
    return ManifestConfig(
        codex_paths=list(raw.get("codex_paths", [])),
        claude_paths=list(raw.get("claude_paths", [])),
        must_have=set(tiers.get("must-have", [])),
        optional=set(tiers.get("optional", [])),
        personal=set(raw.get("personal", [])),
    )


def _merge_config(base: ManifestConfig, must_have: Set[str], optional: Set[str], personal: Set[str]) -> ManifestConfig:
    return ManifestConfig(
        codex_paths=base.codex_paths,
        claude_paths=base.claude_paths,
        must_have=base.must_have | must_have,
        optional=base.optional | optional,
        personal=base.personal | personal,
    )


def _discover_skills(root: Path, source: str, superpowers_root: Optional[Path]) -> List[Dict[str, str]]:
    skills: List[Dict[str, str]] = []
    if not root.exists():
        return skills

    for skill_file in root.rglob("SKILL.md"):
        if not skill_file.is_file():
            continue
        name = skill_file.parent.name
        if superpowers_root and superpowers_root in skill_file.parents:
            name = f"superpowers:{name}"
        skills.append({"name": name, "source": source, "path": str(skill_file)})

    for skill_file in root.glob("*.md"):
        if skill_file.name == "SKILL.md":
            continue
        if not skill_file.is_file():
            continue
        name = skill_file.stem
        skills.append({"name": name, "source": source, "path": str(skill_file)})

    return skills


def build_manifest(config: ManifestConfig, generated_at: Optional[str] = None) -> Dict[str, object]:
    generated_at = generated_at or _utc_now_iso()
    entries: List[Dict[str, object]] = []

    codex_paths = [_expand_path(p) for p in config.codex_paths]
    claude_paths = [_expand_path(p) for p in config.claude_paths]

    superpowers_root = None
    for path in codex_paths:
        if path.parts[-2:] == ("superpowers", "skills"):
            superpowers_root = path
            break

    seen: Set[Tuple[str, str]] = set()

    for path in codex_paths:
        for skill in _discover_skills(path, "codex", superpowers_root):
            name = skill["name"]
            if name in config.personal:
                continue
            key = (skill["name"], skill["source"])
            if key in seen:
                continue
            seen.add(key)
            sha256, size = _sha256(Path(skill["path"]))
            tier = "must-have" if name in config.must_have else "optional"
            entries.append(
                {
                    "name": name,
                    "source": skill["source"],
                    "tier": tier,
                    "path": _display_path(Path(skill["path"])),
                    "sha256": sha256,
                    "bytes": size,
                }
            )

    for path in claude_paths:
        for skill in _discover_skills(path, "claude", None):
            name = skill["name"]
            if name in config.personal:
                continue
            key = (skill["name"], skill["source"])
            if key in seen:
                continue
            seen.add(key)
            sha256, size = _sha256(Path(skill["path"]))
            tier = "must-have" if name in config.must_have else "optional"
            entries.append(
                {
                    "name": name,
                    "source": skill["source"],
                    "tier": tier,
                    "path": _display_path(Path(skill["path"])),
                    "sha256": sha256,
                    "bytes": size,
                }
            )

    entries.sort(key=lambda item: (item["source"], item["name"]))
    return {
        "version": 1,
        "generated_at": generated_at,
        "skills": entries,
    }


def compare_manifests(expected: Dict[str, object], actual: Dict[str, object]) -> Tuple[List[str], List[str], List[str]]:
    expected_map = {(s["name"], s["source"]): s for s in expected.get("skills", [])}
    actual_map = {(s["name"], s["source"]): s for s in actual.get("skills", [])}

    errors: List[str] = []
    warnings: List[str] = []
    extras: List[str] = []

    for key, entry in expected_map.items():
        name, source = key
        actual_entry = actual_map.get(key)
        tier = entry.get("tier", "optional")
        if actual_entry is None:
            msg = f"missing {name} ({source})"
            if tier == "must-have":
                errors.append(msg)
            else:
                warnings.append(msg)
            continue
        if actual_entry.get("sha256") != entry.get("sha256") or actual_entry.get("bytes") != entry.get("bytes"):
            msg = f"mismatch {name} ({source})"
            if tier == "must-have":
                errors.append(msg)
            else:
                warnings.append(msg)

    for key in actual_map:
        if key not in expected_map:
            extras.append(f"extra {key[0]} ({key[1]})")

    return errors, warnings, extras


def _load_manifest(path: Path) -> Dict[str, object]:
    return json.loads(path.read_text())


def _write_manifest(path: Path, manifest: Dict[str, object]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(manifest, indent=2) + "\n")


def main() -> int:
    parser = argparse.ArgumentParser(description="Generate or check skills manifest.")
    parser.add_argument("--config", default="specs/skills/manifest.config.json", help="Manifest config path")
    parser.add_argument("--out", default="specs/skills/manifest.json", help="Output manifest path")
    parser.add_argument("--must-have", action="append", default=[], help="Comma-separated must-have skills")
    parser.add_argument("--optional", action="append", default=[], help="Comma-separated optional skills")
    parser.add_argument("--personal", action="append", default=[], help="Comma-separated personal skills to exclude")

    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("generate", help="Generate manifest JSON")
    subparsers.add_parser("check", help="Compare generated manifest to tracked manifest")

    args = parser.parse_args()
    config_path = _expand_path(args.config)
    out_path = _expand_path(args.out)

    base_config = _load_config(config_path)
    merged = _merge_config(
        base_config,
        _parse_list(args.must_have),
        _parse_list(args.optional),
        _parse_list(args.personal),
    )

    manifest = build_manifest(merged)

    if args.command == "generate":
        _write_manifest(out_path, manifest)
        return 0

    if not out_path.exists():
        print(f"manifest not found: {out_path}")
        return 2

    expected = _load_manifest(out_path)
    errors, warnings, extras = compare_manifests(expected, manifest)

    for msg in errors:
        print(f"ERROR: {msg}")
    for msg in warnings:
        print(f"WARN: {msg}")
    for msg in extras:
        print(f"WARN: {msg}")

    if errors:
        return 2
    if warnings or extras:
        return 1
    print("OK: skills manifest matches")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
