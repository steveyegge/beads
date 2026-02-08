# 📿 Beads Newsletter: v0.49.4 - v0.49.5
**February 05, 2026 to February 08, 2026**

Welcome to another Beads release! This short but productive sprint brings v0.49.5 with critical stability improvements, enhanced federation capabilities, and better developer ergonomics. While this release cycle was brief, the team focused on hardening the codebase and fixing several important issues that improve reliability across the board.

## 🆕 New Commands & Options

The highlight of this release is the expansion of Beads' command-line capabilities with several powerful new additions that enhance collaboration and federation workflows.

The **`bd acquire`** command introduces merge slot management, allowing teams to coordinate complex merge operations without conflicts. When multiple team members need to work on interconnected issues, acquiring the merge slot ensures orderly resolution. Simply run `bd acquire` before starting your merge work, and the system will queue your request appropriately.

Federation gets a major boost with **`bd add-peer`**, which streamlines the process of connecting to other Beads instances. This command accepts optional SQL credentials, making it easier to establish secure connections between distributed teams. For example: `bd add-peer remote.example.com --sql-user readonly --sql-pass secret` creates a federation link while maintaining proper access controls.

The **`bd add-waiter`** command brings sophisticated synchronization primitives to Beads workflows. Gates and waiters allow teams to create dependency checkpoints - particularly useful when issues must be resolved in a specific order. Add a waiter with `bd add-waiter --gate release-v2 --issue BD-1234` to ensure proper sequencing.

Comment management becomes more streamlined with **`bd add`** for adding comments directly to issues from the command line. No more context switching to add quick notes: `bd add BD-567 "Verified fix in staging environment"` keeps your workflow in the terminal.

## 🔧 Major Features & Bug Fixes

The most significant improvement in this release addresses storage reliability through proper request drainage before shutdown (commit `fix: drain in-flight requests`). Previously, abrupt server stops could occasionally corrupt in-flight operations. The new implementation ensures all pending requests complete before closing storage connections, dramatically improving data integrity during restarts and deployments.

Search functionality receives a substantial upgrade with new content and null-check filters (`feat: add content and null-check filters`). Users can now search through issue content more effectively, with the ability to filter out null values and search within specific content fields. This makes finding relevant issues in large repositories significantly faster.

The SQL injection vulnerability fix (`security: add SQL identifier validation`) represents a critical security improvement. Dynamic table and database names are now properly validated before use in SQL queries, closing a potential attack vector. While no exploits were observed in the wild, we strongly recommend upgrading to this version.

Testing infrastructure saw major performance improvements, with the test suite execution time dropping from approximately 158 seconds to just 50 seconds (`perf(tests): speed up cmd/bd test suite`). This 3x speedup makes the development cycle much more pleasant and enables more frequent testing.

## 🐛 Minor Improvements & Fixes

The codebase underwent significant cleanup with the addition of `gofmt` checks to the CI pipeline, ensuring consistent code formatting across all contributions. The team also consolidated various utility functions, reducing duplication and improving maintainability.

Doctor mode received several fixes, including improved configuration checking for daemon roles and better error messages when suggesting daemon stops. The YAML configuration reader now handles edge cases more gracefully, preventing mysterious failures during startup.

Import and export operations are now more robust, with fixes for label synchronization, dependency handling, and proper dirty flag management. The system now correctly syncs labels during import by removing database labels absent from JSONL files, ensuring consistency between file and database states.

Documentation improvements include the addition of a new `messaging.md` file and corrections to markdown link syntax in the community tools documentation. The JetBrains community plugin is now properly documented, making it easier for IDE users to integrate Beads into their workflow.

## 🚀 Getting Started

To upgrade to v0.49.5, use your package manager of choice:

```bash
# Homebrew
brew upgrade beads

# Direct download
curl -L https://github.com/beads/beads/releases/download/v0.49.5/beads-linux-amd64 -o bd
chmod +x bd
```

New users can get started with Beads by initializing a new repository:

```bash
bd init my-project
bd create "First issue"
bd list
```

For teams looking to leverage the new federation features, start by adding a peer connection:

```bash
bd add-peer teammate.local
bd sync
```

The improved search capabilities make finding issues easier than ever:

```bash
bd search --content "critical bug" --no-null
```

## 📚 Resources

For complete details about this release, including all commits and technical specifics, visit the [full changelog](https://github.com/beads/beads/blob/main/CHANGELOG.md) or see the [GitHub release page](https://github.com/beads/beads/releases/tag/v0.49.5).

The community continues to grow, with new tools and integrations being added regularly. Check out the updated [COMMUNITY_TOOLS.md](https://github.com/beads/beads/blob/main/COMMUNITY_TOOLS.md) for the latest ecosystem additions.

---

*Thank you to all contributors who made this release possible! Special thanks to @developmeh, @ronniehyslop, @seanbearden, @PabloLION, and @turian for their valuable contributions during this release cycle. 🙏*