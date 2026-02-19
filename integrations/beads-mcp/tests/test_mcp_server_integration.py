"""Real integration tests for MCP server using fastmcp.Client."""

import asyncio
import os
import shutil
import tempfile
from pathlib import Path

import pytest
from fastmcp.client import Client

from beads_mcp.server import mcp
from tests._bd_binary import REQUIRED_FLOW_SUBCOMMANDS, probe_bd_capabilities, resolve_bd_executable


def _workspace_root_from_context_output(output: str) -> str:
    """Extract workspace root from context(show) output."""
    for line in output.splitlines():
        if line.startswith("Workspace root:"):
            return line.split(":", 1)[1].strip()
    raise AssertionError(f"Could not parse workspace root from context output: {output}")


async def _run_bd_json(
    bd_executable: str,
    workspace_root: str,
    *args: str,
) -> dict:
    """Run bd command with --json and return parsed object payload."""
    process = await asyncio.create_subprocess_exec(
        bd_executable,
        *args,
        "--json",
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
        cwd=workspace_root,
    )
    stdout, stderr = await process.communicate()
    stdout_text = stdout.decode().strip()
    stderr_text = stderr.decode().strip()
    if not stdout_text:
        raise AssertionError(
            f"bd command returned empty stdout (code={process.returncode}): {stderr_text}"
        )
    import json

    try:
        payload = json.loads(stdout_text)
    except json.JSONDecodeError as exc:
        raise AssertionError(
            f"bd command did not return JSON (code={process.returncode}): {stdout_text} | {stderr_text}"
        ) from exc
    if not isinstance(payload, dict):
        raise AssertionError(f"Expected JSON object payload, got {type(payload)}: {payload}")
    return payload


@pytest.fixture(scope="session")
def bd_executable():
    """Resolve bd executable via capability-probed custom-fork candidates."""
    try:
        return resolve_bd_executable()
    except RuntimeError as exc:
        pytest.fail(str(exc))


def test_binary_resolver_returns_absolute_executable_path(bd_executable):
    """Binary resolver should produce a concrete executable path."""
    assert Path(bd_executable).is_absolute()
    assert os.access(bd_executable, os.X_OK)


