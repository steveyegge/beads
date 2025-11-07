"use strict";

(function (global) {
  const DEFAULT_STATUSES = [
    { id: "open", label: "Ready", shortcut: "Shift+R" },
    { id: "in_progress", label: "In Progress", shortcut: "Shift+I" },
    { id: "closed", label: "Done", shortcut: "Shift+D" },
  ];

  const STATUS_LOOKUP = DEFAULT_STATUSES.reduce((acc, entry) => {
    acc[entry.id] = entry;
    return acc;
  }, {});

  const shortcutUtils =
    (typeof global.bdShortcutUtils === "object" &&
      global.bdShortcutUtils !== null &&
      global.bdShortcutUtils) ||
    null;

  function persistMessage(value) {
    if (typeof global === "undefined") {
      return;
    }
    try {
      if (value && typeof value === "string" && value.trim()) {
        global.__bdStatusActionsMessage = value;
      } else if (Object.prototype.hasOwnProperty.call(global, "__bdStatusActionsMessage")) {
        delete global.__bdStatusActionsMessage;
      }
    } catch {
      /* noop */
    }
  }

  function consumePersistedMessage() {
    if (typeof global === "undefined") {
      return "";
    }
    try {
      const value =
        typeof global.__bdStatusActionsMessage === "string"
          ? global.__bdStatusActionsMessage
          : "";
      if (Object.prototype.hasOwnProperty.call(global, "__bdStatusActionsMessage")) {
        delete global.__bdStatusActionsMessage;
      }
      return value;
    } catch {
      return "";
    }
  }

  function persistStatusUpdate(payload) {
    if (typeof global === "undefined") {
      return;
    }
    try {
      if (payload && typeof payload === "object") {
        global.__bdStatusActionsStatus = {
          issueId: payload.issueId || "",
          status: payload.status || "",
          label: payload.label || "",
        };
      } else if (Object.prototype.hasOwnProperty.call(global, "__bdStatusActionsStatus")) {
        delete global.__bdStatusActionsStatus;
      }
    } catch {
      /* noop */
    }
  }

  function consumePersistedStatus() {
    if (typeof global === "undefined") {
      return null;
    }
    try {
      const value = global.__bdStatusActionsStatus;
      if (Object.prototype.hasOwnProperty.call(global, "__bdStatusActionsStatus")) {
        delete global.__bdStatusActionsStatus;
      }
      if (value && typeof value === "object") {
        return {
          issueId: typeof value.issueId === "string" ? value.issueId : "",
          status: typeof value.status === "string" ? value.status : "",
          label: typeof value.label === "string" ? value.label : "",
        };
      }
    } catch {
      /* noop */
    }
    return null;
  }

  function normalizeStatus(value) {
    return typeof value === "string" ? value.trim().toLowerCase() : "";
  }

  function labelForStatus(status) {
    const entry = STATUS_LOOKUP[normalizeStatus(status)];
    return entry ? entry.label : status;
  }

  function createCustomEvent(type, detail) {
    if (typeof CustomEvent === "function") {
      return new CustomEvent(type, { detail, bubbles: false });
    }
    if (typeof document !== "undefined" && document.createEvent) {
      const evt = document.createEvent("CustomEvent");
      evt.initCustomEvent(type, false, false, detail);
      return evt;
    }
    return null;
  }

  function dispatchDomEvent(type, detail) {
    const target =
      (typeof document !== "undefined" && document.body) ||
      (typeof document !== "undefined" && document);
    if (!target || typeof target.dispatchEvent !== "function") {
      return;
    }
    const evt = createCustomEvent(type, detail);
    if (evt) {
      target.dispatchEvent(evt);
    }
  }

  function focusElement(element) {
    if (!element || typeof element.focus !== "function") {
      return;
    }
    try {
      element.focus({ preventScroll: true });
    } catch {
      try {
        element.focus();
      } catch {
        /* ignore focus errors */
      }
    }
  }

  function resolveEventTarget(config) {
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

  function bdStatusActions(config = {}) {
    const issueId = config.issueId || "";
    const initialStatus = normalizeStatus(config.initialStatus) || "open";
    const transport =
      typeof config.transport === "function"
        ? config.transport
        : (url, options) => {
            if (typeof fetch !== "function") {
              throw new Error("fetch unavailable and no transport provided");
            }
            return fetch(url, options);
          };
    const dispatch =
      typeof config.dispatch === "function"
        ? config.dispatch
        : dispatchDomEvent;
    const eventTarget = resolveEventTarget(config);

    const controller = {
      issueId,
      status: initialStatus,
      message: "",
      error: "",
      isPending: false,
      statuses: DEFAULT_STATUSES,
      eventTarget,
      confirm: {
        isOpen: false,
        isSubmitting: false,
        target: "",
        trigger: null,
        previousFocus: null,
      },

      statusLabel() {
        return labelForStatus(this.status);
      },

      labelFor(target) {
        return labelForStatus(target);
      },

      isActive(target) {
        return this.status === normalizeStatus(target);
      },

      isDisabled(target) {
        const normalized = normalizeStatus(target);
        return this.isPending || !normalized || normalized === this.status;
      },

      labelForAction(target) {
        const normalized = normalizeStatus(target);
        if (!normalized) {
          return target;
        }
        if (this.status === "closed" && normalized === "open") {
          return "Reopen";
        }
        const entry = STATUS_LOOKUP[normalized];
        if (entry && entry.label) {
          return entry.label;
        }
        return target;
      },

      shouldConfirm(target, event) {
        if (normalizeStatus(target) !== "closed") {
          return false;
        }
        if (!event) {
          return true;
        }
        if (event.shiftKey) {
          return false;
        }
        if (typeof event.detail === "number" && event.detail === 0) {
          return false;
        }
        const pointerType =
          typeof event.pointerType === "string"
            ? event.pointerType.toLowerCase()
            : "";
        if (!pointerType && event.type !== "click") {
          return false;
        }
        if (!pointerType) {
          return true;
        }
        return (
          pointerType === "mouse" ||
          pointerType === "pen" ||
          pointerType === "touch"
        );
      },

      requestStatusChange(target, event) {
        const normalized = normalizeStatus(target);
        if (!normalized) {
          return;
        }
        if (event && typeof event.preventDefault === "function") {
          event.preventDefault();
        }
        if (event && typeof event.stopPropagation === "function") {
          event.stopPropagation();
        }

        if (this.shouldConfirm(normalized, event)) {
          this.openConfirm(normalized, event);
          return;
        }

        const triggerSource =
          event && typeof event.type === "string"
            ? event.type.toLowerCase()
            : "direct";

        this.setStatus(normalized, {
          trigger: triggerSource,
          source: "status-actions",
        });
      },

      openConfirm(target, event) {
        const normalized = normalizeStatus(target);
        if (!normalized || this.confirm.isSubmitting) {
          return;
        }
        this.confirm.target = normalized;
        this.confirm.isOpen = true;
        this.confirm.isSubmitting = false;
        this.error = "";
        this.confirm.previousFocus =
          (typeof document !== "undefined" && document.activeElement) || null;
        this.confirm.trigger =
          (event && event.currentTarget) ||
          (state.refs && state.refs.trigger) ||
          null;
        focusElement(state.refs && state.refs.confirm);
      },

      closeConfirm() {
        if (!this.confirm.isOpen || this.confirm.isSubmitting) {
          return;
        }
        this.confirm.isOpen = false;
        this.confirm.target = "";
        const focusTarget = this.confirm.trigger || this.confirm.previousFocus;
        this.confirm.trigger = null;
        this.confirm.previousFocus = null;
        if (focusTarget) {
          focusElement(focusTarget);
        }
      },

      cancelConfirm() {
        if (this.confirm.isSubmitting) {
          return;
        }
        this.closeConfirm();
      },

      async confirmChange() {
        if (!this.confirm.isOpen || this.confirm.isSubmitting) {
          return;
        }
        if (!this.confirm.target) {
          this.closeConfirm();
          return;
        }
        this.confirm.isSubmitting = true;
        const success = await this.setStatus(this.confirm.target, {
          trigger: "confirm",
          source: "status-actions",
        });
        this.confirm.isSubmitting = false;
        if (success) {
          this.closeConfirm();
        } else {
          focusElement(state.refs && state.refs.confirm);
        }
      },

      async setStatus(target, meta = {}) {
        const nextStatus = normalizeStatus(target);
        if (!nextStatus || nextStatus === this.status || this.isPending) {
          return true;
        }

        if (!this.issueId) {
          this.error = "Issue ID missing for status action.";
          return false;
        }

        const previousStatus = this.status;
        this.status = nextStatus;
        this.isPending = true;
        this.error = "";
        this.message = "";

        const url = `/api/issues/${encodeURIComponent(this.issueId)}/status`;
        const requestOptions = {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
          },
          body: JSON.stringify({ status: nextStatus }),
        };

        try {
          const response = await transport(url, requestOptions);
          if (!response || response.ok === false) {
            let detail = "";
            if (response && typeof response.json === "function") {
              try {
                const payload = await response.json();
                if (payload && payload.error) {
                  detail = String(payload.error);
                }
              } catch {
                // ignore parsing error
              }
            }
            const statusCode =
              response && typeof response.status === "number"
                ? response.status
                : "failed";
            const message =
              detail || `Failed to update status (HTTP ${statusCode}).`;
            throw new Error(message);
          }

          let payload = null;
          if (response && typeof response.json === "function") {
            try {
              payload = await response.json();
            } catch {
              payload = null;
            }
          }

          const resolved =
            normalizeStatus(payload?.issue?.status) || nextStatus;
          this.status = resolved;

          const label = labelForStatus(resolved);
          this.message = `Status updated to ${label}.`;
          this.error = "";
          persistMessage(this.message);
          persistStatusUpdate({
            issueId: this.issueId,
            status: resolved,
            label,
          });

          dispatch("issue:status-applied", {
            issueId: this.issueId,
            status: resolved,
            label,
            source: "status-actions",
          });

          dispatch("events:update", {
            issueId: this.issueId,
            status: resolved,
            source: "status-actions",
          });

          if (resolved === "closed") {
            dispatch("issue:detail-clear", {
              issueId: this.issueId,
              status: resolved,
              source: meta.source || "status-actions",
            });
          }

          return true;
        } catch (err) {
          this.status = previousStatus;
          this.message = "";
          this.error =
            err instanceof Error ? err.message : String(err || "Unknown error");
          return false;
        } finally {
          this.isPending = false;
        }
      },

      handleShortcut(event) {
        if (!event || event.defaultPrevented) {
          return;
        }
        if (
          shortcutUtils &&
          typeof shortcutUtils.shouldIgnoreEvent === "function" &&
          shortcutUtils.shouldIgnoreEvent(event)
        ) {
          return;
        }
        if (!event.shiftKey || event.altKey || event.ctrlKey || event.metaKey) {
          return;
        }

        const key = (event.key || "").toLowerCase();
        let target = "";
        if (key === "r") {
          target = "open";
        } else if (key === "i") {
          target = "in_progress";
        } else if (key === "d") {
          target = "closed";
        }

        if (!target) {
          return;
        }

        event.preventDefault();
        event.stopPropagation();
        this.setStatus(target, {
          trigger: "shortcut",
          source: "status-actions",
        });
      },
    };

    const state = {
      statusHandler: null,
      refs: {},
    };

    controller.init = function init(refs) {
      state.refs = refs || {};
      if (
        !this.eventTarget ||
        typeof this.eventTarget.addEventListener !== "function" ||
        state.statusHandler
      ) {
        return;
      }

      const handler = (event) => {
        const detail = event?.detail || {};
        if (!detail.issueId || String(detail.issueId) !== controller.issueId) {
          return;
        }
        const nextStatus = normalizeStatus(
          detail.status || detail.issue?.status
        );
        if (!nextStatus || nextStatus === controller.status) {
          return;
        }
        controller.status = nextStatus;
        controller.isPending = false;
        controller.error = "";
        if (detail.source !== "status-actions") {
          const label = detail.label || labelForStatus(nextStatus);
          controller.message = `Status updated to ${label}.`;
        }
      };

      state.statusHandler = handler;
      this.eventTarget.addEventListener("issue:status-applied", handler);

      const persisted = consumePersistedMessage();
      if (persisted) {
        controller.message = persisted;
      }

      const persistedStatus = consumePersistedStatus();
      if (persistedStatus && persistedStatus.issueId === controller.issueId) {
        const restored = normalizeStatus(persistedStatus.status);
        if (restored) {
          controller.status = restored;
        }
        const label = persistedStatus.label || labelForStatus(controller.status);
        dispatch("issue:status-applied", {
          issueId: controller.issueId,
          status: controller.status,
          label,
          source: "status-actions:restore",
        });
      }
    };

    controller.destroy = function destroy() {
      if (
        this.eventTarget &&
        state.statusHandler &&
        typeof this.eventTarget.removeEventListener === "function"
      ) {
        this.eventTarget.removeEventListener(
          "issue:status-applied",
          state.statusHandler
        );
      }
      state.statusHandler = null;
      state.refs = {};
    };

    return controller;
  }

  const helpers = {
    labelForStatus,
    statuses: DEFAULT_STATUSES.slice(),
  };

  global.bdStatusActions = bdStatusActions;
  global.bdStatusHelpers = Object.assign(global.bdStatusHelpers || {}, helpers);
})(typeof window !== "undefined" ? window : globalThis);
