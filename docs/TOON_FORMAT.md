# TOON Format Guide for bdtoon

## Overview

**TOON** (Token-Oriented Object Notation) is a compact, human-readable encoding of JSON data that uses ~40% fewer tokens while maintaining full JSON semantics. This guide explains TOON syntax and how it applies to bdtoon issues.

### Why TOON?

- **Token Efficiency**: 74% accuracy vs JSON's 70% with 40% fewer tokens (mixed-structure benchmarks)
- **LLM-Friendly**: Explicit structure markers ([n] for arrays, {fields} headers) help models parse reliably
- **Human-Readable**: YAML-like indentation with CSV-style compactness
- **JSON Compatible**: Deterministic, lossless round-trip conversion
- **Git-Friendly**: Structure makes diffs and merges cleaner than JSON

## TOON Syntax Basics

### 1. Objects (Maps)

**JSON:**
```json
{
  "id": "bd-1",
  "title": "Fix auth bug",
  "priority": 1
}
```

**TOON:**
```
{id,title,priority}:
  bd-1,Fix auth bug,1
```

The `{field,names}:` header declares fields once, then values stream as CSV rows.

### 2. Arrays with Uniform Elements

**JSON:**
```json
{
  "issues": [
    {"id": "1", "title": "First"},
    {"id": "2", "title": "Second"}
  ]
}
```

**TOON:**
```
issues[2]{id,title}:
  1,First
  2,Second
```

The `[2]` marker shows array length. Objects in arrays use the tabular format.

### 3. Nested Structures

**JSON:**
```json
{
  "issue": {
    "id": "bd-1",
    "labels": ["bug", "urgent"]
  }
}
```

**TOON:**
```
issue:
  {id,labels}:
    bd-1
    labels[2]:
      bug
      urgent
```

Indentation shows nesting levels. Arrays are indented under their field.

### 4. Null/Empty Handling

**JSON:**
```json
{
  "closed_at": null,
  "description": "",
  "labels": []
}
```

**TOON:**
```
{closed_at,description,labels[0]}:
  null,,
```

- `null` represents null values
- Empty string shown as empty field between commas
- `[0]` for empty arrays (length marker required)

## bdtoon Issue in TOON Format

### Single Issue Example

