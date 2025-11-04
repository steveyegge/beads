# Hash-Based ID Generation Design

**Status:** Implemented (bd-166)  
**Version:** 2.0  
**Last Updated:** 2025-10-30

## Overview

bd v2.0 replaces sequential auto-increment IDs (bd-1, bd-2) with content-hash based IDs (bd-af78e9a2) and hierarchical sequential children (bd-af78e9a2.1, .2, .3).

This eliminates ID collisions in distributed workflows while maintaining human-friendly IDs for related work.

## ID Format

### Top-Level IDs (Hash-Based)
```
Format: {prefix}-{6-8-char-hex} (progressive on collision)
Examples: 
  bd-a3f2dd   (6 chars, common case ~97%)
  bd-a3f2dda  (7 chars, rare collision ~3%)
  bd-a3f2dda8 (8 chars, very rare double collision)
```

- **Prefix:** Configurable (bd, ticket, bug, etc.)
- **Hash:** First 6 characters of SHA256 hash (extends to 7-8 on collision)
- **Total length:** 9-11 chars for "bd-" prefix

### Hierarchical Child IDs (Sequential)
```
Format: {parent-id}.{child-number}
Examples:
  bd-a3f2dd.1       (depth 1, 6-char parent)
  bd-a3f2dda.1.2    (depth 2, 7-char parent on collision)
  bd-a3f2dd.1.2.3   (depth 3, max depth)
```

- **Max depth:** 3 levels (prevents over-decomposition)
- **Max breadth:** Unlimited (tested up to 347 children)
- **Max ID length:** ~17 chars at depth 3 (6-char parent + .N.N.N)

## Hash Generation Algorithm

```go
func GenerateHashID(prefix, title, description string, created time.Time, workspaceID string) string {
    h := sha256.New()
    h.Write([]byte(title))
    h.Write([]byte(description))
    h.Write([]byte(created.Format(time.RFC3339Nano)))
    h.Write([]byte(workspaceID))
    hash := hex.EncodeToString(h.Sum(nil))
    return fmt.Sprintf("%s-%s", prefix, hash[:8])
}
```

### Hash Inputs

1. **Title** - Primary identifier for the issue
2. **Description** - Additional context for uniqueness
3. **Created timestamp** - RFC3339Nano format for nanosecond precision
4. **Workspace ID** - Prevents collisions across databases/teams

### Design Decisions

**Why include timestamp?**
- Ensures different issues with identical title+description get unique IDs
- Nanosecond precision makes simultaneous creation unlikely

**Why include workspace ID?**
- Prevents collisions when merging databases from different teams
- Can be hostname, UUID, or team identifier

**Why NOT include priority/type?**
- These fields are mutable and shouldn't affect identity
- Changing priority shouldn't change the issue ID

**Why 6 chars (with progressive extension)?**
- 6 chars (24 bits) = ~16 million possible IDs
- Progressive collision handling: extend to 7-8 chars only when needed
- Optimizes for common case: 97% get short, readable 6-char IDs
- Rare collisions get slightly longer but still reasonable IDs
- Inspired by Git's abbreviated commit SHAs

## Collision Analysis

### Birthday Paradox Probability

For 6-character hex IDs (24-bit space = 2^24 = 16,777,216):

| # Issues | 6-char Collision | 7-char Collision | 8-char Collision |
|----------|------------------|------------------|------------------|
| 100      | ~0.03%           | ~0.002%          | ~0.0001%         |
| 1,000    | 2.94%            | 0.19%            | 0.01%            |
| 10,000   | 94.9%            | 17.0%            | 1.16%            |

**Formula:** P(collision) ≈ 1 - e^(-n²/2N)

**Progressive Strategy:** Start with 6 chars. On INSERT collision, try 7 chars from same hash. On second collision, try 8 chars. This means ~97% of IDs in a 1,000 issue database stay at 6 chars.

### Real-World Risk Assessment

**Low Risk (<10,000 issues):**
- Single team projects: ~1% chance over lifetime
- Mitigation: Workspace ID prevents cross-team collisions
- Fallback: If collision detected, append counter (bd-af78e9a2-2)

**Medium Risk (10,000-50,000 issues):**
- Large enterprise projects
- Recommendation: Monitor collision rate
- Consider 16-char IDs in v3 if collisions occur

**High Risk (>50,000 issues):**
- Multi-team platforms with shared database
- Recommendation: Use 16-char IDs (64 bits) for 2^64 space
- Implementation: Change hash[:8] to hash[:16]

### Collision Detection

The database schema enforces uniqueness via PRIMARY KEY constraint. If a hash collision occurs:

1. INSERT fails with UNIQUE constraint violation
2. Client detects error and retries with modified input
3. Options:
   - Append counter to description: "Fix auth (2)"
   - Wait 1ns and regenerate (different timestamp)
   - Use 16-char hash mode

## Performance

