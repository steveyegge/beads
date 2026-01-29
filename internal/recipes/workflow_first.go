package recipes

// WorkflowFirstAddendum appends a concise workflow-first outline.
// Keep it short to avoid context bloat in editor rules.
const WorkflowFirstAddendum = `## Workflow-First Outline (Optional)

Use this structure for risky or complex changes. Keep each section 1-3 bullets.

- **Validate**: inputs, permissions, preconditions
- **Safety**: limits, rollbacks, idempotency, failure handling
- **Execute**: the core operation
- **Broadcast**: logs, events, user-facing updates
`

// WorkflowFirstTemplate is the standard template plus the workflow-first addendum.
const WorkflowFirstTemplate = Template + "\n" + WorkflowFirstAddendum
