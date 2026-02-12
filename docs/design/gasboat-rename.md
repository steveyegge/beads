# Gasboat Rename Plan

> gastown → gasboat — in honor of becoming Kubernetes-only.

## Scope

| Repo | References | Files | Automation |
|------|-----------|-------|------------|
| **gastown** (primary) | ~9,095 | ~768 | ~95% sed-able |
| **beads** (consumer) | ~683 | ~82 | ~95% sed-able |

## Rename Layers (gastown repo)

### Build-blocking (must be correct for compilation)

1. **Go module paths** — 3 `go.mod` files
   - `github.com/steveyegge/gastown` → `github.com/steveyegge/gasboat`
   - `github.com/steveyegge/gastown/controller` → `gasboat/controller`
   - `github.com/steveyegge/gastown/mobile` → `gasboat/mobile`
   - Cascades to **441 Go import paths** across all `.go` files

2. **Proto packages** — 17 `.proto` files
   - `package gastown.v1` → `gasboat.v1`
   - Directory: `proto/gastown/v1/` → `proto/gasboat/v1/`
   - Regenerate with `buf generate` → updates 75+ `.pb.go` files
   - Connect stubs: `gastownv1connect/` → `gasboatv1connect/`

3. **CRD types** — `api/v1alpha1/gastown_types.go`
   - `type GasTown struct` → `type GasBoat struct`
   - `type GasTownSpec struct` → `type GasBoatSpec struct`
   - `type GasTownStatus struct` → `type GasBoatStatus struct`

### Infrastructure (must be correct for deploy)

4. **Docker images** — 3 image names
   - `ghcr.io/groblegark/gastown/gastown-agent` → `gasboat/gasboat-agent`
   - `ghcr.io/groblegark/gastown/agent-controller` → `gasboat/agent-controller`
   - `ghcr.io/groblegark/gastown-toolchain` → `gasboat-toolchain`

5. **Helm charts** — 2 charts
   - `helm/gastown/Chart.yaml` name field + directory
   - `helm/agent-pod/Chart.yaml` keywords/descriptions
   - All `values*.yaml` image repository references
   - fics-helm-chart wrapper dependency

6. **K8s namespaces** — deploy manifests
   - `namespace: gastown` → `namespace: gasboat`
   - gastown-next → gasboat-next, gastown-ha → gasboat-ha

7. **CI/CD** — 17 workflow files
   - Image metadata, registry paths, repository references

### Documentation & strings

