# Agent Self-Enhancement via Toolchain Sidecars

> **Status**: Draft
> **Date**: 2026-02-12
> **Repos**: gastown (controller, helm, agent image), beads (bead metadata, daemon RPC)

## Overview

K8s agent pods today run a single `gastown-agent` container (plus coop sidecar). Agents that need compilation, Docker builds, AWS CLI, or other dev tools cannot install them at runtime -- the container filesystem is ephemeral and the image is locked down.

This design adds an **optional toolchain sidecar** to agent pods. Agents declare the tools they need via bead metadata; the controller injects a matching sidecar container that shares the workspace volume. Agents can also **self-enhance**: build a custom image, push it to the registry, update their bead metadata, and get restarted with the new sidecar -- all without human intervention.

### Key Decisions

- **No Docker DaemonSet.** Containerd's node-level image store already caches pulled layers. Kaniko builds images without a Docker daemon. Registry-based layer caching (`--cache=true`) handles the build side. This avoids the security and operational burden of privileged DaemonSets.
- **Profile-based with custom override.** Named sidecar profiles (e.g., `toolchain-full`, `toolchain-minimal`) cover common cases. Agents can specify a custom image for full control.
- **K8s native sidecars (v1.29+).** Toolchain containers use `restartPolicy: Always` in `initContainers` for proper lifecycle management.
- **Coop session resume across restarts.** When a sidecar change triggers pod recreation, coop resumes the agent session from the PVC-persisted state.

---

## Sidecar Spec Design

### Bead Metadata Schema

Agents request sidecars by writing metadata keys on their bead:

| Key | Type | Description |
|-----|------|-------------|
| `sidecar_profile` | string | Named profile: `toolchain-full`, `toolchain-minimal`, `none` |
| `sidecar_image` | string | Custom image override (takes precedence over profile) |
| `sidecar_resources_cpu` | string | CPU limit override (e.g., `"1"`) |
| `sidecar_resources_memory` | string | Memory limit override (e.g., `"2Gi"`) |

### Go Types (gastown controller)

File: `gastown/controller/internal/podmanager/manager.go`

```go
// SidecarProfile is a named toolchain preset.
type SidecarProfile struct {
    Name      string                       // "toolchain-full", "toolchain-minimal"
    Image     string                       // e.g., "ghcr.io/groblegark/gastown-toolchain:latest"
    Resources *corev1.ResourceRequirements // per-profile defaults
    VolumeMounts []corev1.VolumeMount      // shared workspace, tmp, etc.
}

// ToolchainSidecarSpec configures an optional toolchain sidecar container.
// Resolved from bead metadata (sidecar_profile / sidecar_image).
type ToolchainSidecarSpec struct {
    // Profile is the named preset. Empty means no sidecar.
    Profile string

    // Image overrides the profile image. Takes precedence.
    Image string

    // Resources overrides profile defaults for the sidecar.
    Resources *corev1.ResourceRequirements
}
```

Add to `AgentPodSpec`:

```go
type AgentPodSpec struct {
    // ... existing fields ...

    // ToolchainSidecar configures an optional toolchain sidecar.
    // When set, the pod gets a native sidecar (initContainer with
    // restartPolicy: Always) that shares the workspace volume.
    ToolchainSidecar *ToolchainSidecarSpec
}
```

### Profile Registry

Profiles are defined in controller config and exposed via Helm values. The controller resolves `sidecar_profile` metadata to an image + resources.

```go
// ProfileRegistry maps profile names to concrete specs.
type ProfileRegistry struct {
    profiles map[string]SidecarProfile
}

func (r *ProfileRegistry) Resolve(meta map[string]string) *ToolchainSidecarSpec {
    if img := meta["sidecar_image"]; img != "" {
        return &ToolchainSidecarSpec{Image: img}
    }
    if name := meta["sidecar_profile"]; name != "" {
        if p, ok := r.profiles[name]; ok {
            return &ToolchainSidecarSpec{Profile: name, Image: p.Image, Resources: p.Resources}
        }
    }
    return nil
}
```

---

## Toolchain Base Image

File: `gastown/docker/toolchain/Dockerfile`

A multi-stage build with ARG-gated tool installation for forkability.

