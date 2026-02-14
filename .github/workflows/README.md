# GitHub Actions Workflows (Disabled)

All CI/CD for this fork has moved to [RWX](https://www.rwx.com). See `.rwx/` for active workflow definitions.

The `.disabled` files are kept for reference only and do not run.

| RWX Workflow | Replaces | Purpose |
|-------------|----------|---------|
| `.rwx/ci.yml` | `ci.yml` | Build, lint, test |
| `.rwx/docker.yml` | `docker.yml` | Docker build & push to GHCR |
| `.rwx/helm.yml` | `helm.yml` | Helm chart lint & publish |
| `.rwx/release.yml` | `release.yml`, `fork-release.yml` | GoReleaser binary releases |

Upstream-only workflows (not needed on fork): `deploy-docs.yml`, `mirror-ecr.yml`, `nightly.yml`, `test-pypi.yml`
