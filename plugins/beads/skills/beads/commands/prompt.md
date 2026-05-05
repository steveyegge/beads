---
description: Capture user prompts as traceable beads
---

# Prompt Capture

Use `bd prompt capture` to preserve the raw user request that caused work to happen.

```bash
echo 'Raw user prompt text' | bd prompt capture \
  --title "Short prompt summary" \
  --summary "Normalized summary" \
  --session "$BEADS_SESSION_ID" \
  --source-tool codex \
  --parent <parent-id> \
  --stdin \
  --json
```

Prompt capture creates a normal task bead with:

- Description set to the raw prompt text.
- Labels `prompt` and `user-request`, plus any labels passed with `--labels`.
- Metadata fields for `kind=prompt`, `source=user_prompt`, `captured_at`, actor, cwd, repo, git branch/head, session id, source tool, and summary when provided.
- Optional parent-child dependency when `--parent` is provided.

Use prompt beads as provenance. Promote or split them into ordinary feature, task, bug, decision, or question beads when the work becomes clear.
