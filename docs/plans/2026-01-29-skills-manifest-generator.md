# Skills Manifest Generator Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a local skills manifest generator with tiering and a helper target to run the generator plus `bd spec scan`.

**Architecture:** Implement a Python script that scans skill directories, hashes skill files, and writes `specs/skills/manifest.json`. Provide a config file for tier defaults and CLI flags that can override tiers. Add a Makefile target to run the generator and then `bd spec scan`.

**Tech Stack:** Python 3 (standard library), Makefile, JSON.

### Task 1: Add tests for manifest generation and comparison

**Files:**
- Create: `scripts/skills_manifest_test.py`

**Step 1: Write the failing test**

```python
class SkillsManifestTest(unittest.TestCase):
    def test_discover_and_tiers(self):
        # setup temp dirs with SKILL.md
        # build manifest and assert tiers and exclusions
```

**Step 2: Run test to verify it fails**

Run: `python3 -m unittest scripts/skills_manifest_test.py`
Expected: FAIL with ImportError for `skills_manifest` or missing functions.

**Step 3: Write minimal implementation**

Create `scripts/skills_manifest.py` with functions:
- `build_manifest`
- `compare_manifests`

**Step 4: Run test to verify it passes**

Run: `python3 -m unittest scripts/skills_manifest_test.py`
Expected: PASS

**Step 5: Commit**

```bash
git add scripts/skills_manifest_test.py scripts/skills_manifest.py
git commit -m "feat: add skills manifest generator tests"
```

### Task 2: Add config file for tier defaults

**Files:**
- Create: `specs/skills/manifest.config.json`

**Step 1: Write the failing test**

```python
# Extend tests to load config and ensure must-have/optional tiers merge
```

**Step 2: Run test to verify it fails**

Run: `python3 -m unittest scripts/skills_manifest_test.py`
Expected: FAIL for missing config handling.

**Step 3: Implement config support**

Add config load and CLI tier overrides in `scripts/skills_manifest.py`.

**Step 4: Run test to verify it passes**

Run: `python3 -m unittest scripts/skills_manifest_test.py`
Expected: PASS

**Step 5: Commit**

```bash
git add specs/skills/manifest.config.json scripts/skills_manifest.py
git commit -m "feat: add skills manifest config"
```

### Task 3: Add helper Makefile targets

**Files:**
- Modify: `Makefile`

**Step 1: Write the failing test**

Not applicable (Makefile).

**Step 2: Implement targets**

Add:
- `skills-manifest-generate`
- `skills-manifest-check`
- `skills-manifest-sync` (generate + `bd spec scan`)

**Step 3: Verify manually**

Run:
- `python3 scripts/skills_manifest.py generate`
- `python3 scripts/skills_manifest.py check`

**Step 4: Commit**

```bash
git add Makefile
git commit -m "feat: add skills manifest make targets"
```

### Task 4: Generate baseline manifest

**Files:**
- Create: `specs/skills/manifest.json`

**Step 1: Generate manifest**

Run: `python3 scripts/skills_manifest.py generate`

**Step 2: Run spec scan**

Run: `bd spec scan`

**Step 3: Commit**

```bash
git add specs/skills/manifest.json
git commit -m "chore: add skills manifest baseline"
```