```dockerfile
FROM public.ecr.aws/docker/library/ubuntu:24.04 AS base
ARG TARGETARCH
ARG INSTALL_GO=true
ARG INSTALL_NODE=true
ARG INSTALL_PYTHON=true
ARG INSTALL_RUST=false
ARG INSTALL_AWS=true
ARG INSTALL_DOCKER_CLI=true

# Common tools always included
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl git jq make unzip ca-certificates && \
    rm -rf /var/lib/apt/lists/*

# Go (conditional)
FROM base AS go-true
RUN curl -fsSL https://go.dev/dl/go1.24.1.linux-${TARGETARCH}.tar.gz | \
    tar -C /usr/local -xz
FROM base AS go-false
# no-op

# Node (conditional)
FROM base AS node-true
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
    apt-get install -y nodejs && rm -rf /var/lib/apt/lists/*
FROM base AS node-false

# Rust (conditional)
FROM base AS rust-true
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | \
    sh -s -- -y --default-toolchain stable --profile minimal
FROM base AS rust-false

# ... similar for python, aws, docker-cli ...

# Final assembly: copy from conditional stages
FROM base AS final
COPY --from=go-${INSTALL_GO} /usr/local/go /usr/local/go
# ... remaining COPY --from directives ...

# Kaniko for daemonless image building
COPY --from=gcr.io/kaniko-project/executor:v1.23.2 \
    /kaniko/executor /usr/local/bin/kaniko

ENV PATH="/usr/local/go/bin:/root/.cargo/bin:${PATH}"

# Shared workspace mount point (matches agent container)
VOLUME /home/agent/gt
WORKDIR /home/agent/gt

# Native sidecar: sleep forever, tools accessed via exec/shared volume
CMD ["sleep", "infinity"]
```

### Profiles

| Profile | Tools | Image Size (est.) |
|---------|-------|-------------------|
| `toolchain-full` | go, node, python, aws, docker-cli, kaniko | ~1.5 GB |
| `toolchain-minimal` | git, jq, make, curl | ~200 MB |

Users fork by overriding build ARGs or providing a custom Dockerfile.

---

## Image Building (Kaniko)

Agents build custom sidecar images using kaniko from within the toolchain sidecar. No Docker daemon required.

### How It Works

1. Agent writes a `Dockerfile` in the workspace
2. Agent executes kaniko via the toolchain sidecar:
   ```bash
   kubectl exec $POD -c toolchain -- /usr/local/bin/kaniko \
     --context=/home/agent/gt/my-image \
     --destination=ghcr.io/groblegark/custom-sidecar:$TAG \
     --cache=true \
     --cache-repo=ghcr.io/groblegark/kaniko-cache
   ```
3. In practice, the agent runs this from inside its own pod via shared PID namespace or `exec` into the toolchain container

### Caching Strategy

- **Build side**: Kaniko `--cache=true` with `--cache-repo` pushes layer cache to the registry. Subsequent builds with unchanged layers hit cache.
- **Pull side**: Containerd's node-level image store caches all pulled images. When `FROM gastown-toolchain:latest` layers are already on the node, only delta layers for the custom image are pulled.
- **No DaemonSet needed.** The combination of registry cache (build) + containerd node cache (pull) provides the caching layer without running a Docker daemon on each node.

### Registry Credentials

Kaniko reads credentials from:
- `/kaniko/.docker/config.json` (mounted from K8s Secret)
- Service account with image pull secret

The controller mounts the existing `gitCredentialsSecret` (which typically has `ghcr.io` access) into the toolchain sidecar at the kaniko config path.

---

## Controller Changes

### Pod Spec Injection

File: `gastown/controller/internal/podmanager/manager.go` (`buildPod` method)

When `ToolchainSidecar` is set on `AgentPodSpec`, `buildPod()` adds a native sidecar:

```go
if spec.ToolchainSidecar != nil {
    toolchainContainer := corev1.Container{
        Name:  "toolchain",
        Image: spec.ToolchainSidecar.Image,
        RestartPolicy: ptr(corev1.ContainerRestartPolicyAlways), // K8s native sidecar
        VolumeMounts: []corev1.VolumeMount{
            {Name: VolumeWorkspace, MountPath: MountWorkspace},
            {Name: VolumeTmp, MountPath: MountTmp},
        },
        Resources: resolveResources(spec.ToolchainSidecar.Resources),
        SecurityContext: &corev1.SecurityContext{
            RunAsUser:  ptr(AgentUID),
            RunAsGroup: ptr(AgentGID),
        },
    }
    pod.Spec.InitContainers = append(pod.Spec.InitContainers, toolchainContainer)
}
```

### Reconciliation: Event-Driven Sidecar Updates

File: `gastown/controller/internal/reconciler/reconciler.go`

The reconciler already watches for bead mutations via SSE/NATS. When a bead's `sidecar_profile` or `sidecar_image` metadata changes:

1. The `BEAD_MUTATED` event triggers reconciliation
2. Reconciler compares the desired sidecar spec (from bead metadata) against the running pod's sidecar
3. If they differ, the pod is deleted and recreated with the new spec
4. Coop resumes the agent session from PVC-persisted state

Detection logic in reconciler:

