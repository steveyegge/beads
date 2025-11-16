# Benchmark Validation Protocol

**Purpose**: Prevent catastrophic benchmark failures like the 2025-11-16 incident

**Use this skill**: BEFORE running any performance benchmarks

## Incident Summary (2025-11-16)

Entire PR #320 was based on corrupted benchmarks:
- Used stale binaries (10x slower than fresh)
- Claimed fork prevented data loss (FALSE)
- Claimed fork was faster (FALSE - actually 3% slower)
- Multiple "corrections" compounded the error
- All claims retracted after fresh binary rebuild

**See**: `~/Downloads/benchmark-postmortem.md` for full details

---

## MANDATORY Pre-Benchmark Checklist

### Step 1: Clean Build (REQUIRED)

```bash
# Navigate to project
cd /path/to/project

# Clean everything
git clean -fdx
go clean -cache

# Fresh build
go build -o /tmp/binary-test ./cmd/bd

# Verify version
/tmp/binary-test --version
git rev-parse HEAD
# These MUST match!

# Record environment
cat > /tmp/benchmark-env.txt << EOF
Binary: /tmp/binary-test
Built: $(date)
Git commit: $(git rev-parse HEAD)
Go version: $(go version)
OS: $(uname -a)
Binary checksum: $(shasum -a 256 /tmp/binary-test)
EOF
```

**If binary version != git HEAD: STOP - Binary corrupted**

---

### Step 2: Baseline Sanity Check (REQUIRED)

```bash
# Test basic operations
tmpdir=$(mktemp -d)
cd $tmpdir

# Time init (should be ~200-500ms)
time /tmp/binary-test init --prefix test

# Time create (should be ~50-200ms)
time /tmp/binary-test create "Test issue"

# Time list (should be <100ms)
time /tmp/binary-test list

cd - && rm -rf $tmpdir
```

**RED FLAGS (STOP IMMEDIATELY)**:
- ❌ Init takes >1 second
- ❌ Create takes >500ms
- ❌ List takes >200ms
- ❌ Any operation 10x slower than expected

**If any red flag: Binary is corrupted - REBUILD**

---

### Step 3: Run Benchmarks (3+ Times)

```bash
# Run each benchmark at least 3 times
for i in 1 2 3; do
    ./benchmark.sh /tmp/binary-test > results-run-$i.txt 2>&1
    sleep 5  # Cool down
done

# Check consistency
echo "Run 1 time: $(grep "Total time:" results-run-1.txt)"
echo "Run 2 time: $(grep "Total time:" results-run-2.txt)"
echo "Run 3 time: $(grep "Total time:" results-run-3.txt)"

# Calculate variance
# Should be <10% between runs
```

**RED FLAGS**:
- ❌ Variance between runs >10%
- ❌ Any data loss in any run
- ❌ Crashes or errors

**If any red flag: Results unstable - INVESTIGATE**

---

### Step 4: Independent Verification (REQUIRED)

Before publishing ANY benchmark claims:

```bash
# Save all raw results
mkdir -p /tmp/benchmark-review
cp results-*.txt /tmp/benchmark-review/
cp /tmp/benchmark-env.txt /tmp/benchmark-review/

# Create summary
cat > /tmp/benchmark-review/SUMMARY.txt << EOF
Benchmark: $(date)
Binary: /tmp/binary-test
Runs: 3

Results:
$(grep "Total time:" results-run-*.txt)

Claims: [YOUR CLAIMS HERE]

Environment: See benchmark-env.txt
Raw outputs: results-run-*.txt
EOF
```

**Then ask another agent**:
1. "Do summaries match raw files?"
2. "Are numbers internally consistent?"
3. "Do claims follow from evidence?"
4. "Would you bet money on these claims?"

**Do NOT publish until independent agent approves**

---

### Step 5: Claim Validation (REQUIRED)

Before making ANY performance/correctness claim, answer:

1. **Can I explain WHY this difference exists?**
   - Not just "the data shows it"
   - Actual technical reason

2. **Have I run 3+ times with consistent results?**
   - Variance <10%
   - Same conclusion each time

3. **Would I bet money on this claim?**
   - If no: don't make the claim

4. **Has independent agent verified?**
   - Raw data reviewed
   - Claims approved

**If ANY answer is "no": DO NOT MAKE THE CLAIM**

---

## Red Flags: When to STOP

Immediately stop and investigate if:

1. **Performance Anomalies**
   - ❌ 10x slower than expected
   - ❌ 10x faster than expected
   - ❌ Results vary >10% between runs

2. **Data Issues**
   - ❌ Any data loss (even 0.1%)
   - ❌ Corrupted output
   - ❌ Crashes or errors

3. **Binary Issues**
   - ❌ Version != git HEAD
   - ❌ Built >1 hour ago
   - ❌ Fails baseline checks

4. **Process Issues**
   - ❌ Summaries don't match raw files
   - ❌ Agent questions your data
   - ❌ Can't explain WHY difference exists

**When red flag appears: STOP. REBUILD. RE-RUN.**

---

## If Things Look Wrong

**DO NOT**:
- ❌ Patch summaries to match narrative
- ❌ Defend claims when questioned
- ❌ Make excuses for anomalies
- ❌ Publish without verification

**DO**:
- ✅ Stop immediately
- ✅ Rebuild binaries fresh
- ✅ Re-run all benchmarks
- ✅ Investigate discrepancies
- ✅ Retract if wrong

---

## Example: Correct Benchmark Session

```bash
# 1. Clean build
cd ~/projects/beads
git clean -fdx && go clean -cache
go build -o /tmp/bd-test ./cmd/bd
/tmp/bd-test --version  # Verify

# 2. Sanity check
tmpdir=$(mktemp -d) && cd $tmpdir
time /tmp/bd-test init --prefix test  # Should be ~300ms
cd - && rm -rf $tmpdir

# 3. Run benchmark 3x
for i in 1 2 3; do
    ./benchmark.sh /tmp/bd-test > /tmp/run-$i.txt
done

# 4. Check consistency
grep "Total time:" /tmp/run-*.txt
# All should be within 10%

# 5. Independent verification
# [Send to another agent for review]

# 6. Only then: make claims
```

---

## Checklist Summary

Before publishing benchmark results:

- [ ] Fresh build from clean state (git clean -fdx)
- [ ] Binary version matches git HEAD
- [ ] Baseline checks pass (<1s for basic ops)
- [ ] Ran 3+ times with <10% variance
- [ ] Independent agent verified raw data
- [ ] Can explain WHY differences exist
- [ ] Would bet money on claims

**If ANY unchecked: DO NOT PUBLISH**

---

## Emergency Protocol

If at any point during benchmarking:
- Results look suspicious
- Agent questions your data
- Red flags appear
- Something feels wrong

**IMMEDIATELY**:
1. STOP all work
2. Do NOT publish
3. Do NOT defend claims
4. REBUILD binaries fresh
5. RE-RUN all benchmarks
6. SEEK verification

**Better to delay than publish garbage**

---

See also:
- `~/Downloads/benchmark-postmortem.md` - Full incident analysis
- `CLAUDE.md` - Project-specific benchmark notes
- `.git/hooks/pre-commit` - Automated checks

**This skill exists to prevent another disaster. Use it.**
