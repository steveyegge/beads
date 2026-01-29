import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, os.path.abspath("scripts"))

import skills_manifest as sm


class SkillsManifestTest(unittest.TestCase):
    def test_discover_and_tiers(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            codex_root = tmp_path / "codex" / "skills"
            super_root = tmp_path / "codex" / "superpowers" / "skills"
            claude_root = tmp_path / "claude" / "skills"

            (codex_root / "writing-clearly-and-concisely").mkdir(parents=True)
            (codex_root / "writing-clearly-and-concisely" / "SKILL.md").write_text("codex skill")

            (super_root / "brainstorming").mkdir(parents=True)
            (super_root / "brainstorming" / "SKILL.md").write_text("super skill")

            (claude_root / "prompting.md").parent.mkdir(parents=True, exist_ok=True)
            (claude_root / "prompting.md").write_text("claude skill")

            config = sm.ManifestConfig(
                codex_paths=[str(codex_root), str(super_root)],
                claude_paths=[str(claude_root)],
                must_have={"superpowers:brainstorming"},
                optional={"writing-clearly-and-concisely"},
                personal={"prompting"},
            )

            manifest = sm.build_manifest(config, generated_at="2026-01-29T00:00:00Z")
            names = {entry["name"]: entry for entry in manifest["skills"]}

            self.assertIn("superpowers:brainstorming", names)
            self.assertEqual(names["superpowers:brainstorming"]["tier"], "must-have")

            self.assertIn("writing-clearly-and-concisely", names)
            self.assertEqual(names["writing-clearly-and-concisely"]["tier"], "optional")

            self.assertNotIn("prompting", names)

    def test_compare_manifests_errors_and_warnings(self):
        expected = {
            "version": 1,
            "generated_at": "2026-01-29T00:00:00Z",
            "skills": [
                {
                    "name": "superpowers:brainstorming",
                    "source": "codex",
                    "tier": "must-have",
                    "path": "~/.codex/superpowers/skills/brainstorming/SKILL.md",
                    "sha256": "aaa",
                    "bytes": 10,
                },
                {
                    "name": "writing-clearly-and-concisely",
                    "source": "codex",
                    "tier": "optional",
                    "path": "~/.codex/skills/writing-clearly-and-concisely/SKILL.md",
                    "sha256": "bbb",
                    "bytes": 20,
                },
            ],
        }

        actual = {
            "version": 1,
            "generated_at": "2026-01-29T00:00:00Z",
            "skills": [
                {
                    "name": "writing-clearly-and-concisely",
                    "source": "codex",
                    "tier": "optional",
                    "path": "~/.codex/skills/writing-clearly-and-concisely/SKILL.md",
                    "sha256": "ccc",
                    "bytes": 20,
                }
            ],
        }

        errors, warnings, extras = sm.compare_manifests(expected, actual)

        self.assertEqual(len(errors), 1)
        self.assertIn("superpowers:brainstorming", errors[0])
        self.assertEqual(len(warnings), 1)
        self.assertIn("writing-clearly-and-concisely", warnings[0])
        self.assertEqual(extras, [])


if __name__ == "__main__":
    unittest.main()
