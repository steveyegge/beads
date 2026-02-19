"""CLI-vs-MCP parity assertions for deterministic control-plane states."""

from __future__ import annotations

import asyncio
import json
import os
import shutil
import tempfile
from contextlib import asynccontextmanager

import pytest
from fastmcp.client import Client

from beads_mcp.server import mcp
from tests._bd_binary import resolve_bd_executable


@pytest.fixture(scope="session")
def bd_executable() -> str:
    """Resolve the custom-fork bd binary with capability probes."""
    try:
        return resolve_bd_executable()
    except RuntimeError as exc:
        pytest.fail(str(exc))


def _run(coro):
    """Run an async coroutine from sync tests."""
    return asyncio.run(coro)


async def _run_bd_json(bd_executable: str, workspace_root: str, *args: str) -> dict:
    """Run a bd command with --json and parse object output.

    `bd show --json` returns a single-element array; normalize that to object.
    """
    process = await asyncio.create_subprocess_exec(
        bd_executable,
        *args,
        "--json",
        stdin=asyncio.subprocess.DEVNULL,
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
    try:
        payload = json.loads(stdout_text)
    except json.JSONDecodeError as exc:
        raise AssertionError(
            f"bd command did not return JSON (code={process.returncode}): {stdout_text} | {stderr_text}"
        ) from exc
    if isinstance(payload, list):
        if len(payload) == 1 and isinstance(payload[0], dict):
            return payload[0]
        raise AssertionError(f"Expected single-object JSON list, got: {payload}")
    if not isinstance(payload, dict):
        raise AssertionError(f"Expected JSON object payload, got {type(payload)}: {payload}")
    return payload


async def _mcp_tool_json(client: Client, tool_name: str, payload: dict) -> dict:
    """Call MCP tool and parse JSON payload."""
    result = await client.call_tool(tool_name, payload)
    text = result.content[0].text if result.content else "{}"
    decoded = json.loads(text)
    if not isinstance(decoded, dict):
        raise AssertionError(f"Expected dict payload from MCP tool {tool_name}, got {type(decoded)}")
    return decoded


@asynccontextmanager
async def _mcp_session(bd_executable: str):
    """Create isolated workspace + MCP client session for parity scenarios."""
    from beads_mcp import tools

    workspace_root = tempfile.mkdtemp(prefix="beads_mcp_parity_", dir="/tmp")
    env = os.environ.copy()
    env.pop("BEADS_DB", None)
    env.pop("BEADS_DIR", None)

    init_process = await asyncio.create_subprocess_exec(
        bd_executable,
        "init",
        "--prefix",
        "parity",
        stdin=asyncio.subprocess.DEVNULL,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
        cwd=workspace_root,
        env=env,
    )
    _, init_stderr = await init_process.communicate()
    if init_process.returncode != 0:
        shutil.rmtree(workspace_root, ignore_errors=True)
        raise AssertionError(f"failed to init parity workspace: {init_stderr.decode()}")

    old_env = {
        "BEADS_NO_DAEMON": os.environ.get("BEADS_NO_DAEMON"),
        "BEADS_PATH": os.environ.get("BEADS_PATH"),
        "BEADS_CONTEXT_SET": os.environ.get("BEADS_CONTEXT_SET"),
        "BEADS_WORKING_DIR": os.environ.get("BEADS_WORKING_DIR"),
        "BEADS_DB": os.environ.get("BEADS_DB"),
        "BEADS_DIR": os.environ.get("BEADS_DIR"),
    }
    os.environ["BEADS_NO_DAEMON"] = "1"
    os.environ["BEADS_PATH"] = bd_executable
    os.environ.pop("BEADS_DB", None)
    os.environ.pop("BEADS_DIR", None)

    tools._connection_pool.clear()
    try:
        async with Client(mcp) as client:
            await client.call_tool("context", {"workspace_root": workspace_root})
            yield client, workspace_root
    finally:
        tools._connection_pool.clear()
        for key, value in old_env.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value
        shutil.rmtree(workspace_root, ignore_errors=True)


def _assert_core_parity(direct: dict, via_mcp: dict, expected_result: str, expected_command: str) -> None:
    """Assert deterministic parity fields."""
    assert direct["result"] == via_mcp["result"] == expected_result
    assert direct["command"] == via_mcp["command"] == expected_command
    assert direct["ok"] == via_mcp["ok"]


def test_cli_mcp_parity_claimed_state(bd_executable):
    """claimed: both direct CLI and MCP claim paths should return claimed."""

    async def scenario():
        async with _mcp_session(bd_executable) as (client, workspace_root):
            direct_created = await _run_bd_json(
                bd_executable,
                workspace_root,
                "create",
                "Direct parity claim target",
                "-p",
                "1",
            )
            direct_id = direct_created["id"]
            direct = await _run_bd_json(
                bd_executable,
                workspace_root,
                "--actor",
                "parity-claimed-direct",
                "flow",
                "claim-next",
                "--limit",
                "5",
            )

            await _run_bd_json(
                bd_executable,
                workspace_root,
                "create",
                "MCP parity claim target",
                "-p",
                "1",
            )
            via_mcp = await _mcp_tool_json(
                client,
                "flow",
                {"action": "claim_next", "actor": "parity-claimed-mcp", "ready_limit": 5},
            )

            _assert_core_parity(direct, via_mcp, "claimed", "flow claim-next")
            assert direct.get("issue_id")
            assert via_mcp.get("issue_id")

            direct_issue = await _run_bd_json(bd_executable, workspace_root, "show", direct_id)
            assert direct_issue["status"] == "in_progress"
            mcp_issue = await _run_bd_json(bd_executable, workspace_root, "show", via_mcp["issue_id"])
            assert mcp_issue["status"] == "in_progress"

    _run(scenario())


def test_cli_mcp_parity_wip_blocked_state(bd_executable):
    """wip_blocked: both paths should reject second claim for the same actor."""

    async def scenario():
        async with _mcp_session(bd_executable) as (client, workspace_root):
            await _run_bd_json(bd_executable, workspace_root, "create", "Parity WIP 1", "-p", "1")
            await _run_bd_json(bd_executable, workspace_root, "create", "Parity WIP 2", "-p", "1")

            actor = "parity-wip-actor"
            first = await _run_bd_json(
                bd_executable, workspace_root, "--actor", actor, "flow", "claim-next", "--limit", "5"
            )
            assert first["result"] == "claimed"

            direct = await _run_bd_json(
                bd_executable, workspace_root, "--actor", actor, "flow", "claim-next", "--limit", "5"
            )
            via_mcp = await _mcp_tool_json(
                client, "flow", {"action": "claim_next", "actor": actor, "ready_limit": 5}
            )

            _assert_core_parity(direct, via_mcp, "wip_blocked", "flow claim-next")
            assert first["issue_id"] in direct.get("details", {}).get("in_progress_ids", [])
            assert first["issue_id"] in via_mcp.get("details", {}).get("in_progress_ids", [])

    _run(scenario())


def test_cli_mcp_parity_no_ready_state(bd_executable):
    """no_ready: both direct CLI and MCP should return no_ready on empty queue."""

    async def scenario():
        async with _mcp_session(bd_executable) as (client, workspace_root):
            actor = "parity-no-ready"
            direct = await _run_bd_json(
                bd_executable, workspace_root, "--actor", actor, "flow", "claim-next", "--limit", "5"
            )
            via_mcp = await _mcp_tool_json(
                client, "flow", {"action": "claim_next", "actor": actor, "ready_limit": 5}
            )
            _assert_core_parity(direct, via_mcp, "no_ready", "flow claim-next")

    _run(scenario())


def test_cli_mcp_parity_policy_violation_state(bd_executable):
    """policy_violation: close-safe unsafe reason should be rejected identically."""

    async def scenario():
        async with _mcp_session(bd_executable) as (client, workspace_root):
            direct_target = await _run_bd_json(
                bd_executable, workspace_root, "create", "Direct policy target", "-p", "1"
            )
            mcp_target = await _run_bd_json(
                bd_executable, workspace_root, "create", "MCP policy target", "-p", "1"
            )

            direct = await _run_bd_json(
                bd_executable,
                workspace_root,
                "flow",
                "close-safe",
                "--issue",
                direct_target["id"],
                "--reason",
                "Updated error handling path",
                "--verified",
                "pytest -q",
            )
            via_mcp = await _mcp_tool_json(
                client,
                "flow",
                {
                    "action": "close_safe",
                    "issue_id": mcp_target["id"],
                    "reason": "Updated error handling path",
                    "verification": "pytest -q",
                },
            )

            _assert_core_parity(direct, via_mcp, "policy_violation", "flow close-safe")

            direct_issue = await _run_bd_json(bd_executable, workspace_root, "show", direct_target["id"])
            mcp_issue = await _run_bd_json(bd_executable, workspace_root, "show", mcp_target["id"])
            assert direct_issue["status"] == "open"
            assert mcp_issue["status"] == "open"

    _run(scenario())


def test_cli_mcp_parity_contention_state_fixture_driven(bd_executable, monkeypatch):
    """contention: fixture-driven parity compares deterministic payload shape."""
    from beads_mcp import server as server_module

    fixture_payload = {
        "ok": True,
        "command": "flow claim-next",
        "result": "contention",
        "details": {
            "actor": "parity-contention",
            "contention_ids": ["bd-1", "bd-2"],
            "message": "Ready issues were contended during claim",
        },
        "events": ["claim_contention"],
    }

    async def fake_run_command(self, *args, **kwargs):
        if len(args) >= 2 and args[0] == "flow" and args[1] == "claim-next":
            return fixture_payload
        raise AssertionError(f"Unexpected command in contention parity test: {args}")

    monkeypatch.setattr(server_module.BdCliClient, "_run_command", fake_run_command)

    async def scenario():
        async with _mcp_session(bd_executable) as (client, _workspace_root):
            via_mcp = await _mcp_tool_json(
                client, "flow", {"action": "claim_next", "actor": "parity-contention", "ready_limit": 5}
            )
            _assert_core_parity(fixture_payload, via_mcp, "contention", "flow claim-next")
            assert via_mcp["details"]["contention_ids"] == ["bd-1", "bd-2"]

    _run(scenario())


def test_cli_mcp_parity_partial_state_fixture_driven(bd_executable, monkeypatch):
    """partial_state: fixture-driven parity compares deterministic recovery payloads."""
    from beads_mcp import server as server_module
    from beads_mcp.bd_client import BdCommandError

    async def scenario():
        async with _mcp_session(bd_executable) as (client, _workspace_root):
            parent = await _mcp_tool_json(
                client, "create", {"title": "Partial state parent", "issue_type": "task", "brief": False}
            )
            payload = {
                "ok": False,
                "command": "flow create-discovered",
                "result": "partial_state",
                "issue_id": "parity-partial-create",
                "details": {
                    "partial_state": "issue_created_without_discovered_from_link",
                    "depends_on_id": parent["id"],
                },
                "recovery_command": f"bd dep add parity-partial-create {parent['id']} --type discovered-from",
                "events": ["created", "dependency_add_failed"],
            }

            async def fake_run_command(self, *args, **kwargs):
                if len(args) >= 2 and args[0] == "flow" and args[1] == "create-discovered":
                    raise BdCommandError("simulated partial-state", stdout=json.dumps(payload), returncode=4)
                raise AssertionError(f"Unexpected command in partial-state parity test: {args}")

            monkeypatch.setattr(server_module.BdCliClient, "_run_command", fake_run_command)

            via_mcp = await _mcp_tool_json(
                client,
                "flow",
                {
                    "action": "create_discovered",
                    "title": "Parity partial child",
                    "description": "fixture-driven parity",
                    "discovered_from_id": parent["id"],
                },
            )
            _assert_core_parity(payload, via_mcp, "partial_state", "flow create-discovered")
            assert via_mcp["recovery_command"] == payload["recovery_command"]

    _run(scenario())
