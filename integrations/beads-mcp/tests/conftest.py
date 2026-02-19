"""Pytest configuration and fixtures for beads-mcp tests.

This module provides safety checks to prevent test pollution in production databases (bd-2c5a).
"""

import os
import sys
from pathlib import Path

import pytest


def pytest_configure(config):
    """Called before test collection starts - ensure we're not polluting production."""
    # CRITICAL (bd-2c5a): Prevent tests from polluting production database

    # Ensure local package imports resolve when pytest is launched from repo root.
    src_dir = Path(__file__).resolve().parents[1] / "src"
    if str(src_dir) not in sys.path:
        sys.path.insert(0, str(src_dir))

    # Prefer project-local virtualenv deps when running from repo root.
    py_ver = f"python{sys.version_info.major}.{sys.version_info.minor}"
    site_packages = Path(__file__).resolve().parents[1] / ".venv" / "lib" / py_ver / "site-packages"
    if site_packages.exists() and str(site_packages) not in sys.path:
        sys.path.insert(0, str(site_packages))
    
    # Set test mode flag
    os.environ["BEADS_TEST_MODE"] = "1"
    
    # Get the project root (where .git exists)
    current_dir = Path(__file__).parent.absolute()
    project_root = current_dir
    
    while project_root.parent != project_root:
        if (project_root / ".git").exists():
            break
        project_root = project_root.parent
    
    # If BEADS_DB or BEADS_WORKING_DIR point to production .beads/, fail immediately
    beads_db = os.environ.get("BEADS_DB", "")
    working_dir = os.environ.get("BEADS_WORKING_DIR", "")
    
    production_beads = str(project_root / ".beads")
    
    if beads_db and beads_db.startswith(production_beads):
        pytest.exit(
            f"PRODUCTION DATABASE POLLUTION DETECTED (bd-2c5a):\n"
            f"  BEADS_DB={beads_db}\n"
            f"  Production .beads/: {production_beads}\n"
            f"  Tests MUST use isolated temp databases.\n"
            f"  Remove BEADS_DB env var or point it to a temp directory.",
            returncode=1,
        )
    
    if working_dir and working_dir.startswith(str(project_root)):
        # Working dir in project is OK ONLY if it's not the project root itself
        if Path(working_dir).resolve() == project_root.resolve():
            pytest.exit(
                f"PRODUCTION DATABASE POLLUTION RISK (bd-2c5a):\n"
                f"  BEADS_WORKING_DIR={working_dir}\n"
                f"  Project root: {project_root}\n"
                f"  Tests should use isolated temp directories.\n"
                f"  Remove BEADS_WORKING_DIR or set it to a temp directory.",
                returncode=1,
            )


def pytest_runtest_setup(item):
    """Called before each test - verify test isolation."""
    # Check if test is using bd_client fixture
    if "bd_client" in item.fixturenames:
        # Verify BEADS_DB is not set to production during test execution
        beads_db = os.environ.get("BEADS_DB", "")
        if beads_db and ".beads/beads.db" in beads_db:
            # Get temp directory
            import tempfile
            if not beads_db.startswith(tempfile.gettempdir()):
                pytest.fail(
                    f"Test {item.name} is using production database (bd-2c5a):\n"
                    f"  BEADS_DB={beads_db}\n"
                    f"  This test must use a temporary database.",
                    pytrace=False,
                )
