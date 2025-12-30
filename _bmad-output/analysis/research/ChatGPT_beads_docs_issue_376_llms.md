# Beads docs + issue #376 + `llms.txt`: аудит і стратегія співіснування (люди vs ШІ)

## Вхідні дані / Scope
1) Проаналізувати гілку `docs/docusaurus-site` у репозиторії `joyshmitz/beads` та сайт документації `https://joyshmitz.github.io/beads/` для оцінки технічної суті та поточного стану контенту.  
2) Детально вивчити дискусію в issue #376 репозиторію `steveyegge/beads` для виявлення anti‑patterns та причин невдоволення спільноти.  
3) Дослідити специфікацію та кращі практики `llms.txt` для створення контексту, оптимізованого для ШІ.  
4) Окремо — принципи читабельності та UX для людей на тому ж сайті docs (як окрема задача від ШІ).  
5) Пошук методологій технічного письма (Diátaxis, Google Technical Writing) та шаблонів успішних Open Source проектів (Good Docs Project, Write the Docs).  
6) Синтезувати рекомендації: структура для людей + специфікація для `llms.txt` + стратегія співіснування.

> **Мета документа:** перетворити “розрізнені спостереження” на практичний план документації: що змінити в IA/контенті, як додати `llms.txt/llms-full.txt`, і як уникнути повторення конфлікту “AI‑docs vs реальність продукту”.

---

## (1) Поточний стан `joyshmitz/beads`: Docusaurus та контент

### Факти (зріз реальності)
- У гілці `docs/docusaurus-site` присутній Docusaurus‑проєкт (папка `website/`) — типовий стек для GitHub Pages.
- На сайті документації вже є `/llms.txt` та згадується “optional” `/llms-full.txt`.
- Перелік сторінок у `llms.txt` відображає командно‑орієнтовану документацію: `create/view/search/update/close/delete`, плюс `project files`, `output format`, `tags/status/setup`.

### Оцінка технічної суті контенту
Сильні сторони:
- Є хороший фундамент під **Reference** (довідник команд/параметрів) і базову **How-to** навігацію.

Слабкі сторони (типові ризики):
- “Команди ≠ модель системи”: якщо нема **Architecture/Explanation**, користувачі вміють запускати команди, але не розуміють інваріанти, стани, режими відмов та відновлення.
- Для інструмента, що торкається Git/worktrees/локальної БД, критично потрібні **failure modes + recovery**.

**Висновок по стану:** виглядає як хороша DX‑обгортка, але потрібні 1–2 сторінки **Explanation/Architecture** і системний **Recovery Playbook**, інакше docs залишаються “меню без карти”.

---

## (2) Issue #376 `steveyegge/beads`: anti‑patterns та причини невдоволення

### Ключовий сигнал issue
Назва: **“I want to love Beads but the AI generated docs make it impossible”**.

### Конкретні болі користувача (симптоми)
- Документація виглядає як **AI‑generated** та містить **надто впевнені/маркетингові твердження**, які заважають говорити про реальні баги.
- Описані реальні проблеми:
  - merge conflicts
  - locked database
  - lost issues
  - проблеми з worktrees
  - створення кількох БД / розсинхрон worktrees
- Запит: **human‑edited архітектурний огляд** того, як працюють разом Git + JSON + SQLite, і як відновлюватися зі зламаних станів.

### Anti‑patterns (узагальнення з прив’язкою до issue)
1) **Docs як маркетинг замість операційної правди**
   - Коли тон документації “надто переможний”, а інструмент крихкий — довіра руйнується.

2) **Відсутність системної моделі**
   - Без сторінки “Data model + invariants + lifecycle” користувачі і контриб’ютори не розуміють, де саме корінь проблем.

3) **Немає Recovery Playbook**
   - Для інструмента з локальною БД та worktrees “як полагодити стан” — must‑have.

**Висновок:** невдоволення — не “проти ШІ”, а проти **невідповідності** docs до реальної поведінки системи + відсутності зрозумілої архітектури і recovery.

---

## (3) `llms.txt`: специфікація та кращі практики

