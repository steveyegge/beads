#!/bin/bash
# Wrapper script to run local beads-mcp for development
# Automatically changes to the script's directory (integrations/beads-mcp)
cd "$(dirname "${BASH_SOURCE[0]}")"
exec uv run python -m beads_mcp
