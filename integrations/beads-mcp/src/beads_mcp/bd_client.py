"""Client for interacting with bd (beads) CLI and daemon."""

import asyncio
import json
import os
import re
from abc import ABC, abstractmethod
from typing import List, Optional

from .config import load_config
from .models import (
    AddDependencyParams,
    BlockedIssue,
    CloseIssueParams,
    CreateIssueParams,
    InitParams,
    Issue,
    ListIssuesParams,
    ReadyWorkParams,
    ReopenIssueParams,
    ShowIssueParams,
    Stats,
    UpdateIssueParams,
)


class BdError(Exception):
    """Base exception for bd CLI errors."""

    pass


class BdNotFoundError(BdError):
    """Raised when bd command is not found."""

    @staticmethod
    def installation_message(attempted_path: str) -> str:
        """Get helpful installation message.

        Args:
            attempted_path: Path where we tried to find bd

        Returns:
            Formatted error message with installation instructions
        """
        return (
            f"bd CLI not found at: {attempted_path}\n\n"
            "The beads Claude Code plugin requires the bd CLI to be installed separately.\n\n"
            "Install bd CLI:\n"
            "  curl -fsSL https://raw.githubusercontent.com/steveyegge/beads/main/install.sh | bash\n\n"
            "Or visit: https://github.com/steveyegge/beads#installation\n\n"
            "After installation, restart Claude Code to reload the MCP server."
        )


class BdCommandError(BdError):
    """Raised when bd command fails."""

    stderr: str
    returncode: int

    def __init__(self, message: str, stderr: str = "", returncode: int = 1):
        super().__init__(message)
        self.stderr = stderr
        self.returncode = returncode


class BdVersionError(BdError):
    """Raised when bd version is incompatible with MCP server."""

    pass


class BdClientBase(ABC):
    """Abstract base class for bd clients (CLI or daemon)."""

    @abstractmethod
    async def ready(self, params: Optional[ReadyWorkParams] = None) -> List[Issue]:
        """Get ready work (issues with no blockers)."""
        pass

    @abstractmethod
    async def list_issues(self, params: Optional[ListIssuesParams] = None) -> List[Issue]:
        """List issues with optional filters."""
        pass

    @abstractmethod
    async def show(self, params: ShowIssueParams) -> Issue:
        """Show detailed issue information."""
        pass

    @abstractmethod
    async def create(self, params: CreateIssueParams) -> Issue:
        """Create a new issue."""
        pass

    @abstractmethod
    async def update(self, params: UpdateIssueParams) -> Issue:
        """Update an existing issue."""
        pass

    @abstractmethod
    async def close(self, params: CloseIssueParams) -> List[Issue]:
        """Close one or more issues."""
        pass

    @abstractmethod
    async def reopen(self, params: ReopenIssueParams) -> List[Issue]:
        """Reopen one or more closed issues."""
        pass

    @abstractmethod
    async def add_dependency(self, params: AddDependencyParams) -> None:
        """Add a dependency between issues."""
        pass

    @abstractmethod
    async def stats(self) -> Stats:
        """Get repository statistics."""
        pass

    @abstractmethod
    async def blocked(self) -> List[BlockedIssue]:
        """Get blocked issues."""
        pass

    @abstractmethod
    async def init(self, params: Optional[InitParams] = None) -> str:
        """Initialize a new beads database."""
        pass