### Що таке `llms.txt`
`/llms.txt` — LLM‑friendly (часто і human‑friendly) індекс сайту, який:
- коротко пояснює, що це за продукт/документація
- дає canonical‑посилання на ключові сторінки
- (опційно) дає посилання на `/llms-full.txt` як “повний плейнтекст”

Практика з екосистеми `AnswerDotAI/llms-txt`:
- робити `.md`‑версії сторінок за тим самим URL (де доречно), щоб зменшити шум HTML/JS.

### Практики, які реально працюють
- **`/llms.txt` як індекс**, не як “друга документація”.
- **`/llms-full.txt` як артефакт** (згенерований дамп чистого Markdown/тексту).
- **Версіонування**: вказувати версію docs/релізу або дату оновлення.
- **Agent guidance**: короткі правила використання (dos/don’ts), особливо для операцій, що можуть зламати репо.

---

## (4) Людський UX документації як окрема задача

### Мета human‑docs
Знизити когнітивне навантаження і скоротити шлях “питання → відповідь”.

### Інформаційна архітектура (IA)
- **Start/Overview**: що це, для кого, коли не підходить (чесність = UX).
- **Tutorial**: 1 happy‑path сценарій, який гарантовано завершується успіхом.
- **How‑to**: задачі/рецепти (worktrees, merge, recovery).
- **Reference**: повний довідник команд, прапорців, форматів.
- **Explanation**: архітектура, інваріанти, failure modes, trade‑offs.

### Шаблон сторінки для читання
- Prereqs → Steps → Expected output → Troubleshooting → Next
- Для небезпечних зон (worktrees/DB/merge) — **обов’язковий Recovery блок**.

---

## (5) Методології технічного письма та OSS‑шаблони

### Diátaxis
Розділяє документацію на 4 типи:
- Tutorials
- How‑to guides
- Reference
- Explanation

Це лікує головну хворобу docs: “довідник намагається бути туторіалом і провалюється”.

### Google Technical Writing
Покриває ясність, структуру, терміни, приклади, речення, інформаційну щільність.

### The Good Docs Project / Write the Docs
- Шаблони для різних типів сторінок (concept/how‑to/reference)
- Спільнотні best practices для developer documentation

---

## (6) Синтез: структура для людей + `llms.txt` + стратегія співіснування

### A. Структура сайту для людей (Diátaxis‑скелет)
1) **Tutorials**
   - Install + First bead (5–10 хв, happy‑path)
2) **How‑to**
   - Worktrees без болю
   - Як відновитися після DB lock
   - Як розрулити merge conflicts
3) **Reference**
   - `bd create/view/search/update/close/delete`
   - Project files, output format, tags/status/setup
4) **Explanation (архітектура!)**
   - Data model: Git ↔ JSON ↔ SQLite
   - Invariants + lifecycle/state machine
   - Failure modes & recovery

> Це напряму закриває запит з issue #376: не “ще більше тексту”, а “карта місцевості + як вижити”.

### B. `llms.txt` як AI‑індекс
- `/llms.txt` = коротка рампа: як читати docs + canonical‑лінки + посилання на `/llms-full.txt`.
- `/llms-full.txt` = згенерований артефакт з чистого контенту.
- (Опційно) `.md`‑версії сторінок для машинного читання.

### C. Правила співіснування (щоб не повторити #376)
1) **Human docs — source of truth**. `llms-full.txt` генерується з них.
2) **Жодного “хайпу” у Reference/How‑to**. Там має бути операційна правда.
3) **Docs повинні тестуватися**: приклади команд мають бути перевірені CI (smoke/doctest‑подібно).
4) **Recovery‑first**: де можливий “режим жахів” — recovery секція стандарт.

---

## Мінімальний пакет правок з максимальним ефектом
1) Додати сторінку **Architecture Overview (Git + JSON + SQLite)**.
2) Додати сторінку/розділ **Failure modes & Recovery**.
3) Підвести навігацію під **Diátaxis** (навіть без повного переписування тексту).
4) Розширити `llms.txt` блоком **Agent guidance** + canonical‑посилання на recovery.
5) Додати CI‑перевірку прикладів команд (щоб docs “не брехали”).

---