```go
func sidecarChanged(desired *ToolchainSidecarSpec, actual *corev1.Pod) bool {
    current := findContainer(actual.Spec.InitContainers, "toolchain")
    if desired == nil && current == nil { return false }
    if desired == nil || current == nil { return true }
    return current.Image != desired.Image
}
```

### Rate Limiting

Sidecar changes are rate-limited to **3 per hour per agent** to prevent runaway self-enhancement loops. The controller tracks change timestamps per bead ID and rejects mutations that exceed the limit.

```go
type SidecarRateLimiter struct {
    mu       sync.Mutex
    changes  map[string][]time.Time // beadID -> timestamps
    maxPerHr int
}
```

---

## Agent Prompting

### Capabilities Manifest

A config bead (type `config`, label `toolchain-manifest`) stores available tools and versions, materialized by `gt config materialize`. Agents read this via `gt prime`.

### gt prime Integration

File: `gastown/internal/claude/config/` (settings templates)

`gt prime` output includes a `## Toolchain` section when a toolchain sidecar is present:

```
## Toolchain Sidecar

Status: active (toolchain-full)
Container: toolchain
Shared volume: /home/agent/gt

Available tools:
  go 1.24.1  | /usr/local/go/bin/go
  node 22.x  | /usr/bin/node
  python 3.12 | /usr/bin/python3
  kaniko     | /usr/local/bin/kaniko
  aws-cli 2.x | /usr/local/bin/aws
  docker-cli | /usr/local/bin/docker (client only, no daemon)

To run tools in the sidecar:
  gt toolchain exec -- go build ./...
  gt toolchain exec -- npm install

To build a custom sidecar image:
  1. Write Dockerfile to workspace
  2. gt toolchain build --tag ghcr.io/groblegark/my-sidecar:v1
  3. bd update $BEAD_ID --set sidecar_image=ghcr.io/groblegark/my-sidecar:v1
  4. Session will restart automatically with new sidecar
```

### CLAUDE.md Toolchain Section

The entrypoint generates a toolchain section in the project CLAUDE.md:

```markdown
# Toolchain

This agent has a toolchain sidecar (`toolchain-full` profile).
Tools are available via `gt toolchain exec -- <command>`.

## Self-Enhancement

You can build a custom sidecar if you need additional tools:
1. Create a Dockerfile that extends the current image
2. Build with kaniko: `gt toolchain build --tag <registry>/<image>:<tag>`
3. Update bead: `bd update $BEAD_ID --set sidecar_image=<image>`
4. Your session will resume automatically after pod restart (~30s)
```

### Environment Discovery

The toolchain sidecar sets env vars for tool discovery:

| Variable | Value | Purpose |
|----------|-------|---------|
| `GT_TOOLCHAIN_PROFILE` | `toolchain-full` | Current profile name |
| `GT_TOOLCHAIN_CONTAINER` | `toolchain` | Container name for exec |
| `GT_TOOLCHAIN_IMAGE` | `ghcr.io/...` | Current sidecar image |
| `GT_HAS_GO` | `1` | Go available |
| `GT_HAS_NODE` | `1` | Node.js available |
| `GT_HAS_KANIKO` | `1` | Can build images |

---

## Self-Enhancement Lifecycle

The full self-enhancement loop:

```
Agent needs a tool not in current sidecar
  |
  v
Agent writes Dockerfile extending toolchain base
  |
  v
gt toolchain build --tag ghcr.io/groblegark/custom:v1
  (kaniko builds + pushes, ~2-5 min)
  |
  v
bd update $BEAD_ID --set sidecar_image=ghcr.io/groblegark/custom:v1
  (bead mutation via daemon RPC)
  |
  v
BEAD_MUTATED event fires (NATS/SSE)
  |
  v
Controller reconciler detects sidecar_image change
  |
  v
Controller deletes pod (graceful: coop saves session to PVC)
  |
  v
Controller creates pod with new sidecar image
  (containerd pulls only delta layers from node cache)
  |
  v
Coop resumes session from PVC state
  (entrypoint.sh: coop --resume from latest session log)
  |
  v
Agent continues work with new tools available
```

### Session Persistence

- **Coop state**: Persisted to PVC at `/home/agent/gt/.state/coop/`
- **Claude state**: Persisted at `/home/agent/gt/.state/claude/`
- **Workspace**: Persisted on PVC at `/home/agent/gt/`
- **Resume mechanism**: `entrypoint.sh` starts coop with `--resume` flag, which picks up from the last session log

### Failure Modes

