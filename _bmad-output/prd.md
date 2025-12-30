---
stepsCompleted: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11]
lastStep: 11
status: complete
inputDocuments:
  - "analysis/research/ChatGPT_beads_docs_issue_376_llms.md"
  - "analysis/research/Claud_compass_artifact_wf-59cced25-da11-4149-8da0-9f78d975e040_text_markdown.md"
  - "analysis/research/Gemini_Документація beads_ ШІ та людина.md"
documentCounts:
  briefs: 0
  research: 3
  brainstorming: 0
  projectDocs: 0
workflowType: 'prd'
lastStep: 0
project_name: 'Beads Documentation Strategy'
user_name: 'Ubuntu'
date: '2025-12-30'
---

# Product Requirements Document - Beads Documentation Strategy

**Author:** Ubuntu
**Date:** 2025-12-30

## Executive Summary

Beads Documentation Strategy вирішує критичну проблему сучасних DevTools:
документація, що працює для людей, часто непридатна для AI-агентів, і навпаки.

Issue #376 ("I want to love Beads but the AI generated docs make it impossible")
виявила системні проблеми: маркетингова мова замість технічної точності,
відсутня архітектурна прозорість (Git/JSON/SQLite), немає процедур відновлення.

Цей PRD визначає стратегію створення документації з єдиним джерелом істини
(Markdown у Git), що генерує два оптимізовані представлення:
- **Для людей**: Docusaurus сайт зі структурою Diátaxis (Tutorials, How-to, Reference, Explanation)
- **Для AI-агентів**: llms.txt стандарт з llms-full.txt для повного контексту

### What Makes This Special

1. **Human-curated authority**: AI може генерувати чернетки, але фінальна версія — результат людської курації
2. **Dual optimization**: Один Markdown → два представлення без дублювання контенту
3. **Recovery-first design**: Кожна небезпечна операція має явний Recovery розділ
4. **Anti-marketing stance**: Жодних "переможних заяв" у Reference/How-to — тільки операційна правда

## Project Classification

**Technical Type:** developer_tool (documentation system)
**Domain:** scientific (documentation methodology)
**Complexity:** medium
**Project Context:** Greenfield - новий проект

Проект базується на синтезі досліджень від ChatGPT, Claude та Gemini,
що забезпечує мульти-перспективний аналіз проблеми документації.

## Success Criteria

### User Success

- **Розробник знаходить відповідь** на питання за <3 хвилини (vs "неможливо зрозуміти")
- **AI-агент отримує повний контекст** з одного запиту llms-full.txt (<50K токенів)
- **При збої — чіткий шлях відновлення** в <5 кроків з конкретними командами
- **Копіпастні приклади** працюють на чистому середовищі без модифікацій
- **Readability**: Flesch-Kincaid Grade Level ≤ 8 для How-to документів
- **Navigation clarity**: ≤3 кліки до будь-якої критичної інформації
- **Emotional success**: Користувач відчуває впевненість після прочитання Recovery docs
- **Cognitive load**: Жодна сторінка не перевищує 2000 слів

### Business Success

- **PR #784 merged** — перший milestone, maintainer validation achieved ✓
- **Issue #376 addressed** via PR #784 + follow-up improvements
- **Baseline measurement**: Зафіксувати поточну кількість "documentation confusion" issues
- **Зменшення support burden**: <50% питань "як це працює?" порівняно з baseline
- **Community contributions**: Мінімум 3 PR на покращення документації протягом 3 місяців
- **Docs NPS**: Net Promoter Score для документації >30

### Technical Success

- **llms.txt валідний**: Проходить lint-перевірку, розмір <10KB, всі посилання робочі
- **llms-full.txt актуальний**: Автогенерується в CI/CD при кожному коміті в docs/
- **Single Source of Truth**: Markdown → llms.txt генерація повністю автоматична
- **Build reproducibility**: Той самий commit = той самий llms.txt output
- **Deployment to steveyegge.github.io** — не fork
- **Path-based workflow trigger** на main branch
- **Clean separation**: research/planning artifacts окремо від production docs
- **Zero broken links**: Link checker у CI pipeline
- **Code example coverage**: 100% CLI команд мають working examples