## Додаток: готовий шаблон `llms.txt` (скелет)

```text
# Beads Documentation (LLM Index)

Updated: YYYY-MM-DD
Docs version: vX.Y.Z (or commit)

## What this is
Short 3–6 lines: what Beads is, what it manipulates (git/worktrees/db), and what this index contains.

## Agent guidance (read this first)
- Do: prefer read-only commands first; show recovery steps when errors occur.
- Don’t: mutate worktrees or DB without an explicit backup/recovery path.
- If DB is locked or state is inconsistent: jump to Recovery.

## Canonical docs
- Overview: /docs/overview
- Tutorial: /docs/tutorials/first-bead
- How-to: /docs/how-to/worktrees
- How-to: /docs/how-to/recovery
- Reference: /docs/reference/cli
- Explanation: /docs/explanation/architecture

## Optional
- Full plaintext: /llms-full.txt
```

---

## Чекліст якості (люди + ШІ)

### A. Спільні критерії (правда, підтримуваність, сталість)
- [ ] **Єдина “правда”**: людські docs — _source of truth_; будь‑які AI‑артефакти генеруються з них.
- [ ] **Версіонування**: на сайті видно версію/коміт (або дату) для співставлення з поведінкою продукту.
- [ ] **Немає “переможних заяв” без вимірювань** у Reference/How‑to (маркетинг ≠ документація).
- [ ] **Біті лінки ловляться** (CI link‑check) і виправляються до релізу.
- [ ] **Приклади команд — копіпастні** (працюють на чистому середовищі) і мають очікуваний результат.
- [ ] **Небезпечні операції** мають явні попередження + safe‑path + recovery.

### B. Людська документація (UX/читабельність/IA)
**Інформаційна архітектура (Diátaxis)**
- [ ] Tutorials: є хоча б 1 end‑to‑end туторіал (happy‑path) на 5–10 хв.
- [ ] How‑to: є задачі‑рецепти для типових робочих ситуацій (worktrees, merge, recovery).
- [ ] Reference: повний і сухий довідник (без лірики), з чіткими параметрами та прикладами.
- [ ] Explanation: є архітектурний огляд + інваріанти + trade‑offs.

**Скановність і стиль**
- [ ] Сторінки мають стабільний шаблон: **Prereqs → Steps → Expected output → Troubleshooting → Next**.
- [ ] Заголовки “говорять дієсловами” (що я зроблю/дізнаюсь), а не абстракціями.
- [ ] Терміни узгоджені (глосарій або “Terms” сторінка), без синонімного хаосу.
- [ ] Мінімум “стіни тексту”: списки/таблиці/код‑блоки там, де вони знижують когнітивне навантаження.

**Операційна реальність (особливо після issue #376)**
- [ ] Є сторінка **Architecture Overview (Git ↔ JSON ↔ SQLite)**.
- [ ] Є сторінка **Failure modes & Recovery** (DB lock, merge conflicts, out‑of‑sync worktrees, multiple DBs).
- [ ] На сторінках, де можливі збої, є **Recovery** секція (не “можливо”, а “як саме”).

### C. AI‑контекст (`llms.txt` / `llms-full.txt`)
- [ ] `/llms.txt` існує як **короткий індекс**, а не дубль документації.
- [ ] `llms.txt` містить: дату/версію, canonical‑посилання, **Agent guidance (dos/don’ts)**, лінк на recovery.
- [ ] `/llms-full.txt` існує як **повний плейнтекст**, згенерований з людських docs.
- [ ] (Опційно) Підтримуються `.md`‑версії ключових сторінок або інший low‑noise формат для читання моделями.
- [ ] AI‑контент не “галюцинує” UX‑навігацію: у `llms-full` немає меню/хедера/сміття.

### D. Автоматизація якості (мінімальний CI‑набір)
- [ ] **Link checker** для сайту/markdown.
- [ ] **Docs lint** (стиль/заголовки/посилання).
- [ ] **Smoke‑тести прикладів CLI** (doctest‑подібно): хоча б 3–5 ключових сценаріїв.
- [ ] Автогенерація `llms-full.txt` як артефакт у релізі (і перевірка, що він оновився при зміні docs).

