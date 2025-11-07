import "./shell_state.js";
import "./shortcut_guard.js";
import "./navigation.js";
import "./palette.js";
import "./status_actions.js";
import "./delete_issue.js";
import "./event_stream.js";
import "./labels.js";
import "./detail_editor.js";
import "./quick_create.js";
import "./saved_views.js";
import "./multiselect.js";
import "./htmx_focus.js";
import "./theme.js";

const ISSUE_LIST_SELECTOR = "[data-role='issue-list']";
const ISSUE_SHELL_SELECTOR = "[data-role='issue-shell']";
const ISSUE_ROW_SELECTOR = "[data-role='issue-row']";
const ISSUE_SELECT_SELECTOR = "[data-role='issue-select']";
const FILTER_FORM_SELECTOR = "[data-role='search-form']";
const FILTER_FIELD_SELECTOR = "[data-filter-field]";
const FILTER_ACTION_SELECTOR = "[data-action]";
const DEFAULT_ISSUE_LIMIT = 50;
const BULK_TOOLBAR_SELECTOR = "[data-role='bulk-toolbar']";
const BULK_FORM_SELECTOR = "[data-role='bulk-form']";
const BULK_STATUS_SELECTOR = "[data-role='bulk-status']";
const BULK_PRIORITY_SELECTOR = "[data-role='bulk-priority']";
const BULK_COUNT_SELECTOR = "[data-role='bulk-count']";
const BULK_MESSAGE_SELECTOR = "[data-role='bulk-message']";
const ISSUE_DETAIL_SELECTOR = "[data-role='issue-detail']";
const BULK_TEMPLATE_SELECTOR = "#bulk-editor-template";
const DETAIL_PLACEHOLDER_TEMPLATE_SELECTOR = "#issue-detail-placeholder-template";
const HELP_OVERLAY_SELECTOR = "[data-role='help-overlay']";
const HELP_CLOSE_SELECTOR = "[data-role='help-close']";
const LIVE_UPDATE_BANNER_SELECTOR = "[data-role='live-update-warning']";
const LIVE_UPDATE_BANNER_MESSAGE_SELECTOR =
  "[data-role='live-update-warning-message']";
const GLOBAL_TOAST_SELECTOR = "[data-role='ui-toast']";
const GLOBAL_TOAST_MESSAGE_SELECTOR = "[data-role='ui-toast-message']";
const GLOBAL_TOAST_DISMISS_SELECTOR = "[data-role='ui-toast-dismiss']";
const QUICK_CREATE_TRIGGER_SELECTOR = "[data-testid='quick-create-trigger']";

const GLOBAL_DEFAULTS =
  (typeof globalThis.bdShellDefaults === "object" &&
    globalThis.bdShellDefaults) ||
  {};