class BdCliClient(BdClientBase):
    """Client for calling bd CLI commands and parsing JSON output."""

    bd_path: str
    beads_db: str | None
    actor: str | None
    no_auto_flush: bool
    no_auto_import: bool
    working_dir: str | None

    def __init__(
        self,
        bd_path: str | None = None,
        beads_db: str | None = None,
        actor: str | None = None,
        no_auto_flush: bool | None = None,
        no_auto_import: bool | None = None,
        working_dir: str | None = None,
    ):
        """Initialize bd client.

        Args:
            bd_path: Path to bd executable (optional, loads from config if not provided)
            beads_db: Path to beads database file (optional, loads from config if not provided)
            actor: Actor name for audit trail (optional, loads from config if not provided)
            no_auto_flush: Disable automatic JSONL sync (optional, loads from config if not provided)
            no_auto_import: Disable automatic JSONL import (optional, loads from config if not provided)
            working_dir: Working directory for bd commands (optional, loads from config/env if not provided)
        """
        config = load_config()
        self.bd_path = bd_path if bd_path is not None else config.beads_path
        self.beads_db = beads_db if beads_db is not None else config.beads_db
        self.actor = actor if actor is not None else config.beads_actor
        self.no_auto_flush = no_auto_flush if no_auto_flush is not None else config.beads_no_auto_flush
        self.no_auto_import = no_auto_import if no_auto_import is not None else config.beads_no_auto_import
        self.working_dir = working_dir if working_dir is not None else config.beads_working_dir

    def _get_working_dir(self) -> str:
        """Get working directory for bd commands.

        Returns:
            Working directory path, falls back to current directory if not configured
        """
        if self.working_dir:
            return self.working_dir
        # Use process working directory (set by MCP client at spawn time)
        return os.getcwd()

    def _global_flags(self) -> list[str]:
        """Build list of global flags for bd commands.

        Returns:
            List of global flag arguments
        """
        flags = []
        if self.beads_db:
            flags.extend(["--db", self.beads_db])
        if self.actor:
            flags.extend(["--actor", self.actor])
        if self.no_auto_flush:
            flags.append("--no-auto-flush")
        if self.no_auto_import:
            flags.append("--no-auto-import")
        return flags

    async def _run_command(self, *args: str, cwd: str | None = None) -> object:
        """Run bd command and parse JSON output.

        Args:
            *args: Command arguments to pass to bd
            cwd: Optional working directory override for this command

        Returns:
            Parsed JSON output (dict or list)

        Raises:
            BdNotFoundError: If bd command not found
            BdCommandError: If bd command fails
        """
        cmd = [self.bd_path, *args, *self._global_flags(), "--json"]
        working_dir = cwd if cwd is not None else self._get_working_dir()

        # Log database routing for debugging
        import sys
        working_dir = self._get_working_dir()
        db_info = self.beads_db if self.beads_db else "auto-discover"
        print(f"[beads-mcp] Running bd command: {' '.join(args)}", file=sys.stderr)
        print(f"[beads-mcp]   Database: {db_info}", file=sys.stderr)
        print(f"[beads-mcp]   Working dir: {working_dir}", file=sys.stderr)

        try:
            process = await asyncio.create_subprocess_exec(
                *cmd,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                cwd=working_dir,
            )
            stdout, stderr = await process.communicate()
        except FileNotFoundError as e:
            raise BdNotFoundError(BdNotFoundError.installation_message(self.bd_path)) from e

        if process.returncode != 0:
            raise BdCommandError(
                f"bd command failed: {stderr.decode()}",
                stderr=stderr.decode(),
                returncode=process.returncode or 1,
            )

        stdout_str = stdout.decode().strip()
        if not stdout_str:
            return {}

        try:
            result: object = json.loads(stdout_str)
            return result
        except json.JSONDecodeError as e:
            raise BdCommandError(
                f"Failed to parse bd JSON output: {e}",
                stderr=stdout_str,
            ) from e

    async def _check_version(self) -> None:
        """Check that bd CLI version meets minimum requirements.

        Raises:
            BdVersionError: If bd version is incompatible
            BdNotFoundError: If bd command not found
        """
        # Minimum required version
        min_version = (0, 9, 0)

        try:
            process = await asyncio.create_subprocess_exec(
                self.bd_path,
                "version",
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                cwd=self._get_working_dir(),
            )
            stdout, stderr = await process.communicate()
        except FileNotFoundError as e:
            raise BdNotFoundError(BdNotFoundError.installation_message(self.bd_path)) from e

        if process.returncode != 0:
            raise BdCommandError(
                f"bd version failed: {stderr.decode()}",
                stderr=stderr.decode(),
                returncode=process.returncode or 1,
            )

        # Parse version from output like "bd version 0.9.2"
        version_output = stdout.decode().strip()
        match = re.search(r"(\d+)\.(\d+)\.(\d+)", version_output)
        if not match:
            raise BdVersionError(f"Could not parse bd version from: {version_output}")

        version = tuple(int(x) for x in match.groups())

        if version < min_version:
            min_ver_str = ".".join(str(x) for x in min_version)
            cur_ver_str = ".".join(str(x) for x in version)
            install_cmd = "curl -fsSL https://raw.githubusercontent.com/steveyegge/beads/main/install.sh | bash"
            raise BdVersionError(
                f"bd version {cur_ver_str} is too old. "
                f"This MCP server requires bd >= {min_ver_str}. "
                f"Update with: {install_cmd}"
            )

    async def ready(self, params: ReadyWorkParams | None = None) -> list[Issue]:
        """Get ready work (issues with no blocking dependencies).

        Args:
            params: Query parameters

        Returns:
            List of ready issues
        """
        params = params or ReadyWorkParams()
        args = ["ready", "--limit", str(params.limit)]

        if params.priority is not None:
            args.extend(["--priority", str(params.priority)])
        if params.assignee:
            args.extend(["--assignee", params.assignee])

        data = await self._run_command(*args)
        if not isinstance(data, list):
            return []

        return [Issue.model_validate(issue) for issue in data]

    async def list_issues(self, params: ListIssuesParams | None = None) -> list[Issue]:
        """List issues with optional filters.

        Args:
            params: Query parameters

        Returns:
            List of issues
        """
        params = params or ListIssuesParams()
        args = ["list"]

        if params.status:
            args.extend(["--status", params.status])
        if params.priority is not None:
            args.extend(["--priority", str(params.priority)])
        if params.issue_type:
            args.extend(["--type", params.issue_type])
        if params.assignee:
            args.extend(["--assignee", params.assignee])
        if params.limit:
            args.extend(["--limit", str(params.limit)])

        data = await self._run_command(*args)
        if not isinstance(data, list):
            return []

        return [Issue.model_validate(issue) for issue in data]

    async def show(self, params: ShowIssueParams) -> Issue:
        """Show issue details.

        Args:
            params: Issue ID to show

        Returns:
            Issue details

        Raises:
            BdCommandError: If issue not found
        """
        data = await self._run_command("show", params.issue_id)
        if not isinstance(data, dict):
            raise BdCommandError(f"Invalid response for show {params.issue_id}")

        return Issue.model_validate(data)

    async def create(self, params: CreateIssueParams) -> Issue:
        """Create a new issue.

        Args:
            params: Issue creation parameters

        Returns:
            Created issue
        """
        args = ["create", params.title, "-p", str(params.priority), "-t", params.issue_type]

        if params.description:
            args.extend(["-d", params.description])
        if params.design:
            args.extend(["--design", params.design])
        if params.acceptance:
            args.extend(["--acceptance", params.acceptance])
        if params.external_ref:
            args.extend(["--external-ref", params.external_ref])
        if params.assignee:
            args.extend(["--assignee", params.assignee])
        if params.id:
            args.extend(["--id", params.id])
        for label in params.labels:
            args.extend(["-l", label])
        if params.deps:
            args.extend(["--deps", ",".join(params.deps)])

        data = await self._run_command(*args)
        if not isinstance(data, dict):
            raise BdCommandError("Invalid response for create")

        return Issue.model_validate(data)

    async def update(self, params: UpdateIssueParams) -> Issue:
        """Update an issue.

        Args:
            params: Issue update parameters

        Returns:
            Updated issue
        """
        args = ["update", params.issue_id]

        if params.status:
            args.extend(["--status", params.status])
        if params.priority is not None:
            args.extend(["--priority", str(params.priority)])
        if params.assignee:
            args.extend(["--assignee", params.assignee])
        if params.title:
            args.extend(["--title", params.title])
        if params.design:
            args.extend(["--design", params.design])
        if params.acceptance_criteria:
            args.extend(["--acceptance", params.acceptance_criteria])
        if params.notes:
            args.extend(["--notes", params.notes])
        if params.external_ref:
            args.extend(["--external-ref", params.external_ref])

        data = await self._run_command(*args)
        if not isinstance(data, dict):
            raise BdCommandError(f"Invalid response for update {params.issue_id}")

        return Issue.model_validate(data)

    async def close(self, params: CloseIssueParams) -> list[Issue]:
        """Close an issue.

        Args:
            params: Close parameters

        Returns:
            List containing closed issue
        """
        args = ["close", params.issue_id, "--reason", params.reason]

        data = await self._run_command(*args)
        if not isinstance(data, list):
            raise BdCommandError(f"Invalid response for close {params.issue_id}")

        return [Issue.model_validate(issue) for issue in data]

    async def reopen(self, params: ReopenIssueParams) -> list[Issue]:
        """Reopen one or more closed issues.

        Args:
            params: Reopen parameters

        Returns:
            List of reopened issues
        """
        args = ["reopen", *params.issue_ids]

        if params.reason:
            args.extend(["--reason", params.reason])

        data = await self._run_command(*args)
        if not isinstance(data, list):
            raise BdCommandError(f"Invalid response for reopen {params.issue_ids}")

        return [Issue.model_validate(issue) for issue in data]

    async def add_dependency(self, params: AddDependencyParams) -> None:
        """Add a dependency between issues.

        Args:
            params: Dependency parameters
        """
        # bd dep add doesn't return JSON, just prints confirmation
        cmd = [
            self.bd_path,
            "dep",
            "add",
            params.from_id,
            params.to_id,
            "--type",
            params.dep_type,
            *self._global_flags(),
        ]

        try:
            process = await asyncio.create_subprocess_exec(
                *cmd,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                cwd=self._get_working_dir(),
            )
            _stdout, stderr = await process.communicate()
        except FileNotFoundError as e:
            raise BdNotFoundError(BdNotFoundError.installation_message(self.bd_path)) from e

        if process.returncode != 0:
            raise BdCommandError(
                f"bd dep add failed: {stderr.decode()}",
                stderr=stderr.decode(),
                returncode=process.returncode or 1,
            )

    async def quickstart(self) -> str:
        """Get bd quickstart guide.

        Returns:
            Quickstart guide text
        """
        cmd = [self.bd_path, "quickstart"]

        try:
            process = await asyncio.create_subprocess_exec(
                *cmd,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                cwd=self._get_working_dir(),
            )
            stdout, stderr = await process.communicate()
        except FileNotFoundError as e:
            raise BdNotFoundError(BdNotFoundError.installation_message(self.bd_path)) from e

        if process.returncode != 0:
            raise BdCommandError(
                f"bd quickstart failed: {stderr.decode()}",
                stderr=stderr.decode(),
                returncode=process.returncode or 1,
            )

        return stdout.decode()

    async def stats(self) -> Stats:
        """Get statistics about issues.

        Returns:
            Statistics object
        """
        data = await self._run_command("stats")
        if not isinstance(data, dict):
            raise BdCommandError("Invalid response for stats")

        return Stats.model_validate(data)

    async def blocked(self) -> list[BlockedIssue]:
        """Get blocked issues.

        Returns:
            List of blocked issues with blocking information
        """
        data = await self._run_command("blocked")
        if not isinstance(data, list):
            return []

        return [BlockedIssue.model_validate(issue) for issue in data]

    async def init(self, params: InitParams | None = None) -> str:
        """Initialize bd in current directory.

        Args:
            params: Initialization parameters

        Returns:
            Initialization output message
        """
        params = params or InitParams()
        cmd = [self.bd_path, "init"]

        if params.prefix:
            cmd.extend(["--prefix", params.prefix])

        # NOTE: Do NOT add --db flag for init!
        # init creates a NEW database in the current directory.
        # Only add actor-related flags.
        if self.actor:
            cmd.extend(["--actor", self.actor])

        try:
            process = await asyncio.create_subprocess_exec(
                *cmd,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                cwd=self._get_working_dir(),
            )
            stdout, stderr = await process.communicate()
        except FileNotFoundError as e:
            raise BdNotFoundError(BdNotFoundError.installation_message(self.bd_path)) from e

        if process.returncode != 0:
            raise BdCommandError(
                f"bd init failed: {stderr.decode()}",
                stderr=stderr.decode(),
                returncode=process.returncode or 1,
            )

        return stdout.decode()