### Measurable Outcomes

| Метрика | Поточний стан | Ціль |
|---------|---------------|------|
| PR #784 status | Open (requested changes) | Merged |
| Час пошуку відповіді | "Неможливо" | <3 хв |
| Recovery documentation | Відсутня | 100% failure modes покриті |
| Architecture clarity | 0 документів | 1 авторитетний документ |
| llms.txt compliance | Часткова | Повна відповідність стандарту |
| Readability score | Невідомо | Flesch-Kincaid ≤ 8 |
| Docs NPS | Невідомо | >30 |

## Product Scope

### MVP - Minimum Viable Product

1. **Завершити merge PR #784** — виправити deployment config (steveyegge.github.io)
2. **Architecture.md** — людино-написаний документ про Git/JSON/SQLite взаємодію
3. **Recovery Runbook** — процедури для DB corruption, merge conflicts, circular deps
4. **Clean separation** — перемістити docs/research/ з production docs
5. **Baseline measurement** — зафіксувати поточні метрики

### Growth Features (Post-MVP)

- Повна Diátaxis реструктуризація (Tutorials, How-to, Reference, Explanation)
- Покращена навігація (breadcrumbs, sidebar, ≤3 кліки)
- Версіонування документації
- docusaurus-plugin-llms-txt інтеграція
- Docs NPS опитування

### Vision (Future)

- Інтерактивні code examples з валідацією
- Community-driven documentation improvements
- Автоматичні smoke-тести прикладів CLI
- Multi-language support (i18n)
- Readability scoring в CI pipeline

## User Journeys

### Journey 1: Alex — Frustrated Developer Seeks Clarity

Alex — senior backend developer, який інтегрує Beads у workflow своєї команди. Після 3 днів спроб він наражається на `database locked` помилку і не може знайти рішення в документації. Issue #376 резонує з ним.

Він знаходить оновлену документацію з чітким Recovery Runbook. За 5 хвилин він виконує `bd sync --force-rebuild` і проблема вирішена.

**Revealed Requirements:** Recovery Runbook, Architecture.md, Search functionality

### Journey 2: Claude Agent — Learning & Operating with Beads

**Phase A: Discovery (llms.txt)** — Agent читає `/llms.txt` для концептуального розуміння: "Beads = distributed graph issue tracker, Git + JSONL + SQLite."

**Phase B: Active Use (bd prime)** — Agent відкриває repo з `.beads/`. Hook викликає `bd prime` → операційний контекст, команди, SESSION CLOSE PROTOCOL.

**Phase C: Troubleshooting** — llms.txt Recovery секція + bd prime команди.

**Revealed Requirements:** llms.txt (conceptual), bd prime (operational), Hook integration

### Journey 3: Maria — First-Time Contributor

Maria бачить Issue #376, знаходить `CONTRIBUTING.md`, і за 2 години створює PR з покращенням Recovery секції.

**Revealed Requirements:** CONTRIBUTING.md, Clear guidelines

### Journey 4: Steve Yegge — Maintainer Review

Steve відкриває PR #784, бачить чітку структуру, CI checks passed, залишає конструктивний feedback за 10 хвилин.

**Revealed Requirements:** CI/CD preview, Automated checks

### Journey Requirements Summary

| Journey | User Type | Artifact | Capability Areas |
|---------|-----------|----------|------------------|
| Alex | Human Developer | Docusaurus | Recovery, Architecture, Search |
| Claude (learning) | LLM | llms.txt | Concepts, Agent Guidance |
| Claude (operating) | LLM | bd prime | Commands, Workflows, Session |
| Maria | Contributor | CONTRIBUTING.md | Guidelines, Templates |
| Steve | Maintainer | CI/CD | Preview, Checks |

