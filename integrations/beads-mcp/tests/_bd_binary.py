"""Shared bd binary resolution and capability probes for integration tests."""

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
from pathlib import Path

REQUIRED_FLOW_SUBCOMMANDS = (
    "claim-next",
    "create-discovered",
    "block-with-context",
    "close-safe",
)


def _resolve_path(candidate: str) -> str | None:
    """Resolve a candidate binary path to an absolute executable path."""
    if not candidate:
        return None
    resolved = shutil.which(candidate) if "/" not in candidate else candidate
    if not resolved:
        return None
    path = Path(resolved)
    if not path.exists() or not os.access(str(path), os.X_OK):
        return None
    return str(path.resolve())


def _candidate_paths() -> list[str]:
    """Build ordered candidate paths for the custom bd binary."""
    configured = os.environ.get("BEADS_PATH") or os.environ.get("BEADS_BD_PATH")
    if configured:
        resolved = _resolve_path(configured)
        return [resolved] if resolved else []

    candidates: list[str] = []

    explicit_candidates = os.environ.get("BEADS_TEST_BD_CANDIDATES", "")
    if explicit_candidates:
        for item in explicit_candidates.split(os.pathsep):
            resolved = _resolve_path(item.strip())
            if resolved:
                candidates.append(resolved)

    repo_root = Path(__file__).resolve().parents[3]
    repo_bd = _resolve_path(str(repo_root / "bd"))
    if repo_bd:
        candidates.append(repo_bd)

    path_bd = _resolve_path("bd")
    if path_bd:
        candidates.append(path_bd)

    # Preserve order while removing duplicates.
    unique: list[str] = []
    seen: set[str] = set()
    for candidate in candidates:
        if candidate and candidate not in seen:
            unique.append(candidate)
            seen.add(candidate)
    return unique


def probe_bd_capabilities(bd_executable: str) -> None:
    """Fail-fast probe for required flow subcommands plus runtime write viability."""
    process = subprocess.run(
        [bd_executable, "flow", "--help"],
        capture_output=True,
        text=True,
        check=False,
        stdin=subprocess.DEVNULL,
    )
    help_text = f"{process.stdout}\n{process.stderr}"
    if process.returncode != 0:
        raise RuntimeError(
            f"`{bd_executable} flow --help` failed with code {process.returncode}. "
            "Set BEADS_PATH/BEADS_BD_PATH to your custom-fork bd binary."
        )

    missing = [item for item in REQUIRED_FLOW_SUBCOMMANDS if item not in help_text]
    if missing:
        raise RuntimeError(
            f"bd binary `{bd_executable}` is missing required flow subcommands: {', '.join(missing)}. "
            "Set BEADS_PATH/BEADS_BD_PATH to your custom-fork bd binary."
        )

    # Ensure the candidate can actually perform write operations in an isolated workspace.
    with tempfile.TemporaryDirectory(prefix="beads_mcp_probe_", dir="/tmp") as workspace_root:
        env = os.environ.copy()
        env.pop("BEADS_DB", None)
        env.pop("BEADS_DIR", None)
        init = subprocess.run(
            [bd_executable, "init", "--prefix", "probe", "--quiet"],
            capture_output=True,
            text=True,
            check=False,
            stdin=subprocess.DEVNULL,
            cwd=workspace_root,
            env=env,
        )
        if init.returncode != 0:
            raise RuntimeError(
                f"`{bd_executable} init` failed in probe workspace (code {init.returncode}): "
                f"{(init.stderr or init.stdout).strip()}"
            )

        create = subprocess.run(
            [bd_executable, "create", "probe issue", "-p", "2", "--json"],
            capture_output=True,
            text=True,
            check=False,
            stdin=subprocess.DEVNULL,
            cwd=workspace_root,
            env=env,
        )
        if create.returncode != 0:
            raise RuntimeError(
                f"`{bd_executable} create` failed after probe init (code {create.returncode}): "
                f"{(create.stderr or create.stdout).strip()}"
            )


def resolve_bd_executable() -> str:
    """Resolve and probe the bd executable used by integration tests."""
    candidates = _candidate_paths()
    if not candidates:
        raise RuntimeError(
            "No bd executable found. Set BEADS_PATH or BEADS_BD_PATH to your custom-fork bd binary."
        )

    errors: list[str] = []
    for candidate in candidates:
        try:
            probe_bd_capabilities(candidate)
            return candidate
        except RuntimeError as exc:
            errors.append(f"{candidate}: {exc}")

    raise RuntimeError(
        "No compatible bd binary found after capability probing.\n"
        + "\n".join(errors)
    )
