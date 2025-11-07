const shellState =
  typeof globalThis.bdEnsureShellState === "function"
    ? globalThis.bdEnsureShellState()
    : null;

const SAFE_ESCAPE =
  typeof globalThis.CSS !== "undefined" && typeof globalThis.CSS.escape === "function"
    ? globalThis.CSS.escape.bind(globalThis.CSS)
    : (value) => {
        if (typeof value !== "string") {
          value = String(value || "");
        }
        return value.replace(/[^a-zA-Z0-9_\-]/g, "\\$&");
      };

let inflight = null;
let lastFetchAt = 0;
const MIN_REFRESH_INTERVAL = 1500;

function setCount(id, count) {
  if (typeof document === "undefined") {
    return;
  }
  const selector = `[data-role='queue-count'][data-queue-id='${SAFE_ESCAPE(id)}']`;
  const el = document.querySelector(selector);
  if (!el) {
    return;
  }
  el.textContent = String(count);
  el.dataset.count = String(count);
}

function applyCounts(counts) {
  if (!counts) {
    return;
  }
  Object.keys(counts).forEach((id) => {
    setCount(id, counts[id]);
  });
}

function refreshCounts(options) {
  const now = Date.now();
  if (!options || options.immediate !== true) {
    if (inflight) {
      return inflight;
    }
    if (now - lastFetchAt < MIN_REFRESH_INTERVAL) {
      return inflight;
    }
  }
  if (typeof fetch !== "function") {
    return Promise.resolve();
  }
  inflight = fetch("/api/queues/counts", {
    headers: {
      Accept: "application/json",
    },
  })
    .then((resp) => {
      if (!resp || !resp.ok) {
        throw new Error(`queue count fetch failed (${resp ? resp.status : "unknown"})`);
      }
      return resp.json();
    })
    .then((payload) => {
      lastFetchAt = now;
      inflight = null;
      if (
        !payload ||
        typeof payload !== "object" ||
        !Array.isArray(payload.queues)
      ) {
        return;
      }
      const counts = {};
      payload.queues.forEach((entry) => {
        if (!entry || typeof entry.id !== "string") {
          return;
        }
        const id = entry.id.trim();
        if (!id) {
          return;
        }
        const count =
          typeof entry.count === "number"
            ? entry.count
            : typeof entry.count === "string"
              ? Number(entry.count)
              : NaN;
        counts[id] = Number.isFinite(count) ? Math.max(0, Math.round(count)) : 0;
      });
      if (shellState && typeof shellState.setQueueCounts === "function") {
        shellState.setQueueCounts(counts);
      } else {
        applyCounts(counts);
      }
    })
    .catch((error) => {
      inflight = null;
      if (typeof console !== "undefined" && console.warn) {
        console.warn("[bd-ui] queue count refresh failed", error);
      }
    });
  return inflight;
}

function installListeners() {
  if (!shellState || typeof shellState.subscribeQueueCounts !== "function") {
    return;
  }
  shellState.subscribeQueueCounts(applyCounts);

  if (typeof document !== "undefined" && typeof document.addEventListener === "function") {
    document.addEventListener("queue:counts-refresh", () => refreshCounts({ immediate: false }));
  }
}

function init() {
  installListeners();
  refreshCounts({ immediate: true });
}

const api = {
  refresh: refreshCounts,
  init,
};

const existing = globalThis.bdQueueCounts || {};
globalThis.bdQueueCounts = Object.assign({}, existing, api);

if (typeof shellState !== "undefined" && shellState) {
  init();
}
