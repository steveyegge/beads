# Beads Newsletter: v0.49.6
**February 08, 2026**

## Hotfix: Embedded Dolt Mode Restored

v0.49.6 is a same-day hotfix for v0.49.5. The previous release incorrectly removed
embedded Dolt support from Beads. That removal was only intended for Gas Town
(the multi-agent workspace manager), not for Beads itself.

### What was restored

- **dolthub/driver dependency** and embedded connection path
- **Advisory flock** (AccessLock) for concurrent process protection
- **Embedded connector lifecycle** management in DoltStore
- **CGO build tags** across ~45 files
- **Vendored go-icu-regex** library for ICU regex support
- **nocgo stub files** for non-CGO builds
- **Bootstrap and embedded_uow** helpers

### What this means for users

If you were using embedded Dolt mode (the default for local development), v0.49.5
would have broken your workflow by requiring a running `dolt sql-server`. v0.49.6
restores the ability to use Dolt without a separate server process.

If you were already using server mode, this release is fully compatible - no changes
needed.

### Upgrade

```bash
brew upgrade beads
# or
curl -sSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh | bash
```
