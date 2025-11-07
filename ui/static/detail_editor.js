"use strict";

(function (global) {
  function defaultTransport(input, init) {
    if (typeof fetch === "function") {
      return fetch(input, init);
    }
    return Promise.reject(new Error("fetch unavailable"));
  }

  function defaultDispatch(type, detail) {
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
      try {
        target.dispatchEvent(event);
      } catch {
        /* ignore dispatch errors */
      }
    }
  }

  async function readErrorMessage(response, fallback) {
    if (!response) {
      return fallback;
    }
    try {
      if (typeof response.clone === "function") {
        const clone = response.clone();
        const data = await clone.json();
        if (data && typeof data.error === "string" && data.error.trim()) {
          return data.error.trim();
        }
        if (data && typeof data.message === "string" && data.message.trim()) {
          return data.message.trim();
        }
      }
    } catch {
      /* ignore JSON parse failure */
    }
    try {
      if (typeof response.text === "function") {
        const text = await response.text();
        if (typeof text === "string" && text.trim()) {
          return text.trim();
        }
      }
    } catch {
      /* ignore text read failure */
    }
    return fallback;
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

  function createDetailEditor(config = {}) {
    const issueId =
      typeof config.issueId === "string" ? config.issueId.trim() : "";
    const field =
      typeof config.field === "string"
        ? config.field.trim().toLowerCase()
        : "";
    const label =
      typeof config.label === "string" && config.label.trim()
        ? config.label.trim()
        : field || "Field";
    const initialValue =
      typeof config.initialValue === "string" ? config.initialValue : "";
    const transport =
      typeof config.transport === "function"
        ? config.transport
        : defaultTransport;
    const dispatch =
      typeof config.dispatch === "function"
        ? config.dispatch
        : defaultDispatch;

    const state = {
      refs: {},
      lastActiveElement: null,
    };

    function restoreFocus() {
      const target =
        (state.refs && state.refs.trigger) || state.lastActiveElement;
      if (target && typeof target.focus === "function") {
        try {
          target.focus({ preventScroll: true });
        } catch {
          try {
            target.focus();
          } catch {
            /* ignore focus restore errors */
          }
        }
      }
      state.lastActiveElement = null;
    }

    return {
      issueId,
      field,
      label,
      value: initialValue,
      draft: initialValue,
      isEditing: false,
      isSubmitting: false,
      error: "",

      get editLabel() {
        return this.hasValue() ? "Edit" : "Add";
      },

      hasValue() {
        return typeof this.value === "string" && this.value.trim().length > 0;
      },

      init(refs) {
        state.refs = refs || {};
      },

      startEdit() {
        if (!this.issueId || !this.field) {
          return;
        }
        this.error = "";
        this.draft = this.value;
        this.isEditing = true;
        state.lastActiveElement =
          (typeof document !== "undefined" && document.activeElement) || null;
        const trigger = state.refs && state.refs.trigger;
        if (trigger && typeof trigger.setAttribute === "function") {
          trigger.setAttribute("aria-expanded", "true");
        }
        focusElement(state.refs && state.refs.textarea);
      },

      cancel() {
        if (this.isSubmitting) {
          return;
        }
        this.isEditing = false;
        this.draft = this.value;
        this.error = "";
        const trigger = state.refs && state.refs.trigger;
        if (trigger && typeof trigger.setAttribute === "function") {
          trigger.setAttribute("aria-expanded", "false");
        }
        restoreFocus();
      },

      clearError() {
        if (this.error) {
          this.error = "";
        }
      },

      async submit() {
        if (!this.issueId) {
          this.error = "Issue identifier unavailable.";
          return false;
        }
        if (!this.field) {
          this.error = "Field identifier unavailable.";
          return false;
        }

        const payload = {};
        payload[this.field] = this.draft;

        this.isSubmitting = true;
        this.error = "";

        try {
          const response = await transport(
            `/api/issues/${encodeURIComponent(this.issueId)}`,
            {
              method: "PATCH",
              headers: {
                "Content-Type": "application/json",
              },
              body: JSON.stringify(payload),
            }
          );

          if (!response || response.ok === false) {
            const fallback = `${this.label} update failed.`;
            const message = await readErrorMessage(response, fallback);
            throw new Error(message);
          }

          this.value = this.draft;
          this.isEditing = false;

          const trigger = state.refs && state.refs.trigger;
          if (trigger && typeof trigger.setAttribute === "function") {
            trigger.setAttribute("aria-expanded", "false");
          }

          restoreFocus();

          dispatch("issue:detail-saved", {
            issueId: this.issueId,
            field: this.field,
            label: this.label,
            source: "detail-editor",
          });
          dispatch("events:update", {
            issueId: this.issueId,
            source: "detail-editor",
            fields: [this.field],
          });
          return true;
        } catch (error) {
          this.error =
            error instanceof Error
              ? error.message
              : String(error || "Update failed.");
          return false;
        } finally {
          this.isSubmitting = false;
        }
      },
    };
  }

  const factory = function bdDetailEditor(config) {
    return createDetailEditor(config);
  };

  factory.create = createDetailEditor;

  global.bdDetailEditor = factory;
})(typeof window !== "undefined" ? window : globalThis);
