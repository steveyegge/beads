"use strict";

(function (global) {
  const ISSUE_TYPES = [
    { id: "task", label: "Task" },
    { id: "feature", label: "Feature" },
    { id: "bug", label: "Bug" },
    { id: "chore", label: "Chore" },
    { id: "epic", label: "Epic" },
  ];

  const ISSUE_TYPE_SET = ISSUE_TYPES.reduce((acc, entry) => {
    acc[entry.id] = true;
    return acc;
  }, {});

  const PRIORITY_OPTIONS = [
    { value: 0, label: "P0 Critical" },
    { value: 1, label: "P1 High" },
    { value: 2, label: "P2 Medium" },
    { value: 3, label: "P3 Low" },
    { value: 4, label: "P4 Backlog" },
  ];

  const REGISTRY_KEY =
    typeof Symbol !== "undefined"
      ? Symbol.for("bdQuickCreateRegistry")
      : "__bdQuickCreateRegistry__";

  function getRegistry(globalObj) {
    if (!globalObj) {
      return null;
    }
    let registry = globalObj[REGISTRY_KEY];
    if (!registry) {
      registry = new Set();
      globalObj[REGISTRY_KEY] = registry;
    }
    return registry;
  }

  function registerQuickCreate(globalObj, controller) {
    const registry = getRegistry(globalObj);
    if (registry && typeof registry.add === "function") {
      registry.add(controller);
    }
    globalObj.__bdQuickCreateLast = controller;
  }

  function unregisterQuickCreate(globalObj, controller) {
    const registry = globalObj ? globalObj[REGISTRY_KEY] : null;
    if (registry && typeof registry.delete === "function") {
      registry.delete(controller);
    }
    if (globalObj && globalObj.__bdQuickCreateLast === controller) {
      globalObj.__bdQuickCreateLast = null;
    }
  }

  function getLatestQuickCreate(globalObj) {
    const registry = globalObj ? globalObj[REGISTRY_KEY] : null;
    if (registry && typeof registry.forEach === "function") {
      let latest = null;
      registry.forEach((controller) => {
        latest = controller;
      });
      if (latest) {
        return latest;
      }
    }
    return globalObj ? globalObj.__bdQuickCreateLast || null : null;
  }

  const shortcutUtils =
    (typeof global.bdShortcutUtils === "object" &&
      global.bdShortcutUtils !== null &&
      global.bdShortcutUtils) ||
    null;

  const DEFAULT_PRIORITY = 2;
  const DEFAULT_TYPE = "task";
  const PRIORITY_STORAGE_KEY = "bd:quick-create:priority";

  function normalizeTitle(value) {
    return typeof value === "string" ? value.trim() : "";
  }

  function normalizeDescription(value) {
    if (typeof value !== "string") {
      return "";
    }
    return value.replace(/\s+$/g, "");
  }

  function normalizeIssueId(value) {
    if (typeof value !== "string") {
      return "";
    }
    return value.trim();
  }

  function normalizeIssueType(value) {
    if (typeof value !== "string") {
      return DEFAULT_TYPE;
    }
    const normalized = value.trim().toLowerCase();
    return ISSUE_TYPE_SET[normalized] ? normalized : DEFAULT_TYPE;
  }

  function clampPriority(value) {
    if (typeof value === "string" && value.trim() !== "") {
      const parsed = Number(value);
      if (!Number.isNaN(parsed)) {
        value = parsed;
      }
    }
    if (typeof value !== "number" || Number.isNaN(value)) {
      return DEFAULT_PRIORITY;
    }
    if (value < 0) {
      return 0;
    }
    if (value > 4) {
      return 4;
    }
    return Math.round(value);
  }

  function readStoredPriority(globalObj) {
    try {
      const storage = globalObj && globalObj.localStorage;
      if (!storage || typeof storage.getItem !== "function") {
        return null;
      }
      const raw = storage.getItem(PRIORITY_STORAGE_KEY);
      if (typeof raw !== "string" || raw.trim() === "") {
        return null;
      }
      return clampPriority(raw);
    } catch {
      return null;
    }
  }

  function writeStoredPriority(globalObj, value) {
    try {
      const storage = globalObj && globalObj.localStorage;
      if (!storage || typeof storage.setItem !== "function") {
        return;
      }
      storage.setItem(PRIORITY_STORAGE_KEY, String(clampPriority(value)));
    } catch {
      // Ignore storage write failures.
    }
  }

  function defaultTransport(input, init) {
    if (typeof fetch === "function") {
      return fetch(input, init);
    }
    return Promise.reject(new Error("fetch unavailable"));
  }

  function getEventTarget(config) {
    if (config && config.eventTarget) {
      return config.eventTarget;
    }
    if (typeof document !== "undefined") {
      if (document.body && typeof document.body.addEventListener === "function") {
        return document.body;
      }
      if (typeof document.addEventListener === "function") {
        return document;
      }
    }
    return null;
  }

  function createCustomEvent(type, detail) {
    if (typeof CustomEvent === "function") {
      return new CustomEvent(type, { detail, bubbles: false });
    }
    if (typeof document !== "undefined" && typeof document.createEvent === "function") {
      const evt = document.createEvent("CustomEvent");
      evt.initCustomEvent(type, false, false, detail);
      return evt;
    }
    return null;
  }

  function defaultDispatch(type, detail) {
    if (typeof document === "undefined") {
      return;
    }
    const target = document.body || document;
    if (!target || typeof target.dispatchEvent !== "function") {
      return;
    }
    const evt = createCustomEvent(type, detail);
    if (evt) {
      target.dispatchEvent(evt);
    }
  }

  async function extractErrorMessage(response, fallback) {
    if (!response) {
      return fallback;
    }

    const readPayload = async (source) => {
      if (!source || typeof source.json !== "function") {
        return "";
      }
      try {
        const data = await source.json();
        if (data && typeof data.error === "string" && data.error.trim()) {
          return data.error.trim();
        }
        if (data && typeof data.message === "string" && data.message.trim()) {
          return data.message.trim();
        }
      } catch {
        try {
          if (typeof source.text === "function") {
            const text = await source.text();
            if (typeof text === "string" && text.trim()) {
              return text.trim();
            }
          }
        } catch {
          // Ignore secondary parse failures.
        }
      }
      return "";
    };

    if (typeof response.clone === "function") {
      const clone = response.clone();
      const message = await readPayload(clone);
      if (message) {
        return message;
      }
    } else {
      const message = await readPayload(response);
      if (message) {
        return message;
      }
    }

    if (response.status) {
      const statusText =
        typeof response.statusText === "string" && response.statusText.trim()
          ? response.statusText.trim()
          : "";
      if (statusText) {
        return `${fallback} (${statusText})`;
      }
      return `${fallback} (HTTP ${response.status})`;
    }

    return fallback;
  }

  function shouldIgnoreShortcut(event) {
    if (!event) {
      return true;
    }
    if (event.altKey || event.ctrlKey || event.metaKey) {
      return true;
    }
    if (
      shortcutUtils &&
      typeof shortcutUtils.shouldIgnoreEvent === "function" &&
      shortcutUtils.shouldIgnoreEvent(event)
    ) {
      return true;
    }
    if (!event.target) {
      return false;
    }
    const el = event.target;
    const tagName = (el.tagName || "").toLowerCase();
    if (tagName === "input" || tagName === "textarea" || tagName === "select") {
      return true;
    }
    if (el.isContentEditable) {
      return true;
    }
    return false;
  }

  function bdQuickCreate(config = {}) {
    const transport =
      typeof config.transport === "function" ? config.transport : defaultTransport;
    const dispatch =
      typeof config.dispatch === "function" ? config.dispatch : defaultDispatch;
    const eventTarget = getEventTarget(config);

    const defaultType = normalizeIssueType(config.defaultIssueType);
    const baseConfigPriority = clampPriority(
      Object.prototype.hasOwnProperty.call(config, "defaultPriority")
        ? config.defaultPriority
        : DEFAULT_PRIORITY
    );
    const storedPriority = readStoredPriority(global);
    let defaultPriority =
      typeof storedPriority === "number" ? storedPriority : baseConfigPriority;

    const state = {
      refs: {},
      detach: null,
      activeIssueId: normalizeIssueId(config.initialDiscoveredFrom),
      discoveredValue: "",
      discoveredTouched: false,
      pendingMode: "reset",
    };

    const component = {
      issueTypes: ISSUE_TYPES.map((entry) => ({ ...entry })),
      priorityOptions: PRIORITY_OPTIONS.map((entry) => ({ ...entry })),
      isOpen: false,
      isSubmitting: false,
      title: "",
      description: "",
      issueType: defaultType,
      priority: defaultPriority,
      message: "",
      error: "",

      init(refs) {
        state.refs = refs || {};
      },

      focusTitle() {
        const input = state.refs && state.refs.title;
        if (!input || typeof input.focus !== "function") {
          return;
        }
        const focus = () => {
          try {
            input.focus({ preventScroll: true });
          } catch {
            try {
              input.focus();
            } catch {
              // Ignore focus failure.
            }
          }
        };
        if (typeof requestAnimationFrame === "function") {
          requestAnimationFrame(focus);
        } else {
          setTimeout(focus, 0);
        }
      },

      open() {
        if (this.isOpen) {
          return;
        }
        if (typeof globalThis !== "undefined" && globalThis.bdShellState) {
          try {
            const selection =
              typeof globalThis.bdShellState.getSelection === "function"
                ? globalThis.bdShellState.getSelection()
                : [];
            if (Array.isArray(selection) && selection.length > 0) {
              applyActiveIssue(selection[0]);
            }
          } catch {
            // Ignore shell state retrieval errors.
          }
        }
        if (typeof document !== "undefined") {
          const detail = document.querySelector(
            "[data-testid='issue-detail'][data-issue-id]"
          );
          if (detail && detail.dataset && detail.dataset.issueId) {
            applyActiveIssue(detail.dataset.issueId);
          }
        }
        this.isOpen = true;
        this.isSubmitting = false;
        this.error = "";
        this.focusTitle();
      },

      close() {
        if (this.isSubmitting) {
          return;
        }
        this.isOpen = false;
      },

      toggle() {
        if (this.isOpen) {
          this.close();
        } else {
          this.open();
        }
      },

      clearMessage() {
        this.message = "";
      },

      clearError() {
        this.error = "";
      },

      handleShortcut(event) {
        if (!event || event.defaultPrevented) {
          return;
        }
        const key = typeof event.key === "string" ? event.key.toLowerCase() : "";
        if (key !== "c" || shouldIgnoreShortcut(event)) {
          return;
        }
        event.preventDefault();
        event.stopPropagation();
        this.open();
      },

      handleDiscoveredInput(value) {
        setDiscovered(value, "manual");
      },

      setIssueType(value) {
        this.issueType = normalizeIssueType(value);
      },

      setPriority(value) {
        const nextPriority = clampPriority(value);
        this.priority = nextPriority;
        defaultPriority = nextPriority;
        writeStoredPriority(global, nextPriority);
      },

      setActiveIssue(issueId) {
        applyActiveIssue(issueId);
      },

      reset() {
        this.isOpen = false;
        this.isSubmitting = false;
        this.title = "";
        this.description = "";
        this.issueType = defaultType;
        this.setPriority(defaultPriority);
        this.message = "";
        this.error = "";
        state.discoveredTouched = false;
        setDiscovered("", "reset");
      },

      teardown() {
        if (state.detach) {
          state.detach();
          state.detach = null;
        }
        unregisterQuickCreate(global, component);
      },

      submit() {
        const trimmedTitle = normalizeTitle(this.title);
        if (!trimmedTitle) {
          this.error = "Title is required.";
          this.message = "";
          return Promise.resolve(false);
        }
        if (this.isSubmitting) {
          return Promise.resolve(false);
        }

        this.isSubmitting = true;
        this.error = "";
        this.message = "";

        const payload = {
          title: trimmedTitle,
          issue_type: this.issueType,
          priority: this.priority,
        };

        const description = normalizeDescription(this.description);
        if (description) {
          payload.description = description;
        }

        if (state.discoveredValue) {
          payload.discovered_from = state.discoveredValue;
        }

        const requestInit = {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
          },
          body: JSON.stringify(payload),
        };

        const request = Promise.resolve()
          .then(() => transport("/api/issues", requestInit))
          .then(async (response) => {
            if (!response || !response.ok) {
              this.error = await extractErrorMessage(
                response,
                "Unable to create issue."
              );
              return false;
            }

            let body = {};
            try {
              body = await response.json();
            } catch {
              body = {};
            }

            const issue = body && body.issue ? body.issue : {};
            const issueId =
              issue && issue.id ? String(issue.id) : trimmedTitle || "";

            this.message = issueId ? `Created ${issueId}.` : "Issue created.";
            this.error = "";
            this.title = "";
            this.description = "";
            this.issueType = defaultType;
            this.setPriority(defaultPriority);
            this.isOpen = false;
            state.discoveredTouched = false;
            setDiscovered("", "reset");

            dispatch("events:update", {
              issueId: issueId,
              source: "quick-create",
              issue,
            });

            dispatch("issue:created", {
              issueId: issueId,
              issue,
              source: "quick-create",
            });

            return true;
          })
          .catch((error) => {
            const message =
              error instanceof Error
                ? error.message
                : String(error || "Unknown error");
            this.error = message;
            this.message = "";
            return false;
          })
          .finally(() => {
            this.isSubmitting = false;
          });

        return request;
      },
    };

    component.setPriority(defaultPriority);
    registerQuickCreate(global, component);

    Object.defineProperty(component, "discoveredFrom", {
      enumerable: true,
      configurable: true,
      get() {
        return state.discoveredValue;
      },
      set(value) {
        state.discoveredValue = normalizeIssueId(value);
        const mode = state.pendingMode || "manual";
        if (mode === "auto" || mode === "reset") {
          state.discoveredTouched = false;
        } else {
          state.discoveredTouched =
            state.discoveredValue.length > 0 &&
            state.discoveredValue !== state.activeIssueId;
        }
        state.pendingMode = null;
      },
    });

    Object.defineProperty(component, "canSubmit", {
      enumerable: true,
      get() {
        return !this.isSubmitting && normalizeTitle(this.title).length > 0;
      },
    });

    function setDiscovered(value, mode) {
      state.pendingMode = mode || "manual";
      component.discoveredFrom = value;
      if (state.refs && state.refs.discovered && typeof state.refs.discovered.value !== "undefined") {
        try {
          state.refs.discovered.value = component.discoveredFrom;
        } catch {
          // Ignore DOM sync failures.
        }
      }
    }

    function applyActiveIssue(issueId) {
      const normalized = normalizeIssueId(issueId);
      state.activeIssueId = normalized;
      if (!state.discoveredTouched) {
        setDiscovered(normalized || "", "auto");
      }
    }

    setDiscovered("", "reset");

    if (eventTarget && typeof eventTarget.addEventListener === "function") {
      const handler = (event) => {
        const detail = event && event.detail ? event.detail : {};
        if (detail && detail.issueId) {
          applyActiveIssue(detail.issueId);
        }
      };
      eventTarget.addEventListener("issue:detail-loaded", handler);
      state.detach = () =>
        eventTarget.removeEventListener("issue:detail-loaded", handler);
    }

    return component;
  }

  global.bdQuickCreate = bdQuickCreate;
  bdQuickCreate.getLatestController = function getLatestController() {
    return getLatestQuickCreate(global);
  };
})(typeof window !== "undefined" ? window : globalThis);
