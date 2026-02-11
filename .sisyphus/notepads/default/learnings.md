
## Resource Commands Implementation

Created `cmd/bd/resource.go` with CLI commands for managing resources (models, agents, skills).

### Commands Implemented:
- `bd resource list` - List all resources with filtering by type, tag, source
- `bd resource add` - Add new resource with name, type, identifier, tags, config
- `bd resource update` - Update resource properties (name, config, activate/deactivate)
- `bd resource tag add` - Add tags to resources
- `bd resource tag remove` - Remove tags from resources
- `bd resource delete` - Soft delete (sets is_active=false)
- `bd resource resolve` - Find best resource for a profile (cheap/performance/balanced)

### Key Patterns:
- Used Storage interface methods: SaveResource, GetResource, ListResources
- Followed existing command patterns from list.go and other commands
- Supports both JSON output (--json) and pretty-printed output
- Uses cobra flags for all parameters
- Validates input (type, identifier, name requirements)
- Uses resolver.StandardResolver for profile-based resource selection

### File Structure:
- Main resource command group in "advanced" category
- Subcommands organized with nested cobra.Command structs
- Helper functions: contains(), formatJSON()
- All flags registered in init() function