const SORT_SELECTION_DEFAULT_PRIMARY = "default-primary";
const SORT_SELECTION_DEFAULT_FALLBACK = "default-fallback";
const SORT_SELECTION_NONE = "none";
const DEFAULT_FALLBACK_SORT = "title-asc";
const MAX_SORT_CONTROLS = 5;
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
const sanitizeSortOrder = (values) => {
  const list = Array.isArray(values)
    ? values
    : typeof values === "string"
      ? values.split(",")
      : [];
  const seen = new Set();
  const result = [];
  list.forEach((entry) => {
    const token = typeof entry === "string" ? entry.trim().toLowerCase() : "";
    if (!token) {
      return;
    }
    if (
      token === "none" ||
      token === "default" ||
      token === SORT_SELECTION_DEFAULT_PRIMARY ||
      token === SORT_SELECTION_DEFAULT_FALLBACK
    ) {
      return;
    }
    if (!SORT_VALUE_SET.has(token)) {
      return;
    }
    if (seen.has(token)) {
      return;
    }
    seen.add(token);
    result.push(token);
  });
  return result;
};
const DEFAULT_SORT_ORDER = (() => {
  if (Array.isArray(GLOBAL_DEFAULTS.sortOrder)) {
    const sanitized = sanitizeSortOrder(GLOBAL_DEFAULTS.sortOrder);
    if (sanitized.length) {
      return sanitized;
    }
  }
  if (typeof GLOBAL_DEFAULTS.order === "string") {
    const sanitized = sanitizeSortOrder(GLOBAL_DEFAULTS.order.split(","));
    if (sanitized.length) {
      return sanitized;
    }
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
const DEFAULT_SORT_PRIMARY = DEFAULT_SORT_ORDER[0] || "priority-asc";
const deriveSortSelections = (order) => {
  const sanitized = sanitizeSortOrder(order);
  if (!sanitized.length) {
    return [SORT_SELECTION_DEFAULT_PRIMARY];
  }
  const selections = [];
  const matchesDefault =
    sanitized.length >= DEFAULT_SORT_ORDER.length &&
    DEFAULT_SORT_ORDER.every((token, index) => sanitized[index] === token);
  let startIndex = 0;
  if (matchesDefault) {
    selections.push(SORT_SELECTION_DEFAULT_PRIMARY);
    startIndex = DEFAULT_SORT_ORDER.length;
  } else {
    selections.push(sanitized[0]);
    startIndex = 1;
  }
  for (let i = startIndex; i < sanitized.length; i += 1) {
    const token = sanitized[i];
    if (token === DEFAULT_FALLBACK_SORT) {
      selections.push(SORT_SELECTION_DEFAULT_FALLBACK);
    } else {
      selections.push(token);
    }
  }
  if (!selections.length) {
    selections.push(SORT_SELECTION_DEFAULT_PRIMARY);
  }
  return selections.slice(0, MAX_SORT_CONTROLS);
};
const expandSortSelections = (values) => {
  const selections = Array.isArray(values) ? values : [];
  const seen = new Set();
  const order = [];
  const appendToken = (token) => {
    if (!token || seen.has(token) || !SORT_VALUE_SET.has(token)) {
      return;
    }
    seen.add(token);
    order.push(token);
  };
  const appendDefaultPrimary = () => {
    DEFAULT_SORT_ORDER.forEach((token) => appendToken(token));
  };
  if (!selections.length) {
    appendDefaultPrimary();
    return order;
  }
  selections.forEach((value, index) => {
    const raw = typeof value === "string" ? value.trim().toLowerCase() : "";
    if (!raw) {
      return;
    }
    if (raw === SORT_SELECTION_NONE) {
      return;
    }
    if (raw === SORT_SELECTION_DEFAULT_FALLBACK) {
      appendToken(DEFAULT_FALLBACK_SORT);
      return;
    }
    if (raw === SORT_SELECTION_DEFAULT_PRIMARY) {
      appendDefaultPrimary();
      return;
    }
    if (SORT_VALUE_SET.has(raw)) {
      appendToken(raw);
    }
  });
  if (!order.length) {
    appendDefaultPrimary();
  }
  return order.slice(0, MAX_SORT_CONTROLS);
};
const shouldOfferAnotherSort = (value, position) => {
  const normalized = typeof value === "string" ? value.trim().toLowerCase() : "";
  if (!normalized || normalized === SORT_SELECTION_NONE) {
    return false;
  }
  if (position === 0) {
    return normalized !== SORT_SELECTION_DEFAULT_PRIMARY;
  }
  return normalized !== SORT_SELECTION_DEFAULT_FALLBACK;
};

const normalizeSortSelection = (value, fallback, allowNone = false) => {
  const base = typeof fallback === "string" ? fallback : "";
  if (typeof value !== "string") {
    return allowNone && base === "none" ? "none" : base || DEFAULT_SORT_PRIMARY;
  }
  const trimmed = value.trim().toLowerCase();
  if (!trimmed) {
    return allowNone && base === "none" ? "none" : base || DEFAULT_SORT_PRIMARY;
  }
  if (allowNone && (trimmed === "none" || trimmed === "default")) {
    return "none";
  }
  if (SORT_VALUE_SET.has(trimmed)) {
    return trimmed;
  }
  return allowNone && base === "none" ? "none" : base || DEFAULT_SORT_PRIMARY;
};

const escapeForSelector =
  typeof CSS !== "undefined" && CSS && typeof CSS.escape === "function"
    ? (value) => CSS.escape(String(value))
    : (value) =>
        String(value).replace(
          /([!"#$%&'()*+,./:;<=>?@[\\\]^`{|}~])/g,
          "\\$1",
        );

const recentlyDeletedIssues = new Map();

const rememberLocalDeletion = (issueId) => {
  if (!issueId) {
    return;
  }
  recentlyDeletedIssues.set(issueId, Date.now());
};

const wasRecentlyDeleted = (issueId, windowMs = 5000) => {
  if (!issueId || !recentlyDeletedIssues.has(issueId)) {
    return false;
  }
  const timestamp = recentlyDeletedIssues.get(issueId);
  if (Date.now() - timestamp <= windowMs) {
    recentlyDeletedIssues.delete(issueId);
    return true;
  }
  recentlyDeletedIssues.delete(issueId);
  return false;
};

const shellUtils = globalThis.bdShellUtils || {};
const normalizeList =
  typeof shellUtils.normalizeList === "function"
    ? shellUtils.normalizeList
    : (values) => {
        let list = [];
        if (Array.isArray(values)) {
          list = values.slice();
        } else if (typeof values === "string" && values.trim()) {
          list = values.split(",");
        }
        const seen = new Set();
        const result = [];
        list.forEach((value) => {
          if (typeof value !== "string") {
            return;
          }
          const trimmed = value.trim();
          if (!trimmed) {
            return;
          }
          const key = trimmed.toLowerCase();
          if (seen.has(key)) {
            return;
          }
          seen.add(key);
          result.push(trimmed);
        });
        return result;
      };

const normalizePrefixField = (value) => {
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
};

const normalizeFilterCandidate = (input) => {
  const source = input && typeof input === "object" ? input : {};
  const priorityValue =
    typeof source.priority === "number"
      ? String(source.priority)
      : typeof source.priority === "string"
        ? source.priority.trim()
        : "";
  const normalized = {
    query:
      typeof source.query === "string"
        ? source.query.trim()
        : typeof source.search === "string"
          ? source.search.trim()
          : "",
    status:
      typeof source.status === "string"
        ? source.status.trim()
        : typeof source.queue === "string"
          ? source.queue.trim()
          : "",
    issueType:
      typeof source.issueType === "string"
        ? source.issueType.trim()
        : typeof source.type === "string"
          ? source.type.trim()
          : "",
    priority: priorityValue,
    assignee:
      typeof source.assignee === "string" ? source.assignee.trim() : "",
    labelsAll: normalizeList(source.labelsAll ?? source.labels ?? []),
    labelsAny: normalizeList(source.labelsAny ?? source.labels_any ?? []),
    prefix: normalizePrefixField(source.prefix ?? source.idPrefix ?? ""),
  };

  const rawSortSelections = Array.isArray(source.sortOrderSelections)
    ? source.sortOrderSelections
    : Array.isArray(source.sortSelections)
      ? source.sortSelections
      : null;

  let sortOrder = rawSortSelections
    ? expandSortSelections(rawSortSelections)
    : [];

  if (!sortOrder.length && Array.isArray(source.sortOrder)) {
    sortOrder = sanitizeSortOrder(source.sortOrder);
  }
  if (
    !sortOrder.length &&
    typeof source.order === "string" &&
    source.order.trim()
  ) {
    sortOrder = sanitizeSortOrder(source.order.split(","));
  }
  if (!sortOrder.length) {
    const legacyOrder = sanitizeSortOrder([
      source.sortPrimary ?? source.sort ?? source.orderPrimary,
      source.sortSecondary ?? source.sort_secondary ?? source.orderSecondary,
    ]);
    if (legacyOrder.length) {
      sortOrder = legacyOrder;
    }
  }
  if (!sortOrder.length) {
    sortOrder = DEFAULT_SORT_ORDER.slice();
  }
  sortOrder = sortOrder.slice(0, MAX_SORT_CONTROLS);
  const sortSelections = deriveSortSelections(sortOrder);
  normalized.sortOrder = sortOrder;
  normalized.sortSelections = sortSelections;
  normalized.sortPrimary = sortOrder[0] || "";
  normalized.sortSecondary = sortOrder[1] || "";
  return normalized;
};

const formatLabelsForInput = (labels) => {
  if (!Array.isArray(labels) || !labels.length) {
    return "";
  }
  return labels.join(", ");
};

const buildSearchRequest = (filters) => {
  const normalized = normalizeFilterCandidate(filters);
  const params = new URLSearchParams();
  if (normalized.query) {
    params.set("q", normalized.query);
  }
  if (normalized.status) {
    params.set("status", normalized.status);
  }
  if (normalized.issueType) {
    params.set("type", normalized.issueType);
  }
  if (normalized.priority) {
    params.set("priority", normalized.priority);
  }
  if (normalized.assignee) {
    params.set("assignee", normalized.assignee);
  }
  normalized.labelsAll.forEach((label) => {
    params.append("labels", label);
  });
  normalized.labelsAny.forEach((label) => {
    params.append("labels_any", label);
  });
  if (normalized.prefix) {
    params.set("id_prefix", normalized.prefix);
  }
  if (!params.has("limit")) {
    params.set("limit", String(DEFAULT_ISSUE_LIMIT));
  }
  if (Array.isArray(normalized.sortOrder) && normalized.sortOrder.length) {
    params.set("order", normalized.sortOrder.join(","));
    if (normalized.sortOrder[0]) {
      params.set("sort", normalized.sortOrder[0]);
    }
    if (normalized.sortOrder[1]) {
      params.set("sort_secondary", normalized.sortOrder[1]);
    }
  }
  return { params, filters: normalized };
};

const filtersShallowEqual = (a, b) => {
  if (a === b) {
    return true;
  }
  if (!a || !b) {
    return false;
  }
  return (
    a.query === b.query &&
    a.status === b.status &&
    a.issueType === b.issueType &&
    a.priority === b.priority &&
    a.assignee === b.assignee &&
    a.prefix === b.prefix &&
    listsEqual(a.labelsAll, b.labelsAll) &&
    listsEqual(a.labelsAny, b.labelsAny) &&
    listsEqual(a.sortOrder, b.sortOrder)
  );
};

const focusHelpers =
  (typeof globalThis.bdHtmxFocusHelpers === "object" &&
    globalThis.bdHtmxFocusHelpers !== null &&
    globalThis.bdHtmxFocusHelpers) ||
  {};

const shortcutUtils =
  (typeof globalThis.bdShortcutUtils === "object" &&
    globalThis.bdShortcutUtils !== null &&
    globalThis.bdShortcutUtils) ||
  {};

const shouldRestoreIssueListFocus =
  typeof focusHelpers.shouldRestoreIssueListFocus === "function"
    ? focusHelpers.shouldRestoreIssueListFocus
    : (target) =>
        !!(
          target &&
          typeof target.matches === "function" &&
          target.matches("[data-role='issue-list']")
        );

let pendingIssueListFocusRestore = false;

const requestQueueCountRefresh = (filtersOverride) => {
  const source =
    filtersOverride && typeof filtersOverride === "object"
      ? filtersOverride
      : typeof shellState.getFilters === "function"
        ? shellState.getFilters()
        : defaultFilters;
  const normalized = normalizeFilterCandidate(source);
  scheduleIssueListRefresh(normalized);
};
const getLatestQuickCreateController = () => {
  const api = globalThis.bdQuickCreate;
  if (!api) {
    return null;
  }
  if (typeof api.getLatestController === "function") {
    return api.getLatestController();
  }
  return globalThis.__bdQuickCreateLast || null;
};

const handleQuickCreateTriggerClick = (event) => {
  const target = event?.target;
  if (!target) {
    return;
  }
  let trigger = null;
  if (typeof target.closest === "function") {
    trigger = target.closest(QUICK_CREATE_TRIGGER_SELECTOR);
  }
  if (!trigger && target.dataset && target.dataset.testid === "quick-create-trigger") {
    trigger = target;
  }
  if (!trigger) {
    return;
  }
  const controller = getLatestQuickCreateController();
  if (!controller || typeof controller.open !== "function") {
    return;
  }
  controller.open();
};

const getLiveUpdateBanner = () => {
  if (typeof document === "undefined") {
    return null;
  }
  return document.querySelector(LIVE_UPDATE_BANNER_SELECTOR);
};

const showLiveUpdateBanner = (message) => {
  if (typeof document === "undefined") {
    return;
  }
  const banner = getLiveUpdateBanner();
  if (!banner) {
    return;
  }
  if (message) {
    const messageEl = banner.querySelector(
      LIVE_UPDATE_BANNER_MESSAGE_SELECTOR,
    );
    if (messageEl) {
      messageEl.textContent = message;
    }
  }
  banner.hidden = false;
  banner.dataset.state = "visible";

  const body = document.body || null;
  if (body) {
    body.dataset.liveUpdates = "off";
    body.classList.add("ui-body--degraded");
  }
};

const hideLiveUpdateBanner = () => {
  if (typeof document === "undefined") {
    return;
  }
  const banner = getLiveUpdateBanner();
  const body = document.body || null;
  if (!banner) {
    return;
  }
  banner.dataset.state = "hidden";
  banner.hidden = true;
  if (body) {
    body.dataset.liveUpdatesInitial = "on";
    body.dataset.liveUpdates = "on";
    body.classList.remove("ui-body--degraded");
  }
};

let globalToastTimer = null;
let lastToastMessage = "";
let lastToastTimestamp = 0;

const ensureGlobalToast = () => {
  if (typeof document === "undefined") {
    return null;
  }
  let toast = document.querySelector(GLOBAL_TOAST_SELECTOR);
  if (!toast) {
    toast = document.createElement("div");
    toast.className = "ui-toast ui-toast--alert";
    toast.dataset.role = "ui-toast";
    toast.dataset.state = "hidden";
    toast.hidden = true;

    const messageEl = document.createElement("span");
    messageEl.dataset.role = "ui-toast-message";
    toast.appendChild(messageEl);

    const dismissButton = document.createElement("button");
    dismissButton.type = "button";
    dismissButton.className = "ui-toast__dismiss";
    dismissButton.dataset.role = "ui-toast-dismiss";
    dismissButton.textContent = "Dismiss";
    toast.appendChild(dismissButton);

    if (document.body) {
      document.body.appendChild(toast);
    }
  }

  const dismiss = toast.querySelector(GLOBAL_TOAST_DISMISS_SELECTOR);
  if (dismiss && !dismiss.dataset.bound) {
    dismiss.dataset.bound = "true";
    dismiss.addEventListener("click", () => {
      hideGlobalToast();
    });
  }

  return toast;
};

const hideGlobalToast = () => {
  const toast = ensureGlobalToast();
  if (!toast) {
    return;
  }
  toast.classList.remove("is-visible");
  toast.dataset.state = "hidden";
  toast.hidden = true;
  if (globalToastTimer) {
    clearTimeout(globalToastTimer);
    globalToastTimer = null;
  }
};

const showGlobalToast = (message, options = {}) => {
  const toast = ensureGlobalToast();
  if (!toast) {
    return;
  }

  const trimmed = typeof message === "string" ? message.trim() : "";
  if (!trimmed) {
    return;
  }

  const now = Date.now();
  if (trimmed === lastToastMessage && now - lastToastTimestamp < 1500) {
    return;
  }

  lastToastMessage = trimmed;
  lastToastTimestamp = now;

  const messageEl = toast.querySelector(GLOBAL_TOAST_MESSAGE_SELECTOR);
  if (messageEl) {
    messageEl.textContent = trimmed;
  }

  toast.hidden = false;
  toast.dataset.state = "visible";
  toast.classList.add("is-visible");
  toast.classList.add("ui-toast--alert");

  if (globalToastTimer) {
    clearTimeout(globalToastTimer);
    globalToastTimer = null;
  }

  const duration =
    typeof options.duration === "number" ? options.duration : 6000;
  if (duration > 0 && typeof setTimeout === "function") {
    globalToastTimer = setTimeout(() => {
      hideGlobalToast();
    }, duration);
  }
};

const normalizeSelectionList =
  typeof shellUtils.normalizeList === "function"
    ? shellUtils.normalizeList
    : (values) => {
        if (!Array.isArray(values)) {
          return [];
        }
        const seen = new Set();
        const result = [];
        values.forEach((value) => {
          if (value == null) {
            return;
          }
          const id = String(value).trim();
          if (!id || seen.has(id)) {
            return;
          }
          seen.add(id);
          result.push(id);
        });
        return result;
      };

const listsEqual =
  typeof shellUtils.listsEqual === "function"
    ? shellUtils.listsEqual
    : (a, b) => {
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
      };

const ensureShellState =
  typeof globalThis.bdEnsureShellState === "function"
    ? globalThis.bdEnsureShellState
    : (initial) => {
        if (
          globalThis.bdShellState &&
          typeof globalThis.bdShellState.getFilters === "function"
        ) {
          return globalThis.bdShellState;
        }
        const state = {
          getFilters() {
            return {
              query: "",
              status: "open",
              issueType: "",
              priority: "",
              assignee: "",
              labelsAll: [],
              labelsAny: [],
              prefix: "",
            };
          },
          setFilters() {},
          subscribe() {
            return () => {};
          },
          getSelection() {
            return [];
          },
          setSelection() {},
          subscribeSelection() {
            return () => {};
          },
          clearSelection() {},
        };
        globalThis.bdShellState = state;
        return state;
      };

const shellState = ensureShellState();

const defaultFilters = normalizeFilterCandidate(
  typeof shellState.getFilters === "function"
    ? shellState.getFilters()
    : {},
);
let lastKnownFilters = defaultFilters;
let lastAppliedFilters = null;
let pendingListRefresh = null;
let listRefreshController = null;
let formSyncing = false;
let teardownSearchPanel = () => {};

const getSearchForm = () => {
  if (typeof document === "undefined") {
    return null;
  }
  return document.querySelector(FILTER_FORM_SELECTOR);
};

const readFiltersFromForm = (form) => {
  if (!form || typeof form.querySelectorAll !== "function") {
    return { filters: lastKnownFilters };
  }
  const raw = {};
  const sortSelections = [];
  const fields = form.querySelectorAll(FILTER_FIELD_SELECTOR);
  fields.forEach((field) => {
    if (!field || !field.dataset) {
      return;
    }
    const key = field.dataset.filterField || "";
    if (!key) {
      return;
    }
    const value = typeof field.value === "string" ? field.value : "";
    switch (key) {
      case "labelsAll":
        raw.labelsAll = normalizeList(value);
        break;
      case "labelsAny":
        raw.labelsAny = normalizeList(value);
        break;
      case "prefix":
        raw.prefix = normalizePrefixField(value);
        break;
      case "priority":
        raw.priority = value.trim();
        break;
      case "sortOrder": {
        const indexValue = field.dataset.sortIndex;
        const index = indexValue != null ? Number(indexValue) : sortSelections.length;
        if (!Number.isNaN(index) && index >= 0 && index < MAX_SORT_CONTROLS) {
          sortSelections[index] = value.trim();
        }
        break;
      }
      default:
        raw[key] = value.trim();
        break;
    }
  });
  if (sortSelections.length) {
    raw.sortOrderSelections = sortSelections;
  }
  const normalized = normalizeFilterCandidate(
    Object.assign({}, defaultFilters, raw),
  );
  return { filters: normalized };
};

const ensureSortControls = (form, sortOrder) => {
  if (!form || typeof form.querySelector !== "function") {
    return;
  }
  const container = form.querySelector("[data-role='search-sort']");
  if (!container) {
    return;
  }
  const template = document.getElementById("search-sort-template");
  let controls = Array.from(container.querySelectorAll("[data-sort-control]"));
  if (!controls.length && template && template.content) {
    container.appendChild(template.content.cloneNode(true));
    controls = Array.from(container.querySelectorAll("[data-sort-control]"));
  }
  const selections = deriveSortSelections(sortOrder).slice(0, MAX_SORT_CONTROLS);
  if (!selections.length) {
    selections.push(SORT_SELECTION_DEFAULT_PRIMARY);
  }
  let desiredCount = selections.length;
  const lastSelection = selections[selections.length - 1];
  if (
    shouldOfferAnotherSort(lastSelection, selections.length - 1) &&
    desiredCount < MAX_SORT_CONTROLS
  ) {
    desiredCount += 1;
  }
  if (desiredCount < 1) {
    desiredCount = 1;
  }
  while (controls.length < desiredCount) {
    if (!template || !template.content) {
      break;
    }
    container.appendChild(template.content.cloneNode(true));
    controls = Array.from(container.querySelectorAll("[data-sort-control]"));
  }
  while (controls.length > desiredCount) {
    const control = controls.pop();
    if (control && typeof control.remove === "function") {
      control.remove();
    }
  }
  controls.forEach((control, index) => {
    const labelEl = control.querySelector("[data-sort-label]");
    if (labelEl) {
      labelEl.textContent = index === 0 ? "Sort by" : "Then sort by";
    }
    const select = control.querySelector("select[data-filter-field='sortOrder']");
    if (!select) {
      return;
    }
    select.dataset.sortIndex = String(index);
    const value =
      index < selections.length
        ? selections[index]
        : index === 0
          ? SORT_SELECTION_DEFAULT_PRIMARY
          : SORT_SELECTION_DEFAULT_FALLBACK;
    if (select.value !== value) {
      const hasOption = Array.from(select.options).some(
        (option) => option.value === value,
      );
      if (hasOption) {
        select.value = value;
      } else if (index === 0) {
        select.value = SORT_SELECTION_DEFAULT_PRIMARY;
      } else if (select.options.length > 0) {
        select.value = select.options[0].value;
      }
    }
  });
};

const writeFiltersToForm = (form, filters) => {
  if (!form || typeof form.querySelectorAll !== "function") {
    return;
  }
  const normalized = normalizeFilterCandidate(filters);
  ensureSortControls(form, normalized.sortOrder);
  const fields = form.querySelectorAll(FILTER_FIELD_SELECTOR);
  const sortSelections = normalized.sortSelections || deriveSortSelections(normalized.sortOrder);
  fields.forEach((field) => {
    if (!field || !field.dataset) {
      return;
    }
    const key = field.dataset.filterField || "";
    if (!key) {
      return;
    }
    let value = "";
    switch (key) {
      case "labelsAll":
        value = formatLabelsForInput(normalized.labelsAll);
        break;
      case "labelsAny":
        value = formatLabelsForInput(normalized.labelsAny);
        break;
      case "prefix":
        value = normalized.prefix;
        break;
      case "sortOrder": {
        const indexValue = field.dataset.sortIndex;
        const index = indexValue != null ? Number(indexValue) : 0;
        const selection =
          !Number.isNaN(index) && index < sortSelections.length
            ? sortSelections[index]
            : index === 0
              ? SORT_SELECTION_DEFAULT_PRIMARY
              : SORT_SELECTION_DEFAULT_FALLBACK;
        value = selection;
        break;
      }
      default:
        value = normalized[key] || "";
        break;
    }
    if (field.value !== value) {
      field.value = value;
    }
  });
};

const refreshIssueList = (filters, options = {}) => {
  const targetFilters = normalizeFilterCandidate(filters);
  const issueList =
    typeof document !== "undefined"
      ? document.querySelector(ISSUE_LIST_SELECTOR)
      : null;
  const request = buildSearchRequest(targetFilters);
  if (!issueList) {
    lastAppliedFilters = request.filters;
    return;
  }
  const currentSelected =
    options.selectedId != null
      ? String(options.selectedId)
      : options.preserveSelection === false
        ? ""
        : getActiveIssueId();
  if (currentSelected) {
    request.params.set("selected", currentSelected);
  }
  const query = request.params.toString();
  const url = query ? `/fragments/issues?${query}` : "/fragments/issues";
  issueList.setAttribute("hx-get", url);

  if (
    !options.force &&
    lastAppliedFilters &&
    filtersShallowEqual(lastAppliedFilters, request.filters)
  ) {
    return;
  }
  lastAppliedFilters = request.filters;

  if (
    listRefreshController &&
    typeof listRefreshController.abort === "function"
  ) {
    listRefreshController.abort();
  }

  if (globalThis.htmx && typeof globalThis.htmx.ajax === "function") {
    globalThis.htmx.ajax("GET", url, {
      target: issueList,
      swap: "innerHTML",
    });
    return;
  }

  listRefreshController =
    typeof AbortController === "function" ? new AbortController() : null;

  fetch(url, {
    headers: {
      "HX-Request": "true",
    },
    signal: listRefreshController ? listRefreshController.signal : undefined,
  })
    .then((resp) => {
      if (!resp || !resp.ok) {
        throw new Error("issue list refresh failed");
      }
      return resp.text();
    })
    .then((html) => {
      issueList.innerHTML = html;
    })
    .catch((error) => {
      if (error && error.name === "AbortError") {
        return;
      }
      if (typeof console !== "undefined" && console.error) {
        console.error("[bd-ui] list refresh failed", error);
      }
    });
};

const scheduleIssueListRefresh = (filters, options = {}) => {
  const delay = options.immediate === true ? 0 : Math.max(0, options.delay || 150);
  if (pendingListRefresh) {
    clearTimeout(pendingListRefresh);
    pendingListRefresh = null;
  }
  if (typeof setTimeout !== "function") {
    refreshIssueList(filters, options);
    return;
  }
  pendingListRefresh = setTimeout(() => {
    pendingListRefresh = null;
    refreshIssueList(filters, options);
  }, delay);
};

const setupSearchPanel = () => {
  if (typeof document === "undefined") {
    scheduleIssueListRefresh(lastKnownFilters, { immediate: true });
    return () => {};
  }
  const form = getSearchForm();
  if (!form) {
    scheduleIssueListRefresh(lastKnownFilters, { immediate: true });
    return () => {};
  }

  const syncForm = (filters) => {
    formSyncing = true;
    writeFiltersToForm(form, filters);
    formSyncing = false;
  };

  syncForm(lastKnownFilters);

  const onFieldInput = (event) => {
    if (!event || !event.target) {
      return;
    }
    const target = event.target;
    if (
      !target.matches ||
      !target.matches(FILTER_FIELD_SELECTOR)
    ) {
      return;
    }
    const { filters: normalized } = readFiltersFromForm(form);
    if (
      target.dataset &&
      target.dataset.filterField === "sortOrder"
    ) {
      ensureSortControls(form, normalized.sortOrder);
    }
    if (filtersShallowEqual(lastKnownFilters, normalized)) {
      return;
    }
    lastKnownFilters = normalized;
    shellState.setFilters(normalized);
  };

  form.addEventListener("input", onFieldInput);
  form.addEventListener("change", onFieldInput);

  const applyButton = form.querySelector("[data-action='apply']");
  const resetButton = form.querySelector("[data-action='reset']");

  const handleApply = (event) => {
    event?.preventDefault?.();
    const { filters: normalized } = readFiltersFromForm(form);
    lastKnownFilters = normalized;
    shellState.setFilters(normalized);
    scheduleIssueListRefresh(normalized, { immediate: true });
  };

  const handleReset = (event) => {
    event?.preventDefault?.();
    lastKnownFilters = normalizeFilterCandidate(defaultFilters);
    syncForm(lastKnownFilters);
    shellState.setFilters(lastKnownFilters);
    scheduleIssueListRefresh(lastKnownFilters, { immediate: true });
  };

  if (applyButton) {
    applyButton.addEventListener("click", handleApply);
  }
  if (resetButton) {
    resetButton.addEventListener("click", handleReset);
  }

  let unsubscribeShell = () => {};
  if (typeof shellState.subscribe === "function") {
    unsubscribeShell = shellState.subscribe((filters) => {
      const normalized = normalizeFilterCandidate(filters);
      lastKnownFilters = normalized;
      syncForm(normalized);
      scheduleIssueListRefresh(normalized, { immediate: true });
    });
  } else {
    scheduleIssueListRefresh(lastKnownFilters, { immediate: true });
  }

  return () => {
    form.removeEventListener("input", onFieldInput);
    form.removeEventListener("change", onFieldInput);
    if (applyButton) {
      applyButton.removeEventListener("click", handleApply);
    }
    if (resetButton) {
      resetButton.removeEventListener("click", handleReset);
    }
    unsubscribeShell();
  };
};

globalThis.bdApplyFilters = (filters) => {
  const normalized = normalizeFilterCandidate(filters);
  lastKnownFilters = normalized;
  shellState.setFilters(normalized);
  scheduleIssueListRefresh(normalized, { immediate: true });
};

let issueListRoot = null;
let multiSelect = null;
let multiSelectSubscription = null;
let detailLoadController = null;
let helpOverlayPreviousFocus = null;


const EVENT_LABELS = {
  created: "Created",
  updated: "Updated",
  closed: "Closed",
};

const STATUS_LABEL_FALLBACK = {
  open: "Ready",
  in_progress: "In Progress",
  blocked: "Blocked",
  closed: "Done",
};

const statusHelpers = () => globalThis.bdStatusHelpers || {};

const toStatusLabel = (status) => {
  const helpers = statusHelpers();
  if (helpers && typeof helpers.labelForStatus === "function") {
    return helpers.labelForStatus(status);
  }
  const normalized = typeof status === "string" ? status.toLowerCase() : "";
  return STATUS_LABEL_FALLBACK[normalized] || status || "";
};

const updateIssueStatusDom = (detail) => {
  if (!detail || !detail.issueId) {
    return;
  }
  const issueId = String(detail.issueId);
  const status = detail.status || "";
  const label = detail.label || toStatusLabel(status);

  const detailRoot = document.querySelector("[data-testid='issue-detail']");
  if (
    detailRoot &&
    detailRoot.getAttribute("data-issue-id") === issueId
  ) {
    const statusBadge = detailRoot.querySelector("[data-field='status']");
    if (statusBadge) {
      statusBadge.textContent = label;
      statusBadge.dataset.status = status;
    }
    detailRoot.dataset.status = status;
  }

  const rows = document.querySelectorAll("[data-role='issue-row']");
  rows.forEach((row) => {
    if (row?.dataset?.issueId === issueId) {
      row.dataset.status = status;
      row.setAttribute("aria-label", `${issueId} · ${row.querySelector(".ui-issue-row-title")?.textContent || ""} (${label})`);
    }
  });
};

const dispatchCustomEvent = (type, detail) => {
  if (typeof document === "undefined") {
    return;
  }
  const target = document.body || document;
  if (!target || typeof target.dispatchEvent !== "function") {
    return;
  }
  let event = null;
  if (typeof CustomEvent === "function") {
    event = new CustomEvent(type, { detail, bubbles: false });
  } else if (typeof document.createEvent === "function") {
    event = document.createEvent("CustomEvent");
    event.initCustomEvent(type, false, false, detail);
  }
  if (event) {
    target.dispatchEvent(event);
  }
};

const getIssueListElement = () => document.querySelector(ISSUE_LIST_SELECTOR);

function getIssueRowsContainer(root) {
  if (!root || typeof root.querySelector !== "function") {
    return null;
  }
  return root.querySelector("[data-role='issue-list-rows']") || root;
}

function collectIssueIds(root) {
  const container =
    getIssueRowsContainer(root) ||
    getIssueRowsContainer(issueListRoot) ||
    getIssueRowsContainer(getIssueListElement());
  if (!container || typeof container.querySelectorAll !== "function") {
    return [];
  }
  const ids = [];
  container.querySelectorAll(ISSUE_ROW_SELECTOR).forEach((row) => {
    const id = row?.dataset?.issueId;
    if (id) {
      ids.push(id);
    }
  });
  return ids;
}

function findBulkToolbar(root) {
  const shell =
    root && typeof root.closest === "function"
      ? root.closest(ISSUE_SHELL_SELECTOR)
      : null;
  if (shell && typeof shell.querySelector === "function") {
    return shell.querySelector(BULK_TOOLBAR_SELECTOR);
  }
  if (issueListRoot && typeof issueListRoot.closest === "function") {
    const fallbackShell = issueListRoot.closest(ISSUE_SHELL_SELECTOR);
    if (fallbackShell && typeof fallbackShell.querySelector === "function") {
      return fallbackShell.querySelector(BULK_TOOLBAR_SELECTOR);
    }
  }
  return document.querySelector(BULK_TOOLBAR_SELECTOR);
}

function getDetailSection() {
  return document.querySelector(ISSUE_DETAIL_SELECTOR);
}

function getTemplateContent(selector) {
  const template = document.querySelector(selector);
  if (!template) {
    return null;
  }
  if (template.content && typeof template.content.cloneNode === "function") {
    return template.content.cloneNode(true);
  }
  const fragment = document.createDocumentFragment();
  const wrapper = document.createElement("div");
  wrapper.innerHTML = template.innerHTML;
  while (wrapper.firstChild) {
    fragment.appendChild(wrapper.firstChild);
  }
  return fragment;
}

function renderDetailPlaceholder(detail, message) {
  if (!detail) {
    return;
  }
  const fragment = getTemplateContent(DETAIL_PLACEHOLDER_TEMPLATE_SELECTOR);
  if (fragment) {
    detail.innerHTML = "";
    detail.appendChild(fragment);
    if (message) {
      const note = detail.querySelector(".ui-placeholder p");
      if (note) {
        note.textContent = message;
      }
    }
    return;
  }
  const text =
    typeof message === "string" && message.trim()
      ? message.trim()
      : "Choose an item from the list to inspect details.";
  detail.innerHTML = `<div class="ui-placeholder"><h2>Select an issue</h2><p>${text}</p></div>`;
}

const handleIssueDeleted = (detail = {}) => {
  const issueId = detail && detail.issueId ? String(detail.issueId) : "";
  if (!issueId) {
    return;
  }

  if (typeof document !== "undefined") {
    const row = document.querySelector(
      `[data-role='issue-row'][data-issue-id='${escapeForSelector(issueId)}']`,
    );
    if (row && typeof row.remove === "function") {
      row.remove();
    }
  }

  const shellState = globalThis.bdShellState;
  if (
    shellState &&
    typeof shellState.getSelection === "function" &&
    typeof shellState.setSelection === "function"
  ) {
    const selection = shellState.getSelection();
    if (Array.isArray(selection) && selection.includes(issueId)) {
      shellState.setSelection(selection.filter((id) => id !== issueId));
    }
  }

  const detailSection = getDetailSection();
  if (
    detailSection &&
    detailSection.dataset &&
    detailSection.dataset.issueId === issueId
  ) {
    detailSection.removeAttribute("data-issue-id");
    renderDetailPlaceholder(
      detailSection,
      "Issue deleted. Select another item to continue.",
    );
  }

  const source = detail && detail.source ? detail.source : "";
  let suppressToast = false;
  if (source === "issue-delete") {
    rememberLocalDeletion(issueId);
  } else if (source === "sse") {
    suppressToast = wasRecentlyDeleted(issueId);
  }

  if (!suppressToast && typeof showGlobalToast === "function") {
    const message =
      source === "issue-delete"
        ? `Deleted ${issueId}.`
        : `Issue ${issueId} was deleted.`;
    showGlobalToast(message);
  }

  scheduleIssueListRefresh(lastKnownFilters, { immediate: true });
};

const handleDetailCleared = (detail = {}) => {
  const issueId =
    detail && typeof detail.issueId === "string"
      ? detail.issueId
      : detail && detail.issueId
      ? String(detail.issueId)
      : "";

  const section = getDetailSection();
  if (section) {
    const currentId = section.dataset ? section.dataset.issueId || "" : "";
    if (!issueId || !currentId || currentId === issueId) {
      section.removeAttribute("data-issue-id");
      renderDetailPlaceholder(
        section,
        "Issue marked as done. Select another item to continue."
      );
    }
  }

  let selectionCleared = false;
  if (shellState && typeof shellState.clearSelection === "function") {
    shellState.clearSelection();
    selectionCleared = true;
  }

  if (!selectionCleared) {
    if (multiSelect && typeof multiSelect.clear === "function") {
      const selection = multiSelect.clear();
      syncSelectionToDom(selection);
    } else {
      syncSelectionToDom([]);
    }
  }
};

const installDetailClearHandler = () => {
  if (typeof document === "undefined") {
    return;
  }
  const root = document.body || document;
  if (!root || typeof root.addEventListener !== "function") {
    return;
  }
  root.addEventListener("issue:detail-clear", (event) => {
    handleDetailCleared(event?.detail || {});
  });
};

const installDetailSavedHandler = () => {
  if (typeof document === "undefined") {
    return;
  }
  const root = document.body || document;
  if (!root || typeof root.addEventListener !== "function") {
    return;
  }
  root.addEventListener("issue:detail-saved", (event) => {
    const detail = event?.detail || {};
    const field =
      typeof detail.field === "string"
        ? detail.field.trim().toLowerCase()
        : "";
    let message = "Issue updated.";
    if (field === "description") {
      message = "Description updated.";
    } else if (field === "notes") {
      message = "Notes updated.";
    } else if (typeof detail.label === "string" && detail.label.trim()) {
      message = `${detail.label.trim()} updated.`;
    }
    if (typeof showGlobalToast === "function") {
      showGlobalToast(message, { duration: 4000 });
    }
  });
};

function cancelDetailLoad() {
  if (detailLoadController) {
    detailLoadController.abort();
    detailLoadController = null;
  }
}

function ensureBulkPanel(selection) {
  const detail = getDetailSection();
  if (!detail) {
    return null;
  }
  const count = Array.isArray(selection) ? selection.length : 0;
  if (detail.dataset.mode !== "bulk") {
    cancelDetailLoad();
    detail.dataset.mode = "bulk";
    detail.removeAttribute("data-issue-id");
    detail.innerHTML = "";
    const fragment = getTemplateContent(BULK_TEMPLATE_SELECTOR);
    if (fragment) {
      detail.appendChild(fragment);
    } else {
      detail.innerHTML =
        '<div class="ui-placeholder"><h2>Bulk editing unavailable</h2><p>Unable to render the bulk editor.</p></div>';
    }
  }
  if (count > 0) {
    detail.dataset.bulkCount = String(count);
  } else {
    detail.removeAttribute("data-bulk-count");
  }
  return detail.querySelector(BULK_TOOLBAR_SELECTOR);
}

function getActiveIssueId() {
  if (
    issueListRoot &&
    issueListRoot.dataset &&
    issueListRoot.dataset.activeIssueId
  ) {
    return issueListRoot.dataset.activeIssueId;
  }
  const activeRow = document.querySelector(
    `${ISSUE_ROW_SELECTOR}.is-active`
  );
  return activeRow?.dataset?.issueId || "";
}

async function loadIssueDetail(issueId) {
  const detail = getDetailSection();
  if (!detail) {
    return;
  }

  cancelDetailLoad();
  detail.dataset.mode = "detail";
  detail.removeAttribute("data-bulk-count");

  if (!issueId) {
    detail.removeAttribute("data-issue-id");
    renderDetailPlaceholder(detail);
    return;
  }

  detail.removeAttribute("data-issue-id");
  renderDetailPlaceholder(detail, "Loading issue details...");

  const controller = new AbortController();
  detailLoadController = controller;

  try {
    const response = await fetch(
      `/fragments/issue?id=${encodeURIComponent(issueId)}`,
      {
        headers: {
          "HX-Request": "true",
        },
        signal: controller.signal,
      }
    );
    if (!response.ok) {
      throw new Error(`HTTP ${response.status}`);
    }
    const html = await response.text();
    if (
      controller.signal.aborted ||
      detail.dataset.mode === "bulk"
    ) {
      return;
    }
    detail.innerHTML = html;
    detail.dataset.issueId = issueId;
  } catch (error) {
    if (
      controller.signal.aborted ||
      detail.dataset.mode === "bulk"
    ) {
      return;
    }
    renderDetailPlaceholder(
      detail,
      "Issue details are unavailable right now."
    );
    if (typeof console !== "undefined" && console.warn) {
      console.warn("[bd-ui] issue detail load failed", error);
    }
  } finally {
    if (detailLoadController === controller) {
      detailLoadController = null;
    }
  }
}

function hideBulkPanel(selection) {
  const detail = getDetailSection();
  if (!detail || detail.dataset.mode !== "bulk") {
    return;
  }
  detail.dataset.mode = "detail";
  detail.removeAttribute("data-bulk-count");
  detail.removeAttribute("data-issue-id");

  const normalized = Array.isArray(selection) ? selection : [];
  const nextId =
    normalized.length === 1 ? normalized[0] : getActiveIssueId();

  if (nextId) {
    loadIssueDetail(nextId);
  } else {
    cancelDetailLoad();
    renderDetailPlaceholder(detail);
  }
}

function getHelpOverlay() {
  if (typeof document === "undefined") {
    return null;
  }
  return document.querySelector(HELP_OVERLAY_SELECTOR);
}

function isHelpOverlayOpen() {
  const overlay = getHelpOverlay();
  return !!(overlay && !overlay.hidden);
}

function openHelpOverlay() {
  const overlay = getHelpOverlay();
  if (!overlay || !overlay.hidden) {
    return;
  }
  helpOverlayPreviousFocus = null;
  if (
    typeof document !== "undefined" &&
    document.activeElement &&
    typeof document.activeElement.focus === "function"
  ) {
    helpOverlayPreviousFocus = document.activeElement;
  }
  overlay.hidden = false;
  overlay.dataset.state = "open";
  if (document.body) {
    document.body.dataset.helpOpen = "true";
  }
  const closeButton = overlay.querySelector(HELP_CLOSE_SELECTOR);
  if (closeButton && typeof closeButton.focus === "function") {
    closeButton.focus();
  }
}

function closeHelpOverlay(options = {}) {
  const overlay = getHelpOverlay();
  if (!overlay || overlay.hidden) {
    return;
  }
  overlay.hidden = true;
  delete overlay.dataset.state;
  if (document.body && document.body.dataset.helpOpen) {
    delete document.body.dataset.helpOpen;
  }
  if (
    !options.suppressRestore &&
    helpOverlayPreviousFocus &&
    typeof helpOverlayPreviousFocus.focus === "function" &&
    !overlay.contains(helpOverlayPreviousFocus)
  ) {
    try {
      helpOverlayPreviousFocus.focus();
    } catch {
      /* ignore focus restore errors */
    }
  }
  helpOverlayPreviousFocus = null;
}

function isQuestionShortcut(event) {
  if (!event || typeof event.key !== "string") {
    return false;
  }
  if (event.key === "?") {
    return true;
  }
  return event.key === "/" && event.shiftKey === true;
}

function initializeHelpOverlay() {
  const overlay = getHelpOverlay();
  if (!overlay || overlay.dataset.bound === "true") {
    return;
  }

  overlay.addEventListener("click", (event) => {
    if (event.target === overlay || event.target?.dataset?.role === "help-close") {
      event.preventDefault?.();
      closeHelpOverlay();
    }
  });

  const closeButton = overlay.querySelector(HELP_CLOSE_SELECTOR);
  if (closeButton && typeof closeButton.addEventListener === "function") {
    closeButton.addEventListener("click", (event) => {
      event.preventDefault?.();
      closeHelpOverlay();
    });
  }

  overlay.dataset.bound = "true";
}

function applySelectionToDom(selection) {
  const root = issueListRoot || getIssueListElement();
  if (!root || typeof root.querySelectorAll !== "function") {
    return;
  }
  const normalized = Array.isArray(selection) ? selection : [];
  const selected = new Set(normalized);

  root.querySelectorAll("[data-issue-id]").forEach((item) => {
    const issueId = item?.dataset?.issueId;
    const isSelected = issueId ? selected.has(issueId) : false;
    item.dataset.selected = isSelected ? "true" : "false";
    const checkbox = item.querySelector(ISSUE_SELECT_SELECTOR);
    if (checkbox) {
      checkbox.checked = isSelected;
      checkbox.setAttribute("aria-checked", isSelected ? "true" : "false");
    }
  });

  let activeId = normalized.length > 0 ? normalized[0] : "";
  if (!activeId) {
    activeId = root.dataset?.activeIssueId || "";
  }
  if (!activeId) {
    const existingActive = root.querySelector(
      `${ISSUE_ROW_SELECTOR}.is-active`
    );
    activeId = existingActive?.dataset?.issueId || "";
  }

  const rows = root.querySelectorAll(ISSUE_ROW_SELECTOR);
  rows.forEach((row) => {
    const issueId = row?.dataset?.issueId || "";
    const isActive = Boolean(activeId) && issueId === activeId;
    row.classList.toggle("is-active", isActive);
    row.setAttribute("aria-selected", isActive ? "true" : "false");
    row.setAttribute("tabindex", isActive ? "0" : "-1");
  });

  if (activeId) {
    root.dataset.activeIssueId = activeId;
  } else if (root.dataset && root.dataset.activeIssueId) {
    delete root.dataset.activeIssueId;
  }
}

function setBulkMessage(toolbar, message, tone) {
  if (!toolbar) {
    return;
  }
  const messageEl = toolbar.querySelector(BULK_MESSAGE_SELECTOR);
  if (!messageEl) {
    return;
  }
  const text = typeof message === "string" ? message.trim() : "";
  if (!text) {
    messageEl.hidden = true;
    messageEl.textContent = "";
    toolbar.removeAttribute("data-bulk-tone");
    return;
  }
  messageEl.hidden = false;
  messageEl.textContent = text;
  if (tone) {
    toolbar.dataset.bulkTone = tone;
  } else {
    toolbar.removeAttribute("data-bulk-tone");
  }
}

function updateBulkToolbar(selection) {
  const root = issueListRoot || getIssueListElement();
  if (!root) {
    return;
  }
  const count = Array.isArray(selection) ? selection.length : 0;
  let toolbar = null;
  if (count > 1) {
    toolbar = ensureBulkPanel(selection);
  } else {
    hideBulkPanel(selection || []);
  }
  if (!toolbar) {
    toolbar = findBulkToolbar(root);
  }
  if (!toolbar) {
    return;
  }
  bindBulkToolbar(toolbar);
  toolbar.hidden = count <= 1;
  toolbar.dataset.selectedCount = String(count);
  const countEl = toolbar.querySelector(BULK_COUNT_SELECTOR);
  if (countEl) {
    countEl.textContent =
      count === 1 ? "1 issue selected" : `${count} issues selected`;
  }
  const statusValue =
    toolbar.querySelector(BULK_STATUS_SELECTOR)?.value.trim() || "";
  const priorityControl = toolbar.querySelector(BULK_PRIORITY_SELECTOR);
  const priorityRaw =
    priorityControl && typeof priorityControl.value === "string"
      ? priorityControl.value.trim()
      : "";
  const hasPriority = priorityRaw !== "";
  const applyButton = toolbar.querySelector("[data-action='bulk-apply']");
  if (applyButton) {
    applyButton.disabled = count <= 1 || (!statusValue && !hasPriority);
  }
  if (count <= 1) {
    setBulkMessage(toolbar, "", "");
  }
}

function syncSelectionToDom(selection) {
  const snapshot = Array.isArray(selection)
    ? selection
    : multiSelect && typeof multiSelect.getSelection === "function"
      ? multiSelect.getSelection()
      : [];
  applySelectionToDom(snapshot);
  updateBulkToolbar(snapshot);
}

async function handleBulkSubmit(event) {
  event.preventDefault();
  const toolbar =
    event.currentTarget?.closest?.(BULK_TOOLBAR_SELECTOR) || event.currentTarget;
  if (!toolbar || !multiSelect || typeof multiSelect.getSelection !== "function") {
    return;
  }
  const selection = multiSelect.getSelection();
  if (selection.length <= 1) {
    setBulkMessage(
      toolbar,
      "Select more than one issue to update.",
      "error"
    );
    return;
  }
  const statusSelect = toolbar.querySelector(BULK_STATUS_SELECTOR);
  const prioritySelect = toolbar.querySelector(BULK_PRIORITY_SELECTOR);
  const statusValue =
    statusSelect && typeof statusSelect.value === "string"
      ? statusSelect.value.trim()
      : "";
  const priorityValue =
    prioritySelect && typeof prioritySelect.value === "string"
      ? prioritySelect.value.trim()
      : "";
  const hasStatus = !!statusValue;
  const hasPriority = priorityValue !== "";
  if (!hasStatus && !hasPriority) {
    setBulkMessage(toolbar, "Choose a status or priority to apply.", "error");
    return;
  }
  let priority = null;
  if (hasPriority) {
    const parsed = Number(priorityValue);
    if (Number.isNaN(parsed) || parsed < 0 || parsed > 4) {
      setBulkMessage(toolbar, "Priority must be between 0 and 4.", "error");
      return;
    }
    priority = parsed;
  }

  toolbar.dataset.state = "pending";
  setBulkMessage(toolbar, "Applying updates...", "info");

  const payload = {
    ids: selection,
    action: {},
  };
  if (hasStatus) {
    payload.action.status = statusValue;
  }
  if (hasPriority) {
    payload.action.priority = priority;
  }

  try {
    const response = await fetch("/api/issues/bulk", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(payload),
    });
    let data = null;
    if (response.ok) {
      try {
        data = await response.json();
      } catch {
        data = null;
      }
    }
    if (!response.ok) {
      const errorMessage =
        (data &&
          typeof data.error === "string" &&
          data.error.trim()) ||
        `Bulk update failed (HTTP ${response.status}).`;
      throw new Error(errorMessage);
    }
    const results = Array.isArray(data?.results) ? data.results : [];
    const failures = results.filter((result) => !result.success);
    if (failures.length) {
      const message =
        (failures[0] && failures[0].error) ||
        "One or more operations failed.";
      throw new Error(message);
    }
    const updatedCount = results.length || selection.length;
    const summaryParts = [];
    if (hasStatus) {
      summaryParts.push(`status ${toStatusLabel(statusValue)}`);
    }
    if (hasPriority) {
      summaryParts.push(`priority P${priority}`);
    }
    const summary =
      summaryParts.length > 0 ? summaryParts.join(" & ") : "updated";
    setBulkMessage(
      toolbar,
      `${updatedCount} issue${updatedCount === 1 ? "" : "s"} updated (${summary}).`,
      "success"
    );
  } catch (error) {
    const message =
      error instanceof Error ? error.message : String(error || "Bulk update failed.");
    setBulkMessage(toolbar, message, "error");
  } finally {
    toolbar.dataset.state = "ready";
    updateBulkToolbar(multiSelect ? multiSelect.getSelection() : []);
  }
}

function bindBulkToolbar(toolbar) {
  if (!toolbar || toolbar.dataset.bound === "true") {
    return;
  }
  toolbar.addEventListener("submit", handleBulkSubmit);
  toolbar.addEventListener("change", (event) => {
    const target = event.target;
    if (
      target &&
      (target.matches(BULK_STATUS_SELECTOR) ||
        target.matches(BULK_PRIORITY_SELECTOR))
    ) {
      setBulkMessage(toolbar, "", "");
      updateBulkToolbar(multiSelect ? multiSelect.getSelection() : []);
    }
  });
  toolbar.addEventListener("click", (event) => {
    const action = event.target.closest("[data-action]");
    if (!action) {
      return;
    }
    const actionName = action.dataset.action;
    if (actionName === "bulk-clear") {
      event.preventDefault();
      if (multiSelect && typeof multiSelect.clear === "function") {
        multiSelect.clear();
      }
      const statusEl = toolbar.querySelector(BULK_STATUS_SELECTOR);
      const priorityEl = toolbar.querySelector(BULK_PRIORITY_SELECTOR);
      if (statusEl) {
        statusEl.value = "";
      }
      if (priorityEl) {
        priorityEl.value = "";
      }
      setBulkMessage(toolbar, "", "");
      updateBulkToolbar([]);
    }
  });
  toolbar.dataset.bound = "true";
}

function initializeMultiSelect() {
  const root = getIssueListElement();
  if (!root) {
    issueListRoot = null;
    return;
  }
  issueListRoot = root;
  const ids = collectIssueIds(root);
  if (
    !multiSelect &&
    globalThis.bdMultiSelect &&
    typeof globalThis.bdMultiSelect.createModel === "function"
  ) {
    multiSelect = globalThis.bdMultiSelect.createModel({
      items: ids,
      store: shellState,
    });
    if (multiSelect && typeof multiSelect.subscribe === "function") {
      multiSelectSubscription = multiSelect.subscribe((selection) => {
        syncSelectionToDom(selection);
      });
    }
  } else if (multiSelect && typeof multiSelect.setItems === "function") {
    multiSelect.setItems(ids);
  }
  syncSelectionToDom(multiSelect ? multiSelect.getSelection() : []);
  const toolbar = findBulkToolbar(root);
  bindBulkToolbar(toolbar);
}

function handleIssueListClick(event) {
  if (!issueListRoot || !multiSelect) {
    return;
  }
  const checkbox = event.target.closest
    ? event.target.closest(ISSUE_SELECT_SELECTOR)
    : null;
  if (checkbox) {
    const item = checkbox.closest("[data-issue-id]");
    const issueId = item?.dataset?.issueId;
    if (!issueId) {
      return;
    }
    event.stopPropagation();
    if (event.shiftKey) {
      event.preventDefault();
      const selection = multiSelect.toggle(issueId, { range: true });
      syncSelectionToDom(selection);
    }
    return;
  }
  const row = event.target.closest
    ? event.target.closest(ISSUE_ROW_SELECTOR)
    : null;
  if (!row || !issueListRoot.contains(row)) {
    return;
  }
  const issueId = row.dataset?.issueId;
  if (!issueId) {
    return;
  }
  if (event.metaKey || event.ctrlKey || event.shiftKey) {
    event.preventDefault();
    event.stopPropagation();
    const selection = multiSelect.toggle(issueId, { range: event.shiftKey });
    syncSelectionToDom(selection);
    return;
  }
  if (typeof multiSelect.setSelection === "function") {
    const selection = multiSelect.setSelection([issueId]);
    syncSelectionToDom(selection);
  }
  dispatchCustomEvent("issue:detail-loaded", {
    issueId,
    source: "issue-list",
  });
}

function handleIssueListChange(event) {
  if (!multiSelect) {
    return;
  }
  const checkbox = event.target.closest
    ? event.target.closest(ISSUE_SELECT_SELECTOR)
    : null;
  if (!checkbox) {
    return;
  }
  const item = checkbox.closest("[data-issue-id]");
  const issueId = item?.dataset?.issueId;
  if (!issueId || typeof multiSelect.getSelection !== "function") {
    return;
  }
  const current = multiSelect.getSelection();
  const isSelected = current.includes(issueId);
  let selection = current;
  if (checkbox.checked && !isSelected) {
    selection = multiSelect.toggle(issueId);
  } else if (!checkbox.checked && isSelected) {
    selection = multiSelect.toggle(issueId);
  }
  syncSelectionToDom(selection);
}

function handleIssueListKeydown(event) {
  if (!multiSelect || typeof multiSelect.handleKey !== "function") {
    return false;
  }
  const row = event.target?.closest?.(ISSUE_ROW_SELECTOR);
  const issueId = row?.dataset?.issueId;
  if (issueId && multiSelect.handleKey(event, issueId)) {
    syncSelectionToDom(multiSelect.getSelection());
    return true;
  }
  return false;
}

const bindStatusAppliedListener = () => {
  if (typeof document === "undefined") {
    return;
  }
  const target = document.body || document;
  if (!target || typeof target.addEventListener !== "function") {
    return;
  }
  target.addEventListener("issue:status-applied", (event) => {
    updateIssueStatusDom(event?.detail || {});
  });
};

bindStatusAppliedListener();

const focusIssueList = () => {
  const issueList = document.querySelector(ISSUE_LIST_SELECTOR);
  if (!issueList) {
    return;
  }

  const focus = () => {
    try {
      issueList.focus({ preventScroll: true });
    } catch {
      issueList.focus();
    }
  };

  if (document.readyState === "complete") {
    focus();
    setTimeout(focus, 120);
  } else {
    requestAnimationFrame(focus);
    window.addEventListener(
      "load",
      () => {
        focus();
        setTimeout(focus, 60);
      },
      { once: true }
    );
  }
};


const formatTimestamp = (isoString) => {
  if (!isoString) {
    return "";
  }
  try {
    const date = new Date(isoString);
    return Number.isNaN(date.getTime()) ? "" : date.toLocaleString();
  } catch {
    return "";
  }
};

const applySlashNLineBreaks = (root) => {
  if (!root || typeof root.querySelectorAll !== "function") {
    return;
  }
  const splitRegex = /\r\n|\\n|\n/g;
  const processElement = (element) => {
    if (!element || typeof element.childNodes === "undefined") {
      return;
    }
    const nodes = Array.from(element.childNodes);
    nodes.forEach((node) => {
      if (!node) {
        return;
      }
      if (node.nodeType === Node.TEXT_NODE) {
        const raw = node.textContent;
        if (typeof raw !== "string") {
          return;
        }
        if (raw.trim() === "") {
          return;
        }
        if (raw.indexOf("\\n") === -1 && raw.indexOf("\n") === -1) {
          return;
        }
        const parts = raw.split(splitRegex);
        if (parts.length <= 1) {
          return;
        }
        const frag = document.createDocumentFragment();
        parts.forEach((part, index) => {
          frag.appendChild(document.createTextNode(part));
          if (index < parts.length - 1) {
            frag.appendChild(document.createElement("br"));
          }
        });
        node.parentNode.replaceChild(frag, node);
        return;
      }
      if (node.nodeType === Node.ELEMENT_NODE) {
        processElement(node);
      }
    });
  };
  const targets = root.querySelectorAll('[data-linebreak="slash-n"]');
  targets.forEach((element) => {
    processElement(element);
  });
};

const LIVE_UPDATE_LOCK_KEY = "bd-event-stream-owner";
const LIVE_UPDATE_LOCK_TTL = 15000;
const LIVE_UPDATE_HEARTBEAT_MS = 4000;
const LIVE_UPDATE_CHANNEL_NAME = "bd-event-stream";

const liveUpdateCoordinator = createLiveUpdateCoordinator();
const liveUpdateMessenger = createLiveUpdateMessenger(
  typeof BroadcastChannel === "function"
    ? new BroadcastChannel(LIVE_UPDATE_CHANNEL_NAME)
    : null,
  liveUpdateCoordinator.id,
);

const handleLiveUpdateEvent = () => {
  hideLiveUpdateBanner();
  scheduleIssueListRefresh(lastKnownFilters, { delay: 120 });
};

const handleLiveUpdateState = (state) => {
  if (state === "open" || state === "connecting") {
    scheduleIssueListRefresh(lastKnownFilters, { immediate: state === "open" });
  }
  if (state === "open") {
    hideLiveUpdateBanner();
  } else if (state === "connecting") {
    showLiveUpdateBanner("Connecting to live updates...");
  } else if (state === "waiting") {
    showLiveUpdateBanner("Live updates paused - retrying connection.");
  } else if (state === "error") {
    showLiveUpdateBanner(
      "Live updates unavailable - waiting for the daemon to respond.",
    );
  } else if (state === "unsupported") {
    showLiveUpdateBanner("Live updates unavailable in this browser.");
  }
};

function generateLiveUpdateId() {
  if (typeof crypto !== "undefined" && crypto?.randomUUID) {
    return crypto.randomUUID();
  }
  return `tab-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function hasLocalStorageSupport() {
  if (typeof window === "undefined" || !window.localStorage) {
    return false;
  }
  try {
    const probeKey = "__bd_live_update_probe__";
    window.localStorage.setItem(probeKey, probeKey);
    window.localStorage.removeItem(probeKey);
    return true;
  } catch (error) {
    return false;
  }
}

function createLiveUpdateCoordinator() {
  if (typeof window === "undefined") {
    return {
      id: "server",
      isLeader: () => true,
      subscribe(listener) {
        if (typeof listener === "function") {
          listener("leader");
        }
        return () => {};
      },
    };
  }

  const storage = hasLocalStorageSupport() ? window.localStorage : null;
  const tabId = generateLiveUpdateId();
  let role = storage ? "unknown" : "leader";
  let heartbeatTimer = null;
  let recheckTimer = null;
  const listeners = new Set();

  const notify = (nextRole) => {
    if (role === nextRole) {
      return;
    }
    role = nextRole;
    listeners.forEach((listener) => {
      if (typeof listener === "function") {
        try {
          listener(role);
        } catch (error) {
          // Ignore subscriber errors.
        }
      }
    });
  };

  const readLock = () => {
    if (!storage) {
      return null;
    }
    try {
      const raw = storage.getItem(LIVE_UPDATE_LOCK_KEY);
      return raw ? JSON.parse(raw) : null;
    } catch (error) {
      return null;
    }
  };

  const writeLock = () => {
    if (!storage) {
      return;
    }
    try {
      storage.setItem(
        LIVE_UPDATE_LOCK_KEY,
        JSON.stringify({ id: tabId, ts: Date.now() }),
      );
    } catch (error) {
      // Ignore storage errors.
    }
  };

  const clearLock = () => {
    if (!storage) {
      return;
    }
    try {
      const entry = readLock();
      if (entry && entry.id === tabId) {
        storage.removeItem(LIVE_UPDATE_LOCK_KEY);
      }
    } catch (error) {
      // Ignore storage errors.
    }
  };

  const isStale = (entry) => {
    if (!entry || typeof entry.ts !== "number") {
      return true;
    }
    return Date.now() - entry.ts > LIVE_UPDATE_LOCK_TTL;
  };

  const stopHeartbeat = () => {
    if (heartbeatTimer) {
      clearInterval(heartbeatTimer);
      heartbeatTimer = null;
    }
  };

  const ensureHeartbeat = () => {
    if (heartbeatTimer || !storage) {
      return;
    }
    heartbeatTimer = setInterval(() => {
      const entry = readLock();
      if (!entry || entry.id !== tabId) {
        stopHeartbeat();
        notify("follower");
        return;
      }
      writeLock();
    }, LIVE_UPDATE_HEARTBEAT_MS);
  };

  const evaluate = () => {
    if (!storage) {
      notify("leader");
      return;
    }
    const entry = readLock();
    if (!entry || isStale(entry)) {
      writeLock();
      ensureHeartbeat();
      notify("leader");
      return;
    }
    if (entry.id === tabId) {
      ensureHeartbeat();
      notify("leader");
      return;
    }
    stopHeartbeat();
    notify("follower");
  };

  if (storage) {
    window.addEventListener("storage", (event) => {
      if (event?.key === LIVE_UPDATE_LOCK_KEY) {
        evaluate();
      }
    });
    window.addEventListener("beforeunload", () => {
      clearLock();
      stopHeartbeat();
    });
    if (!recheckTimer) {
      recheckTimer = setInterval(() => {
        const entry = readLock();
        if (!entry || isStale(entry)) {
          evaluate();
        }
      }, LIVE_UPDATE_HEARTBEAT_MS * 2);
    }
  }

  evaluate();

  return {
    id: tabId,
    isLeader: () => role === "leader",
    subscribe(listener) {
      if (typeof listener === "function") {
        listeners.add(listener);
        listener(role);
        return () => listeners.delete(listener);
      }
      return () => {};
    },
  };
}

function createLiveUpdateMessenger(channel, senderId) {
  const listeners = new Set();

  if (channel && typeof channel.addEventListener === "function") {
    channel.addEventListener("message", (event) => {
      const data = event?.data;
      if (!data || data.senderId === senderId) {
        return;
      }
      listeners.forEach((listener) => {
        if (typeof listener === "function") {
          try {
            listener(data);
          } catch (error) {
            // Ignore subscriber errors.
          }
        }
      });
    });
  }

  return {
    broadcast(payload) {
      if (!channel || !payload) {
        return;
      }
      try {
        channel.postMessage({
          ...payload,
          senderId,
          timestamp: Date.now(),
        });
      } catch (error) {
        // Ignore broadcast failures.
      }
    },
    subscribe(listener) {
      if (typeof listener === "function") {
        listeners.add(listener);
        return () => listeners.delete(listener);
      }
      return () => {};
    },
    isMirroringEnabled() {
      return Boolean(channel);
    },
  };
}

const attachEventStream = () => {
  const streamAPI = globalThis.bdEventStream;
  if (!streamAPI || typeof streamAPI.init !== "function") {
    return;
  }

  const options = {};
  const body = typeof document !== "undefined" ? document.body : null;
  const streamUrl =
    (body && body.dataset && body.dataset.eventStream
      ? body.dataset.eventStream.trim()
      : "") || "/events";

  options.url = streamUrl;
  options.onEvent = () => {
    handleLiveUpdateEvent();
    liveUpdateMessenger.broadcast({ type: "events:update" });
  };
  options.onStateChange = (state) => {
    handleLiveUpdateState(state);
    liveUpdateMessenger.broadcast({ type: "events:state", state });
  };

  liveUpdateMessenger.subscribe((message) => {
    if (!message || typeof message.type !== "string") {
      return;
    }
    if (message.type === "events:update") {
      handleLiveUpdateEvent();
    } else if (message.type === "events:state" && message.state) {
      handleLiveUpdateState(message.state);
    }
  });

  const startStream = () => {
    streamAPI.init(options);
  };

  const stopStream = () => {
    if (typeof streamAPI.get === "function") {
      const controller = streamAPI.get();
      if (controller && typeof controller.stop === "function") {
        controller.stop();
      }
    }
  };

  liveUpdateCoordinator.subscribe((role) => {
    if (role === "leader") {
      hideLiveUpdateBanner();
      startStream();
      return;
    }
    if (role === "follower") {
      stopStream();
      if (!liveUpdateMessenger.isMirroringEnabled()) {
        showLiveUpdateBanner(
          "Live updates are active in another tab. This view will refresh when you interact.",
        );
      }
    }
  });
};

const installIssueDeletedHandler = () => {
  if (typeof document === "undefined") {
    return;
  }
  const root = document.body || document;
  if (!root || typeof root.addEventListener !== "function") {
    return;
  }
  root.addEventListener("issue:deleted", (event) => {
    handleIssueDeleted(event?.detail || {});
  });
};

const installEventsUpdateFallback = () => {
  if (typeof document === "undefined") {
    return;
  }
  const root = document.body || document;
  if (!root || typeof root.addEventListener !== "function") {
    return;
  }
  root.addEventListener("events:update", () => {
    const htmx = globalThis.htmx;
    if (htmx && typeof htmx.ajax === "function") {
      const listRows = document.querySelectorAll("[data-role='issue-list-rows']");
      let dispatched = false;
      listRows.forEach((el) => {
        const url = el.getAttribute("hx-get");
        if (!url) {
          return;
        }
        const targetSelector = el.getAttribute("hx-target");
        let targetEl = el;
        if (targetSelector) {
          const resolved = document.querySelector(targetSelector);
          if (resolved) {
            targetEl = resolved;
          }
        }
        const swap = el.getAttribute("hx-swap") || "innerHTML";
        htmx.ajax("GET", url, {
          target: targetEl,
          swap,
        });
        dispatched = true;
      });
      if (dispatched) {
        return;
      }
    }

    scheduleIssueListRefresh(lastKnownFilters, { delay: 120, force: true });
  });
};

const parseRetryAfterHeader = (value) => {
  if (typeof value !== "string") {
    return Number.NaN;
  }
  const trimmed = value.trim();
  if (!trimmed) {
    return Number.NaN;
  }
  const asNumber = Number(trimmed);
  if (!Number.isNaN(asNumber)) {
    return asNumber;
  }
  const asDate = Date.parse(trimmed);
  if (Number.isNaN(asDate)) {
    return Number.NaN;
  }
  return Math.max(0, Math.round((asDate - Date.now()) / 1000));
};

const extractErrorPayload = (xhr) => {
  const fallback = {
    message: "Live data unavailable.",
    details: "",
    retryAfter: Number.NaN,
  };
  if (!xhr) {
    return fallback;
  }

  const headerValue =
    typeof xhr.getResponseHeader === "function"
      ? xhr.getResponseHeader("Retry-After")
      : null;
  const retryAfter = parseRetryAfterHeader(headerValue);

  const text = xhr.responseText || "";
  if (!text) {
    return { ...fallback, retryAfter };
  }

  try {
    const payload = JSON.parse(text);
    const message =
      typeof payload.error === "string" && payload.error.trim()
        ? payload.error.trim()
        : fallback.message;
    const details =
      typeof payload.details === "string" && payload.details.trim()
        ? payload.details.trim()
        : "";
    return { message, details, retryAfter };
  } catch {
    const trimmed = text.trim();
    if (trimmed) {
      return { ...fallback, details: trimmed, retryAfter };
    }
  }

  return { ...fallback, retryAfter };
};

const handleHtmxResponseError = (event) => {
  const detail = event?.detail || {};
  const xhr = detail.xhr || null;
  const status = Number(detail.status ?? xhr?.status ?? 0);
  if (!Number.isFinite(status) || status < 500) {
    return;
  }

  const payload = extractErrorPayload(xhr);
  const combinedMessage = payload.details
    ? `${payload.message} ${payload.details}`
    : payload.message;

  showGlobalToast(combinedMessage);
  showLiveUpdateBanner(payload.details || payload.message);

  if (
    Number.isFinite(payload.retryAfter) &&
    payload.retryAfter > 0 &&
    payload.retryAfter <= 60 &&
    typeof setTimeout === "function"
  ) {
    setTimeout(() => {
      scheduleIssueListRefresh(lastKnownFilters, { immediate: true });
    }, payload.retryAfter * 1000);
  }
};

const installGlobalHtmxErrorHandler = () => {
  const handler = (event) => {
    try {
      handleHtmxResponseError(event);
    } catch (error) {
      if (typeof console !== "undefined" && console.warn) {
        console.warn("htmx response error handler failed", error);
      }
    }
  };

  if (globalThis.htmx && typeof globalThis.htmx.on === "function") {
    globalThis.htmx.on("htmx:responseError", handler);
  }
  if (
    typeof document !== "undefined" &&
    document.body &&
    typeof document.body.addEventListener === "function"
  ) {
    document.body.addEventListener("htmx:responseError", handler);
  }
};

document.addEventListener("keydown", (event) => {
  const overlayOpen = isHelpOverlayOpen();

  if (overlayOpen) {
    if (event.key === "Escape") {
      event.preventDefault?.();
      event.stopPropagation?.();
      closeHelpOverlay();
      return;
    }
    if (isQuestionShortcut(event)) {
      event.preventDefault?.();
      event.stopPropagation?.();
      closeHelpOverlay();
      return;
    }
    return;
  }

  if (!isQuestionShortcut(event)) {
    return;
  }

  if (
    shortcutUtils &&
    typeof shortcutUtils.shouldIgnoreEvent === "function" &&
    shortcutUtils.shouldIgnoreEvent(event)
  ) {
    return;
  }

  event.preventDefault?.();
  event.stopPropagation?.();
  openHelpOverlay();
});

window.bdShell = (config) => {
  const initialFilters =
    config && typeof config.initialFilters === "object"
      ? normalizeFilterCandidate(config.initialFilters)
      : normalizeFilterCandidate({
          status:
            config && typeof config.initialQueue === "string"
              ? config.initialQueue
              : "open",
        });
  const state = ensureShellState(initialFilters);
  return {
    filters: initialFilters,
    init() {
      if (typeof state.subscribe === "function") {
        this.unsubscribe = state.subscribe((filters) => {
          this.filters = normalizeFilterCandidate(filters);
        });
      }
    },
    destroy() {
      if (typeof this.unsubscribe === "function") {
        this.unsubscribe();
        this.unsubscribe = null;
      }
    },
  };
};

document.addEventListener("DOMContentLoaded", () => {
  const navigation = window.bdNavigation || {};
  const issueList = document.querySelector(ISSUE_LIST_SELECTOR);
  let navigator =
    issueList && typeof navigation.createIssueListNavigator === "function"
      ? navigation.createIssueListNavigator(issueList, {
          onSelectionChange(row) {
            if (row) {
              row.scrollIntoView({
                block: "nearest",
                inline: "nearest",
              });
            }
          },
        })
      : null;

  teardownSearchPanel = setupSearchPanel();

  initializeMultiSelect();
  initializeHelpOverlay();
  scheduleIssueListRefresh(lastKnownFilters, { immediate: true });

  document.body.addEventListener("htmx:beforeSwap", (event) => {
    const target = event?.detail?.target || event.target;

    if (shouldRestoreIssueListFocus(target)) {
      const issueList = document.querySelector(ISSUE_LIST_SELECTOR);
      const active = document.activeElement;
      pendingIssueListFocusRestore = !!(
        issueList &&
        active &&
        issueList.contains(active)
      );
    } else {
      pendingIssueListFocusRestore = false;
    }

    const detail = getDetailSection();
    if (!target || !detail) {
      return;
    }
    if (target === detail && detail.dataset.mode === "bulk") {
      event.preventDefault();
      if (event.detail) {
        event.detail.shouldSwap = false;
      }
    }
  });

  document.body.addEventListener("htmx:afterSwap", (event) => {
    const target = event?.detail?.target;
    if (!target) {
      return;
    }
    if (shouldRestoreIssueListFocus(target)) {
      if (navigator) {
        navigator.refresh();
      }
      initializeMultiSelect();
      if (pendingIssueListFocusRestore) {
        focusIssueList();
      }
      pendingIssueListFocusRestore = false;
      scheduleIssueListRefresh(lastKnownFilters, { immediate: true });
      return;
    }
    if (
      target.matches("[data-role='issue-detail']") ||
      target.matches("[data-testid='issue-detail']")
    ) {
      const issueId = target.dataset?.issueId || "";
      applySlashNLineBreaks(target);
      dispatchCustomEvent("issue:detail-loaded", {
        issueId,
        source: "htmx",
        target,
      });
      const activeRow = document.querySelector(
        "[data-role='issue-row'].is-active"
      );
      if (activeRow && typeof activeRow.focus === "function") {
        const focusRow = () => {
          try {
            activeRow.focus({ preventScroll: true });
          } catch {
            activeRow.focus();
          }
        };
        if (typeof setTimeout === "function") {
          setTimeout(focusRow, 0);
        } else {
          focusRow();
        }
      }
    }
  });

  if (issueList) {
    issueList.addEventListener(
      "focus",
      (event) => {
        const activeRow = document.querySelector(
          "[data-role='issue-row'].is-active"
        );
        if (
          activeRow &&
          activeRow !== document.activeElement &&
          typeof activeRow.focus === "function"
        ) {
          try {
            activeRow.focus({ preventScroll: true });
          } catch {
            activeRow.focus();
          }
        }
      },
      true
    );

    issueList.addEventListener("keydown", (event) => {
      if (handleIssueListKeydown(event)) {
        event.preventDefault();
        event.stopPropagation();
        return;
      }
      if (navigator && navigator.handleKey(event)) {
        event.preventDefault();
        event.stopPropagation();
      }
    });
  }

  document.body.addEventListener("click", handleQuickCreateTriggerClick);
  document.body.addEventListener("click", handleIssueListClick);
  document.body.addEventListener("change", handleIssueListChange);

  focusIssueList();
  installGlobalHtmxErrorHandler();
  attachEventStream();
  installDetailClearHandler();
  installDetailSavedHandler();
  installIssueDeletedHandler();
  installEventsUpdateFallback();
  console.info("Beads UI shell ready.");
});

window.addEventListener(
  "beforeunload",
  () => {
    if (typeof teardownSearchPanel === "function") {
      try {
        teardownSearchPanel();
      } catch {
        /* ignore teardown errors */
      }
    }
  },
  { once: true },
);
