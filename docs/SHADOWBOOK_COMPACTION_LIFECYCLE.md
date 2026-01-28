# Shadowbook Compaction & Lifecycle Management

## The Problem

Over time, specs accumulate. Issues get created, updated, closed. The JSONL file grows:

```
.beads/issues.jsonl
- Month 1: 2 KB (10 specs, 20 issues)
- Month 3: 50 KB (50 specs, 200 issues, full event history)
- Month 6: 200 KB (150 specs, 500 issues, lots of comments)
- Month 12: 800 KB (300 specs, 1000+ issues)
```

Each git clone pulls full history. CI/CD pipelines slow down. Context bloat increases token consumption for AI agents.

**But the data is valuable.** You don't want to delete it. You want to **compress it intelligently while preserving knowledge.**

---

## The Vision: Semantic Memory Decay

Beads README mentions **"Compaction: Semantic 'memory decay' summarizes old closed tasks to save context window."**

Shadowbook extends this idea to specs:

When a spec is **closed** (all linked issues are done), it enters **memory decay**:

1. **Phase 1 (Hot):** Spec is active, all details preserved
2. **Phase 2 (Warm):** Spec completed, linked to archived issues
3. **Phase 3 (Cold):** Old spec, summarized for historical reference
4. **Phase 4 (Frozen):** Archived, minimal footprint, full detail available on-demand

This preserves knowledge while saving space and tokens.

---

## Ideas (Keeping Beads Vision Intact)

### Idea 1: Spec Lifecycle Tracking

**What:** Track 4 states for each spec

```go
type SpecLifecycle string

const (
    SpecActive      SpecLifecycle = "active"    // In development
    SpecComplete    SpecLifecycle = "complete"  // All issues closed
    SpecArchived    SpecLifecycle = "archived"  // Summarized
    SpecRetired     SpecLifecycle = "retired"   // Historical reference
)
```

**Database Schema:**

```sql
ALTER TABLE spec_registry ADD COLUMN lifecycle TEXT DEFAULT 'active';
ALTER TABLE spec_registry ADD COLUMN completed_at DATETIME;
ALTER TABLE spec_registry ADD COLUMN summary TEXT;           -- AI-generated summary
ALTER TABLE spec_registry ADD COLUMN summary_tokens INTEGER;  -- Context saved
ALTER TABLE spec_registry ADD COLUMN archived_at DATETIME;
```

**How it works:**

```bash
bd spec scan                    # Finds all specs

bd spec show specs/auth.md      # Shows as ACTIVE (has open issues)

# After all linked issues closed:
bd spec show specs/auth.md      # Shows as COMPLETE

# Compact spec:
bd spec compact specs/auth.md   # Generates summary, marks ARCHIVED

# View archived spec:
bd spec show specs/auth.md --history  # Shows summary + original
```

**Beads Vision Preserved:**
- Still git-backed (lifecycle is in .beads/)
- Still distributed (teams sync state)
- Still offline-first (no cloud needed)
- Still simple (just timestamps + summary field)

---

### Idea 2: Semantic Compression (AI-Generated Summaries)

**What:** Use Claude API to summarize old specs

When a spec is closed, generate a one-paragraph summary:

```
Original spec (auth.md):
- 150 lines
- 5 sections
- 12 linked issues
- 3 months of changes

Compacted:
"OAuth 2.0 authentication with Google/GitHub. Implemented PKCE flow,
secure token storage with 30-day refresh, logout endpoint. MFA added
in v2 (TOTP + SMS). Total effort: 240 engineer-hours. Lessons: never
assume provider API stability, test token revocation flows thoroughly."

Size: 150 lines → 2 lines
Tokens: 500 → 50
Searchable: Yes
Recoverable: Yes (original in archive)
```

**Implementation:**

