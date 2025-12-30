# Workflow Guide: Beads Documentation Project

_Створено: 2025-12-30_
_Оновлено: 2025-12-30 (BMAD-Beads інтеграція)_

---

## Поточний статус

```
✅ Phase 1-2: Discovery & Solutioning (COMPLETE)
✅ Phase 3: Epic/Story Creation (COMPLETE)
⏳ Phase 4: Implementation (IN PROGRESS)
   ├── ✅ Story 1.1: Fix Deployment URLs (bd-fyy.1) - DONE
   ├── ✅ Story 1.2: Environment-Based URL Config (bd-fyy.2) - DONE (review)
   └── 🎯 Story 1.3: Update Sidebar Navigation (bd-fyy.3) - NEXT
```

---

## BMAD + Beads: Два джерела правди

### Проблема яку вирішили

```
BMAD workflows використовують:     Beads використовує:
├── bmad:stage:backlog            ├── status: open
├── bmad:stage:ready-for-dev      ├── status: in_progress
├── bmad:stage:in-progress        └── status: closed
├── bmad:stage:review
└── bmad:stage:done

+ sprint-status.yaml (derived view)
```

**Проблема:** Labels накопичувались замість заміни, sprint-status.yaml розсинхронізовувався.

### Рішення: bd-stage helper

```bash
# Одна команда для ексклюзивної зміни stage
_bmad/bin/bd-stage <issue-id> <stage>

# Приклад:
_bmad/bin/bd-stage bd-fyy.3 in-progress
```

**Як працює:** Використовує `bd update --add-label` + `--remove-label` в одній атомарній команді.

**Інтегровано в workflows:** dev-story, code-review, create-story, sprint-planning.

### Архітектура синхронізації

```
┌─────────────────────────────────────────────────────────────┐
│  BEADS = Source of Truth                                    │
│  ├── status: open/in_progress/closed (bd native)            │
│  └── labels: bmad:stage:* (workflow tracking)               │
│                                                             │
│  sprint-status.yaml = Derived View                          │
│  └── Оновлюється через beads-sync agent                     │
│                                                             │
│  epics.md = Reference (read-only)                           │
└─────────────────────────────────────────────────────────────┘
```

---

## Stage Lifecycle

```
backlog → ready-for-dev → in-progress → review → done
   │           │              │           │        │
   │      bd-stage        bd-stage    bd-stage  bd close
   │                          │
   │                    bd update --status in_progress
   │
Автоматично при create-story
```

### Команди для кожного переходу

| Перехід | Команда |
|---------|---------|
| → ready-for-dev | `_bmad/bin/bd-stage <id> ready-for-dev` |
| → in-progress | `_bmad/bin/bd-stage <id> in-progress` + `bd update <id> --status in_progress` |
| → review | `_bmad/bin/bd-stage <id> review` |
| → done | `bd close <id> "reason"` + `_bmad/bin/bd-stage <id> done` |

---

## Повний Development Workflow

### 1. Знайти роботу

```bash
bd ready                           # Показує issues без блокерів
bd show <id>                       # Деталі конкретного issue
```

### 2. Взяти в роботу

```bash
_bmad/bin/bd-stage <id> in-progress
bd update <id> --status in_progress
```

### 3. Виконати (через BMAD workflow)

```bash
/bmad:bmm:workflows:dev-story      # Автоматично знаходить ready story
```

### 4. Code Review

```bash
_bmad/bin/bd-stage <id> review
/bmad:bmm:workflows:code-review
```

### 5. Завершити

```bash
bd close <id> "Story completed - description"
_bmad/bin/bd-stage <id> done
```

### 6. Синхронізація

```bash
bd sync                            # Sync beads to git
# Або через agent:
/bmad:bmad-beads:agents:beads-sync → option 2 (Full sync)
```

---

## Sprint Status Sync

### Автоматичний sync (рекомендовано)

```bash
# beads-sync agent оновлює sprint-status.yaml з beads
/bmad:bmad-beads:agents:beads-sync
# Вибрати: 2 (Full bidirectional sync)
```

### Ручний sync (якщо потрібно)

```bash
# Перевірити розбіжності
bd list --json | grep -E "(status|labels)"
cat _bmad-output/sprint-status.yaml | grep status

# Оновити sprint-status.yaml вручну якщо потрібно
```

---

## Поточні Epics та Stories

### Epic 1: Foundation & Deployment [bd-fyy] 🟡 IN-PROGRESS

