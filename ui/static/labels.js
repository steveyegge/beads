"use strict";

const LABEL_STORAGE_KEY = "beads.ui.labels.recent";
const RECENT_LIMIT = 12;

function normalizeLabel(value) {
  if (typeof value !== "string") {
    return "";
  }
  return value.trim();
}

function uniqueLabels(values) {
  const list = Array.isArray(values) ? values : [];
  const seen = new Set();
  const result = [];
  for (const value of list) {
    const label = normalizeLabel(value);
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

function readRecents(storage) {
  if (!storage || typeof storage.getItem !== "function") {
    return [];
  }
  try {
    const raw = storage.getItem(LABEL_STORAGE_KEY);
    if (!raw) {
      return [];
    }
    const parsed = JSON.parse(raw);
    return uniqueLabels(Array.isArray(parsed) ? parsed : []);
  } catch {
    return [];
  }
}

function writeRecents(storage, labels) {
  if (!storage || typeof storage.setItem !== "function") {
    return;
  }
  try {
    storage.setItem(
      LABEL_STORAGE_KEY,
      JSON.stringify(uniqueLabels(labels).slice(0, RECENT_LIMIT))
    );
  } catch {
    // Swallow storage errors (e.g. quota exceeded).
  }
}

function rememberLabel(storage, label) {
  const normalized = normalizeLabel(label);
  if (!normalized) {
    return readRecents(storage);
  }
  const current = readRecents(storage);
  const next = [normalized].concat(
    current.filter((entry) => entry.toLowerCase() !== normalized.toLowerCase())
  );
  writeRecents(storage, next);
  return next;
}

function getStorage(provided) {
  if (provided) {
    return provided;
  }
  try {
    if (typeof window !== "undefined" && window.localStorage) {
      return window.localStorage;
    }
  } catch {
    // Ignore storage access failures (e.g. disabled cookies).
  }
  return null;
}

function defaultTransport(input, init) {
  if (typeof fetch === "function") {
    return fetch(input, init);
  }
  return Promise.reject(new Error("fetch unavailable"));
}

function getEventTarget() {
  if (typeof document === "undefined") {
    return null;
  }
  if (document.body && typeof document.body.addEventListener === "function") {
    return document.body;
  }
  if (typeof document.addEventListener === "function") {
    return document;
  }
  return null;
}

function defaultDispatch(type, detail) {
  const target = getEventTarget();
  if (!target) {
    return;
  }
  if (typeof CustomEvent === "function") {
    target.dispatchEvent(new CustomEvent(type, { detail }));
    return;
  }
  if (typeof document !== "undefined" && typeof document.createEvent === "function") {
    const evt = document.createEvent("CustomEvent");
    evt.initCustomEvent(type, false, false, detail);
    target.dispatchEvent(evt);
  }
}

async function extractErrorMessage(response, fallback) {
  if (!response) {
    return fallback;
  }
  const message = fallback || "Label update failed.";
  try {
    const data = await response.json();
    if (data && typeof data.error === "string" && data.error.trim()) {
      return data.error;
    }
  } catch {
    // Ignore parse errors.
  }
  return message;
}

function encodeIssuePath(issueId) {
  return `/api/issues/${encodeURIComponent(issueId)}/labels`;
}

function bdLabelEditor(config = {}) {
  if (!config || !config.issueId) {
    throw new Error("bdLabelEditor requires an issueId");
  }

  const storage = getStorage(config.storage);
  const state = {
    issueId: String(config.issueId),
    labels: uniqueLabels(config.initialLabels),
    recents: uniqueLabels(readRecents(storage)),
    transport: typeof config.transport === "function" ? config.transport : defaultTransport,
    dispatch: typeof config.dispatch === "function" ? config.dispatch : defaultDispatch,
    inflightAdds: new Map(),
    inflightRemovals: new Map(),
  };

  const component = {
    issueId: state.issueId,
    labels: state.labels.slice(),
    recents: state.recents.slice(),
    inputValue: "",
    error: "",
    isBusy: false,
    addLabel(label) {
      const normalized = normalizeLabel(label ?? this.inputValue);
      if (!normalized) {
        this.error = "Label is required.";
        return Promise.resolve(false);
      }

      const lower = normalized.toLowerCase();
      if (state.labels.some((existing) => existing.toLowerCase() === lower)) {
        this.error = "";
        return Promise.resolve(false);
      }

      if (state.inflightAdds.has(lower)) {
        return state.inflightAdds.get(lower);
      }

      this.isBusy = true;
      this.error = "";

      const request = state.transport(encodeIssuePath(state.issueId), {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ label: normalized }),
      });

      const promise = Promise.resolve(request)
        .then(async (response) => {
          if (!response || !response.ok) {
            this.error = await extractErrorMessage(response, "Unable to add label.");
            return false;
          }
          let payload = {};
          try {
            payload = await response.json();
          } catch {
            payload = {};
          }
          const nextLabels = uniqueLabels(payload.labels || state.labels.concat([normalized]));
          state.labels = nextLabels;
          this.labels = nextLabels.slice();
          const updatedRecents = rememberLabel(storage, normalized);
          state.recents = updatedRecents;
          this.recents = updatedRecents.slice();
          this.inputValue = "";
          this.error = "";
          state.dispatch("issue:labels-updated", {
            issueId: state.issueId,
            labels: this.labels.slice(),
            source: "ui",
          });
          return true;
        })
        .catch((err) => {
          const message =
            err && typeof err.message === "string" && err.message.trim()
              ? err.message
              : "Unable to add label.";
          this.error = message;
          return false;
        })
        .then((result) => {
          state.inflightAdds.delete(lower);
          this.isBusy = false;
          return result;
        });

      state.inflightAdds.set(lower, promise);
      return promise;
    },
    removeLabel(label) {
      const normalized = normalizeLabel(label);
      if (!normalized) {
        return Promise.resolve(false);
      }
      const lower = normalized.toLowerCase();
      if (!state.labels.some((existing) => existing.toLowerCase() === lower)) {
        return Promise.resolve(false);
      }
      if (state.inflightRemovals.has(lower)) {
        return state.inflightRemovals.get(lower);
      }

      this.isBusy = true;
      this.error = "";

      const request = state.transport(encodeIssuePath(state.issueId), {
        method: "DELETE",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ label: normalized }),
      });

      const promise = Promise.resolve(request)
        .then(async (response) => {
          if (!response || !response.ok) {
            this.error = await extractErrorMessage(response, "Unable to remove label.");
            return false;
          }
          let payload = {};
          try {
            payload = await response.json();
          } catch {
            payload = {};
          }
          const nextLabels = uniqueLabels(payload.labels || state.labels.filter((item) => item.toLowerCase() !== lower));
          state.labels = nextLabels;
          this.labels = nextLabels.slice();
          this.error = "";
          state.dispatch("issue:labels-updated", {
            issueId: state.issueId,
            labels: this.labels.slice(),
            source: "ui",
          });
          return true;
        })
        .catch((err) => {
          const message =
            err && typeof err.message === "string" && err.message.trim()
              ? err.message
              : "Unable to remove label.";
          this.error = message;
          return false;
        })
        .then((result) => {
          state.inflightRemovals.delete(lower);
          this.isBusy = false;
          return result;
        });

      state.inflightRemovals.set(lower, promise);
      return promise;
    },
    submitLabel() {
      return this.addLabel(this.inputValue);
    },
    handleSuggestion(label) {
      this.inputValue = label || "";
      if (this.inputValue) {
        this.addLabel(this.inputValue);
      }
    },
    syncLabels(next) {
      const normalized = uniqueLabels(next);
      state.labels = normalized;
      this.labels = normalized.slice();
    },
    refreshRecents() {
      const recents = readRecents(storage);
      state.recents = recents;
      this.recents = recents.slice();
    },
    clearError() {
      this.error = "";
    },
    teardown() {
      if (state.detach) {
        state.detach();
        state.detach = null;
      }
    },
  };

  Object.defineProperty(component, "suggestions", {
    enumerable: true,
    get() {
      const active = new Set(
        this.labels.map((label) => label.toLowerCase())
      );
      return state.recents.filter(
        (label) => !active.has(label.toLowerCase())
      );
    },
  });

  Object.defineProperty(component, "canSubmit", {
    enumerable: true,
    get() {
      return !this.isBusy && normalizeLabel(this.inputValue) !== "";
    },
  });

  const target = getEventTarget();
  if (target) {
    const handler = (event) => {
      const detail = event && event.detail ? event.detail : {};
      if (!detail || detail.issueId !== state.issueId) {
        return;
      }
      if (Array.isArray(detail.labels)) {
        const next = uniqueLabels(detail.labels);
        state.labels = next;
        component.labels = next.slice();
      }
    };
    target.addEventListener("issue:labels-applied", handler);
    state.detach = () => target.removeEventListener("issue:labels-applied", handler);
  }

  return component;
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    bdLabelEditor,
    _internals: {
      normalizeLabel,
      uniqueLabels,
      rememberLabel,
    },
  };
}

globalThis.bdLabelEditor = bdLabelEditor;