**Artifact Relationship:** llms.txt (Learning) + bd prime (Operating) → Docusaurus (Human navigation)

## Developer Tool Specific Requirements

### Project-Type Overview

Beads Documentation Strategy — це documentation system для developer tool (Beads CLI). Проект фокусується на створенні dual-audience документації: Docusaurus сайт для людей та llms.txt для AI-агентів.

**Ключова характеристика:** Документація як продукт — не допоміжний артефакт, а core deliverable з власними quality standards.

### Language & Platform Matrix

| Aspect | Specification |
|--------|---------------|
| Primary Language | English (documentation) |
| Tool Language | Go (≥1.21 required) |
| Target Platforms | Linux, macOS, Windows |
| i18n | Post-MVP (Vision) |

### Installation Methods

| Method | Priority | Prerequisites | Documentation Status |
|--------|----------|---------------|---------------------|
| `go install` | Primary | Go ≥1.21, GOPATH configured | Required |
| GitHub Binary Releases | Secondary | None | Required |
| Homebrew | Future | macOS/Linux | Post-MVP |

### API Surface & CLI Commands

**Core CLI Commands to Document:**

| Command | Purpose | Prerequisites |
|---------|---------|---------------|
| `bd init` | Initialize beads in repository | Git repo exists |
| `bd sync` | Synchronize with remote | .beads/ exists |
| `bd prime` | Generate AI context | .beads/ exists |
| `bd status` | Show current state | .beads/ exists |
| `bd list` | List issues/beads | .beads/ exists |
| `bd ready` | Show available work | .beads/ exists |
| `bd update` | Update issue status | Issue ID exists |
| `bd close` | Close completed issue | Issue ID exists |
| `bd dep` | Manage dependencies | Issues exist |
| `bd blocked` | Show blocked items | Dependencies set |

**Documentation Requirements:**
- 100% CLI команд мають working examples з explicit prerequisites
- Copy-paste ready commands з очікуваним станом середовища
- Expected output показаний для кожної команди
- Failure scenarios та recovery steps для кожної команди

### Code Examples Strategy

| Category | MVP | Growth |
|----------|-----|--------|
| CLI Commands | 100% coverage + manual verification | Automated smoke tests in CI |
| Recovery Procedures | Step-by-step з prerequisites | CI validation |
| Integration Examples | bd prime hook, Git workflows | Cross-IDE testing |
| Error Scenarios | Common errors + resolution | Comprehensive error catalog |

**Example Format Standard:**
```bash
# Prerequisites: Git repo with .beads/ initialized
# Expected state: Clean working directory

$ bd sync --force-rebuild

# Expected output:
Rebuilding local cache...
Synced 42 beads from remote.

# If .beads/ doesn't exist:
# Error: "No .beads directory found. Run 'bd init' first."
```

### IDE Integration

| IDE | Integration Method | Failure Behavior | Documentation |
|-----|-------------------|------------------|---------------|
| VSCode | Claude Code extension + bd prime hook | Silent fallback, log warning | Primary |
| JetBrains | Claude Code plugin | Graceful degradation | Secondary |
| Vim/Neovim | Claude Code CLI | Manual bd prime | Supported |

**Hook Integration Pattern:**
```
Repo open → .beads/ detected → bd prime triggered → AI context injected
                ↓ (failure)
         Log warning → Continue without context → User notified
```

### Token Budget Monitoring

| Artifact | Target Size | Monitoring |
|----------|-------------|------------|
| llms.txt | <10KB | CI lint check |
| llms-full.txt | <50K tokens | CI size validation |

**If llms-full.txt exceeds budget:**
1. Prioritize by Diátaxis category (Reference > How-to > Tutorial > Explanation)
2. Split into llms-full-core.txt + llms-full-extended.txt
3. Document in llms.txt which sections are in which file

### Implementation Considerations