**Benchmark Results (Apple M1 Max):**
```
BenchmarkGenerateHashID-10     3758022    317.4 ns/op
BenchmarkGenerateChildID-10   19689157     60.96 ns/op
```

- Hash ID generation: **~317ns** (well under 1μs requirement) ✅
- Child ID generation: **~61ns** (trivial string concat)
- No performance concerns for interactive CLI use

## Comparison to Sequential IDs

| Aspect | Sequential (v1) | Hash-Based (v2) |
|--------|----------------|-----------------|
| Collision risk | HIGH (offline work) | NONE (top-level) |
| ID length | 5-8 chars | 9-11 chars (avg ~9) |
| Predictability | Predictable (bd-1, bd-2) | Unpredictable |
| Offline-first | ❌ Requires coordination | ✅ Fully offline |
| Merge conflicts | ❌ Same ID, different content | ✅ Different IDs |
| Human-friendly | ✅ Easy to remember | ⚠️ Harder to remember |
| Code complexity | ~2,100 LOC collision resolution | <100 LOC |

## CLI Usage

### Prefix Handling

**Storage:** Always includes prefix (bd-a3f2dd)
**CLI Input:** Prefix optional (both bd-a3f2dd AND a3f2dd accepted)
**CLI Output:** Always shows prefix (copy-paste clarity)
**External refs:** Always use prefix (git commits, docs, Slack)

```bash
# All of these work (prefix optional in input):
bd show a3f2dd
bd show bd-a3f2dd
bd show a3f2dd.1
bd show bd-a3f2dd.1.2

# Output always shows prefix:
bd-a3f2dd [epic] Auth System
  Status: open
  ...
```

### Git-Style Prefix Matching

Like Git commit SHAs, bd accepts abbreviated IDs:

```bash
bd show af78      # Matches bd-af78e9a2 if unique
bd show af7       # ERROR: ambiguous (matches bd-af78e9a2 and bd-af78e9a2.1)
```

## Migration Strategy

### Database Migration

```bash
# Preview migration
bd migrate --hash-ids --dry-run

# Execute migration
bd migrate --hash-ids

# What it does:
# 1. Create child_counters table
# 2. For each existing issue:
#    - Generate hash ID from content
#    - Update all references in dependencies
#    - Update all text mentions in descriptions/notes
# 3. Drop issue_counters table
# 4. Update config to hash_id_mode=true
```

### Backward Compatibility

- Sequential IDs continue working in v1.x
- Hash IDs are opt-in until v2.0
- Migration is one-way (no rollback)
- Export to JSONL preserves both old and new IDs during transition

## Workspace ID Generation

**Recommended approach:**
1. **First run:** Generate UUID and store in `config` table
2. **Subsequent runs:** Reuse stored workspace ID
3. **Collision:** If two databases have same workspace ID, collisions possible but rare

**Alternative approaches:**
- Hostname: Simple but not unique (multiple DBs on same machine)
- Git remote URL: Requires git repository
- Manual config: User sets team identifier (e.g., "team-auth")

**Implementation:**
```go
func (s *SQLiteStorage) getWorkspaceID(ctx context.Context) (string, error) {
    var id string
    err := s.db.QueryRowContext(ctx, 
        `SELECT value FROM config WHERE key = ?`, 
        "workspace_id").Scan(&id)
    if err == sql.ErrNoRows {
        // Generate new UUID
        id = uuid.New().String()
        _, err = s.db.ExecContext(ctx,
            `INSERT INTO config (key, value) VALUES (?, ?)`,
            "workspace_id", id)
    }
    return id, err
}
```

## Future Considerations

### 16-Character Hash IDs (v3.0)

If collision rates become problematic:

```go
// Change from:
return fmt.Sprintf("%s-%s", prefix, hash[:8])

// To:
return fmt.Sprintf("%s-%s", prefix, hash[:16])

// Example: bd-af78e9a2c4d5e6f7
```

**Tradeoffs:**
- ✅ Collision probability: ~0% even at 100M issues
- ❌ Longer IDs: 19 chars vs 11 chars
- ❌ Less human-friendly

### Custom Hash Algorithms

For specialized use cases:
- BLAKE3: Faster than SHA256 (not needed for interactive CLI)
- xxHash: Non-cryptographic but faster (collision resistance?)
- MurmurHash: Used by Jira (consider for compatibility)

## References

- **Epic:** bd-165 (Hash-based IDs with hierarchical children)
- **Implementation:** internal/types/id_generator.go
- **Tests:** internal/types/id_generator_test.go
- **Related:** bd-168 (CreateIssue integration), bd-169 (JSONL format)

## Summary

Hash-based IDs eliminate distributed ID collision problems at the cost of slightly longer, less memorable IDs. Hierarchical children provide human-friendly sequential IDs within naturally-coordinated contexts (epic ownership).

This design enables true offline-first workflows and eliminates ~2,100 lines of complex collision resolution code.
