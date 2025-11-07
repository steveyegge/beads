"use strict";

(function (global) {
  function normalizeIssueId(value) {
    if (typeof value === "string") {
      return value.trim();
    }
    if (value == null) {
      return "";
    }
    return String(value).trim();
  }

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
    const target =
      (document.body && typeof document.body.dispatchEvent === "function"
        ? document.body
        : document);
    if (!target || typeof target.dispatchEvent !== "function") {
      return;
    }
    let event = null;
    if (typeof CustomEvent === "function") {
      event = new CustomEvent(type, { detail, bubbles: true });
    } else if (typeof document.createEvent === "function") {
      event = document.createEvent("CustomEvent");
      event.initCustomEvent(type, true, false, detail);
    }
    if (event) {
      try {
        target.dispatchEvent(event);
      } catch {
        /* ignore dispatch failures */
      }
    }
  }

  async function readErrorMessage(response, fallback) {
    if (!response) {
      return fallback;
    }
    if (typeof response.clone === "function") {
      try {
        const clone = response.clone();
        const data = await clone.json();
        if (data && typeof data.error === "string" && data.error.trim()) {
          return data.error.trim();
        }
        if (data && typeof data.message === "string" && data.message.trim()) {
          return data.message.trim();
        }
      } catch {
        /* ignore JSON parse failure */
      }
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

  function bdIssueDelete(config = {}) {
    const issueId = normalizeIssueId(config.issueId);
    const issueTitle =
      typeof config.issueTitle === "string" ? config.issueTitle.trim() : "";
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

    function focusElement(element) {
      if (!element || typeof element.focus !== "function") {
        return;
      }
      const focus = () => {
        try {
          element.focus({ preventScroll: true });
        } catch {
          try {
            element.focus();
          } catch {
            /* ignore focus errors */
          }
        }
      };
      if (typeof requestAnimationFrame === "function") {
        requestAnimationFrame(focus);
      } else {
        setTimeout(focus, 0);
      }
    }

    async function sendRequest(id, confirmation) {
      const encodedId = encodeURIComponent(id);
      const encodedConfirm = encodeURIComponent(confirmation);
      const url = `/api/issues/${encodedId}?confirm=${encodedConfirm}`;
      return transport(url, {
        method: "DELETE",
        headers: {
          Accept: "application/json",
        },
        credentials: "same-origin",
      });
    }

    return {
      issueId,
      issueTitle,
      confirmation: "",
      error: "",
      isOpen: false,
      isSubmitting: false,

      init(refs) {
        state.refs = refs || {};
      },

      matches() {
        if (!this.issueId || !this.confirmation) {
          return false;
        }
        return (
          this.issueId.toLowerCase() === this.confirmation.toLowerCase().trim()
        );
      },

      open() {
        if (this.isSubmitting) {
          return;
        }
        this.error = "";
        this.confirmation = "";
        this.isOpen = true;
        if (typeof document !== "undefined" && document.activeElement) {
          state.lastActiveElement = document.activeElement;
        }
        const input =
          state.refs && state.refs.confirmation ? state.refs.confirmation : null;
        focusElement(input);
      },

      close() {
        if (this.isSubmitting) {
          return;
        }
        this.isOpen = false;
        this.confirmation = "";
        this.error = "";
        const previous = state.lastActiveElement;
        state.lastActiveElement = null;
        if (previous && typeof previous.focus === "function") {
          try {
            previous.focus({ preventScroll: true });
          } catch {
            try {
              previous.focus();
            } catch {
              /* ignore refocus errors */
            }
          }
        }
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
        const confirmation = this.confirmation
          ? this.confirmation.trim()
          : "";
        if (!confirmation) {
          this.error = `Type ${this.issueId} to confirm.`;
          return false;
        }
        if (!this.matches()) {
          this.error = `Type ${this.issueId} to confirm.`;
          return false;
        }

        this.isSubmitting = true;
        try {
          const response = await sendRequest(this.issueId, confirmation);
          if (!response || !response.ok) {
            const message = await readErrorMessage(
              response,
              "Unable to delete issue.",
            );
            this.error = message || "Unable to delete issue.";
            return false;
          }
        } catch (error) {
          const message =
            error instanceof Error
              ? error.message
              : String(error || "Unable to delete issue.");
          this.error = message || "Unable to delete issue.";
          return false;
        } finally {
          this.isSubmitting = false;
        }

        this.isOpen = false;
        this.confirmation = "";
        this.error = "";
        const previous = state.lastActiveElement;
        state.lastActiveElement = null;
        if (previous && typeof previous.focus === "function") {
          try {
            previous.focus({ preventScroll: true });
          } catch {
            try {
              previous.focus();
            } catch {
              /* ignore focus errors */
            }
          }
        }

        const eventDetail = {
          issueId: this.issueId,
          source: "issue-delete",
        };
        dispatch("issue:deleted", Object.assign({ issue: { id: this.issueId } }, eventDetail));
        dispatch(
          "events:update",
          Object.assign(
            {
              issueIds: [this.issueId],
              issues: [this.issueId],
              events: [
                {
                  type: "deleted",
                  issue: { id: this.issueId },
                },
              ],
            },
            eventDetail,
          ),
        );
        return true;
      },
    };
  }

  const exports = {
    bdIssueDelete,
  };

  if (typeof module !== "undefined" && module.exports) {
    module.exports = exports;
  }

  global.bdIssueDelete = bdIssueDelete;
})(typeof window !== "undefined" ? window : globalThis);

