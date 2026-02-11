# OpenCode Resources Integration Plan

## Overview
This document outlines how the beads resources system integrates with OpenCode for intelligent model/agent selection.

## Current Implementation

### 1. Resource Management System ✅
- **Storage**: Dolt-based tables (resource_types, resources, resource_tags)
- **CLI Commands**: `bd resource [list|add|update|delete|tag|resolve]`
- **Types**: model, agent, skill
- **Tagging**: Capability-based tags (cheap, fast, smart, expensive, complex)

### 2. Resolver System ✅
- **Location**: `internal/resolver/resolver.go`
- **Algorithm**: Score-based matching with profile support
- **Profiles**:
  - `cheap`: Prioritize low-cost models (gpt-3.5, haiku), penalize expensive
  - `performance`: Prioritize high-capability (gpt-4, opus), penalize weak models
  - `balanced`: No strong preference

### 3. Linear Integration ✅
- **Sub-issues Header**: `GraphQL-Features: sub_issues` enabled for parent-child linking
- **Project Sync**: Epics ↔ Projects bidirectional sync
- **Hierarchy**: Full support for Epic → Task → Subtask

## Integration Points with OpenCode

### Phase 1: Resource Discovery (Current)
```yaml
# Resources stored in beads can be exposed to OpenCode
resources:
  - identifier: gpt-4-turbo
    type: model
    tags: [smart, complex, expensive]
  
  - identifier: gpt-3.5-turbo
    type: model  
    tags: [cheap, fast]
    
  - identifier: claude-opus
    type: model
    tags: [smart, complex]
```

### Phase 2: Task-to-Resource Mapping (Proposed)

#### 2.1 Task Requirements
Tasks in beads can specify resource requirements:

```bash
# Create task with resource requirements
bd create "Complex Architecture Review" \
  --description "Review system architecture" \
  --requires-model "performance" \
  --requires-tags "smart,complex"

# Alternative: Tags on the issue itself
bd create "Quick Fix" \
  --description "Simple bug fix" \
  --tags "use-cheap-model"
```

#### 2.2 Resource Resolution at Runtime

When OpenCode picks up a task:

1. **Read Task Requirements** from beads
   ```go
   task := store.GetIssue(ctx, taskID)
   requirements := extractResourceRequirements(task)
   ```

2. **Query Available Resources**
   ```go
   resources := store.GetResources(ctx, types.ResourceFilter{
       Type: strPtr("model"),
       Tags: requirements.Tags,
   })
   ```

3. **Select Best Match**
   ```go
   resolver := resolver.NewStandardResolver()
   bestModel := resolver.ResolveBest(resources, resolver.Requirement{
       Type:    "model",
       Tags:    requirements.Tags,
       Profile: requirements.Profile,
   })
   ```

4. **Configure OpenCode Session**
   ```go
   // Use selected model for this task
   opencode.SetModel(bestModel.Identifier)
   ```

### Phase 3: Agent Routing (Future)

#### 3.1 Agent Resources
```yaml
# Define agents as resources
resources:
  - identifier: sisyphus-junior
    type: agent
    tags: [quick, go, refactoring]
    config:
      temperature: 0.5
      specialties: [code-cleanup, simple-changes]
  
  - identifier: sisyphus-oracle
    type: agent
    tags: [deep, architecture, debugging]
    config:
      temperature: 0.3
      specialties: [complex-problems, research]
```

#### 3.2 Task-to-Agent Matching
Based on task characteristics:
- **Complexity**: Estimated from description, deps, type
- **Estimated tokens**: From historical data
- **Required skills**: From tags, dependencies

```go
// Match task to agent
taskProfile := analyzeTask(task)
agent := resolver.ResolveBest(agents, resolver.Requirement{
    Type:    "agent",
    Tags:    taskProfile.RequiredSkills,
    Profile: taskProfile.Complexity,
})
```

### Phase 4: Cost Tracking & Optimization (Future)

#### 4.1 Token Usage per Resource
```sql
-- Track usage by resource
CREATE TABLE resource_usage (
    id INT PRIMARY KEY AUTO_INCREMENT,
    resource_id INT,
    task_id VARCHAR(255),
    tokens_input INT,
    tokens_output INT,
    cost DECIMAL(10,4),
    created_at DATETIME
);
```

#### 4.2 Cost-Aware Routing
```go
// Prefer cheaper models for simple tasks
if task.IsSimple() && task.EstimatedTokens() < 1000 {
    req.Profile = "cheap"
}
```

## Configuration

### OpenCode Integration Config
```yaml
# .beads/opencode.yaml
resource_integration:
  enabled: true
  
  # Default model selection
  default_model: "gpt-4-turbo"
  
  # Task type to profile mapping
  task_profiles:
    epic: "performance"
    feature: "performance"
    task: "balanced"
    bug: "balanced"
    chore: "cheap"
  
  # Auto-select based on task metadata
  auto_select:
    enabled: true
    max_cost_per_task: 0.50  # USD
    
  # Fallback chain
  fallback:
    - gpt-4-turbo
    - claude-opus
    - gpt-3.5-turbo
```

### Resource Discovery
```bash
# List all available models
bd resource list --type model --json

# Find best model for profile
bd resource resolve --profile cheap --type model

# Find best agent for task type
bd resource resolve --tags "go,refactoring" --type agent
```

## Usage Examples

### Example 1: Simple Bug Fix
```bash
# Create task
bd create "Fix typo in README" -p 2

# OpenCode queries: "cheap model for simple task"
# Resolver returns: gpt-3.5-turbo
# Cost saved vs using GPT-4: ~90%
```

### Example 2: Complex Architecture
```bash
# Create task with requirements
bd create "Design distributed cache" \
  --description "Architect Redis clustering solution" \
  --type epic -p 0

# OpenCode queries: "performance model for complex task"
# Resolver returns: gpt-4-turbo or claude-opus
# Justification: Epic type + high complexity
```

### Example 3: Agent Selection
```bash
# Task requires specific skills
bd create "Refactor legacy Java code" \
  --tags "java,refactoring,legacy"

# OpenCode queries: agent with java + refactoring skills
# Resolver returns: sisyphus-junior (specializes in refactoring)
```

## Benefits

1. **Cost Optimization**: Automatically use cheaper models for simple tasks
2. **Performance**: Use high-capability models for complex work
3. **Skill Matching**: Route to agents with relevant expertise
4. **Transparency**: Resource selection logged in beads for audit
5. **Fallbacks**: Graceful degradation if preferred resource unavailable

## Next Steps

1. ✅ **Complete**: Resource storage and CLI commands
2. ✅ **Complete**: Linear sub-issues header for parent-child
3. 🔄 **Next**: Add task requirements metadata to issue types
4. 🔄 **Next**: Create OpenCode MCP server for resource queries
5. 📋 **Future**: Cost tracking and usage analytics
6. 📋 **Future**: ML-based task complexity estimation

## Files Created/Modified

- `internal/linear/types.go` - Added subIssues field to Client struct
- `internal/linear/client.go` - Added GraphQL-Features header support
- `cmd/bd/resource.go` - New CLI commands for resource management
- `internal/storage/dolt/resources.go` - Storage methods for resources
- `internal/resolver/resolver.go` - Resource selection algorithm (existing)

## Testing

```bash
# Test resource commands
bd resource add --name "Test Model" --type model --identifier test-model --tag cheap
bd resource list --type model
bd resource resolve --profile cheap

# Test Linear sync with parent-child
bd linear push --update-refs
```