# Backwards compatibility alias
BdClient = BdCliClient


def create_bd_client(
    prefer_daemon: bool = False,
    bd_path: Optional[str] = None,
    beads_db: Optional[str] = None,
    actor: Optional[str] = None,
    no_auto_flush: Optional[bool] = None,
    no_auto_import: Optional[bool] = None,
    working_dir: Optional[str] = None,
) -> BdClientBase:
    """Create a bd client (daemon or CLI-based).

    Args:
        prefer_daemon: If True, attempt to use daemon client first, fall back to CLI
        bd_path: Path to bd executable (for CLI client)
        beads_db: Path to beads database (for CLI client)
        actor: Actor name for audit trail
        no_auto_flush: Disable auto-flush (CLI only)
        no_auto_import: Disable auto-import (CLI only)
        working_dir: Working directory for database discovery

    Returns:
        BdClientBase implementation (daemon or CLI)

    Note:
        If prefer_daemon is True and daemon is not running, falls back to CLI client.
        To check if daemon is running without falling back, use BdDaemonClient directly.
    """
    if prefer_daemon:
        try:
            from .bd_daemon_client import BdDaemonClient
            from pathlib import Path

            # Check if daemon socket exists before creating client
            # Walk up from working_dir to find .beads/bd.sock, then check global
            search_dir = Path(working_dir) if working_dir else Path.cwd()
            socket_found = False

            current = search_dir.resolve()
            while True:
                beads_dir = current / ".beads"
                if beads_dir.is_dir():
                    sock_path = beads_dir / "bd.sock"
                    if sock_path.exists():
                        socket_found = True
                        break
                    # Found .beads but no socket - check global before giving up
                    break

                # Move up one directory
                parent = current.parent
                if parent == current:
                    # Reached filesystem root - check global
                    break
                current = parent

            # If no local socket, check global daemon socket at ~/.beads/bd.sock
            if not socket_found:
                global_sock_path = Path.home() / ".beads" / "bd.sock"
                if global_sock_path.exists():
                    socket_found = True

            if socket_found:
                # Daemon is running, use it
                client = BdDaemonClient(
                    working_dir=working_dir,
                    actor=actor,
                )
                return client
            # No socket found, fall through to CLI client
        except ImportError:
            # Daemon client not available (shouldn't happen but be defensive)
            pass
        except Exception:
            # If daemon setup fails for any reason, fall back to CLI
            pass

    # Use CLI client
    return BdCliClient(
        bd_path=bd_path,
        beads_db=beads_db,
        actor=actor,
        no_auto_flush=no_auto_flush,
        no_auto_import=no_auto_import,
        working_dir=working_dir,
    )
