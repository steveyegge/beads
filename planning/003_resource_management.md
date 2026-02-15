# Implementation Plan - Centralized Resource Management

The goal is to complete the implementation of the Centralized Resource Management feature in `beads`. We have a working SQLite storage and local file discovery. We need to add CLI filtering, the resource matching logic, and a simplified Linear adapter.

## User Review Required

> [!IMPORTANT]
> The **Linear Integration** is focused on providing **Tags** (from Linear Labels), not mapping Users to Agents.
> The primary focus is the **Resolver** (Matching Logic) that respects Budget and Tags.

## Proposed Changes

### 1. Refactor Discovery (`internal/discovery`)
- **Objective**: Introduce a `ResourceSource` interface to plug in multiple providers.
- **Changes**:
    - **`discovery.go`**: Define `type ResourceSource interface { Name() string; Discover(ctx) ([]*Resource, error) }`.
    - **`local.go`**: Extract existing filesystem scanning logic into `LocalSource`.
    - **`linear.go`**: Implement `LinearSource` to fetch **Labels** from Linear API.
        - **Mapping**: Linear Labels -> `Resource{Type: "skill", Name: LabelName}` OR simply act as a validator for tags.
        - *Correction*: The plan says "Linear Labels -> Skill". We will fetch them as `Type: Skill` resources for now, as that allows them to be used in matching.
        - **Config**: Read `LINEAR_API_KEY` from env.

### 2. Matching Engine (`internal/resolver`)
- **Objective**: Create the logic to select the best resource for a given task based on Budget and Capabilities.
- **Changes**:
    - Create `internal/resolver/resolver.go`.
    - Implement `StandardResolver` with `ResolveBest(resources, requirement)`.
    - **Requirement Struct**: Includes `Tags` and `Budget` (e.g., "cheap", "performance").
    - **Scoring Logic**:
        - **Tag Match**: High score for matching required tags.
        - **Budget Match**:
            - "cheap" -> Prefers resources with low cost (needs a `cost` field in `config_json` or heuristic).
            - "performance" -> Prefers resources with high capability/cost.

### 3. CLI Enhancements (`cmd/bd/resources.go`)
- **Objective**: Allow users to filter the resource list and verify discovery/matching.
- **Changes**:
    - **List Command**: Add flags `--type`, `--source`, `--tag` (multi-value).
    - **Sync Command**: Update to iterate through all registered sources (Local + Linear).
    - **Match Command**: `bd resources match --tags "coding" --profile "cheap"` to test the resolver.

## Verification Plan

### Automated Tests
- **Unit Tests**:
    - `resolver_test.go`: Verify scoring logic matches the correct resource (e.g., "cheap" profile selects "gpt-3.5" over "gpt-4").
    - `linear_test.go`: Mock the Linear API response.
- **Integration**:
    - `TestResourceSync`: Run a sync with a mock config and verify SQLite contents.

### Manual Verification
1.  **Configure Local**: Add a dummy `skills.yaml` to a local folder.
2.  **Run Sync**: `bd resources sync`.
3.  **List & Filter**: `bd resources list --type skill --tag "coding"`.
4.  **Test Matching**: `bd resources match --tags "coding" --profile "cheap"`.