8. **README, CHANGELOG, docs/** — 83+ markdown files
9. **String literals** — 758+ double-quoted `"gastown"` in Go code
10. **Formula files** — 18 files with gastown in name or content
11. **Comments** — "Gas Town" → "Gas Boat" throughout

## Beads Repo Impact

Beads has **no Go imports** of gastown. All references are string-based:

- **Rig names**: `"gastown"`, `"rig:gastown"` (66+ occurrences)
- **Agent IDs**: `"gastown/witness"`, `"gastown/crew/wolf"`, etc. (50+)
- **Test data**: `"gt-gastown-witness"`, `"gt-gastown-polecat-*"` prefixes
- **Config scopes**: `"town:gt11,rig:gastown"` patterns (30+)
- **External refs**: `"external:gastown:*"` (2)
- **Formula filenames**: `mol-gastown-boot.formula.toml`, etc. (3)
- **Helm values**: `gastown-ha.yaml` (1)
- **Design docs**: References in 16 markdown files

## Automation Script

Three replacement patterns cover ~95%:

```bash
# Pattern 1: lowercase (module paths, imports, strings, rig names)
gastown → gasboat

# Pattern 2: CamelCase (Go type names, CRD types)
GasTown → GasBoat

# Pattern 3: display name (docs, comments, helm descriptions)
Gas Town → Gas Boat
```

### Script outline

```bash
#!/bin/bash
# Phase 1: gastown repo
cd ~/gastown

# Go module + imports (441 files)
find . -name '*.go' -o -name 'go.mod' | xargs sed -i '' \
  -e 's|github.com/steveyegge/gastown|github.com/steveyegge/gasboat|g'

# Proto packages + directories
find . -name '*.proto' | xargs sed -i '' \
  -e 's/package gastown\./package gasboat./g' \
  -e 's|gastown/v1|gasboat/v1|g'
# Rename directories
mv proto/gastown proto/gasboat
mv gen/gastown gen/gasboat
mv mobile/proto/gastown mobile/proto/gasboat
mv mobile/gen/gastown mobile/gen/gasboat

# CRD types
sed -i '' 's/GasTown/GasBoat/g' api/v1alpha1/gastown_types.go
mv api/v1alpha1/gastown_types.go api/v1alpha1/gasboat_types.go

# Docker images
find . -name '*.yaml' -o -name '*.yml' | xargs sed -i '' \
  -e 's|groblegark/gastown|groblegark/gasboat|g'

# Helm chart directory
mv helm/gastown helm/gasboat

# All remaining string replacements
find . -type f \( -name '*.go' -o -name '*.yaml' -o -name '*.yml' \
  -o -name '*.md' -o -name '*.toml' -o -name '*.json' \) | xargs sed -i '' \
  -e 's/GasTown/GasBoat/g' \
  -e 's/gastown/gasboat/g' \
  -e 's/Gas Town/Gas Boat/g'

# Regenerate
buf generate
go mod tidy
make build && make test

# Phase 2: beads repo
cd ~/beads

# String replacements (rig names, agent IDs, test data)
find . -type f \( -name '*.go' -o -name '*.yaml' -o -name '*.yml' \
  -o -name '*.md' -o -name '*.toml' \) \
  ! -path './.git/*' ! -path './.beads/*' | xargs sed -i '' \
  -e 's/GasTown/GasBoat/g' \
  -e 's/gastown/gasboat/g' \
  -e 's/Gas Town/Gas Boat/g'

# Rename formula files
mv .beads/formulas/mol-gastown-boot.formula.toml \
   .beads/formulas/mol-gasboat-boot.formula.toml
mv .beads/formulas/gastown-release.formula.toml \
   .beads/formulas/gasboat-release.formula.toml
mv .beads/formulas/gastown-github-release.formula.toml \
   .beads/formulas/gasboat-github-release.formula.toml

# Rename helm values
mv helm/bd-daemon/values/gastown-ha.yaml \
   helm/bd-daemon/values/gasboat-ha.yaml

make build && make test
```

## GitHub Repository

**Option A**: Rename existing `groblegark/gastown` → `groblegark/gasboat`
- GitHub auto-redirects old URLs
- Preserves stars, issues, PRs, CI history
- Go module proxy may cache old paths (run `GOPROXY=direct go get`)

**Option B**: Create fresh `groblegark/gasboat`
- Clean slate, no redirect confusion
- Must update all references (fics-helm-chart, beads CI, deploy scripts)
- Lose PR/issue history

**Recommendation**: Option A (rename) — simpler, preserves history.

## Cleanup Opportunities (do before rename)

### Stale artifacts to remove
- `agent-controller` binary (191MB, untracked)
- `nats-8.5.4.tgz` (downloaded archive)
- `.events.jsonl`, `daemon/`, `logs/`, `mayor/` (runtime artifacts)
- Add to `.gitignore` if regenerated at runtime

### Outdated design docs to archive
- `BEADS_NO_DAEMON_CHANGEPOINTS.md` — references removed concepts
- `IMPL_PLAN_BEADS_NO_DAEMON.md` — superseded by K8s Habitat epic

### Remaining K8s Habitat tasks (7 remaining)
- bd-mwmfu: Simplify PersistentPreRunE
- bd-pxjbz: Remove local routing in routed.go
- bd-xnuhq: Remove TmuxBackend from beads internal/coop/
- bd-kvbgg: Replace tmux capture-pane with Coop API
- bd-55155: Remove TmuxLister from SessionRegistry (gastown)
- bd-541oe: Remove LocalBackend() in handoff.go (gastown)
- bd-tecmp: Delete SSH/Tmux integration tests (gastown)

## Recommended Sequence

1. Complete remaining K8s Habitat tasks (clean dead code first)
2. Clean stale artifacts + archive outdated docs
3. Run rename script on both repos
4. Verify builds + tests pass
5. Rename GitHub repo (Option A)
6. Rebuild + push Docker images under new names
7. Update Helm chart + fics-helm-chart wrapper
8. Redeploy to gasboat-next namespace
