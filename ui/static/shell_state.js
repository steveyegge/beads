"use strict";

(function (global) {
  const DEFAULT_STATUS = "open";
  const DEFAULT_SORT_ORDER = ["priority-asc", "updated-desc"];
  const DEFAULT_SORT_PRIMARY = DEFAULT_SORT_ORDER[0];
  const DEFAULT_SORT_SECONDARY =
    DEFAULT_SORT_ORDER.length > 1 ? DEFAULT_SORT_ORDER[1] : "";
  const DEFAULT_FALLBACK_SORT = "title-asc";
  const SORT_SELECTION_DEFAULT_PRIMARY = "default-primary";
  const SORT_SELECTION_DEFAULT_FALLBACK = "default-fallback";
  const SORT_SELECTION_NONE = "none";
  const MAX_SORT_CONTROLS = 5;
  const KNOWN_SORTS = new Set([
    "updated-desc",
    "updated-asc",
    "created-desc",
    "created-asc",
    "priority-asc",
    "priority-desc",
    "title-asc",
    "title-desc",
  ]);

  const shellDefaults =
    typeof global.bdShellDefaults === "object" && global.bdShellDefaults
      ? Object.assign({}, global.bdShellDefaults)
      : {};
  if (
    !Array.isArray(shellDefaults.sortOrder) ||
    !shellDefaults.sortOrder.length
  ) {
    shellDefaults.sortOrder = DEFAULT_SORT_ORDER.slice();
  } else {
    const sanitized = sanitizeSortOrder(shellDefaults.sortOrder);
    shellDefaults.sortOrder = sanitized.length
      ? sanitized
      : DEFAULT_SORT_ORDER.slice();
  }
  if (!shellDefaults.sortPrimary) {
    shellDefaults.sortPrimary =
      shellDefaults.sortOrder[0] || DEFAULT_SORT_PRIMARY;
  }
  if (shellDefaults.sortSecondary == null) {
    shellDefaults.sortSecondary =
      shellDefaults.sortOrder[1] || DEFAULT_SORT_SECONDARY || "";
  }
  global.bdShellDefaults = shellDefaults;

  function toId(value) {
    if (typeof value === "string") {
      return value.trim();
    }
    if (value == null) {
      return "";
    }
    return String(value).trim();
  }

  function toText(value) {
    if (typeof value !== "string") {
      return "";
    }
    return value.trim();
  }

  function normalizeList(values) {
    let list = [];
    if (Array.isArray(values)) {
      list = values.slice();
    } else if (typeof values === "string" && values.trim()) {
      list = values.split(",").map((entry) => entry.trim());
    }
    const seen = new Set();
    const result = [];
    for (const value of list) {
      const id = toId(value);
      if (!id || seen.has(id.toLowerCase())) {
        continue;
      }
      seen.add(id.toLowerCase());
      result.push(id);
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

  function normalizeCounts(input) {
    const source = input && typeof input === "object" ? input : {};
    const output = {};
    Object.keys(source).forEach((key) => {
      if (!key) {
        return;
      }
      const value = source[key];
      let numeric = 0;
      if (typeof value === "number") {
        numeric = value;
      } else if (typeof value === "string" && value.trim()) {
        const parsed = Number(value);
        numeric = Number.isFinite(parsed) ? parsed : 0;
      }
      output[key] = numeric >= 0 ? Math.round(numeric) : 0;
    });
    return output;
  }

  function countsEqual(a, b) {
    if (a === b) {
      return true;
    }
    if (!a || !b) {
      return false;
    }
    const keysA = Object.keys(a);
    const keysB = Object.keys(b);
    if (keysA.length !== keysB.length) {
      return false;
    }
    for (let i = 0; i < keysA.length; i += 1) {
      const key = keysA[i];
      if (!Object.prototype.hasOwnProperty.call(b, key)) {
        return false;
      }
      if (a[key] !== b[key]) {
        return false;
      }
    }
    return true;
  }

  function normalizeStatus(value) {
    if (typeof value !== "string") {
      return DEFAULT_STATUS;
    }
    const trimmed = value.trim().toLowerCase();
    if (!trimmed) {
      return DEFAULT_STATUS;
    }
    switch (trimmed) {
      case "ready":
      case "open":
        return "open";
      case "in_progress":
      case "in-progress":
      case "progress":
        return "in_progress";
      case "blocked":
        return "blocked";
      case "closed":
      case "done":
      case "complete":
        return "closed";
      case "recent":
        return "";
      default:
        return trimmed;
    }
  }

  function normalizePriority(value) {
    let numeric = null;
    if (typeof value === "number" && Number.isFinite(value)) {
      numeric = Math.round(value);
    } else if (typeof value === "string" && value.trim()) {
      const trimmed = value.trim();
      const match = /^p?([0-4])$/i.exec(trimmed);
      if (match) {
        numeric = Number(match[1]);
      } else {
        const parsed = Number(trimmed);
        if (Number.isFinite(parsed)) {
          numeric = Math.round(parsed);
        }
      }
    }
    if (numeric == null || numeric < 0 || numeric > 4) {
      return "";
    }
    return String(numeric);
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
      if (!KNOWN_SORTS.has(token) || seen.has(token)) {
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
      const legacy = sanitizeSortOrder([
        base.sortPrimary ?? base.sort ?? base.orderPrimary,
        base.sortSecondary ?? base.sort_secondary ?? base.orderSecondary,
      ]);
      if (legacy.length) {
        order = legacy;
      }
    }
    if (!order.length) {
      order = DEFAULT_SORT_ORDER.slice();
    }
    return order.slice(0, MAX_SORT_CONTROLS);
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

  function normalizeFilters(input) {
    const source = input && typeof input === "object" ? input : {};
    const query = toText(source.query ?? source.search ?? "");
    const status = normalizeStatus(
      source.status ?? source.queue ?? DEFAULT_STATUS
    );
    const issueType = toText(source.issueType ?? source.type ?? "");
    const priority = normalizePriority(source.priority);
    const assignee = toText(source.assignee ?? "");
    const labelsAll = normalizeList(source.labelsAll ?? source.labels ?? []);
    const labelsAny = normalizeList(
      source.labelsAny ??
        source.labels_any ??
        source.anyLabels ??
        source.labelsOr ??
        []
    );
    const prefix = normalizePrefix(
      source.prefix ?? source.idPrefix ?? source.queuePrefix ?? ""
    );
    const sortOrder = normalizeSortOrderInput(
      Object.assign({}, shellDefaults, source),
    );
    const sortPrimary =
      sortOrder[0] || shellDefaults.sortPrimary || DEFAULT_SORT_PRIMARY;
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

  function createShellState(initial) {
    let filters = normalizeFilters(initial);
    let selection = [];
    const filterSubscribers = new Set();
    const selectionSubscribers = new Set();
    const queueCountSubscribers = new Set();
    let queueCounts = {};

    const snapshotFilters = () => ({
      query: filters.query,
      status: filters.status,
      issueType: filters.issueType,
      priority: filters.priority,
      assignee: filters.assignee,
      labelsAll: filters.labelsAll.slice(),
      labelsAny: filters.labelsAny.slice(),
      prefix: filters.prefix,
      sortOrder: filters.sortOrder.slice(),
      sortPrimary: filters.sortPrimary,
      sortSecondary: filters.sortSecondary,
    });

    const notifyFilters = () => {
      const payload = snapshotFilters();
      filterSubscribers.forEach((listener) => {
        try {
          listener(payload);
        } catch (error) {
          if (console && console.error) {
            console.error("bdShellState filter subscriber error", error);
          }
        }
      });
    };

    const notifySelection = () => {
      const payload = selection.slice();
      selectionSubscribers.forEach((listener) => {
        try {
          listener(payload);
        } catch (error) {
          if (console && console.error) {
            console.error("bdShellState selection subscriber error", error);
          }
        }
      });
    };

    const snapshotQueueCounts = () => Object.assign({}, queueCounts);

    const notifyQueueCounts = () => {
      const payload = snapshotQueueCounts();
      queueCountSubscribers.forEach((listener) => {
        try {
          listener(payload);
        } catch (error) {
          if (console && console.error) {
            console.error("bdShellState queue count subscriber error", error);
          }
        }
      });
    };

    return {
      getFilters() {
        return snapshotFilters();
      },
      setFilters(next) {
        const merged = Object.assign({}, filters, next || {});
        const normalized = normalizeFilters(merged);
        if (
          normalized.query === filters.query &&
          normalized.status === filters.status &&
          normalized.issueType === filters.issueType &&
          normalized.priority === filters.priority &&
          normalized.assignee === filters.assignee &&
          normalized.prefix === filters.prefix &&
          normalized.sortPrimary === filters.sortPrimary &&
          normalized.sortSecondary === filters.sortSecondary &&
          listsEqual(normalized.labelsAll, filters.labelsAll) &&
          listsEqual(normalized.labelsAny, filters.labelsAny) &&
          listsEqual(normalized.sortOrder, filters.sortOrder)
        ) {
          return;
        }
        filters = normalized;
        notifyFilters();
      },
      subscribe(listener) {
        if (typeof listener !== "function") {
          return () => {};
        }
        filterSubscribers.add(listener);
        listener(snapshotFilters());
        return () => {
          filterSubscribers.delete(listener);
        };
      },
      getSelection() {
        return selection.slice();
      },
      setSelection(ids) {
        const normalized = normalizeList(ids);
        if (listsEqual(selection, normalized)) {
          return;
        }
        selection = normalized;
        notifySelection();
      },
      subscribeSelection(listener) {
        if (typeof listener !== "function") {
          return () => {};
        }
        selectionSubscribers.add(listener);
        listener(selection.slice());
        return () => {
          selectionSubscribers.delete(listener);
        };
      },
      getQueueCounts() {
        return snapshotQueueCounts();
      },
      setQueueCounts(next) {
        const normalized = normalizeCounts(next);
        if (countsEqual(queueCounts, normalized)) {
          return;
        }
        queueCounts = normalized;
        notifyQueueCounts();
      },
      subscribeQueueCounts(listener) {
        if (typeof listener !== "function") {
          return () => {};
        }
        queueCountSubscribers.add(listener);
        listener(snapshotQueueCounts());
        return () => {
          queueCountSubscribers.delete(listener);
        };
      },
      clearSelection() {
        if (!selection.length) {
          return;
        }
        selection = [];
        notifySelection();
      },
    };
  }

  let bootstrapped = false;

  function ensureShellState(initial) {
    if (
      global.bdShellState &&
      typeof global.bdShellState.getFilters === "function"
    ) {
      if (!bootstrapped && initial && typeof initial === "object") {
        const statusCandidate =
          typeof initial.status === "string" && initial.status.trim()
            ? initial.status.trim()
            : typeof initial.queue === "string" && initial.queue.trim()
              ? initial.queue.trim()
              : "";
        if (statusCandidate) {
          const filters = global.bdShellState.getFilters();
          const normalizedStatus = normalizeStatus(statusCandidate);
          if (filters.status !== normalizedStatus) {
            global.bdShellState.setFilters({ status: normalizedStatus });
          }
        }
        bootstrapped = true;
      }
      return global.bdShellState;
    }
    const state = createShellState(initial);
    global.bdShellState = state;
    bootstrapped = true;
    return state;
  }

  ensureShellState({ status: DEFAULT_STATUS });

  global.bdShellUtils = Object.assign(global.bdShellUtils || {}, {
    normalizeList,
    listsEqual,
  });
  global.bdEnsureShellState = ensureShellState;
})(typeof window !== "undefined" ? window : globalThis);
