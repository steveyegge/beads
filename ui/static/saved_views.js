"use strict";

(function (global) {
  const STORAGE_KEY = "beads_ui_views_v1";
  const GLOBAL_DEFAULTS =
    typeof global.bdShellDefaults === "object" && global.bdShellDefaults
      ? global.bdShellDefaults
      : {};
  const SORT_VALUE_SET = new Set([
    "updated-desc",
    "updated-asc",
    "created-desc",
    "created-asc",
    "priority-asc",
    "priority-desc",
    "title-asc",
    "title-desc",
  ]);
  const RAW_GLOBAL_SORT_ORDER = Array.isArray(GLOBAL_DEFAULTS.sortOrder)
    ? GLOBAL_DEFAULTS.sortOrder
    : typeof GLOBAL_DEFAULTS.order === "string"
      ? GLOBAL_DEFAULTS.order.split(",")
      : [];
  const DEFAULT_SORT_ORDER = (() => {
    const sanitized = sanitizeSortOrder(RAW_GLOBAL_SORT_ORDER);
    if (sanitized.length) {
      return sanitized;
    }
    const legacy = sanitizeSortOrder([
      GLOBAL_DEFAULTS.sortPrimary,
      GLOBAL_DEFAULTS.sortSecondary,
    ]);
    if (legacy.length) {
      return legacy;
    }
    return ["priority-asc", "updated-desc"];
  })();
  const DEFAULT_SORT_PRIMARY = DEFAULT_SORT_ORDER[0];
  const DEFAULT_SORT_SECONDARY =
    DEFAULT_SORT_ORDER.length > 1 ? DEFAULT_SORT_ORDER[1] : "";
  const DEFAULT_FALLBACK_SORT = "title-asc";
  const SORT_SELECTION_DEFAULT_PRIMARY = "default-primary";
  const SORT_SELECTION_DEFAULT_FALLBACK = "default-fallback";
  const SORT_SELECTION_NONE = "none";
  const MAX_SORT_CONTROLS = 5;
  const SORT_LABELS = Object.freeze({
    "updated-desc": "Updated (newest first)",
    "updated-asc": "Updated (oldest first)",
    "created-desc": "Created (newest first)",
    "created-asc": "Created (oldest first)",
    "priority-asc": "Priority (P0 to P4)",
    "priority-desc": "Priority (P4 to P0)",
    "title-asc": "Title (A to Z)",
    "title-desc": "Title (Z to A)",
  });
  const STATUS_LABELS = Object.freeze({
    open: "Ready",
    in_progress: "In Progress",
    blocked: "Blocked",
    closed: "Done",
  });
  const BUILT_IN_VIEWS = Object.freeze([
    {
      id: "builtin-ready",
      name: "Ready",
      filters: {
        status: "open",
        sortOrder: DEFAULT_SORT_ORDER.slice(),
        sortPrimary: DEFAULT_SORT_PRIMARY,
        sortSecondary: DEFAULT_SORT_SECONDARY,
      },
    },
    {
      id: "builtin-in-progress",
      name: "In Progress",
      filters: {
        status: "in_progress",
        sortOrder: DEFAULT_SORT_ORDER.slice(),
        sortPrimary: DEFAULT_SORT_PRIMARY,
        sortSecondary: DEFAULT_SORT_SECONDARY,
      },
    },
    {
      id: "builtin-blocked",
      name: "Blocked",
      filters: {
        status: "blocked",
        sortOrder: DEFAULT_SORT_ORDER.slice(),
        sortPrimary: DEFAULT_SORT_PRIMARY,
        sortSecondary: DEFAULT_SORT_SECONDARY,
      },
    },
    {
      id: "builtin-done",
      name: "Done",
      filters: {
        status: "closed",
        sortOrder: DEFAULT_SORT_ORDER.slice(),
        sortPrimary: DEFAULT_SORT_PRIMARY,
        sortSecondary: DEFAULT_SORT_SECONDARY,
      },
    },
  ]);

  function getStorage(provided) {
    if (provided && typeof provided.getItem === "function") {
      return provided;
    }
    try {
      if (typeof window !== "undefined" && window.localStorage) {
        return window.localStorage;
      }
    } catch {
      /* ignore storage access failures */
    }
    return null;
  }

  function uniqueLabels(values) {
    const list = Array.isArray(values)
      ? values
      : typeof values === "string" && values.trim()
        ? values.split(",")
        : [];
    const seen = new Set();
    const result = [];
    for (const value of list) {
      if (typeof value !== "string") {
        continue;
      }
      const label = value.trim();
      if (!label) {
        continue;
      }
      const key = label.toLowerCase();
      if (seen.has(key)) {
        continue;
      }
      seen.add(key);
      result.push(label);
    }
    return result;
  }

  function listsEqual(a, b) {
    if (a === b) {
      return true;
    }
    if (!Array.isArray(a) || !Array.isArray(b)) {
      return false;
    }
    if (a.length !== b.length) {
      return false;
    }
    for (let i = 0; i < a.length; i += 1) {
      if (a[i] !== b[i]) {
        return false;
      }
    }
    return true;
  }

  function normalizeStatus(value) {
    if (typeof value !== "string") {
      return "open";
    }
    const trimmed = value.trim().toLowerCase();
    if (!trimmed) {
      return "open";
    }
    switch (trimmed) {
      case "ready":
      case "open":
        return "open";
      case "in-progress":
      case "in_progress":
      case "progress":
        return "in_progress";
      case "blocked":
        return "blocked";
      case "closed":
      case "done":
        return "closed";
      case "recent":
        return "";
      default:
        return trimmed;
    }
  }

  function normalizePriority(value) {
    if (typeof value === "number" && Number.isFinite(value)) {
      const bounded = Math.round(value);
      if (bounded >= 0 && bounded <= 4) {
        return String(bounded);
      }
      return "";
    }
    if (typeof value !== "string") {
      return "";
    }
    const trimmed = value.trim();
    if (!trimmed) {
      return "";
    }
    const match = /^p?([0-4])$/i.exec(trimmed);
    if (match) {
      return match[1];
    }
    const parsed = Number(trimmed);
    if (Number.isFinite(parsed) && parsed >= 0 && parsed <= 4) {
      return String(Math.round(parsed));
    }
    return "";
  }

  function normalizePrefix(value) {
    if (typeof value !== "string") {
      return "";
    }
    let prefix = value.trim();
    if (!prefix) {
      return "";
    }
    prefix = prefix.replace(/\s+/g, "");
    if (prefix.endsWith("-")) {
      prefix = prefix.slice(0, -1);
    }
    return prefix.toLowerCase();
  }

  function sanitizeSortOrder(values) {
    const list = Array.isArray(values)
      ? values
      : typeof values === "string"
        ? values.split(",")
        : [];
    const seen = new Set();
    const result = [];
    for (let i = 0; i < list.length; i += 1) {
      const entry = list[i];
      const token = typeof entry === "string" ? entry.trim().toLowerCase() : "";
      if (!token || token === "none" || token === "default") {
        continue;
      }
      if (!SORT_VALUE_SET.has(token) || seen.has(token)) {
        continue;
      }
      seen.add(token);
      result.push(token);
    }
    return result;
  }

  function expandSortSelections(selections) {
    const values = Array.isArray(selections) ? selections : [];
    if (!values.length) {
      return DEFAULT_SORT_ORDER.slice();
    }
    const raw = [];
    for (let i = 0; i < values.length; i += 1) {
      const entry = values[i];
      const token = typeof entry === "string" ? entry.trim().toLowerCase() : "";
      if (!token || token === SORT_SELECTION_NONE) {
        continue;
      }
      if (token === SORT_SELECTION_DEFAULT_PRIMARY) {
        raw.push(...DEFAULT_SORT_ORDER);
        continue;
      }
      if (token === SORT_SELECTION_DEFAULT_FALLBACK) {
        raw.push(DEFAULT_FALLBACK_SORT);
        continue;
      }
      raw.push(token);
    }
    const sanitized = sanitizeSortOrder(raw);
    return sanitized.length ? sanitized : DEFAULT_SORT_ORDER.slice();
  }

  function normalizeSortOrderInput(source) {
    const base = source && typeof source === "object" ? source : {};
    let order = [];
    if (Array.isArray(base.sortOrderSelections)) {
      order = expandSortSelections(base.sortOrderSelections);
    }
    if (!order.length && Array.isArray(base.sortSelections)) {
      order = expandSortSelections(base.sortSelections);
    }
    if (!order.length && Array.isArray(base.sortOrder)) {
      order = sanitizeSortOrder(base.sortOrder);
    }
    if (
      !order.length &&
      typeof base.order === "string" &&
      base.order.trim()
    ) {
      order = sanitizeSortOrder(base.order.split(","));
    }
    if (!order.length) {
      const primary = normalizeSort(
        base.sortPrimary ?? base.sort ?? base.orderPrimary,
        DEFAULT_SORT_PRIMARY,
        false,
      );
      const secondary = normalizeSort(
        base.sortSecondary ?? base.sort_secondary ?? base.orderSecondary,
        DEFAULT_SORT_SECONDARY,
        true,
      );
      order = sanitizeSortOrder([primary, secondary]);
    }
    if (!order.length) {
      order = DEFAULT_SORT_ORDER.slice();
    }
    return order.slice(0, MAX_SORT_CONTROLS);
  }

  function normalizeSort(value, fallback, allowNone) {
    const base = typeof fallback === "string" ? fallback : DEFAULT_SORT_PRIMARY;
    if (typeof value === "string") {
      const trimmed = value.trim().toLowerCase();
      if (!trimmed) {
        return allowNone && base === "none" ? "none" : base;
      }
      if (allowNone && (trimmed === "none" || trimmed === "default")) {
        return "none";
      }
      if (SORT_VALUE_SET.has(trimmed)) {
        return trimmed;
      }
    }
    if (allowNone && base === "none") {
      return "none";
    }
    return base;
  }

  function normalizeFilters(input) {
    const source = input && typeof input === "object" ? input : {};
    const query =
      typeof source.query === "string"
        ? source.query.trim()
        : typeof source.search === "string"
          ? source.search.trim()
          : "";
    const status = normalizeStatus(source.status ?? source.queue ?? "open");
    const issueType =
      typeof source.issueType === "string"
        ? source.issueType.trim()
        : typeof source.type === "string"
          ? source.type.trim()
          : "";
    const priority = normalizePriority(source.priority);
    const assignee =
      typeof source.assignee === "string" ? source.assignee.trim() : "";
    const labelsAll = uniqueLabels(source.labelsAll ?? source.labels ?? []);
    const labelsAny = uniqueLabels(
      source.labelsAny ?? source.labels_any ?? source.labelsOr ?? []
    );
    const prefix = normalizePrefix(
      source.prefix ?? source.idPrefix ?? source.queuePrefix ?? ""
    );
    const sortOrder = normalizeSortOrderInput(
      Object.assign({}, GLOBAL_DEFAULTS, source),
    );
    const sortPrimary = sortOrder[0] || DEFAULT_SORT_PRIMARY;
    const sortSecondary = sortOrder[1] || "";
    return {
      query,
      status,
      issueType,
      priority,
      assignee,
      labelsAll,
      labelsAny,
      prefix,
      sortOrder,
      sortPrimary,
      sortSecondary,
    };
  }

  function filtersEqual(a, b) {
    if (!a || !b) {
      return false;
    }
    if (
      a.query !== b.query ||
      a.status !== b.status ||
      a.issueType !== b.issueType ||
      a.priority !== b.priority ||
      a.assignee !== b.assignee ||
      a.prefix !== b.prefix ||
      a.sortPrimary !== b.sortPrimary ||
      a.sortSecondary !== b.sortSecondary
    ) {
      return false;
    }
    if (!listsEqual(a.sortOrder, b.sortOrder)) {
      return false;
    }
    if (!listsEqual(a.labelsAll, b.labelsAll)) {
      return false;
    }
    if (!listsEqual(a.labelsAny, b.labelsAny)) {
      return false;
    }
    return true;
  }

  function cloneFilters(filters) {
    const normalized = normalizeFilters(filters);
    return {
      query: normalized.query,
      status: normalized.status,
      issueType: normalized.issueType,
      priority: normalized.priority,
      assignee: normalized.assignee,
      labelsAll: normalized.labelsAll.slice(),
      labelsAny: normalized.labelsAny.slice(),
      prefix: normalized.prefix,
      sortOrder: normalized.sortOrder.slice(),
      sortPrimary: normalized.sortPrimary,
      sortSecondary: normalized.sortSecondary,
    };
  }

  function cloneView(view) {
    if (!view) {
      return null;
    }
    return {
      id: view.id,
      name: view.name,
      filters: cloneFilters(view.filters),
    };
  }

  function defaultIdFactory() {
    try {
      if (
        typeof global.crypto !== "undefined" &&
        typeof global.crypto.randomUUID === "function"
      ) {
        return global.crypto.randomUUID();
      }
    } catch {
      /* ignore randomUUID failures */
    }
    const random = Math.floor(Math.random() * 1_000_000);
    return `view-${Date.now()}-${random}`;
  }

  function generateUniqueId(idFactory, seen) {
    let candidate = "";
    do {
      candidate = idFactory();
    } while (!candidate || (seen && seen.has(candidate)));
    if (seen) {
      seen.add(candidate);
    }
    return String(candidate);
  }

  function normalizeView(entry, idFactory, seen) {
    if (entry == null) {
      return null;
    }
    if (typeof entry === "string") {
      const name = entry.trim();
      return {
        id: generateUniqueId(idFactory, seen),
        name: name || "Saved view",
        filters: cloneFilters({}),
      };
    }
    if (typeof entry !== "object") {
      return null;
    }
    let id =
      typeof entry.id === "string" && entry.id.trim()
        ? entry.id.trim()
        : "";
    if (!id || (seen && seen.has(id))) {
      id = generateUniqueId(idFactory, seen);
    } else if (seen) {
      seen.add(id);
    }
    const rawName =
      typeof entry.name === "string" && entry.name.trim()
        ? entry.name.trim()
        : id;
    let filtersSource = entry.filters;
    if (!filtersSource || typeof filtersSource !== "object") {
      filtersSource = {
        query: entry.query ?? entry.search ?? "",
        status: entry.status ?? entry.queue ?? "",
        issueType: entry.issueType ?? entry.type ?? "",
        priority: entry.priority,
        assignee: entry.assignee,
        labelsAll: entry.labels,
        labelsAny: entry.labelsAny ?? entry.labels_any,
        prefix: entry.prefix ?? entry.idPrefix ?? "",
      };
    }
    return {
      id,
      name: rawName,
      filters: cloneFilters(filtersSource),
    };
  }

  function normalizePayload(payload, idFactory) {
    const views = [];
    const seen = new Set();
    let migrated = false;

    const append = (entry) => {
      const normalized = normalizeView(entry, idFactory, seen);
      if (!normalized) {
        return;
      }
      const filtersMeta =
        entry && typeof entry === "object" ? entry.filters : null;
      if (
        !filtersMeta ||
        typeof filtersMeta !== "object" ||
        typeof filtersMeta.query === "undefined" ||
        typeof filtersMeta.status === "undefined" ||
        !Array.isArray(filtersMeta.sortOrder)
      ) {
        migrated = true;
      }
      views.push(normalized);
    };

    if (Array.isArray(payload)) {
      payload.forEach(append);
      return { views, migrated: true };
    }

    if (payload && typeof payload === "object") {
      const version = Number(payload.version) || 0;
      if (Array.isArray(payload.views)) {
        payload.views.forEach((entry) => {
          append(entry);
        });
      }
      if (version !== 3) {
        migrated = true;
      }
      return { views, migrated };
    }

    return { views: [], migrated: false };
  }

  function bdSavedViewsStore(config = {}) {
    const storage = getStorage(config.storage);
    const idFactory =
      typeof config.idFactory === "function"
        ? config.idFactory
        : defaultIdFactory;
  const listeners = new Set();
  let currentFilters = cloneFilters(config.initialFilters || {});
  let views = [];
  let builtInsAdded = false;

    const persist = () => {
      if (!storage) {
        return;
      }
      try {
        const payload = {
          version: 3,
          views: views.map((view) => ({
            id: view.id,
            name: view.name,
            filters: {
              query: view.filters.query,
              status: view.filters.status,
              issueType: view.filters.issueType,
              priority: view.filters.priority,
              assignee: view.filters.assignee,
              labelsAll: view.filters.labelsAll.slice(),
              labelsAny: view.filters.labelsAny.slice(),
              prefix: view.filters.prefix,
              sortOrder: Array.isArray(view.filters.sortOrder)
                ? view.filters.sortOrder.slice()
                : DEFAULT_SORT_ORDER.slice(),
              sortPrimary: view.filters.sortPrimary,
              sortSecondary: view.filters.sortSecondary,
            },
          })),
        };
        storage.setItem(STORAGE_KEY, JSON.stringify(payload));
      } catch (error) {
        console.warn("bdSavedViewsStore.persist failed", error);
      }
    };

    const notify = () => {
      const snapshot = api.list;
      listeners.forEach((listener) => {
        try {
          listener(snapshot);
        } catch (error) {
          console.error("saved searches listener error", error);
        }
      });
    };

    const load = () => {
      if (!storage) {
        views = BUILT_IN_VIEWS.map((view) => ({
          id: view.id,
          name: view.name,
          filters: cloneFilters(view.filters),
        }));
        builtInsAdded = true;
        persist();
        return;
      }
      let raw = null;
      try {
        raw = storage.getItem(STORAGE_KEY);
      } catch {
        raw = null;
      }
      if (!raw) {
        views = BUILT_IN_VIEWS.map((view) => ({
          id: view.id,
          name: view.name,
          filters: cloneFilters(view.filters),
        }));
        builtInsAdded = true;
        persist();
        return;
      }
      let parsed;
      try {
        parsed = JSON.parse(raw);
      } catch {
        parsed = null;
      }
      const { views: loaded, migrated } = normalizePayload(
        parsed,
        () => generateUniqueId(idFactory, null)
      );
      views = loaded.map((view) => ({
        id: view.id,
        name: view.name,
        filters: cloneFilters(view.filters),
      }));
      if (
        views.length === 0 &&
        (!parsed || typeof parsed !== "object")
      ) {
        views = BUILT_IN_VIEWS.map((view) => ({
          id: view.id,
          name: view.name,
          filters: cloneFilters(view.filters),
        }));
        builtInsAdded = true;
        return;
      }
      builtInsAdded = false;
      if (migrated) {
        persist();
      }
    };

    const ensureUniqueId = () => {
      let candidate = idFactory();
      while (!candidate || views.some((view) => view.id === candidate)) {
        candidate = idFactory();
      }
      return String(candidate);
    };

    load();

    const api = {
      get list() {
        return views.map(cloneView);
      },
      getFilters() {
        return cloneFilters(currentFilters);
      },
      setFilters(next) {
        currentFilters = cloneFilters(next || currentFilters);
        return api.getFilters();
      },
      saveView(name) {
        const filters = api.getFilters();
        const trimmed =
          typeof name === "string" ? name.replace(/\s+/g, " ").trim() : "";
        const label = trimmed || `View ${views.length + 1}`;
        const view = {
          id: ensureUniqueId(),
          name: label,
          filters,
        };
        views = views.concat([view]);
        persist();
        notify();
        return cloneView(view);
      },
      moveView(id, delta) {
        const index = views.findIndex((view) => view.id === id);
        if (index === -1) {
          return api.list;
        }
        const offset = Number(delta || 0);
        if (!offset) {
          return api.list;
        }
        const target = Math.max(
          0,
          Math.min(views.length - 1, index + offset)
        );
        if (target === index) {
          return api.list;
        }
        const next = views.slice();
        const [entry] = next.splice(index, 1);
        next.splice(target, 0, entry);
        views = next;
        persist();
        notify();
        return api.list;
      },
      removeView(id) {
        const next = views.filter((view) => view.id !== id);
        if (next.length === views.length) {
          return false;
        }
        views = next;
        persist();
        notify();
        return true;
      },
      renameView(id, name) {
        const trimmed =
          typeof name === "string" ? name.replace(/\s+/g, " ").trim() : "";
        if (!trimmed) {
          return false;
        }
        let updated = false;
        views = views.map((view) => {
          if (view.id !== id) {
            return view;
          }
          if (view.name === trimmed) {
            return view;
          }
          updated = true;
          return {
            id: view.id,
            name: trimmed,
            filters: cloneFilters(view.filters),
          };
        });
        if (updated) {
          persist();
          notify();
        }
        return updated;
      },
      getView(id) {
        const view = views.find((entry) => entry.id === id);
        return view ? cloneView(view) : null;
      },
      subscribe(listener) {
        if (typeof listener !== "function") {
          return () => {};
        }
        listeners.add(listener);
        return () => {
          listeners.delete(listener);
        };
      },
      clear() {
        views = [];
        persist();
        notify();
      },
    };

    if (builtInsAdded) {
      notify();
    }

    return api;
  }

  
function render(controller) {
  const { root, listEl, emptyEl, store, activeViewId, openMenuId } = controller;
  if (!listEl || !emptyEl) {
    return;
  }

  const views = store.list;
  listEl.innerHTML = "";

  const fragment = document.createDocumentFragment();
  views.forEach((view, index) => {
    const item = document.createElement("li");
    item.className = "ui-saved-views__item";
    item.dataset.viewId = view.id;

    const applyButton = document.createElement("button");
    applyButton.type = "button";
    applyButton.className = "ui-saved-views__apply";
    applyButton.dataset.action = "apply";
    applyButton.dataset.viewId = view.id;
    applyButton.dataset.viewName = view.name;
    applyButton.setAttribute("data-testid", "saved-view-item");
    applyButton.textContent = view.name;
    if (view.id === activeViewId.value) {
      applyButton.classList.add("is-active");
    }

    const options = document.createElement("div");
    options.className = "ui-saved-views__options";

    const optionsButton = document.createElement("button");
    optionsButton.type = "button";
    optionsButton.className = "ui-saved-views__options-button";
    optionsButton.dataset.action = "options";
    optionsButton.dataset.viewId = view.id;
    optionsButton.setAttribute("aria-haspopup", "true");
    const isOpen = openMenuId === view.id;
    optionsButton.setAttribute("aria-expanded", isOpen ? "true" : "false");
    optionsButton.setAttribute("aria-label", "Saved search options");
    optionsButton.textContent = "⋯";

    const menu = document.createElement("div");
    menu.className = "ui-saved-views__menu";
    menu.dataset.savedViewMenu = "true";
    menu.dataset.viewId = view.id;
    menu.hidden = !isOpen;
    menu.classList.toggle("is-open", isOpen);
    menu.setAttribute("aria-hidden", isOpen ? "false" : "true");

    const createMenuItem = (action, label, disabled = false) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "ui-saved-views__menu-item";
      button.dataset.action = action;
      button.dataset.viewId = view.id;
      button.textContent = label;
      if (disabled) {
        button.disabled = true;
      }
      return button;
    };

    menu.appendChild(createMenuItem("move-up", "Move up", index === 0));
    menu.appendChild(
      createMenuItem("move-down", "Move down", index === views.length - 1),
    );
    menu.appendChild(createMenuItem("view", "View"));
    menu.appendChild(createMenuItem("delete", "Delete"));

    options.appendChild(optionsButton);
    options.appendChild(menu);

    item.appendChild(applyButton);
    item.appendChild(options);
    fragment.appendChild(item);
  });

  listEl.appendChild(fragment);
  listEl.hidden = views.length === 0;
  emptyEl.hidden = views.length > 0;
  if (root) {
    root.dataset.hasSavedViews = views.length > 0 ? "true" : "false";
  }
}

function createController(root, config = {}) {
  const saveButton = root.querySelector("[data-testid='saved-views-save']");
  const listEl = root.querySelector("[data-testid='saved-views-list']");
  const emptyEl = root.querySelector("[data-testid='saved-views-empty']");

  const shell = config.shell || global.bdShellState || null;
  const applyFilters = (filters) => {
    const nextFilters = cloneFilters(filters);
    if (shell && typeof shell.setFilters === "function") {
      shell.setFilters(nextFilters);
    }
    if (config && typeof config.applyFilters === "function") {
      try {
        config.applyFilters(nextFilters);
      } catch (error) {
        console.warn("saved searches applyFilters callback failed", error);
      }
    }
  };

  const store = bdSavedViewsStore({
    initialFilters:
      (shell && typeof shell.getFilters === "function"
        ? shell.getFilters()
        : config.initialFilters) || {},
    storage: config.storage,
  });

  const controller = {
    root,
    store,
    listEl,
    emptyEl,
    saveButton,
    activeViewId: { value: "" },
    openMenuId: "",
    menuCloser: null,
  };

  const attachMenuCloser = () => {
    if (controller.menuCloser || typeof document === "undefined") {
      return;
    }
    controller.menuCloser = (event) => {
      if (!controller.openMenuId) {
        return;
      }
      const target = event.target;
      if (
        target &&
        (target.closest("[data-saved-view-menu]") ||
          target.closest("[data-action='options']"))
      ) {
        return;
      }
      controller.openMenuId = "";
      detachMenuCloser();
      render(controller);
    };
    document.addEventListener("click", controller.menuCloser);
  };

  const detachMenuCloser = () => {
    if (controller.menuCloser && typeof document !== "undefined") {
      document.removeEventListener("click", controller.menuCloser);
    }
    controller.menuCloser = null;
  };

  const highlightMatchingView = (filters) => {
    const views = store.list;
    const match = views.find((view) =>
      filtersEqual(view.filters, filters || {}),
    );
    controller.activeViewId.value = match ? match.id : "";
  };

  const shellFilters =
    (shell && typeof shell.getFilters === "function"
      ? shell.getFilters()
      : store.getFilters()) || {};
  highlightMatchingView(shellFilters);

  const unsubscribeStore = store.subscribe((views) => {
    if (
      controller.openMenuId &&
      !views.some((view) => view.id === controller.openMenuId)
    ) {
      controller.openMenuId = "";
      detachMenuCloser();
    }
    if (
      controller.activeViewId.value &&
      !views.some((view) => view.id === controller.activeViewId.value)
    ) {
      controller.activeViewId.value = "";
    }
    render(controller);
  });

  let unsubscribeShell = () => {};
  if (shell && typeof shell.subscribe === "function") {
    unsubscribeShell = shell.subscribe((filters) => {
      store.setFilters(filters);
      highlightMatchingView(filters);
      render(controller);
    });
  }

  const handleSave = () => {
    const name = promptForSavedSearchName();
    if (!name) {
      return;
    }
    const view = store.saveView(name);
    controller.activeViewId.value = view ? view.id : "";
    controller.openMenuId = "";
    detachMenuCloser();
    render(controller);
    const filters = view ? view.filters : store.getFilters();
    applyFilters(filters);
  };

  const handleListClick = (event) => {
    const actionEl = event.target.closest("[data-action]");
    if (!actionEl) {
      return;
    }
    const action = actionEl.dataset.action || "";
    const viewId =
      actionEl.dataset.viewId ||
      actionEl.closest("[data-view-id]")?.dataset.viewId ||
      "";

    if (action === "options") {
      event.preventDefault();
      controller.openMenuId =
        controller.openMenuId === viewId ? "" : viewId;
      if (controller.openMenuId) {
        attachMenuCloser();
      } else {
        detachMenuCloser();
      }
      render(controller);
      return;
    }

    if (!viewId) {
      return;
    }

    if (action === "apply") {
      const view = store.getView(viewId);
      if (!view) {
        return;
      }
      controller.activeViewId.value = view.id;
      controller.openMenuId = "";
      detachMenuCloser();
      render(controller);
      applyFilters(view.filters);
      return;
    }

    if (action === "move-up") {
      store.moveView(viewId, -1);
      controller.openMenuId = "";
      detachMenuCloser();
      return;
    }

    if (action === "move-down") {
      store.moveView(viewId, 1);
      controller.openMenuId = "";
      detachMenuCloser();
      return;
    }

    if (action === "view") {
      const view = store.getView(viewId);
      controller.openMenuId = "";
      detachMenuCloser();
      render(controller);
      if (view) {
        showSavedSearchDetailsModal(view);
      }
      return;
    }

    if (action === "delete") {
      const removed = store.removeView(viewId);
      if (removed && controller.activeViewId.value === viewId) {
        controller.activeViewId.value = "";
      }
      controller.openMenuId = "";
      detachMenuCloser();
      render(controller);
    }
  };

  if (saveButton) {
    saveButton.addEventListener("click", handleSave);
  }
  if (listEl) {
    listEl.addEventListener("click", handleListClick);
  }

  render(controller);

  return () => {
    unsubscribeStore();
    unsubscribeShell();
    detachMenuCloser();
    if (saveButton) {
      saveButton.removeEventListener("click", handleSave);
    }
    if (listEl) {
      listEl.removeEventListener("click", handleListClick);
    }
  };
}

function promptForSavedSearchName(initial = "") {
  let seed = typeof initial === "string" ? initial.trim() : "";
  const promptFn =
    (typeof window !== "undefined" && typeof window.prompt === "function")
      ? window.prompt
      : typeof global.prompt === "function"
        ? global.prompt
        : null;
  if (!promptFn) {
    return seed || "Saved search";
  }
  let current = seed;
  for (;;) {
    const result = promptFn("Name this search", current);
    if (result === null) {
      return null;
    }
    const trimmed = String(result).trim();
    if (trimmed) {
      return trimmed;
    }
    current = "";
  }
}

function showSavedSearchDetailsModal(view) {
  if (typeof document === "undefined" || !view) {
    return;
  }
  const existing = document.querySelector("[data-role='saved-search-modal']");
  if (existing) {
    existing.remove();
  }

  const overlay = document.createElement("div");
  overlay.className = "ui-saved-views__modal";
  overlay.dataset.role = "saved-search-modal";
  overlay.setAttribute("role", "dialog");
  overlay.setAttribute("aria-modal", "true");
  overlay.setAttribute("aria-label", `Saved search ${view.name}`);

  const backdrop = document.createElement("div");
  backdrop.className = "ui-saved-views__modal-backdrop";

  const content = document.createElement("div");
  content.className = "ui-saved-views__modal-content";

  const closeButton = document.createElement("button");
  closeButton.type = "button";
  closeButton.className = "ui-saved-views__modal-close";
  closeButton.setAttribute("aria-label", "Close details");
  closeButton.textContent = "×";

  const title = document.createElement("h2");
  title.className = "ui-saved-views__modal-title";
  title.textContent = view.name;

  const body = document.createElement("div");
  body.className = "ui-saved-views__modal-body";
  const message = document.createElement("p");
  message.className = "ui-saved-views__modal-note";
  message.textContent = "Saved search details will return in a future release.";
  body.appendChild(message);
  content.appendChild(closeButton);
  content.appendChild(title);
  content.appendChild(body);
  overlay.appendChild(backdrop);
  overlay.appendChild(content);
  document.body.appendChild(overlay);

  const previousFocus = document.activeElement;
  const close = () => {
    document.removeEventListener("keydown", onKeyDown, true);
    overlay.remove();
    if (previousFocus && typeof previousFocus.focus === "function") {
      previousFocus.focus();
    }
  };

  const onKeyDown = (event) => {
    if (event.key === "Escape") {
      event.preventDefault();
      close();
    }
  };

  document.addEventListener("keydown", onKeyDown, true);
  backdrop.addEventListener("click", close);
  overlay.addEventListener("click", (event) => {
    if (event.target === overlay) {
      close();
    }
  });
  closeButton.addEventListener("click", close);

  if (typeof closeButton.focus === "function") {
    closeButton.focus({ preventScroll: true });
  }
}

function initialize() {
  if (typeof document === "undefined") {
    return;
  }
  const root = document.querySelector("[data-testid='saved-views']");
  if (!root) {
    return;
  }
  const applyFiltersFn =
    typeof global.bdApplyFilters === "function"
      ? global.bdApplyFilters
      : null;
  const detach = createController(root, {
    shell: global.bdShellState || null,
    applyFilters: applyFiltersFn,
  });
  if (detach && typeof detach === "function") {
    window.addEventListener(
      "beforeunload",
      () => {
        detach();
      },
      { once: true },
    );
  }
}

if (typeof document !== "undefined") {
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initialize, {
      once: true,
    });
  } else {
    initialize();
  }
}

global.bdSavedViewsStore = bdSavedViewsStore;
})(typeof window !== "undefined" ? window : globalThis);
