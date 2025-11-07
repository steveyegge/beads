(function (global) {
  "use strict";

  const DEFAULT_LIMIT = 10;
  const DEFAULT_DEBOUNCE = 200;
  const SORT_RELEVANCE = "relevance";
  const SORT_RECENT = "recent";
  const SORT_PRIORITY = "priority";
  const SORT_STORAGE_KEY = "bd:palette:sort";

  const shortcutUtils =
    (typeof global.bdShortcutUtils === "object" &&
      global.bdShortcutUtils !== null &&
      global.bdShortcutUtils) ||
    null;

  function normalizeSortValue(value) {
    if (typeof value !== "string") {
      return "";
    }
    const normalized = value.trim().toLowerCase();
    if (
      normalized === SORT_RELEVANCE ||
      normalized === SORT_RECENT ||
      normalized === SORT_PRIORITY
    ) {
      return normalized;
    }
    return "";
  }

  function readStoredSort(globalObj) {
    try {
      const storage = globalObj && globalObj.localStorage;
      if (!storage || typeof storage.getItem !== "function") {
        return "";
      }
      const value = storage.getItem(SORT_STORAGE_KEY);
      return typeof value === "string" ? value : "";
    } catch {
      return "";
    }
  }

  function writeStoredSort(globalObj, value) {
    try {
      const storage = globalObj && globalObj.localStorage;
      if (!storage || typeof storage.setItem !== "function") {
        return;
      }
      storage.setItem(SORT_STORAGE_KEY, value);
    } catch {
      /* ignore storage errors */
    }
  }

  function cssEscape(value) {
    if (typeof value !== "string") {
      return "";
    }
    if (global.CSS && typeof global.CSS.escape === "function") {
      return global.CSS.escape(value);
    }
    return value.replace(/([!"#$%&'()*+,./:;<=>?@[\\\]^`{|}~])/g, "\\$1");
  }

  function defaultRequest(env, query, limit, sortMode) {
    const params = new URLSearchParams({ q: query, limit: String(limit) });
    if (sortMode) {
      params.set("sort", sortMode);
    }
    const url = `/api/search?${params.toString()}`;
    const headers = { Accept: "application/json" };

    if (env.htmx && typeof env.htmx.ajax === "function") {
      return new Promise((resolve, reject) => {
        const handleSuccess = (xhr) => {
          try {
            const status = Number(xhr?.status || 0);
            if (status < 200 || status >= 300) {
              const statusText =
                typeof xhr?.statusText === "string" && xhr.statusText.trim()
                  ? ` ${xhr.statusText.trim()}`
                  : "";
              throw new Error(`HTTP ${status || "unknown"}${statusText}`);
            }
            const raw =
              typeof xhr?.responseText === "string" && xhr.responseText !== ""
                ? xhr.responseText
                : typeof xhr?.response === "string"
                  ? xhr.response
                  : "{}";
            resolve(JSON.parse(raw || "{}"));
          } catch (error) {
            reject(error);
          }
        };

        const attachEventListeners = (xhr) => {
          if (!xhr || typeof xhr.addEventListener !== "function") {
            return false;
          }
          let handleLoad;
          let handleError;
          const cleanup = () => {
            if (handleLoad) {
              xhr.removeEventListener?.("loadend", handleLoad);
            }
            if (handleError) {
              xhr.removeEventListener?.("error", handleError);
            }
          };
          handleLoad = () => {
            cleanup();
            handleSuccess(xhr);
          };
          handleError = () => {
            cleanup();
            reject(new Error("Network error"));
          };
          xhr.addEventListener("loadend", handleLoad);
          xhr.addEventListener("error", handleError);
          return true;
        };

        const consumeXHR = (xhr) => {
          if (!attachEventListeners(xhr)) {
            handleSuccess(xhr);
          }
        };

        const request = env.htmx.ajax("GET", url, {
          swap: "none",
          headers,
        });
        if (!request) {
          reject(new Error("htmx.ajax returned no request"));
          return;
        }

        if (typeof request.then === "function") {
          request.then(
            (result) => {
              const xhr = result && typeof result === "object" && "xhr" in result ? result.xhr : result;
              if (!xhr) {
                reject(new Error("htmx.ajax Promise resolved without xhr"));
                return;
              }
              consumeXHR(xhr);
            },
            (error) => {
              reject(
                error instanceof Error
                  ? error
                  : new Error(error != null ? String(error) : "Network error"),
              );
            },
          );
          return;
        }

        consumeXHR(request);
      });
    }

    if (typeof fetch === "function") {
      return fetch(url, {
        headers,
        credentials: "same-origin",
      }).then((resp) => {
        if (!resp.ok) {
          throw new Error(`HTTP ${resp.status}`);
        }
        return resp.json();
      });
    }

    throw new Error("No search transport available");
  }

  function defaultNavigate(env, id) {
    if (!env.document || !id) {
      return;
    }
    const selector = `[data-role='issue-row'][data-issue-id='${cssEscape(id)}']`;
    const row = env.document.querySelector(selector);
    if (row && typeof row.click === "function") {
      row.click();
      try {
        row.focus({ preventScroll: true });
      } catch {
        row.focus();
      }
      return;
    }
    const detail = env.document.querySelector("[data-role='issue-detail']");
    if (detail && env.htmx && typeof env.htmx.ajax === "function") {
      const url = `/fragments/issue?id=${encodeURIComponent(id)}`;
      env.htmx.ajax("GET", url, {
        target: detail,
        swap: "innerHTML",
      });
    }
  }

  function bdPalette(userConfig = {}) {
    const env = {
      htmx:
        userConfig.htmx ||
        (typeof global !== "undefined" ? global.htmx : undefined),
      document:
        userConfig.document ||
        (typeof document !== "undefined" ? document : undefined),
      setTimer:
        userConfig.setTimer ||
        (typeof setTimeout === "function"
          ? setTimeout.bind(global)
          : (fn) => {
              fn();
              return 0;
            }),
      clearTimer:
        userConfig.clearTimer ||
        (typeof clearTimeout === "function"
          ? clearTimeout.bind(global)
          : () => {}),
      limit:
        typeof userConfig.limit === "number"
          ? userConfig.limit
          : DEFAULT_LIMIT,
      debounceMs:
        typeof userConfig.debounceMs === "number"
          ? userConfig.debounceMs
          : DEFAULT_DEBOUNCE,
      request: userConfig.request,
      navigateToIssue: userConfig.navigateToIssue,
    };

    const requestFn =
      typeof env.request === "function"
        ? env.request
        : (query, limit, sortMode) => defaultRequest(env, query, limit, sortMode);

    const navigateFn =
      typeof env.navigateToIssue === "function"
        ? env.navigateToIssue
        : (result) => defaultNavigate(env, result?.id);

    const storedSort = normalizeSortValue(readStoredSort(global));

    const state = {
      timer: null,
      pendingToken: null,
    };

    function resetPending() {
      if (state.timer) {
        env.clearTimer(state.timer);
        state.timer = null;
      }
      state.pendingToken = null;
    }

    return {
      isOpen: false,
      query: "",
      results: [],
      activeIndex: -1,
      loading: false,
      error: "",
      limit: env.limit,
      sortMode: storedSort || SORT_RECENT,
      sortPreferenceExplicit: storedSort !== "",

      init() {
        if (typeof this.$watch === "function") {
          this.$watch("isOpen", (open) => {
            if (!open) {
              resetPending();
              this.results = [];
              this.activeIndex = -1;
              this.error = "";
              this.loading = false;
            }

            const body = typeof document !== "undefined" ? document.body : null;
            if (body) {
              body.dataset.commandPaletteOpen = open ? "true" : "false";
            }
          });
        }

        const body = typeof document !== "undefined" ? document.body : null;
        if (body && body.dataset.commandPaletteOpen === undefined) {
          body.dataset.commandPaletteOpen = "false";
        }

        if (typeof global !== "undefined") {
          Object.defineProperty(global, "__bdPaletteReady", {
            value: true,
            configurable: true,
            writable: true,
          });

          try {
            Object.defineProperty(global, "__bdPalette", {
              value: this,
              configurable: true,
              writable: true,
            });
          } catch {
            global.__bdPalette = this;
          }

          if (typeof global.dispatchEvent === "function") {
            try {
              const readyEvent =
                typeof CustomEvent === "function"
                  ? new CustomEvent("command-palette:ready")
                  : { type: "command-palette:ready" };
              global.dispatchEvent(readyEvent);
            } catch {
              /* noop */
            }
          }
        }
      },

      destroy() {
        resetPending();
        if (typeof global !== "undefined" && global.__bdPalette === this) {
          try {
            delete global.__bdPalette;
          } catch {
            global.__bdPalette = undefined;
          }
        }

        if (typeof document !== "undefined" && document.body) {
          delete document.body.dataset.commandPaletteOpen;
        }
      },

      handleGlobalShortcut(event) {
        if (!event || typeof event.key !== "string") {
          return;
        }
        if (
          shortcutUtils &&
          typeof shortcutUtils.shouldIgnoreEvent === "function" &&
          shortcutUtils.shouldIgnoreEvent(event)
        ) {
          return;
        }

        const key = event.key;
        const lower = key.toLowerCase();
        const modifier = event.metaKey || event.ctrlKey;
        const alt = event.altKey;
        const shift = event.shiftKey;
        const code =
          typeof event.code === "string" ? event.code.toLowerCase() : "";

        const applySortShortcut = (mode) => {
          if (!mode) {
            return;
          }
          event.preventDefault?.();
          event.stopPropagation?.();
          this.handleSortChange(mode);
        };

        if (modifier) {
          if (!alt && !shift && lower === "k") {
            event.preventDefault?.();
            event.stopPropagation?.();
            this.toggle();
            return;
          }

          if (!this.isOpen) {
            return;
          }

          if (alt && !shift) {
            switch (code) {
              case "digit1":
                applySortShortcut(SORT_RELEVANCE);
                return;
              case "digit2":
                applySortShortcut(SORT_RECENT);
                return;
              case "digit3":
                applySortShortcut(SORT_PRIORITY);
                return;
              default:
                break;
            }
          }

          // Ignore other modifier combinations to avoid interfering with browser defaults.
          return;
        }

        if (!this.isOpen) {
          return;
        }

        switch (lower) {
          case "escape": {
            event.preventDefault?.();
            event.stopPropagation?.();
            this.close();
            return;
          }
          case "arrowdown": {
            event.preventDefault?.();
            event.stopPropagation?.();
            this.moveSelection(1);
            return;
          }
          case "arrowup": {
            event.preventDefault?.();
            event.stopPropagation?.();
            this.moveSelection(-1);
            return;
          }
          case "enter": {
            event.preventDefault?.();
            event.stopPropagation?.();
            this.activateSelection();
            return;
          }
          default:
            return;
        }
      },

      toggle() {
        if (this.isOpen) {
          this.close();
        } else {
          this.open();
        }
      },

      open() {
        if (this.isOpen) {
          return;
        }
        this.isOpen = true;
        this.applyDefaultSortForQuery();
        const body = typeof document !== "undefined" ? document.body : null;
        if (body) {
          body.dataset.commandPaletteOpen = "true";
        }
        const doc = env.document || (typeof document !== "undefined" ? document : null);
        const focusOnce = () => {
          const input = this.$refs?.query;
          if (!input || typeof input.focus !== "function") {
            return false;
          }
          let visible = true;
          if (typeof input.getClientRects === "function") {
            visible = input.getClientRects().length > 0;
          } else if (typeof input.offsetParent !== "undefined") {
            visible = input.offsetParent !== null;
          }
          if (!visible) {
            return false;
          }
          try {
            input.focus({ preventScroll: true });
          } catch {
            try {
              input.focus();
            } catch {
              return false;
            }
          }
          if (!doc || doc.activeElement === input) {
            return true;
          }
          return false;
        };
        const focusWithRetry = (attempt = 0) => {
          if (focusOnce()) {
            return;
          }
          const maxAttempts = 24;
          if (attempt >= maxAttempts) {
            return;
          }
          const invoke = () => env.setTimer(() => focusWithRetry(attempt + 1), 50);
          if (typeof global.requestAnimationFrame === "function") {
            global.requestAnimationFrame(invoke);
          } else {
            invoke();
          }
        };
        const scheduleFocus = () => focusWithRetry(0);
        if (typeof this.$nextTick === "function") {
          this.$nextTick(scheduleFocus);
        } else {
          scheduleFocus();
        }
        this.scheduleFetch();
      },

      close() {
        if (!this.isOpen) {
          return;
        }
        this.isOpen = false;
        this.query = "";
        this.applyDefaultSortForQuery();
        const body = typeof document !== "undefined" ? document.body : null;
        if (body) {
          body.dataset.commandPaletteOpen = "false";
        }
      },

      handleInput() {
        this.applyDefaultSortForQuery();
        this.scheduleFetch();
      },

      applyDefaultSortForQuery() {
        if (this.sortPreferenceExplicit) {
          return;
        }
        const trimmed = typeof this.query === "string" ? this.query.trim() : "";
        const desired = trimmed ? SORT_RELEVANCE : SORT_RECENT;
        if (this.sortMode !== desired) {
          this.sortMode = desired;
        }
      },

      handleSortChange(value) {
        const normalized = normalizeSortValue(value);
        if (!normalized) {
          return;
        }
        if (this.sortMode === normalized && this.sortPreferenceExplicit) {
          return;
        }
        this.sortMode = normalized;
        this.sortPreferenceExplicit = true;
        writeStoredSort(global, normalized);
        resetPending();
        this.fetchResults();
      },

      scheduleFetch() {
        if (!this.isOpen) {
          return;
        }
        this.applyDefaultSortForQuery();
        if (state.timer) {
          env.clearTimer(state.timer);
        }
        const delay = env.debounceMs;
        if (!delay) {
          this.fetchResults();
          return;
        }
        state.timer = env.setTimer(() => {
          state.timer = null;
          this.fetchResults();
        }, delay);
      },

      fetchResults() {
        const trimmed = typeof this.query === "string" ? this.query.trim() : "";
        if (!trimmed) {
          resetPending();
          this.results = [];
          this.activeIndex = -1;
          this.error = "";
          this.loading = false;
          return;
        }

        let normalizedSort = normalizeSortValue(this.sortMode);
        if (!this.sortPreferenceExplicit) {
          normalizedSort = trimmed ? SORT_RELEVANCE : SORT_RECENT;
        }
        if (!normalizedSort) {
          normalizedSort = SORT_RELEVANCE;
        }
        if (this.sortMode !== normalizedSort) {
          this.sortMode = normalizedSort;
        }

        this.loading = true;
        const token = {};
        state.pendingToken = token;

        const handleSuccess = (payload) => {
          if (state.pendingToken !== token) {
            return;
          }
          const results = Array.isArray(payload?.results)
            ? payload.results
            : [];
          this.results = results;
          this.activeIndex = results.length ? 0 : -1;
          this.error = "";
          this.loading = false;
        };

        const handleError = (error) => {
          if (state.pendingToken !== token) {
            return;
          }
          this.error =
            (error && error.message) || "Search failed. Please try again.";
          this.results = [];
          this.activeIndex = -1;
          this.loading = false;
        };

        try {
          const response = requestFn(trimmed, this.limit, normalizedSort);
          if (response && typeof response.then === "function") {
            response.then(handleSuccess, handleError);
          } else {
            handleSuccess(response);
          }
        } catch (error) {
          handleError(error);
        }
      },

      moveSelection(delta) {
        if (!Array.isArray(this.results) || this.results.length === 0) {
          return;
        }
        const nextIndex =
          this.activeIndex < 0 ? 0 : this.activeIndex + Number(delta || 0);
        const bounded = Math.max(
          0,
          Math.min(this.results.length - 1, nextIndex)
        );
        this.activeIndex = bounded;
      },

      setActive(index) {
        if (!Array.isArray(this.results) || this.results.length === 0) {
          this.activeIndex = -1;
          return;
        }
        const bounded = Math.max(
          -1,
          Math.min(this.results.length - 1, Number(index))
        );
        this.activeIndex = bounded;
      },

      activateSelection(index) {
        const targetIndex =
          typeof index === "number" ? index : this.activeIndex;
        if (
          !Array.isArray(this.results) ||
          targetIndex < 0 ||
          targetIndex >= this.results.length
        ) {
          return;
        }
        const result = this.results[targetIndex];
        navigateFn(result, this);
        this.close();
      },

      handleResultClick(index) {
        this.activateSelection(index);
      },
    };
  }

  if (typeof document !== "undefined") {
    document.addEventListener("alpine:init", () => {
      try {
        const Alpine = global.Alpine;
        if (Alpine && typeof Alpine.data === "function") {
          Alpine.data("bdPaletteFactory", (config = {}) => bdPalette(config));
        }
      } catch {
        /* ignore alpine registration failure */
      }
    });

    try {
      const Alpine = global.Alpine;
      if (Alpine && typeof Alpine.data === "function") {
        Alpine.data("bdPaletteFactory", (config = {}) => bdPalette(config));
      }
    } catch {
      /* ignore immediate alpine registration failure */
    }
  }

  const exports = {
    bdPalette,
  };

  if (typeof module !== "undefined" && module.exports) {
    module.exports = exports;
  }

  global.bdPalette = bdPalette;
  global.bdPaletteFactory = bdPalette;
  global.bdPaletteOpen = function () {
    let invoked = false;

    const instance = global.__bdPalette;
    if (instance && typeof instance.open === "function") {
      instance.open();
      invoked = true;
    }

    const doc = typeof document !== "undefined" ? document : null;
    if (doc && !invoked) {
      const el = doc.querySelector('[data-testid="command-palette"]');
      if (el) {
        const stack = el._x_dataStack || [];
        if (el.__x && typeof el.__x.$data?.open === "function") {
          el.__x.$data.open();
          global.__bdPalette = el.__x.$data;
          invoked = true;
        } else if (stack.length && typeof stack[0].open === "function") {
          stack[0].open();
          global.__bdPalette = stack[0];
          invoked = true;
        } else if (global.Alpine && typeof global.Alpine.evaluate === "function") {
          try {
            global.Alpine.evaluate(el, "open()");
            invoked = true;
          } catch {
            /* ignore */
          }
        }
      }
    }

    if (typeof global.dispatchEvent === "function") {
      try {
        global.dispatchEvent(new CustomEvent("command-palette:open"));
        invoked = true;
      } catch {
        /* ignore */
      }
    }

    return invoked;
  };
})(typeof window !== "undefined" ? window : globalThis);
