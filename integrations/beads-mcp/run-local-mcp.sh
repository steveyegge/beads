#!/bin/bash
# Wrapper script to run local beads-mcp for development
cd /Users/jleechan/projects_other/beads/integrations/beads-mcp
exec uv run python -m beads_mcp
