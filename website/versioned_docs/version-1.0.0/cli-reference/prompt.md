---
id: prompt
title: bd prompt
slug: /cli-reference/prompt
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc prompt`

## bd prompt

Capture user prompts as traceable beads

```
bd prompt
```

### bd prompt capture

Capture a raw user prompt as a bead

```
bd prompt capture [flags]
```

**Flags:**

```
      --body-file string     Read raw prompt text from file (use - for stdin)
  -d, --description string   Raw prompt text
  -l, --labels strings       Additional labels
      --parent string        Parent issue ID for hierarchical child
      --session string       Agent/session identifier
      --silent               Output only the issue ID
      --source-tool string   Tool or agent that captured the prompt
      --stdin                Read raw prompt text from stdin
      --summary string       Short normalized summary of the prompt
      --title string         Prompt bead title
```