**Build & Deploy:**
- Docusaurus static site generation
- GitHub Actions CI/CD
- steveyegge.github.io deployment (not fork)
- Path-based workflow trigger on main branch

**Quality Gates:**

| Gate | Phase | Validation |
|------|-------|------------|
| Zero broken links | MVP | CI link checker |
| llms.txt lint | MVP | CI validation |
| llms-full.txt size | MVP | CI token count |
| Example prerequisites documented | MVP | Manual review |
| Example smoke tests | Growth | Automated CI |
| Readability score | Growth | Flesch-Kincaid (Reference exempt) |

## Project Scoping & Phased Development

### MVP Strategy & Philosophy

**MVP Approach:** Problem-Solving MVP
**Goal:** Вирішити core problem Issue #376 з мінімальними features
**Resource Requirements:** Solo contributor, 1-2 тижні focused work

### MVP Feature Set (Phase 1)

**Core User Journeys Supported:**
- Alex (frustrated developer) — повна підтримка через Recovery Runbook
- Steve Yegge (maintainer) — PR review через CI/CD

**Must-Have Capabilities:**

| # | Feature | Status | Effort | Success Metric |
|---|---------|--------|--------|----------------|
| 1 | Baseline measurement | Not started | 15 min | GitHub issues count recorded |
| 2 | PR #784 merged | In progress | Variable | Deployment on steveyegge.github.io |
| 3 | Architecture.md | Not started | 2-4 hrs | Git/JSON/SQLite взаємодія задокументована |
| 4 | Recovery Runbook | Not started | 2-4 hrs | DB corruption, merge conflicts покриті |

**Already Completed:**
- ✅ Clean separation — docs/research/ переміщено з production docs

### Post-MVP Features

**Phase 2 (Growth):**
- Diátaxis реструктуризація (Tutorials, How-to, Reference, Explanation)
- Покращена навігація (breadcrumbs, sidebar, ≤3 кліки)
- docusaurus-plugin-llms-txt інтеграція
- Example smoke tests в CI
- Docs NPS опитування

**Phase 3 (Vision):**
- Версіонування документації
- Інтерактивні code examples з валідацією
- Community-driven documentation improvements
- Multi-language support (i18n)
- Readability scoring в CI pipeline

### Risk Mitigation Strategy

| Risk Type | Risk | Likelihood | Mitigation |
|-----------|------|------------|------------|
| Technical | llms-full.txt >50K tokens | Medium | Split into core + extended files |
| Market | PR #784 rejected | Low | Address all maintainer feedback |
| Resource | Contributor burnout | Medium | Focus on 4-item MVP only |
| Quality | Examples don't work | Medium | Manual verification checklist |

### Scope Change Protocol

Будь-які зміни до MVP scope потребують:
1. Impact analysis на timeline
2. Explicit trade-off (що видаляємо, якщо додаємо)
3. User approval

## Functional Requirements

### Documentation Content

- **FR1:** Розробник може прочитати Architecture.md для розуміння Git/JSON/SQLite взаємодії
- **FR2:** Розробник може знайти Recovery Runbook для вирішення database corruption
- **FR3:** Розробник може знайти Recovery Runbook для вирішення merge conflicts
- **FR4:** Розробник може знайти Recovery Runbook для вирішення circular dependencies
- **FR5:** Розробник може переглянути CLI command reference з усіма bd командами
- **FR6:** Розробник може скопіювати working example для кожної CLI команди
- **FR7:** Контриб'ютор може прочитати CONTRIBUTING.md для розуміння contribution process

### AI Agent Documentation

- **FR8:** AI-агент може отримати llms.txt для концептуального розуміння Beads
- **FR9:** AI-агент може отримати llms-full.txt для повного контексту (<50K токенів)
- **FR10:** AI-агент може отримати bd prime output для операційного контексту
- **FR11:** AI-агент може знайти Recovery секцію в llms.txt для troubleshooting
- **FR12:** AI-агент може визначити SESSION CLOSE PROTOCOL з bd prime

