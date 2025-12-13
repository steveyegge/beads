# Epoch: Strategic Work Hierarchy for Beads

## Overview

This document defines **Epoch** as a first-class entity in the beads issue tracking system, providing a strategic layer above Epics for organizing long-term development phases.

## Mainstream Context

### Epic (Established Agile Convention)

The mainstream agile community has settled on a fairly consistent hierarchy:

```
Theme / Strategic Objective (broadest)
    └── Initiative (6-12 months, cross-team)
        └── Epic (1-6 months, potentially cross-sprint)
            └── Feature (quarter or less)
                └── Story / User Story (fits in a sprint)
                    └── Task (hours/days)
```

An **Epic** is a large body of work that spans multiple sprints but can typically be delivered within a few months. SAFe distinguishes "Portfolio Epics" (cross-train, requiring Lean business cases) from "ART Epics" (single train), but the core concept is consistent: *an epic is big enough to need decomposition but bounded enough to have a clear completion state*.

### Epoch (Computing Etymology)

In mainstream computing, "epoch" means a **fixed reference point in time** from which measurements are taken—most famously Unix epoch (Jan 1, 1970). It represents:

- A zero point / origin
- A new era or major version boundary  
- A point of no backward compatibility (in package versioning like Debian's epoch number)

The word's etymology (Greek "epochē" = pause, cessation) carries the sense of a *fundamental demarcation point* that resets how you measure things.

## Beads Epoch Definition

### Hierarchy

```
Epoch (strategic era / vision phase)
    └── Epic (major initiative within that era)
        └── Feature / Task / Bug (bd issue types)
            └── Discovered-from chains (tactical emergence)
```

### What an Epoch Represents

An **Epoch** in beads represents:

1. **Strategic Phase** — A coherent period of development with unified goals
2. **Vision Container** — Defines what success looks like for a significant timeframe
3. **Epic Grouping** — Organizes related epics under a common strategic umbrella
4. **Architectural Boundary** — Often tied to major platform shifts or capability unlocks

### Characteristics

| Attribute | Epoch | Epic |
|-----------|-------|------|
| **Scope** | Strategic vision phase | Major deliverable |
| **Duration** | 6-18 months | 1-4 months |
| **Boundaries** | Defined by capability milestones | Defined by shippable outcomes |
| **Contains** | Multiple epics | Features, tasks, bugs |
| **Completion** | Phase transition | Deliverable shipped |

### Example: Strades Trading Platform

```
Epoch: "Foundation" (Q1-Q2 2025)
├── Epic: Beads Harness Pattern
├── Epic: Credential Management Infrastructure
├── Epic: VSCode IDE Control Framework
└── Epic: Event Logging & Observability

Epoch: "Signal Processing" (Q3-Q4 2025)
├── Epic: Schwab RTD Integration
├── Epic: LuxAlgo TradingView Bridge
├── Epic: Alert Aggregation Pipeline
└── Epic: Historical Backtesting Infrastructure

Epoch: "Trading Intelligence" (2026 H1)
├── Epic: SPX Final-Hour Pattern Recognition
├── Epic: Butterfly Spread Detector
├── Epic: Position Sizing & Risk Calculator
└── Epic: Real-time Trade Visualization

Epoch: "Voice & Natural Language" (2026 H2)
├── Epic: Natural Language Trade Expression
├── Epic: Voice Command Integration
├── Epic: Keyboard-First UI Refinement
└── Epic: Context-Aware Command Routing
```

## Semantic Boundaries

| Level | Scope | Duration | Example |
|-------|-------|----------|---------|
| **Epoch** | Strategic vision phase | 6-18 months | "Trading Intelligence" |
| **Epic** | Major deliverable | 1-4 months | "SPX Final-Hour Pattern Recognition" |
| **Feature** | Shippable increment | 1-4 weeks | "Butterfly spread detector" |
| **Task** | Discrete work item | Hours-days | "Implement Greeks calculation" |
| **Bug** | Defect correction | Hours-days | "Fix RTD reconnection timeout" |

## Relationship to Existing Beads Concepts

### Dependency Tracking

Epochs integrate with beads' existing dependency model:

- Epics use `belongs-to-epoch:<epoch-id>` dependency type
- Epoch completion blocked until all child epics closed
- Cross-epoch dependencies explicitly tracked

### Labels

Epochs can leverage the label system for additional categorization:

- `epoch:foundation`, `epoch:signal-processing`
- Phase markers: `phase:active`, `phase:planned`, `phase:completed`

### Agent Workflows

Epochs provide strategic context for agent sessions:

- Agents can filter work by active epoch
- Session context includes current epoch goals
- Discovered work inherits epoch context from parent epic

## Design Principles

1. **Epochs are Declarative** — They define "what we're building toward" not "how we build it"
2. **Epochs Have Clear Boundaries** — Entry/exit criteria define phase transitions
3. **Epochs Enable Prioritization** — Work outside active epoch gets lower priority
4. **Epochs Support Retrospection** — Completed epochs become historical record of evolution

## References

- [Atlassian Agile Epics](https://www.atlassian.com/agile/project-management/epics)
- [SAFe Epic Definition](https://scaledagileframework.com/epic/)
- [Unix Epoch](https://en.wikipedia.org/wiki/Epoch_(computing))
- [Beads Harness Pattern](../BEADS_HARNESS_PATTERN.md)