def test_binary_probe_confirms_required_flow_subcommands(bd_executable):
    """Binary capability probe should enforce required control-plane flow commands."""
    probe_bd_capabilities(bd_executable)
    import subprocess

    help_result = subprocess.run(
        [bd_executable, "flow", "--help"],
        check=False,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    help_text = f"{help_result.stdout}\n{help_result.stderr}"
    for subcommand in REQUIRED_FLOW_SUBCOMMANDS:
        assert subcommand in help_text


@pytest.fixture
async def temp_db(bd_executable):
    """Create a temporary database file and initialize it - fully hermetic."""
    # Create temp directory that will serve as the workspace root
    temp_dir = tempfile.mkdtemp(prefix="beads_mcp_test_", dir="/tmp")

    # Initialize database in this directory (creates .beads/ subdirectory)
    import asyncio

    env = os.environ.copy()
    # Clear any existing BEADS_DIR/BEADS_DB to ensure clean state
    env.pop("BEADS_DB", None)
    env.pop("BEADS_DIR", None)

    # Run bd init in the temp directory - it will create .beads/ subdirectory
    process = await asyncio.create_subprocess_exec(
        bd_executable,
        "init",
        "--prefix",
        "test",
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
        env=env,
        cwd=temp_dir,  # Run in temp dir - bd init creates .beads/ here
    )
    stdout, stderr = await process.communicate()

    if process.returncode != 0:
        pytest.fail(f"Failed to initialize test database: {stderr.decode()}")

    # Return the .beads directory path (not the db file)
    beads_dir = os.path.join(temp_dir, ".beads")
    
    yield beads_dir

    # Cleanup
    shutil.rmtree(temp_dir, ignore_errors=True)


@pytest.fixture
async def mcp_client(bd_executable, temp_db, monkeypatch):
    """Create MCP client with temporary database."""
    from beads_mcp import tools

    # Reset connection pool before test
    tools._connection_pool.clear()

    # Reset context environment variables
    os.environ.pop("BEADS_CONTEXT_SET", None)
    os.environ.pop("BEADS_WORKING_DIR", None)
    os.environ.pop("BEADS_DB", None)
    os.environ.pop("BEADS_DIR", None)

    # temp_db is now the .beads directory path
    # The workspace root is the parent directory
    workspace_root = os.path.dirname(temp_db)

    # Disable daemon mode for tests (prevents daemon accumulation and timeouts)
    os.environ["BEADS_NO_DAEMON"] = "1"
    os.environ["BEADS_PATH"] = bd_executable

    # Create test client
    async with Client(mcp) as client:
        # Automatically set context for the tests
        await client.call_tool("context", {"workspace_root": workspace_root})
        yield client

    # Reset connection pool and context after test
    tools._connection_pool.clear()
    os.environ.pop("BEADS_CONTEXT_SET", None)
    os.environ.pop("BEADS_WORKING_DIR", None)
    os.environ.pop("BEADS_DB", None)
    os.environ.pop("BEADS_DIR", None)
    os.environ.pop("BEADS_NO_DAEMON", None)
    os.environ.pop("BEADS_PATH", None)


@pytest.mark.asyncio
async def test_quickstart_resource(mcp_client):
    """Test beads://quickstart resource."""
    result = await mcp_client.read_resource("beads://quickstart")

    assert result is not None
    content = result[0].text
    assert len(content) > 0
    assert "beads" in content.lower() or "bd" in content.lower()


@pytest.mark.asyncio
async def test_create_issue_tool(mcp_client):
    """Test create_issue tool."""
    result = await mcp_client.call_tool(
        "create",
        {
            "title": "Test MCP issue",
            "description": "Created via MCP server",
            "priority": 1,
            "issue_type": "bug",
            "brief": False,  # Get full Issue object
        },
    )

    # Parse the JSON response from CallToolResult
    import json

    issue_data = json.loads(result.content[0].text)
    assert issue_data["title"] == "Test MCP issue"
    assert issue_data["description"] == "Created via MCP server"
    assert issue_data["priority"] == 1
    assert issue_data["issue_type"] == "bug"
    assert issue_data["status"] == "open"
    assert "id" in issue_data

    return issue_data["id"]


@pytest.mark.asyncio
async def test_show_issue_tool(mcp_client):
    """Test show_issue tool."""
    # First create an issue
    create_result = await mcp_client.call_tool(
        "create",
        {"title": "Issue to show", "priority": 2, "issue_type": "task", "brief": False},
    )
    import json

    created = json.loads(create_result.content[0].text)
    issue_id = created["id"]

    # Show the issue
    show_result = await mcp_client.call_tool("show", {"issue_id": issue_id})

    issue = json.loads(show_result.content[0].text)
    assert issue["id"] == issue_id
    assert issue["title"] == "Issue to show"


@pytest.mark.asyncio
async def test_list_issues_tool(mcp_client):
    """Test list_issues tool."""
    # Create some issues first
    await mcp_client.call_tool(
        "create", {"title": "Issue 1", "priority": 0, "issue_type": "bug", "brief": False}
    )
    await mcp_client.call_tool(
        "create", {"title": "Issue 2", "priority": 1, "issue_type": "feature", "brief": False}
    )

    # List all issues
    result = await mcp_client.call_tool("list", {})

    import json

    issues = json.loads(result.content[0].text)
    assert len(issues) >= 2

    # List with status filter
    result = await mcp_client.call_tool("list", {"status": "open"})
    issues = json.loads(result.content[0].text)
    assert all(issue["status"] == "open" for issue in issues)


@pytest.mark.asyncio
async def test_update_issue_tool(mcp_client):
    """Test update_issue tool."""
    import json

    # Create issue
    create_result = await mcp_client.call_tool(
        "create", {"title": "Issue to update", "priority": 2, "issue_type": "task", "brief": False}
    )
    created = json.loads(create_result.content[0].text)
    issue_id = created["id"]

    # Update issue
    update_result = await mcp_client.call_tool(
        "update",
        {
            "issue_id": issue_id,
            "status": "in_progress",
            "priority": 0,
            "title": "Updated title",
            "brief": False,  # Get full Issue object
        },
    )

    updated = json.loads(update_result.content[0].text)
    assert updated["id"] == issue_id
    assert updated["status"] == "in_progress"
    assert updated["priority"] == 0
    assert updated["title"] == "Updated title"


@pytest.mark.asyncio
async def test_close_issue_tool(mcp_client):
    """Test close_issue tool."""
    import json

    # Create issue
    create_result = await mcp_client.call_tool(
        "create", {"title": "Issue to close", "priority": 1, "issue_type": "bug", "brief": False}
    )
    created = json.loads(create_result.content[0].text)
    issue_id = created["id"]

    # Close issue with brief=False to get full Issue object
    close_result = await mcp_client.call_tool(
        "close", {"issue_id": issue_id, "reason": "Test complete", "brief": False}
    )

    closed_issues = json.loads(close_result.content[0].text)
    assert len(closed_issues) >= 1
    closed = closed_issues[0]
    assert closed["id"] == issue_id
    assert closed["status"] == "closed"
    assert closed["closed_at"] is not None


@pytest.mark.asyncio
async def test_reopen_issue_tool(mcp_client):
    """Test reopen_issue tool."""
    import json

    # Create and close issue
    create_result = await mcp_client.call_tool(
        "create", {"title": "Issue to reopen", "priority": 1, "issue_type": "bug", "brief": False}
    )
    created = json.loads(create_result.content[0].text)
    issue_id = created["id"]

    await mcp_client.call_tool(
        "close", {"issue_id": issue_id, "reason": "Done"}
    )

    # Reopen issue with brief=False to get full Issue object
    reopen_result = await mcp_client.call_tool(
        "reopen", {"issue_ids": [issue_id], "brief": False}
    )

    reopened_issues = json.loads(reopen_result.content[0].text)
    assert len(reopened_issues) >= 1
    reopened = reopened_issues[0]
    assert reopened["id"] == issue_id
    assert reopened["status"] == "open"
    assert reopened["closed_at"] is None


@pytest.mark.asyncio
async def test_reopen_multiple_issues_tool(mcp_client):
    """Test reopening multiple issues via MCP tool."""
    import json

    # Create and close two issues
    issue1_result = await mcp_client.call_tool(
        "create", {"title": "Issue 1 to reopen", "priority": 1, "issue_type": "task", "brief": False}
    )
    issue1 = json.loads(issue1_result.content[0].text)

    issue2_result = await mcp_client.call_tool(
        "create", {"title": "Issue 2 to reopen", "priority": 1, "issue_type": "task", "brief": False}
    )
    issue2 = json.loads(issue2_result.content[0].text)

    await mcp_client.call_tool("close", {"issue_id": issue1["id"], "reason": "Done"})
    await mcp_client.call_tool("close", {"issue_id": issue2["id"], "reason": "Done"})

    # Reopen both issues with brief=False
    reopen_result = await mcp_client.call_tool(
        "reopen", {"issue_ids": [issue1["id"], issue2["id"]], "brief": False}
    )

    reopened_issues = json.loads(reopen_result.content[0].text)
    assert len(reopened_issues) == 2
    reopened_ids = {issue["id"] for issue in reopened_issues}
    assert issue1["id"] in reopened_ids
    assert issue2["id"] in reopened_ids
    assert all(issue["status"] == "open" for issue in reopened_issues)
    assert all(issue["closed_at"] is None for issue in reopened_issues)


@pytest.mark.asyncio
async def test_reopen_with_reason_tool(mcp_client):
    """Test reopening issue with reason parameter via MCP tool."""
    import json

    # Create and close issue
    create_result = await mcp_client.call_tool(
        "create", {"title": "Issue to reopen with reason", "priority": 1, "issue_type": "bug", "brief": False}
    )
    created = json.loads(create_result.content[0].text)
    issue_id = created["id"]

    await mcp_client.call_tool("close", {"issue_id": issue_id, "reason": "Done"})

    # Reopen with reason and brief=False
    reopen_result = await mcp_client.call_tool(
        "reopen",
        {"issue_ids": [issue_id], "reason": "Found regression", "brief": False}
    )

    reopened_issues = json.loads(reopen_result.content[0].text)
    assert len(reopened_issues) >= 1
    reopened = reopened_issues[0]
    assert reopened["id"] == issue_id
    assert reopened["status"] == "open"
    assert reopened["closed_at"] is None


@pytest.mark.asyncio
async def test_ready_work_tool(mcp_client):
    """Test ready_work tool."""
    import json

    # Create a ready issue (no dependencies)
    ready_result = await mcp_client.call_tool(
        "create", {"title": "Ready work", "priority": 1, "issue_type": "task", "brief": False}
    )
    ready_issue = json.loads(ready_result.content[0].text)

    # Create blocked issue
    blocking_result = await mcp_client.call_tool(
        "create", {"title": "Blocking issue", "priority": 1, "issue_type": "task", "brief": False}
    )
    blocking_issue = json.loads(blocking_result.content[0].text)

    blocked_result = await mcp_client.call_tool(
        "create", {"title": "Blocked issue", "priority": 1, "issue_type": "task", "brief": False}
    )
    blocked_issue = json.loads(blocked_result.content[0].text)

    # Add blocking dependency
    await mcp_client.call_tool(
        "dep",
        {
            "issue_id": blocked_issue["id"],
            "depends_on_id": blocking_issue["id"],
            "dep_type": "blocks",
        },
    )

    # Get ready work
    result = await mcp_client.call_tool("ready", {"limit": 100})
    ready_issues = json.loads(result.content[0].text)

    ready_ids = [issue["id"] for issue in ready_issues]
    assert ready_issue["id"] in ready_ids
    assert blocked_issue["id"] not in ready_ids


@pytest.mark.asyncio
async def test_add_dependency_tool(mcp_client):
    """Test add_dependency tool."""
    import json

    # Create two issues
    issue1_result = await mcp_client.call_tool(
        "create", {"title": "Issue 1", "priority": 1, "issue_type": "task", "brief": False}
    )
    issue1 = json.loads(issue1_result.content[0].text)

    issue2_result = await mcp_client.call_tool(
        "create", {"title": "Issue 2", "priority": 1, "issue_type": "task", "brief": False}
    )
    issue2 = json.loads(issue2_result.content[0].text)

    # Add dependency
    result = await mcp_client.call_tool(
        "dep",
        {"issue_id": issue1["id"], "depends_on_id": issue2["id"], "dep_type": "blocks"},
    )

    message = result.content[0].text
    assert "Added dependency" in message
    assert issue1["id"] in message
    assert issue2["id"] in message


@pytest.mark.asyncio
async def test_create_with_all_fields(mcp_client):
    """Test create_issue with all optional fields."""
    import json

    result = await mcp_client.call_tool(
        "create",
        {
            "title": "Full issue",
            "description": "Complete description",
            "priority": 0,
            "issue_type": "feature",
            "assignee": "testuser",
            "labels": ["urgent", "backend"],
            "brief": False,  # Get full Issue object
        },
    )

    issue = json.loads(result.content[0].text)
    assert issue["title"] == "Full issue"
    assert issue["description"] == "Complete description"
    assert issue["priority"] == 0
    assert issue["issue_type"] == "feature"
    assert issue["assignee"] == "testuser"


@pytest.mark.asyncio
async def test_list_with_filters(mcp_client):
    """Test list_issues with various filters."""
    import json

    # Create issues with different attributes
    await mcp_client.call_tool(
        "create",
        {
            "title": "Bug P0",
            "priority": 0,
            "issue_type": "bug",
            "assignee": "alice",
            "brief": False,
        },
    )
    await mcp_client.call_tool(
        "create",
        {
            "title": "Feature P1",
            "priority": 1,
            "issue_type": "feature",
            "assignee": "bob",
            "brief": False,
        },
    )

    # Filter by priority
    result = await mcp_client.call_tool("list", {"priority": 0})
    issues = json.loads(result.content[0].text)
    assert all(issue["priority"] == 0 for issue in issues)

    # Filter by type
    result = await mcp_client.call_tool("list", {"issue_type": "bug"})
    issues = json.loads(result.content[0].text)
    assert all(issue["issue_type"] == "bug" for issue in issues)

    # Filter by assignee
    result = await mcp_client.call_tool("list", {"assignee": "alice"})
    issues = json.loads(result.content[0].text)
    assert all(issue["assignee"] == "alice" for issue in issues)


@pytest.mark.asyncio
async def test_ready_work_with_priority_filter(mcp_client):
    """Test ready_work with priority filter."""
    import json

    # Create issues with different priorities
    await mcp_client.call_tool(
        "create", {"title": "P0 issue", "priority": 0, "issue_type": "bug", "brief": False}
    )
    await mcp_client.call_tool(
        "create", {"title": "P1 issue", "priority": 1, "issue_type": "task", "brief": False}
    )

    # Get ready work with priority filter
    result = await mcp_client.call_tool("ready", {"priority": 0, "limit": 100})
    issues = json.loads(result.content[0].text)
    assert all(issue["priority"] == 0 for issue in issues)


@pytest.mark.asyncio
async def test_update_partial_fields(mcp_client):
    """Test update_issue with partial field updates."""
    import json

    # Create issue
    create_result = await mcp_client.call_tool(
        "create",
        {
            "title": "Original title",
            "description": "Original description",
            "priority": 2,
            "issue_type": "task",
            "brief": False,
        },
    )
    created = json.loads(create_result.content[0].text)
    issue_id = created["id"]

    # Update only status with brief=False to get full Issue
    update_result = await mcp_client.call_tool(
        "update", {"issue_id": issue_id, "status": "in_progress", "brief": False}
    )
    updated = json.loads(update_result.content[0].text)
    assert updated["status"] == "in_progress"
    assert updated["title"] == "Original title"  # Unchanged
    assert updated["priority"] == 2  # Unchanged


@pytest.mark.asyncio
async def test_dependency_types(mcp_client):
    """Test different dependency types."""
    import json

    # Create issues
    issue1_result = await mcp_client.call_tool(
        "create", {"title": "Issue 1", "priority": 1, "issue_type": "task", "brief": False}
    )
    issue1 = json.loads(issue1_result.content[0].text)

    issue2_result = await mcp_client.call_tool(
        "create", {"title": "Issue 2", "priority": 1, "issue_type": "task", "brief": False}
    )
    issue2 = json.loads(issue2_result.content[0].text)

    # Test related dependency
    result = await mcp_client.call_tool(
        "dep",
        {"issue_id": issue1["id"], "depends_on_id": issue2["id"], "dep_type": "related"},
    )

    message = result.content[0].text
    assert "Added dependency" in message
    assert "related" in message


@pytest.mark.asyncio
async def test_stats_tool(mcp_client):
    """Test stats tool."""
    import json

    # Create some issues to get stats
    await mcp_client.call_tool(
        "create", {"title": "Stats test 1", "priority": 1, "issue_type": "bug", "brief": False}
    )
    await mcp_client.call_tool(
        "create", {"title": "Stats test 2", "priority": 2, "issue_type": "task", "brief": False}
    )

    # Get stats
    result = await mcp_client.call_tool("stats", {})
    stats = json.loads(result.content[0].text)

    assert "summary" in stats
    assert "total_issues" in stats["summary"]
    assert "open_issues" in stats["summary"]
    assert stats["summary"]["total_issues"] >= 2


@pytest.mark.asyncio
async def test_blocked_tool(mcp_client):
    """Test blocked tool."""
    import json

    # Create two issues
    blocking_result = await mcp_client.call_tool(
        "create", {"title": "Blocking issue", "priority": 1, "issue_type": "task", "brief": False}
    )
    blocking_issue = json.loads(blocking_result.content[0].text)

    blocked_result = await mcp_client.call_tool(
        "create", {"title": "Blocked issue", "priority": 1, "issue_type": "task", "brief": False}
    )
    blocked_issue = json.loads(blocked_result.content[0].text)

    # Add blocking dependency
    await mcp_client.call_tool(
        "dep",
        {
            "issue_id": blocked_issue["id"],
            "depends_on_id": blocking_issue["id"],
            "dep_type": "blocks",
        },
    )

    # Get blocked issues
    result = await mcp_client.call_tool("blocked", {})
    blocked_issues = json.loads(result.content[0].text)

    # Should have at least the one we created
    blocked_ids = [issue["id"] for issue in blocked_issues]
    assert blocked_issue["id"] in blocked_ids

    # Find our blocked issue and verify it has blocking info
    our_blocked = next(issue for issue in blocked_issues if issue["id"] == blocked_issue["id"])
    assert our_blocked["blocked_by_count"] >= 1
    assert blocking_issue["id"] in our_blocked["blocked_by"]


@pytest.mark.asyncio
async def test_context_init_action(bd_executable):
    """Test context tool with init action.

    Note: This test validates that context(action='init') can be called successfully via MCP.
    Uses a fresh temp directory without an existing database.
    """
    import os
    import tempfile
    import shutil
    from beads_mcp import tools
    from beads_mcp.server import mcp

    # Reset connection pool and context
    tools._connection_pool.clear()
    os.environ.pop("BEADS_CONTEXT_SET", None)
    os.environ.pop("BEADS_WORKING_DIR", None)
    os.environ.pop("BEADS_DB", None)
    os.environ.pop("BEADS_DIR", None)
    os.environ["BEADS_NO_DAEMON"] = "1"

    # Create a fresh temp directory without any beads database
    temp_dir = tempfile.mkdtemp(prefix="beads_init_test_")
    try:
        async with Client(mcp) as client:
            # First set context to the fresh directory
            await client.call_tool("context", {"workspace_root": temp_dir})

            # Call context tool with init action
            result = await client.call_tool("context", {"action": "init", "prefix": "test-init"})
            output = result.content[0].text

            # Verify output contains success message
            assert "bd initialized successfully!" in output
            assert "test-init" in output
    finally:
        tools._connection_pool.clear()
        shutil.rmtree(temp_dir, ignore_errors=True)
        os.environ.pop("BEADS_CONTEXT_SET", None)
        os.environ.pop("BEADS_WORKING_DIR", None)


@pytest.mark.asyncio
async def test_context_show_action(mcp_client, temp_db):
    """Test context tool with show action.

    Verifies that context(action='show') returns workspace information.
    """
    # Call context tool with show action (default when no args)
    result = await mcp_client.call_tool("context", {"action": "show"})
    output = result.content[0].text

    # Verify output contains workspace info
    assert "Workspace root:" in output
    assert "Database:" in output


@pytest.mark.asyncio
async def test_context_default_show(mcp_client, temp_db):
    """Test context tool defaults to show when no args provided."""
    # Call context tool with no args - should default to show
    result = await mcp_client.call_tool("context", {})
    output = result.content[0].text

    # Verify output contains workspace info (same as show action)
    assert "Workspace root:" in output
    assert "Database:" in output


# =============================================================================
# OUTPUT CONTROL PARAMETER TESTS
# =============================================================================


@pytest.mark.asyncio
async def test_create_brief_default(mcp_client):
    """Test create returns OperationResult by default (brief=True)."""
    import json

    result = await mcp_client.call_tool(
        "create",
        {"title": "Brief test issue", "priority": 2, "issue_type": "task"},
    )

    data = json.loads(result.content[0].text)
    # Default brief=True returns OperationResult
    assert "id" in data
    assert data["action"] == "created"
    # Should NOT have full Issue fields
    assert "title" not in data
    assert "description" not in data


@pytest.mark.asyncio
async def test_create_brief_false(mcp_client):
    """Test create returns full Issue when brief=False."""
    import json

    result = await mcp_client.call_tool(
        "create",
        {
            "title": "Full issue test",
            "description": "Full description",
            "priority": 1,
            "issue_type": "bug",
            "brief": False,
        },
    )

    data = json.loads(result.content[0].text)
    # brief=False returns full Issue
    assert data["title"] == "Full issue test"
    assert data["description"] == "Full description"
    assert data["priority"] == 1
    assert data["issue_type"] == "bug"
    assert data["status"] == "open"


@pytest.mark.asyncio
async def test_update_brief_default(mcp_client):
    """Test update returns OperationResult by default (brief=True)."""
    import json

    # Create issue first
    create_result = await mcp_client.call_tool(
        "create", {"title": "Update brief test", "brief": False}
    )
    created = json.loads(create_result.content[0].text)
    issue_id = created["id"]

    # Update with default brief=True
    update_result = await mcp_client.call_tool(
        "update", {"issue_id": issue_id, "status": "in_progress"}
    )

    data = json.loads(update_result.content[0].text)
    assert data["id"] == issue_id
    assert data["action"] == "updated"
    assert "title" not in data


@pytest.mark.asyncio
async def test_update_brief_false(mcp_client):
    """Test update returns full Issue when brief=False."""
    import json

    # Create issue first
    create_result = await mcp_client.call_tool(
        "create", {"title": "Update full test", "brief": False}
    )
    created = json.loads(create_result.content[0].text)
    issue_id = created["id"]

    # Update with brief=False
    update_result = await mcp_client.call_tool(
        "update", {"issue_id": issue_id, "status": "in_progress", "brief": False}
    )

    data = json.loads(update_result.content[0].text)
    assert data["id"] == issue_id
    assert data["status"] == "in_progress"
    assert data["title"] == "Update full test"


@pytest.mark.asyncio
async def test_close_brief_default(mcp_client):
    """Test close returns OperationResult by default (brief=True)."""
    import json

    # Create issue first
    create_result = await mcp_client.call_tool(
        "create", {"title": "Close brief test", "brief": False}
    )
    created = json.loads(create_result.content[0].text)
    issue_id = created["id"]

    # Close with default brief=True
    close_result = await mcp_client.call_tool(
        "close", {"issue_id": issue_id, "reason": "Done"}
    )

    data = json.loads(close_result.content[0].text)
    assert isinstance(data, list)
    assert len(data) == 1
    assert data[0]["id"] == issue_id
    assert data[0]["action"] == "closed"


@pytest.mark.asyncio
async def test_close_brief_false(mcp_client):
    """Test close returns full Issue when brief=False."""
    import json

    # Create issue first
    create_result = await mcp_client.call_tool(
        "create", {"title": "Close full test", "brief": False}
    )
    created = json.loads(create_result.content[0].text)
    issue_id = created["id"]

    # Close with brief=False
    close_result = await mcp_client.call_tool(
        "close", {"issue_id": issue_id, "reason": "Done", "brief": False}
    )

    data = json.loads(close_result.content[0].text)
    assert isinstance(data, list)
    assert len(data) >= 1
    assert data[0]["id"] == issue_id
    assert data[0]["status"] == "closed"
    assert data[0]["title"] == "Close full test"


@pytest.mark.asyncio
async def test_reopen_brief_default(mcp_client):
    """Test reopen returns OperationResult by default (brief=True)."""
    import json

    # Create and close issue first
    create_result = await mcp_client.call_tool(
        "create", {"title": "Reopen brief test", "brief": False}
    )
    created = json.loads(create_result.content[0].text)
    issue_id = created["id"]

    await mcp_client.call_tool("close", {"issue_id": issue_id})

    # Reopen with default brief=True
    reopen_result = await mcp_client.call_tool(
        "reopen", {"issue_ids": [issue_id]}
    )

    data = json.loads(reopen_result.content[0].text)
    assert isinstance(data, list)
    assert len(data) == 1
    assert data[0]["id"] == issue_id
    assert data[0]["action"] == "reopened"


@pytest.mark.asyncio
async def test_show_brief(mcp_client):
    """Test show with brief=True returns BriefIssue."""
    import json

    # Create issue first
    create_result = await mcp_client.call_tool(
        "create",
        {"title": "Show brief test", "description": "Long description", "brief": False},
    )
    created = json.loads(create_result.content[0].text)
    issue_id = created["id"]

    # Show with brief=True
    show_result = await mcp_client.call_tool(
        "show", {"issue_id": issue_id, "brief": True}
    )

    data = json.loads(show_result.content[0].text)
    # BriefIssue has only: id, title, status, priority
    assert data["id"] == issue_id
    assert data["title"] == "Show brief test"
    assert data["status"] == "open"
    assert "priority" in data
    # Should NOT have full Issue fields
    assert "description" not in data
    assert "dependencies" not in data


@pytest.mark.asyncio
async def test_show_fields_projection(mcp_client):
    """Test show with fields parameter for custom projection."""
    import json

    # Create issue first
    create_result = await mcp_client.call_tool(
        "create",
        {
            "title": "Fields test",
            "description": "Test description",
            "priority": 1,
            "brief": False,
        },
    )
    created = json.loads(create_result.content[0].text)
    issue_id = created["id"]

    # Show with specific fields
    show_result = await mcp_client.call_tool(
        "show", {"issue_id": issue_id, "fields": ["id", "title", "priority"]}
    )

    data = json.loads(show_result.content[0].text)
    # Should have only requested fields
    assert data["id"] == issue_id
    assert data["title"] == "Fields test"
    assert data["priority"] == 1
    # Should NOT have other fields
    assert "description" not in data
    assert "status" not in data


@pytest.mark.asyncio
async def test_show_fields_invalid(mcp_client):
    """Test show with invalid fields raises error."""
    import json
    from fastmcp.exceptions import ToolError

    # Create issue first
    create_result = await mcp_client.call_tool(
        "create", {"title": "Invalid fields test", "brief": False}
    )
    created = json.loads(create_result.content[0].text)
    issue_id = created["id"]

    # Show with invalid field should raise ToolError
    with pytest.raises(ToolError) as exc_info:
        await mcp_client.call_tool(
            "show", {"issue_id": issue_id, "fields": ["id", "nonexistent_field"]}
        )

    # Verify error message mentions invalid field
    assert "Invalid field" in str(exc_info.value)


@pytest.mark.asyncio
async def test_show_max_description_length(mcp_client):
    """Test show with max_description_length truncates description."""
    import json

    # Create issue with long description
    long_desc = "A" * 200
    create_result = await mcp_client.call_tool(
        "create",
        {"title": "Truncate test", "description": long_desc, "brief": False},
    )
    created = json.loads(create_result.content[0].text)
    issue_id = created["id"]

    # Show with truncation
    show_result = await mcp_client.call_tool(
        "show", {"issue_id": issue_id, "max_description_length": 50}
    )

    data = json.loads(show_result.content[0].text)
    # Description should be truncated
    assert len(data["description"]) <= 53  # 50 + "..."
    assert data["description"].endswith("...")


@pytest.mark.asyncio
async def test_list_brief(mcp_client):
    """Test list with brief=True returns BriefIssue format."""
    import json

    # Create some issues
    await mcp_client.call_tool(
        "create", {"title": "List brief 1", "priority": 1, "brief": False}
    )
    await mcp_client.call_tool(
        "create", {"title": "List brief 2", "priority": 2, "brief": False}
    )

    # List with brief=True
    result = await mcp_client.call_tool("list", {"brief": True})
    issues = json.loads(result.content[0].text)

    assert len(issues) >= 2
    for issue in issues:
        # BriefIssue has only: id, title, status, priority
        assert "id" in issue
        assert "title" in issue
        assert "status" in issue
        assert "priority" in issue
        # Should NOT have full Issue fields
        assert "description" not in issue
        assert "issue_type" not in issue


@pytest.mark.asyncio
async def test_ready_brief(mcp_client):
    """Test ready with brief=True returns BriefIssue format."""
    import json

    # Create a ready issue
    await mcp_client.call_tool(
        "create", {"title": "Ready brief test", "priority": 1, "brief": False}
    )

    # Ready with brief=True
    result = await mcp_client.call_tool("ready", {"brief": True, "limit": 100})
    issues = json.loads(result.content[0].text)

    assert len(issues) >= 1
    for issue in issues:
        # BriefIssue has only: id, title, status, priority
        assert "id" in issue
        assert "title" in issue
        assert "status" in issue
        assert "priority" in issue
        # Should NOT have full Issue fields
        assert "description" not in issue


@pytest.mark.asyncio
async def test_blocked_brief(mcp_client):
    """Test blocked with brief=True returns BriefIssue format."""
    import json

    # Create blocking dependency
    blocking_result = await mcp_client.call_tool(
        "create", {"title": "Blocker for brief test", "brief": False}
    )
    blocking = json.loads(blocking_result.content[0].text)

    blocked_result = await mcp_client.call_tool(
        "create", {"title": "Blocked for brief test", "brief": False}
    )
    blocked = json.loads(blocked_result.content[0].text)

    await mcp_client.call_tool(
        "dep",
        {"issue_id": blocked["id"], "depends_on_id": blocking["id"], "dep_type": "blocks"},
    )

    # Blocked with brief=True
    result = await mcp_client.call_tool("blocked", {"brief": True})
    issues = json.loads(result.content[0].text)

    # Find our blocked issue
    our_blocked = [i for i in issues if i["id"] == blocked["id"]]
    assert len(our_blocked) == 1
    # BriefIssue format
    assert "title" in our_blocked[0]
    assert "status" in our_blocked[0]
    # Should NOT have BlockedIssue-specific fields
    assert "blocked_by" not in our_blocked[0]


@pytest.mark.asyncio
async def test_show_brief_deps(mcp_client):
    """Test show with brief_deps=True returns compact dependencies."""
    import json

    # Create two issues with dependency
    dep_result = await mcp_client.call_tool(
        "create", {"title": "Dependency issue", "brief": False}
    )
    dep_issue = json.loads(dep_result.content[0].text)

    main_result = await mcp_client.call_tool(
        "create", {"title": "Main issue", "brief": False}
    )
    main_issue = json.loads(main_result.content[0].text)

    await mcp_client.call_tool(
        "dep",
        {"issue_id": main_issue["id"], "depends_on_id": dep_issue["id"], "dep_type": "blocks"},
    )

    # Show with brief_deps=True
    show_result = await mcp_client.call_tool(
        "show", {"issue_id": main_issue["id"], "brief_deps": True}
    )

    data = json.loads(show_result.content[0].text)
    # Full issue data
    assert data["id"] == main_issue["id"]
    assert data["title"] == "Main issue"
    # Dependencies should be compact (BriefDep format)
    assert len(data["dependencies"]) >= 1
    dep = data["dependencies"][0]
    assert "id" in dep
    assert "title" in dep
    assert "status" in dep
    # BriefDep should NOT have full LinkedIssue fields
    assert "description" not in dep


@pytest.mark.asyncio
async def test_flow_claim_next_enforces_wip_gate(mcp_client, monkeypatch):
    """flow(claim_next) should claim once, then return wip_blocked for same actor."""
    import json

    actor = "flow-test-actor"
    monkeypatch.delenv("BEADS_ACTOR", raising=False)
    monkeypatch.delenv("BD_ACTOR", raising=False)

    await mcp_client.call_tool("create", {"title": "Flow claim issue 1", "brief": False})
    await mcp_client.call_tool("create", {"title": "Flow claim issue 2", "brief": False})

    first = await mcp_client.call_tool("flow", {"action": "claim_next", "actor": actor})
    first_payload = json.loads(first.content[0].text)
    assert first_payload["ok"] is True
    assert first_payload["command"] == "flow claim-next"
    assert first_payload["result"] == "claimed"
    assert first_payload["issue_id"]
    assert first_payload["details"]["issue"]["status"] == "in_progress"

    second = await mcp_client.call_tool("flow", {"action": "claim_next", "actor": actor})
    second_payload = json.loads(second.content[0].text)
    assert second_payload["ok"] is True
    assert second_payload["command"] == "flow claim-next"
    assert second_payload["result"] == "wip_blocked"
    assert first_payload["issue_id"] in second_payload["details"]["in_progress_ids"]


@pytest.mark.asyncio
async def test_flow_claim_next_without_actor_override_uses_default_identity_and_wip_gate(
    mcp_client, monkeypatch
):
    """flow(claim_next) without explicit actor should still enforce WIP deterministically."""
    import json

    monkeypatch.delenv("BEADS_ACTOR", raising=False)
    monkeypatch.delenv("BD_ACTOR", raising=False)

    await mcp_client.call_tool("create", {"title": "No actor claim issue", "brief": False})
    await mcp_client.call_tool("create", {"title": "No actor claim issue 2", "brief": False})

    first = await mcp_client.call_tool("flow", {"action": "claim_next"})
    first_payload = json.loads(first.content[0].text)
    assert first_payload["ok"] is True
    assert first_payload["command"] == "flow claim-next"
    assert first_payload["result"] == "claimed"

    second = await mcp_client.call_tool("flow", {"action": "claim_next"})
    second_payload = json.loads(second.content[0].text)
    assert second_payload["ok"] is True
    assert second_payload["command"] == "flow claim-next"
    assert second_payload["result"] == "wip_blocked"


@pytest.mark.asyncio
async def test_flow_claim_next_returns_no_ready_state_when_queue_empty(mcp_client):
    """flow(claim_next) should return deterministic no_ready when queue is empty."""
    import json

    result = await mcp_client.call_tool("flow", {"action": "claim_next", "actor": "flow-no-ready"})
    payload = json.loads(result.content[0].text)
    assert payload["ok"] is True
    assert payload["command"] == "flow claim-next"
    assert payload["result"] == "no_ready"


@pytest.mark.asyncio
async def test_flow_claim_next_contention_returns_non_error_payload(mcp_client, monkeypatch):
    """flow(claim_next) contention state should be surfaced as normal payload."""
    import json
    from beads_mcp import server as server_module

    contention_payload = {
        "ok": True,
        "command": "flow claim-next",
        "result": "contention",
        "details": {
            "actor": "flow-contention-actor",
            "contention_ids": ["bd-1", "bd-2"],
            "message": "Ready issues were contended during claim",
        },
        "events": ["claim_contention"],
    }

    async def fake_run_command(self, *args, **kwargs):
        if len(args) >= 2 and args[0] == "flow" and args[1] == "claim-next":
            return contention_payload
        raise AssertionError(f"Unexpected command in contention test: {args}")

    monkeypatch.setattr(server_module.BdCliClient, "_run_command", fake_run_command)

    result = await mcp_client.call_tool(
        "flow", {"action": "claim_next", "actor": "flow-contention-actor", "ready_limit": 5}
    )
    payload = json.loads(result.content[0].text)

    assert payload["ok"] is True
    assert payload["command"] == "flow claim-next"
    assert payload["result"] == "contention"
    assert payload["details"]["contention_ids"] == ["bd-1", "bd-2"]


@pytest.mark.asyncio
async def test_flow_create_discovered_links_issue(mcp_client):
    """flow(create_discovered) should create issue and link with discovered-from."""
    import json

    parent_result = await mcp_client.call_tool(
        "create", {"title": "Parent task", "issue_type": "task", "brief": False}
    )
    parent = json.loads(parent_result.content[0].text)

    created = await mcp_client.call_tool(
        "flow",
        {
            "action": "create_discovered",
            "title": "Discovered follow-up",
            "description": "Found while implementing parent.",
            "discovered_from_id": parent["id"],
            "issue_type": "bug",
            "priority": 1,
        },
    )
    payload = json.loads(created.content[0].text)
    child_id = payload["issue_id"]
    assert payload["ok"] is True
    assert payload["command"] == "flow create-discovered"
    assert payload["result"] == "created"
    assert child_id

    child = await mcp_client.call_tool("show", {"issue_id": child_id})
    child_payload = json.loads(child.content[0].text)
    dep_ids = [dep["id"] for dep in child_payload["dependencies"]]
    assert parent["id"] in dep_ids


@pytest.mark.asyncio
async def test_flow_create_discovered_fails_fast_on_missing_parent(mcp_client):
    """flow(create_discovered) should return invalid_input before creating when parent is missing."""
    import json

    result = await mcp_client.call_tool(
        "flow",
        {
            "action": "create_discovered",
            "title": "Should not be created",
            "description": "Missing parent",
            "discovered_from_id": "bd-does-not-exist",
        },
    )
    payload = json.loads(result.content[0].text)
    assert payload["ok"] is False
    assert payload["command"] == "flow create-discovered"
    assert payload["result"] == "invalid_input"

    listed = await mcp_client.call_tool("list", {"query": "Should not be created"})
    listed_payload = json.loads(listed.content[0].text) if listed.content else []
    assert listed_payload == []


@pytest.mark.asyncio
async def test_flow_create_discovered_link_error_returns_structured_remediation(mcp_client, monkeypatch):
    """flow(create_discovered) should surface partial_state remediation payload without ToolError."""
    import json
    from beads_mcp import server as server_module
    from beads_mcp.bd_client import BdCommandError

    parent_result = await mcp_client.call_tool(
        "create", {"title": "Structured parent", "issue_type": "task", "brief": False}
    )
    parent = json.loads(parent_result.content[0].text)

    payload = {
        "ok": False,
        "command": "flow create-discovered",
        "result": "partial_state",
        "issue_id": "test-partial-create",
        "details": {
            "partial_state": "issue_created_without_discovered_from_link",
            "depends_on_id": parent["id"],
        },
        "recovery_command": f"bd dep add test-partial-create {parent['id']} --type discovered-from",
        "events": ["created", "dependency_add_failed"],
    }

    async def fake_run_command(self, *args, **kwargs):
        if len(args) >= 2 and args[0] == "flow" and args[1] == "create-discovered":
            raise BdCommandError("simulated partial-state", stdout=json.dumps(payload), returncode=4)
        raise AssertionError(f"Unexpected command in partial-state test: {args}")

    monkeypatch.setattr(server_module.BdCliClient, "_run_command", fake_run_command)

    result = await mcp_client.call_tool(
        "flow",
        {
            "action": "create_discovered",
            "title": "Structured child",
            "description": "Force discovered-from link failure",
            "discovered_from_id": parent["id"],
        },
    )
    payload = json.loads(result.content[0].text)
    assert payload["ok"] is False
    assert payload["command"] == "flow create-discovered"
    assert payload["result"] == "partial_state"
    assert payload["details"]["partial_state"] == "issue_created_without_discovered_from_link"
    assert payload["issue_id"]
    assert "bd dep add" in payload["recovery_command"]


@pytest.mark.asyncio
async def test_flow_close_safe_lints_reason_and_requires_verification(mcp_client):
    """flow(close_safe) should return policy violation for unsafe reasons and close on safe input."""
    import json

    issue_result = await mcp_client.call_tool(
        "create", {"title": "Close-safe target", "issue_type": "task", "brief": False}
    )
    issue = json.loads(issue_result.content[0].text)

    rejected = await mcp_client.call_tool(
        "flow",
        {
            "action": "close_safe",
            "issue_id": issue["id"],
            "reason": "Updated error handling in retry path",
            "verification": "pytest integrations/beads-mcp/tests -q",
        },
    )
    rejected_payload = json.loads(rejected.content[0].text)
    assert rejected_payload["ok"] is False
    assert rejected_payload["command"] == "flow close-safe"
    assert rejected_payload["result"] == "policy_violation"

    close_result = await mcp_client.call_tool(
        "flow",
        {
            "action": "close_safe",
            "issue_id": issue["id"],
            "reason": "Updated retry policy and threshold checks",
            "verification": "pytest integrations/beads-mcp/tests/test_mcp_server_integration.py -q",
        },
    )
    close_payload = json.loads(close_result.content[0].text)
    assert close_payload["ok"] is True
    assert close_payload["command"] == "flow close-safe"
    assert close_payload["result"] == "closed"
    assert close_payload["details"]["issue"]["status"] == "closed"


@pytest.mark.asyncio
async def test_flow_close_safe_forwards_force_and_multi_entries(mcp_client, monkeypatch):
    """flow(close_safe) should preserve --force and repeated --verified/--note entries."""
    import json
    from beads_mcp import server as server_module

    captured: dict[str, object] = {}

    async def fake_run_flow_cli(*args, workspace_root, actor_override=None):
        captured["args"] = list(args)
        captured["workspace_root"] = workspace_root
        captured["actor_override"] = actor_override
        return {
            "ok": True,
            "command": "flow close-safe",
            "result": "closed",
            "issue_id": "bd-close-forward",
            "details": {},
            "events": ["closed"],
        }

    monkeypatch.setattr(server_module, "_run_flow_cli", fake_run_flow_cli)

    result = await mcp_client.call_tool(
        "flow",
        {
            "action": "close_safe",
            "issue_id": "bd-close-forward",
            "reason": "Updated retry policy and threshold checks",
            "verification": ["pytest -q tests/a.py", "pytest -q tests/b.py"],
            "notes": ["line one", "line two"],
            "force": True,
        },
    )
    payload = json.loads(result.content[0].text)
    assert payload["ok"] is True
    assert payload["result"] == "closed"

    args = captured.get("args")
    assert isinstance(args, list)
    assert args[0] == "close-safe"
    assert "--force" in args
    assert args.count("--verified") == 2
    assert "pytest -q tests/a.py" in args
    assert "pytest -q tests/b.py" in args
    assert args.count("--note") == 2
    assert "line one" in args
    assert "line two" in args


@pytest.mark.asyncio
async def test_flow_block_with_context_fails_fast_on_missing_blocker(mcp_client):
    """flow(block_with_context) should return invalid_input for missing blocker before mutation."""
    import json

    issue_result = await mcp_client.call_tool(
        "create", {"title": "Block target", "issue_type": "task", "brief": False}
    )
    issue = json.loads(issue_result.content[0].text)

    result = await mcp_client.call_tool(
        "flow",
        {
            "action": "block_with_context",
            "issue_id": issue["id"],
            "context_pack": "state; repro; next",
            "blocker_id": "bd-missing-blocker",
        },
    )
    payload = json.loads(result.content[0].text)
    assert payload["ok"] is False
    assert payload["command"] == "flow block-with-context"
    assert payload["result"] == "invalid_input"

    shown = await mcp_client.call_tool("show", {"issue_id": issue["id"]})
    shown_payload = json.loads(shown.content[0].text)
    assert shown_payload["status"] == "open"
    assert "Context pack:" not in (shown_payload.get("notes") or "")


@pytest.mark.asyncio
async def test_flow_block_with_context_link_error_returns_structured_remediation(mcp_client, monkeypatch):
    """flow(block_with_context) should surface partial_state remediation payload without ToolError."""
    import json
    from beads_mcp import server as server_module
    from beads_mcp.bd_client import BdCommandError

    issue_result = await mcp_client.call_tool(
        "create", {"title": "Structured block target", "issue_type": "task", "brief": False}
    )
    issue = json.loads(issue_result.content[0].text)
    blocker_result = await mcp_client.call_tool(
        "create", {"title": "Structured blocker", "issue_type": "task", "brief": False}
    )
    blocker = json.loads(blocker_result.content[0].text)

    payload = {
        "ok": False,
        "command": "flow block-with-context",
        "result": "partial_state",
        "issue_id": issue["id"],
        "details": {
            "partial_state": "issue_blocked_without_blocker_link",
            "depends_on_id": blocker["id"],
        },
        "recovery_command": f"bd dep add {issue['id']} {blocker['id']} --type blocks",
        "events": ["blocked", "dependency_add_failed"],
    }

    async def fake_run_command(self, *args, **kwargs):
        if len(args) >= 2 and args[0] == "flow" and args[1] == "block-with-context":
            raise BdCommandError("simulated partial-state", stdout=json.dumps(payload), returncode=4)
        raise AssertionError(f"Unexpected command in partial-state test: {args}")

    monkeypatch.setattr(server_module.BdCliClient, "_run_command", fake_run_command)

    result = await mcp_client.call_tool(
        "flow",
        {
            "action": "block_with_context",
            "issue_id": issue["id"],
            "context_pack": "state; repro; next",
            "blocker_id": blocker["id"],
        },
    )
    payload = json.loads(result.content[0].text)
    assert payload["ok"] is False
    assert payload["command"] == "flow block-with-context"
    assert payload["result"] == "partial_state"
    assert payload["details"]["partial_state"] == "issue_blocked_without_blocker_link"
    assert payload["issue_id"] == issue["id"]
    assert "bd dep add" in payload["recovery_command"]


@pytest.mark.asyncio
async def test_flow_only_mode_blocks_direct_writes_but_allows_flow(mcp_client, monkeypatch):
    """When flow-only mode is enabled, direct lifecycle writes are blocked."""
    import json
    from fastmcp.exceptions import ToolError

    parent_result = await mcp_client.call_tool(
        "create", {"title": "Flow-only parent", "issue_type": "task", "brief": False}
    )
    parent = json.loads(parent_result.content[0].text)

    monkeypatch.setenv("BEADS_MCP_FLOW_ONLY_WRITES", "1")

    with pytest.raises(ToolError):
        await mcp_client.call_tool(
            "create", {"title": "Direct write should fail", "issue_type": "task", "brief": False}
        )

    created = await mcp_client.call_tool(
        "flow",
        {
            "action": "create_discovered",
            "title": "Flow-only child",
            "description": "Created via flow wrapper",
            "discovered_from_id": parent["id"],
        },
    )
    payload = json.loads(created.content[0].text)
    assert payload["ok"] is True
    assert payload["command"] == "flow create-discovered"
    assert payload["result"] == "created"
    assert payload["issue_id"]


@pytest.mark.asyncio
async def test_flow_claim_next_cli_mcp_parity_no_ready(mcp_client, bd_executable):
    """Direct CLI and MCP flow should report the same deterministic no_ready state."""
    import json

    ctx = await mcp_client.call_tool("context", {"action": "show"})
    workspace_root = _workspace_root_from_context_output(ctx.content[0].text)

    direct = await _run_bd_json(
        bd_executable,
        workspace_root,
        "--actor",
        "parity-no-ready",
        "flow",
        "claim-next",
        "--limit",
        "5",
    )
    via_mcp_raw = await mcp_client.call_tool(
        "flow", {"action": "claim_next", "actor": "parity-no-ready", "ready_limit": 5}
    )
    via_mcp = json.loads(via_mcp_raw.content[0].text)

    assert direct["result"] == "no_ready"
    assert via_mcp["result"] == "no_ready"
    assert direct["command"] == via_mcp["command"] == "flow claim-next"


@pytest.mark.asyncio
async def test_flow_create_discovered_cli_mcp_parity_invalid_from(mcp_client, bd_executable):
    """Direct CLI and MCP flow should report the same deterministic invalid_input state."""
    import json

    ctx = await mcp_client.call_tool("context", {"action": "show"})
    workspace_root = _workspace_root_from_context_output(ctx.content[0].text)

    direct = await _run_bd_json(
        bd_executable,
        workspace_root,
        "--actor",
        "parity-invalid-from",
        "flow",
        "create-discovered",
        "--title",
        "Parity child",
        "--from",
        "bd-does-not-exist",
    )
    via_mcp_raw = await mcp_client.call_tool(
        "flow",
        {
            "action": "create_discovered",
            "title": "Parity child",
            "discovered_from_id": "bd-does-not-exist",
            "actor": "parity-invalid-from",
        },
    )
    via_mcp = json.loads(via_mcp_raw.content[0].text)

    assert direct["result"] == "invalid_input"
    assert via_mcp["result"] == "invalid_input"
    assert direct["command"] == via_mcp["command"] == "flow create-discovered"