```python
# backend/modules/spec_compaction.py

class SpecCompactor:
    def __init__(self, claude_client):
        self.claude = claude_client
    
    def should_compact(self, spec: SpecRegistry) -> bool:
        """Spec should be compacted if all linked issues are closed"""
        all_issues = self.db.get_issues_by_spec(spec.spec_id)
        completed = sum(1 for i in all_issues if i.status == "closed")
        return completed == len(all_issues) and len(all_issues) > 0
    
    def generate_summary(self, spec: SpecRegistry) -> str:
        """Use Claude to summarize spec + linked issues"""
        
        # Gather context
        spec_content = self._read_spec_file(spec.spec_id)
        linked_issues = self.db.get_issues_by_spec(spec.spec_id)
        issue_summaries = [
            f"- {i.title} ({i.status}): {i.description}"
            for i in linked_issues
        ]
        
        prompt = f"""
        Summarize this specification and its implementation in 1-2 paragraphs.
        Include: what was built, key decisions, lessons learned, effort estimate.
        
        SPEC:
        {spec_content}
        
        LINKED WORK:
        {chr(10).join(issue_summaries)}
        
        SUMMARY (2 paragraphs max):
        """
        
        summary = self.claude.messages.create(
            model="claude-3-5-sonnet-20241022",
            max_tokens=300,
            messages=[{"role": "user", "content": prompt}]
        ).content[0].text
        
        return summary
    
    def compact_spec(self, spec_id: str) -> dict:
        """Compact a closed spec"""
        spec = self.db.get_spec(spec_id)
        
        if not self.should_compact(spec):
            return {"error": "Spec is still active"}
        
        summary = self.generate_summary(spec)
        
        # Update database
        self.db.update_spec(spec_id, {
            "lifecycle": "archived",
            "archived_at": datetime.now(),
            "summary": summary,
            "summary_tokens": self._count_tokens(summary)
        })
        
        return {
            "spec_id": spec_id,
            "original_size_lines": len(self._read_spec_file(spec_id).split('\n')),
            "summary_size_lines": len(summary.split('\n')),
            "tokens_saved": spec.original_tokens - self._count_tokens(summary),
            "archived_at": datetime.now()
        }
```

**Beads Vision Preserved:**
- Summary stored in SQLite (local)
- No external APIs required (optional AI, works without)
- Can be re-generated if needed
- Original spec file still in git
- Distributed naturally (sync the summary)

---

### Idea 3: Spec Deduplication & Consolidation

**What:** Detect overlapping specs and suggest consolidation

```
specs/auth.md (completed 3 months ago)
  - OAuth 2.0 implementation
  
specs/auth-v2.md (completed 1 month ago)
  - Add MFA support
  
specs/auth-v3.md (active)
  - Add SAML support

Suggestion: Consolidate auth-v1 and auth-v2 into one "Authentication" epic
with sub-specs for each version. Reduces duplication, improves searchability.
```

**Implementation:**

```python
class SpecDeduplicator:
    def find_related_specs(self, spec_id: str) -> list:
        """Find specs covering similar domains"""
        
        spec = self.db.get_spec(spec_id)
        all_specs = self.db.list_specs()
        
        # Simple heuristic: matching keywords
        keywords = extract_keywords(spec.title + spec.content)
        
        related = []
        for other in all_specs:
            if other.spec_id == spec_id:
                continue
            
            other_keywords = extract_keywords(other.title + other.content)
            overlap = len(keywords & other_keywords)
            
            if overlap > 3:  # Significant overlap
                related.append({
                    "spec_id": other.spec_id,
                    "title": other.title,
                    "overlap_score": overlap,
                    "lifecycle": other.lifecycle
                })
        
        return sorted(related, key=lambda x: x["overlap_score"], reverse=True)
    
    def consolidate_specs(self, spec_ids: list) -> str:
        """
        Merge multiple specs into one
        Creates new spec with both as references
        """
        specs = [self.db.get_spec(sid) for sid in spec_ids]
        
        # Read original files
        contents = [self._read_spec_file(s.spec_id) for s in specs]
        
        # Use Claude to consolidate
        prompt = f"""
        Consolidate these related specs into one coherent specification.
        Preserve all requirements, remove duplication.
        
        {chr(10).join(contents)}
        
        CONSOLIDATED SPEC:
        """
        
        consolidated = self.claude.messages.create(
            model="claude-3-5-sonnet-20241022",
            max_tokens=2000,
            messages=[{"role": "user", "content": prompt}]
        ).content[0].text
        
        return consolidated
```

