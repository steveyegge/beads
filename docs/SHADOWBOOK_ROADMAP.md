# Shadowbook Roadmap

A focused roadmap built on the current feature set.

---

## Now (High Impact)

1) **Semantic Compaction (Heuristic)**
- Improve summaries with structured extraction from spec text.
- Keep deterministic behavior and no external dependencies.

2) **Context Budgeting**
- Track per‑project spec token usage.
- Warn when active specs exceed a budget.

3) **Auto‑Link Quality**
- Add `--explain` to show why a match scored well.
- Add `--min-score` guardrails in CI mode.

---

## Next (UX + Integration)

1) **Spec Diff**
- Show changes since last scan.
- Optionally summarize impact.

2) **Spec Tree + Coverage**
- Visualize spec folders with linked‑issue counts.

3) **Notifications**
- Slack/webhook on spec change + open linked issues.

---

## Later (Integrations)

1) **Notion/Confluence Sync**
- Pull spec content to local `specs/`.
- Preserve drift detection on local copies.

2) **GitHub Issues Bridge**
- Map external issues to spec IDs.
- Sync changes back to Beads.

---

## Research (AI‑Assisted)

1) **Semantic Diff Explanation**
2) **Auto‑Task Generation**
3) **Spec‑to‑Code Audit**