**Storage Format** (what's saved in ~/.bdt/issues.toon):

Currently, bdtoon stores issues in **JSON Lines** format (one JSON object per line) because:
- Line-oriented format integrates well with git diffs
- TOON encoding is applied on-demand for LLM prompts (not in storage)
- Avoids binary-like file format during development

**Example Issue (JSON Lines):**
```json
{"id":"1","title":"Fix login bug","description":"Users cannot log in with special characters in password","status":"open","priority":1,"issue_type":"bug","assignee":"alice@example.com","created_at":"2025-12-19T10:30:00Z","updated_at":"2025-12-19T10:30:00Z"}
```

### When Presented to LLMs (TOON Format)

The same issue encoded as TOON for agent prompts:

```
issues[1]{id,title,description,status,priority,issue_type,assignee,created_at,updated_at}:
  1,Fix login bug,Users cannot log in with special characters in password,open,1,bug,alice@example.com,2025-12-19T10:30:00Z,2025-12-19T10:30:00Z
```

### Multiple Issues (TOON Array Format)

```
issues[3]{id,title,status,priority,issue_type}:
  1,Fix login bug,open,1,bug
  2,Add password reset,open,2,feature
  3,Refactor auth module,in_progress,3,task
```

### Complete Issue with Optional Fields (TOON)

```
{id,title,description,design,acceptance_criteria,notes,status,priority,issue_type,assignee,estimated_minutes,created_at,updated_at,closed_at,close_reason}:
  bd-42,Implement pagination,Users can navigate large result sets,Design docs in Confluence,Must handle >10k records,Implementation notes here,closed,1,feature,bob@example.com,480,2025-12-01T08:00:00Z,2025-12-15T14:30:00Z,2025-12-15T14:30:00Z,Done
```

### Issue with Arrays/Dependencies (TOON)

```
{id,title,labels[2],dependencies[1]{type,depends_on_id}}:
  bd-1,Implement caching,[urgent,performance]
  blocks,bd-2
```

Nested objects in arrays show field list with their own values on following lines.

## bdtoon Issue Schema (JSON)

This is the structure that maps between storage formats:

```json
{
  "id": "string",                              // Issue ID (e.g., "1", "bd-42")
  "title": "string",                           // Required: issue title
  "description": "string",                     // Issue details
  "design": "string",                          // Design notes (optional)
  "acceptance_criteria": "string",             // Acceptance criteria (optional)
  "notes": "string",                           // Implementation notes (optional)
  "status": "open|in_progress|blocked|closed", // Current status
  "priority": 0-4,                             // 0=critical, 4=backlog
  "issue_type": "bug|feature|task|epic|chore", // Type of work
  "assignee": "email@example.com",             // Assigned person (optional)
  "estimated_minutes": 480,                    // Time estimate (optional)
  "created_at": "2025-12-19T10:30:00Z",        // Creation timestamp
  "updated_at": "2025-12-19T10:30:00Z",        // Last update timestamp
  "closed_at": "2025-12-19T14:30:00Z",         // Closure timestamp (if closed)
  "close_reason": "Done",                      // Why issue was closed (optional)
  "labels": ["bug", "urgent"],                 // Tags (optional array)
  "dependencies": [                            // Issue relationships (optional)
    {
      "issue_id": "bd-1",
      "depends_on_id": "bd-2",
      "type": "blocks"
    }
  ],
  "comments": [                                // Issue comments (optional)
    {
      "id": 1,
      "author": "alice@example.com",
      "text": "Found the root cause",
      "created_at": "2025-12-19T11:00:00Z"
    }
  ]
}
```

## Field Definitions

### Core Fields (Always Present)

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique identifier for the issue |
| `title` | string | Short summary of the work (max 500 chars) |
| `status` | enum | Current state (open, in_progress, blocked, closed) |
| `priority` | int | 0-4, where 0 is critical and 4 is backlog |
| `issue_type` | enum | Classification: bug, feature, task, epic, chore |
| `created_at` | ISO 8601 | When the issue was created |
| `updated_at` | ISO 8601 | When the issue was last modified |

### Content Fields (Optional)

| Field | Type | Description |
|-------|------|-------------|
| `description` | string | Detailed problem statement or requirements |
| `design` | string | Design notes and approach documentation |
| `acceptance_criteria` | string | Success criteria for completion |
| `notes` | string | Implementation notes and progress tracking |

### Metadata Fields (Optional)

| Field | Type | Description |
|-------|------|-------------|
| `assignee` | string | Email or name of assigned person |
| `estimated_minutes` | int | Time estimate (null if not estimated) |
| `closed_at` | ISO 8601 | When the issue was closed (null if open) |
| `close_reason` | string | Why the issue was closed (only if closed) |

### Relationship Fields (Optional)

| Field | Type | Description |
|-------|------|-------------|
| `labels` | array | String tags for categorization |
| `dependencies` | array | Related issues and blocking relationships |
| `comments` | array | Discussion thread entries |

## TOON Encoding Rules

### 1. String Quoting

TOON uses minimal quoting to save tokens:

```
// Quoted if contains special chars:
"hello world"      // Space
"it's"             // Apostrophe
"foo,bar"          // Comma (when not in header context)
"line1\nline2"     // Newline

// Not quoted:
hello
42
true
null
```

### 2. Field Ordering

TOON encodes object fields alphabetically by key (deterministic ordering):

```
{acceptance_criteria,assignee,closed_at,created_at,description,design,id,issue_type,notes,priority,status,title,updated_at}
```

This ensures the same issue always encodes identically.

### 3. Null Representation

```
// JSON null values shown as "null":
{id,assignee,closed_at}:
  1,null,null

// Empty strings shown as empty field:
{id,title,description}:
  1,My Issue,
```

### 4. Array Length Markers

Array length is always shown for clarity:

```
{issues[3]}:        // Array of 3 items
{labels[0]}:        // Empty array
{labels[5]}:        // Array of 5 items
```

## TOON for Agent Prompts

When presenting issues to AI agents, use this format:

### Example 1: Simple List

```
Current open issues:

issues[2]{id,title,priority,status}:
  bd-1,Fix authentication bug,1,open
  bd-2,Add password reset feature,2,open

Priorities: 0=critical, 4=backlog
Statuses: open, in_progress, blocked, closed
```

### Example 2: Issue with Context

```
Active issue: bd-42

{id,title,description,status,priority,assignee,created_at}:
  bd-42,Implement caching layer,Add Redis caching to reduce DB load,in_progress,1,alice@example.com,2025-12-01T10:00:00Z

Next steps:
1. Implement cache invalidation strategy
2. Add tests for cache behavior
3. Performance benchmarks
```

### Example 3: Complex Prompt with Relationships

```
Epic: bd-100 (Refactor Authentication)

children[3]{id,title,status}:
  bd-101,Extract OAuth provider,closed
  bd-102,Add MFA support,in_progress
  bd-103,Update login UI,open

blocking[1]{id,title}:
  bd-200,Update security docs

Ready work (unblocked, open):
work[2]{id,title,issue_type,priority}:
  bd-103,Update login UI,task,1
  bd-102,Add MFA support,feature,2
```

## Validation for Agents

When agents parse TOON issue data, verify:

1. **Required Fields Present**: id, title, status, priority, issue_type, created_at, updated_at
2. **Valid Enums**:
   - status: open | in_progress | blocked | closed
   - issue_type: bug | feature | task | epic | chore
   - priority: 0-4 (integer)
3. **Field Types**:
   - Timestamps are ISO 8601 format (YYYY-MM-DDTHH:MM:SSZ)
   - Numeric fields are integers (not quoted)
   - Arrays are properly length-marked
4. **Logical Constraints**:
   - If status="closed", closed_at must be set
   - If estimated_minutes set, must be positive integer
   - assignee must be non-empty if present

## Common TOON Patterns in bdtoon

### Pattern 1: List All Issues

```
{id,title,status,priority}[10]:
  1,Bug in login,open,1
  2,Feature request,open,2
  ...
```

### Pattern 2: Single Issue Detail

```
{id,title,description,design,acceptance_criteria,notes,status,priority,issue_type,assignee,estimated_minutes,created_at,updated_at,closed_at,close_reason,labels[2]}:
  bd-42,Title,Description,Design,Criteria,Notes,open,1,feature,alice@example.com,480,2025-12-01T10:00:00Z,2025-12-01T10:00:00Z,null,null,[urgent,performance]
```

### Pattern 3: Issue with Dependencies

```
{id,title,dependencies[2]{type,depends_on_id}}:
  bd-42,Implement caching
  blocks,bd-43
  blocks,bd-44
```

## Round-Trip Conversion

bdtoon ensures data integrity through round-trip validation:

1. **JSON → Internal (types.Issue)**: Unmarshal JSON lines
2. **Internal → TOON**: `internal/toon.EncodeTOON(issues)`
3. **TOON → Internal**: Parse TOON (not automated yet)
4. **Internal → JSON**: Marshal to JSON lines
5. **Verify**: Original JSON == Round-trip JSON

Currently, round-trip testing is done in:
- `cmd/bdt/export_test.go` - TestRoundTripImportExport
- `internal/toon/encode.go` - Test for EncodeLineByLine

## References

- [TOON Format Specification](https://toonformat.dev)
- [TOON Go Implementation](https://github.com/alpkeskin/gotoon)
- [bdtoon Implementation](../history/PHASE_1_3_REFERENCE.md)