**Beads Vision Preserved:**
- Consolidation is optional (doesn't delete specs)
- Links preserved (can trace back to originals)
- Git history intact (consolidation creates new spec, references old)
- Distributed safely (consolidation is metadata, not file deletion)

---

### Idea 4: Archive JSONL (Separate Cold Storage)

**What:** Move old archived specs to separate `.beads/specs-archive.jsonl`

```
.beads/
├── issues.jsonl           # Current active issues (100 KB)
├── specs-archive.jsonl    # Archived specs summary (20 KB)
├── issues-archive.jsonl   # Closed issues, old events (200 KB, gitignored)
└── beads.db              # SQLite cache
```

**Why separate files?**
- Faster git operations on `.beads/issues.jsonl` (only active work)
- Archive can be gitignored if too large
- Keeps main JSONL lean for CI/CD
- Can sync archive separately (to S3, etc.)

**Implementation:**

```python
class SpecArchiver:
    def archive_spec(self, spec_id: str):
        """Move spec from active to archive"""
        
        spec = self.db.get_spec(spec_id)
        
        # Read from active JSONL
        active_issues = self._read_jsonl(".beads/issues.jsonl")
        linked_issues = [i for i in active_issues if i.get("spec_id") == spec_id]
        
        # Write to archive
        archive_issues = self._read_jsonl(".beads/issues-archive.jsonl")
        archive_issues.extend(linked_issues)
        self._write_jsonl(".beads/issues-archive.jsonl", archive_issues)
        
        # Remove from active
        active_issues = [i for i in active_issues if i.get("spec_id") != spec_id]
        self._write_jsonl(".beads/issues.jsonl", active_issues)
        
        # Update spec registry
        self.db.update_spec(spec_id, {"lifecycle": "archived"})
    
    def restore_spec(self, spec_id: str):
        """Bring spec back from archive"""
        
        # Move issues from archive to active
        archive = self._read_jsonl(".beads/issues-archive.jsonl")
        to_restore = [i for i in archive if i.get("spec_id") == spec_id]
        
        active = self._read_jsonl(".beads/issues.jsonl")
        active.extend(to_restore)
        self._write_jsonl(".beads/issues.jsonl", active)
        
        # Remove from archive
        archive = [i for i in archive if i.get("spec_id") != spec_id]
        self._write_jsonl(".beads/issues-archive.jsonl", archive)
        
        self.db.update_spec(spec_id, {"lifecycle": "active"})
```

**Beads Vision Preserved:**
- Still JSONL (git-native)
- Still distributed (archive syncs like code)
- Still transparent (can inspect `.beads/` files)
- Still mergeable (separate files avoid conflicts)
- Optional (doesn't require new tools)

---

### Idea 5: Context Window Awareness

**What:** Track token usage, warn when approaching limit

```python
class ContextTracker:
    def get_context_stats(self) -> dict:
        """Return current context consumption"""
        
        active_specs = self.db.list_specs(lifecycle="active")
        archived_specs = self.db.list_specs(lifecycle="archived")
        
        active_tokens = sum(len(s.content.split()) * 1.3 for s in active_specs)  # rough estimate
        archived_tokens = sum(self._count_tokens(s.summary) for s in archived_specs)
        
        return {
            "active_specs": len(active_specs),
            "active_tokens": active_tokens,
            "archived_specs": len(archived_specs),
            "archived_tokens": archived_tokens,
            "total_tokens": active_tokens + archived_tokens,
            "context_limit": 200000,  # For Claude 3.5 Sonnet
            "usage_percent": (active_tokens + archived_tokens) / 200000 * 100
        }
    
    def get_compaction_recommendations(self) -> list:
        """Suggest specs to compact"""
        
        recommendations = []
        stats = self.get_context_stats()
        
        if stats["usage_percent"] > 70:
            # Find specs that are completed and large
            completed = self.db.list_specs(lifecycle="complete")
            
            for spec in completed:
                size = len(self._read_spec_file(spec.spec_id))
                if size > 500:  # Large spec
                    recommendations.append({
                        "spec_id": spec.spec_id,
                        "current_size": size,
                        "tokens_saved": estimate_savings(spec),
                        "priority": "high"
                    })
        
        return recommendations
```

**CLI Integration:**

```bash
bd spec stats
# Active specs: 15 (45,000 tokens)
# Archived specs: 42 (8,500 tokens via summaries)
# Total context: 53,500 tokens (26% of limit)

bd spec compact --recommend
# Specs to compact for token savings:
#   specs/old-auth-v1.md (2000 lines → 50 lines)
#   specs/legacy-payment.md (1500 lines → 40 lines)
#   specs/deprecated-features.md (800 lines → 30 lines)
# Total savings: ~2500 tokens
```

**Beads Vision Preserved:**
- Just metadata tracking (no deletion)
- Helpful but optional (doesn't force anything)
- Keeps developer in control (can ignore recommendations)
- Transparent (shows all stats)

---

### Idea 6: Spec Correlation & Impact Analysis

**What:** Understand which specs affect each other

```
specs/auth.md (archived)
  ↓ depends on
specs/database.md (archived)
  ↓ used by
specs/api.md (active)
  ↓ required for
specs/mobile-app.md (active)
  
Impact: If auth.md changes, need to review api.md and mobile-app.md
```

**Implementation:**

```python
class SpecDependencyAnalyzer:
    def analyze_dependencies(self) -> dict:
        """Build dependency graph across specs"""
        
        all_specs = self.db.list_specs()
        graph = {}
        
        for spec in all_specs:
            content = self._read_spec_file(spec.spec_id)
            
            # Find references to other specs
            # e.g., "See specs/database.md for schema details"
            referenced = extract_spec_references(content)
            
            graph[spec.spec_id] = {
                "dependencies": referenced,
                "dependents": []  # will fill below
            }
        
        # Build reverse dependencies
        for spec_id, deps in graph.items():
            for dep in deps["dependencies"]:
                if dep in graph:
                    graph[dep]["dependents"].append(spec_id)
        
        return graph
    
    def get_impact_scope(self, spec_id: str) -> dict:
        """When this spec changes, what else is affected?"""
        
        graph = self.analyze_dependencies()
        
        directly_affected = graph[spec_id]["dependents"]
        
        # Transitive closure (affected specs' dependents too)
        transitively_affected = set()
        to_visit = list(directly_affected)
        
        while to_visit:
            current = to_visit.pop()
            if current not in transitively_affected:
                transitively_affected.add(current)
                to_visit.extend(graph[current]["dependents"])
        
        return {
            "spec_id": spec_id,
            "directly_affected_specs": directly_affected,
            "transitively_affected": list(transitively_affected),
            "total_impact": len(directly_affected) + len(transitively_affected),
            "active_specs_affected": [
                s for s in directly_affected + list(transitively_affected)
                if self.db.get_spec(s).lifecycle == "active"
            ]
        }
```

**CLI Usage:**

```bash
bd spec impact specs/auth.md
# Spec: specs/auth.md (ARCHIVED)
# 
# Directly depends on:
#   specs/database.md (ARCHIVED)
#   specs/crypto.md (ARCHIVED)
#
# Specs that depend on this:
#   specs/api.md (ACTIVE) ← needs review!
#   specs/mobile-app.md (ACTIVE) ← needs review!
#
# Impact summary:
#   6 specs total, 3 active (should review these)
#   Risk: MEDIUM (if auth.md is revived, 3 specs need updates)
```

**Beads Vision Preserved:**
- Analysis is read-only (no destructive changes)
- Graph is computed on-demand (no state to sync)
- Helps with planning but doesn't enforce
- Transparent (shows reasoning)

---

## Integration with Sleep Trader & Kite

Shadowbook compaction can integrate with trading apps:

### Use Case 1: Archive Old Market Regimes
```
specs/market-regime-stable.md (ARCHIVED)
  - Used for 6 months, now obsolete
  - Summary: "Tested EMA crossovers in low volatility. Accuracy 62%.
    Worked well Jan-Jun 2025. Deprecated in favor of regime classifier."

bd spec compact specs/market-regime-stable.md
# Saved: 1200 tokens from context window
# Recovery: 1 command (bd spec show --full specs/market-regime-stable.md)
```

### Use Case 2: Spec Dependency for Signal Chaining
```
bd spec impact specs/intraday-alerts.md
# Specs depending on this:
#   specs/sleep-trader.md (ACTIVE)
#   specs/telegram-notifications.md (ACTIVE)
#
# If you change intraday-alerts, test these 2!
```

---

## Lifecycle Commands (Full API)

```bash
# View spec status
bd spec status                          # Show all specs + lifecycle
bd spec status --lifecycle active       # Only active specs
bd spec status --lifecycle archived     # Only archived specs

# Compact a spec
bd spec compact specs/auth.md           # Auto-detect, generate summary
bd spec compact specs/auth.md --summary "Custom summary"  # Override

# Archive without compaction (keep full spec)
bd spec archive specs/old-feature.md    # Move to cold storage

# View archived spec with full detail
bd spec show specs/auth.md --full       # Show summary + original
bd spec show specs/auth.md --history    # Show full change history

# Consolidate related specs
bd spec consolidate specs/auth-v1.md specs/auth-v2.md
# Merges into single "specs/auth.md" with history

# Check impact before changes
bd spec impact specs/auth.md            # What depends on this?

# Context window management
bd spec stats                           # Token usage
bd spec compact --recommend             # Suggest what to compact
bd spec compact --auto-aggressive       # Auto-compact old specs

# Restore from archive
bd spec restore specs/old-feature.md    # Bring back to active
```

---

## Why This Keeps Beads Vision Intact

✅ **Git-backed:** Specs stored in JSONL/SQLite, not proprietary format
✅ **Distributed:** Summaries sync like code
✅ **Offline-first:** No cloud required
✅ **Transparent:** Can inspect `.beads/` directory
✅ **Mergeable:** Separate files avoid conflicts
✅ **Reversible:** Can always restore from git history
✅ **Optional:** Compaction is a feature, not mandatory
✅ **Simple:** Just timestamps + summaries, no complexity
✅ **No vendor lock-in:** Using Claude is optional (could use any LLM)
✅ **Preserves knowledge:** Summary keeps essential context, not lossy deletion

---

## Phase 0 (MVP - Implement First)

- [ ] Spec lifecycle states (active/complete/archived/retired)
- [ ] Track completion timestamps
- [ ] `bd spec status` command
- [ ] Mark spec as archived manually: `bd spec archive <id>`

## Phase 1 (Next)

- [ ] AI-generated summaries: `bd spec compact <id>`
- [ ] Summary storage in database
- [ ] `bd spec show --full` (summary + original)
- [ ] Token counting for context awareness

## Phase 2 (Future)

- [ ] Archive JSONL separation
- [ ] Spec deduplication detection
- [ ] Dependency analysis
- [ ] Automatic compaction recommendations
- [ ] Consolidation suggestions

## Phase 3 (Advanced)

- [ ] Transitive impact analysis
- [ ] Historical trend analysis
- [ ] Seasonal pattern detection (for trading apps)
- [ ] Auto-restore based on query (search archived specs)

---

## References

- Beads: "Compaction: Semantic 'memory decay' summarizes old closed tasks"
- Claude Context Window: 200K tokens (Sonnet 3.5)
- Token counting: https://github.com/anthropics/anthropic-sdk-python
- JSONL format: https://jsonlines.org/