| Failure | Behavior |
|---------|----------|
| Image build fails | Agent sees kaniko error, no bead mutation, pod unchanged |
| Image push fails | Same -- build stays local, agent can retry |
| Bad sidecar image (crash loop) | K8s native sidecar restarts; agent container unaffected |
| Rate limit exceeded | Controller logs warning, skips sidecar update until window clears |
| Registry unreachable | Pull fails, pod stays in ImagePullBackOff; previous pod already deleted |

---

## Security

### Registry Allowlist

Controller config specifies allowed registry prefixes. Images not matching are rejected:

```yaml
agentController:
  sidecarSecurity:
    registryAllowlist:
      - "ghcr.io/groblegark/"
      - "public.ecr.aws/"
    maxCPU: "2"
    maxMemory: "4Gi"
    maxChangesPerHour: 3
```

### Resource Limits

- Per-sidecar limits enforced by the controller (capped to `sidecarSecurity.maxCPU/maxMemory`)
- Pod-level resource budgets (K8s v1.34+ `resources` at pod spec level) can cap total consumption
- Agent container resources are unchanged

### Image Pull Policy

| Scenario | Policy |
|----------|--------|
| Profile image (`:latest`) | `Always` (pick up new builds) |
| Profile image (`:v1.2.3`) | `IfNotPresent` |
| Custom image | `Always` (agents may push same tag) |

---

## Helm Values

File: `gastown/helm/gastown/values.yaml`

```yaml
agentController:
  # ... existing fields ...

  # Toolchain sidecar profiles
  sidecarProfiles:
    toolchain-full:
      image:
        repository: ghcr.io/groblegark/gastown-toolchain
        tag: "latest"
      resources:
        requests:
          cpu: "250m"
          memory: "512Mi"
        limits:
          cpu: "2"
          memory: "4Gi"
    toolchain-minimal:
      image:
        repository: ghcr.io/groblegark/gastown-toolchain
        tag: "minimal"
      resources:
        requests:
          cpu: "50m"
          memory: "128Mi"
        limits:
          cpu: "500m"
          memory: "512Mi"

  # Default sidecar profile (empty = no sidecar by default)
  defaultSidecarProfile: ""

  # Security constraints for custom sidecars
  sidecarSecurity:
    registryAllowlist:
      - "ghcr.io/groblegark/"
    maxCPU: "2"
    maxMemory: "4Gi"
    maxChangesPerHour: 3
```

---

## Phased Rollout

### Phase 1: Foundation
- Add `sidecar_profile`/`sidecar_image` metadata keys to bead schema + daemon RPC validation
- Extend `buildPod()` in podmanager with toolchain sidecar injection
- Define `SidecarProfile` type and `ProfileRegistry` in controller
- Add sidecar profile config to Helm values
- Build and publish `gastown-toolchain:latest` and `:minimal` images
- Tests: unit tests for profile resolution, pod spec generation, metadata validation

### Phase 2: Agent Prompting
- Create toolchain capabilities manifest (config bead schema)
- Extend `gt prime` output with `## Toolchain` section
- Add toolchain section to CLAUDE.md entrypoint generation
- Implement `gt toolchain exec` command (kubectl exec wrapper)
- Implement `gt toolchain status` command
- Environment variable injection for tool discovery

### Phase 3: Self-Enhancement
- `gt toolchain build` command (kaniko wrapper)
- Event-driven sidecar reconciliation via `BEAD_MUTATED` events
- Rate limiting for sidecar changes (3/hr per agent)
- Session persistence validation across sidecar updates
- Self-enhancement workflow integration tests

### Phase 4: Security & Polish
- Registry allowlist enforcement in controller
- Resource limit validation (per-sidecar caps)
- Image pull policy enforcement logic
- CI/CD pipeline for toolchain base image (GitHub Actions + Renovate for tool versions)
- Documentation and runbook for operators

---

## File Reference

| File | Repo | Purpose |
|------|------|---------|
| `controller/internal/podmanager/manager.go` | gastown | `AgentPodSpec`, `buildPod()`, sidecar injection |
| `controller/internal/podmanager/defaults.go` | gastown | Resource defaults, profile registry |
| `controller/internal/reconciler/reconciler.go` | gastown | Event-driven reconciliation |
| `controller/internal/config/config.go` | gastown | Controller config (sidecar profiles) |
| `helm/gastown/values.yaml` | gastown | Helm values (profiles, security) |
| `docker/toolchain/Dockerfile` | gastown | Toolchain base image |
| `internal/protocol/protocol.go` | beads | Bead metadata schema validation |
| `internal/rpc/server_issues_epics.go` | beads | Daemon RPC metadata validation |
| `gt/cmd/toolchain.go` | gastown | `gt toolchain` CLI commands |
| `internal/claude/config/` | gastown | Settings templates, gt prime output |
| `deploy/agent/entrypoint.sh` | gastown | Agent pod entrypoint, CLAUDE.md generation |