### Documentation Delivery

- **FR13:** Користувач може переглядати документацію на steveyegge.github.io
- **FR14:** Користувач може навігувати документацією за ≤3 кліки до критичної інформації
- **FR15:** Користувач може шукати контент у документації
- **FR16:** Система автоматично генерує llms-full.txt при коміті в docs/

### Quality Assurance

- **FR17:** CI перевіряє всі посилання на валідність (zero broken links)
- **FR18:** CI валідує llms.txt формат (lint check)
- **FR19:** CI перевіряє розмір llms-full.txt (<50K токенів)
- **FR20:** Maintainer може переглянути PR preview перед merge

### Developer Experience

- **FR21:** Розробник може встановити Beads через `go install`
- **FR22:** Розробник може завантажити binary release з GitHub
- **FR23:** IDE користувач може отримати AI context викликавши bd prime через hook/integration
- **FR24:** Розробник бачить prerequisites для кожного code example
- **FR25:** Розробник бачить expected output для кожного code example
- **FR26:** Розробник бачить failure scenarios та recovery для кожної команди

### Metrics & Measurement

- **FR27:** Система зберігає baseline measurement GitHub issues з 'documentation' label
- **FR28:** Maintainer може порівняти поточні issues з baseline

### Error Handling & Resilience

- **FR29:** Система показує зрозуміле error message коли .beads/ не існує
- **FR30:** IDE gracefully degrades коли bd prime fails (log warning, continue without context)
- **FR31:** Кожна документація сторінка не перевищує 2000 слів

## Non-Functional Requirements

### Performance

| NFR | Метрика | Критерій | Measurement |
|-----|---------|----------|-------------|
| NFR1 | Час пошуку відповіді | <3 хвилини для типового питання | User testing post-launch |
| NFR2 | llms-full.txt розмір | <50K токенів | CI automated check |
| NFR3 | llms.txt розмір | <10KB | CI automated check |
| NFR4 | Навігація до критичної інформації | ≤3 кліки | Manual navigation audit |

### Accessibility & Readability

| NFR | Метрика | Критерій | Scope |
|-----|---------|----------|-------|
| NFR5 | Readability score | Flesch-Kincaid ≤8 | How-to docs only |
| NFR6 | Cognitive load | ≤2000 слів на сторінку | How-to, Tutorials, Recovery Runbook |
| NFR7 | Exemptions | Може перевищувати word limit та readability score | Reference, Architecture, Explanation |

### Integration & Deployment

| NFR | Метрика | Критерій | Measurement |
|-----|---------|----------|-------------|
| NFR8 | End-to-end deployment | Commit → live site <10 хвилин | CI timing logs |
| NFR9 | llms-full.txt generation | Автоматична при коміті в docs/ | CI workflow |

### Reliability

| NFR | Метрика | Критерій | Measurement |
|-----|---------|----------|-------------|
| NFR10 | Link validity | 100% посилань робочі | CI link checker |
| NFR11 | Example validity | 100% code examples працюють | Manual (MVP), Automated (Growth) |
| NFR12 | Build reproducibility | Той самий commit = той самий output | CI deterministic build |

### Maintainability

| NFR | Метрика | Критерій | Measurement |
|-----|---------|----------|-------------|
| NFR13 | Single Source of Truth | Один Markdown → всі output формати | Architecture constraint |
| NFR14 | CI failure notification | Maintainer alert при failed build | GitHub Actions |

### Usability

| NFR | Метрика | Критерій | Measurement |
|-----|---------|----------|-------------|
| NFR15 | Recovery procedure length | ≤5 кроків для кожного failure scenario | Manual review |

### Deferred Metrics (Growth Phase)

- **Docs NPS >30** — Net Promoter Score опитування після достатньої user base
- **Emotional success survey** — Post-task confidence measurement