| Story | ID | Status | Assignee |
|-------|-----|--------|----------|
| 1.1 Fix Deployment URLs | bd-fyy.1 | ✅ done | Dev |
| 1.2 Environment-Based URL Config | bd-fyy.2 | ✅ review | Dev |
| **1.3 Update Sidebar Navigation** | **bd-fyy.3** | **🎯 ready-for-dev** | **Dev** |

### Epic 2: Recovery Documentation [bd-9g9] ⏸️ BLOCKED by Epic 1

| Story | ID | Status | Assignee |
|-------|-----|--------|----------|
| 2.1 Recovery Overview Page | bd-9g9.1 | backlog | TechWriter |
| 2.2 Database Corruption Recovery | bd-9g9.2 | backlog | TechWriter |
| 2.3 Merge Conflicts Recovery | bd-9g9.3 | backlog | TechWriter |
| 2.4 Circular Dependencies Recovery | bd-9g9.4 | backlog | TechWriter |
| 2.5 Sync Failures Recovery | bd-9g9.5 | backlog | TechWriter |

### Epic 3-5: Blocked by previous epics

```
Epic 3 (Architecture) ← blocked by Epic 2
Epic 4 (AI Docs) ← blocked by Epic 3
Epic 5 (QA Pipeline) ← blocked by Epic 4
```

---

## Інструменти BMAD-Beads

### Створені helpers

| Файл | Призначення |
|------|-------------|
| `_bmad/bin/bd-stage` | Ексклюзивна зміна bmad:stage:* labels |
| `_bmad/bin/bd-stage-sync` | Stage + bd sync + sprint-status update |

### BMAD Agents

| Agent | Коли використовувати |
|-------|---------------------|
| `beads-sync` | Синхронізація MD ↔ Beads |
| `dev` | Імплементація stories |
| `sm` | Sprint planning, epic creation |
| `tech-writer` | Документація |

### BMAD Workflows

| Workflow | Призначення |
|----------|-------------|
| `dev-story` | Імплементація story (red-green-refactor) |
| `code-review` | Adversarial code review |
| `create-story` | Створення нової story з epics |
| `sprint-status` | Перегляд sprint progress |

---

## Lessons Learned

### 1. Label Management

❌ **Неправильно:**
```bash
bd label add bd-xxx "bmad:stage:in-progress"
# Labels накопичуються!
```

✅ **Правильно:**
```bash
_bmad/bin/bd-stage bd-xxx in-progress
# Видаляє всі інші stages, додає новий
```

### 2. bd update порядок аргументів

❌ **Неправильно:**
```bash
bd update bd-xxx --remove-label "bmad:stage:backlog" --add-label "bmad:stage:done"
# remove виконується ПІСЛЯ add!
```

✅ **Правильно:**
```bash
bd update bd-xxx --add-label "bmad:stage:done" --remove-label "bmad:stage:backlog"
# add ПЕРЕД remove
```

### 3. Wildcard в labels не працює

```bash
bd update bd-xxx --remove-label "bmad:stage:*"  # НЕ працює
# Треба перелічувати всі stages явно
```

---

## Quick Reference

### Session Start

```bash
bd prime                           # Контекст для AI
bd ready                           # Готова робота
bd stats                           # Статистика проекту
```

### Session End (SESSION CLOSE PROTOCOL)

```bash
git status                         # Що змінилось
git add <files>                    # Stage code
bd sync                            # Commit beads
git commit -m "..."                # Commit code
bd sync                            # New beads changes
git push                           # Push to remote
```

---

## Файли проекту

| Файл | Призначення |
|------|-------------|
| `_bmad-output/prd.md` | Product Requirements |
| `_bmad-output/architecture.md` | Architecture Decisions |
| `_bmad-output/epics.md` | Epic & Story definitions |
| `_bmad-output/sprint-status.yaml` | Sprint tracking (derived) |
| `_bmad-output/project-context.md` | AI Agent Rules |
| `_bmad-output/workflow-guide.md` | Цей файл |
| `_bmad/bin/bd-stage` | Stage label helper |

---

## Контекст середовища

- **Локація:** `/data/projects/beads-llm-human`
- **Branch:** `beads-llm-human` (working) → PR to `docs/docusaurus-site`
- **BMAD:** ✅ v6.0.0-alpha.19
- **Beads:** ✅ v0.41.0
- **Upstream:** `joyshmitz/beads` (fork of `steveyegge/beads`)

**Gitignored:**
- `_bmad/`, `_bmad-output/`, `node_modules/`
