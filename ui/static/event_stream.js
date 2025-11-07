"use strict";

(function (global) {
  const EVENT_TYPES = ["created", "updated", "closed", "deleted"];
  const DEFAULT_BACKOFF_INITIAL = 1000;
  const DEFAULT_BACKOFF_MAX = 30000;
  const DEFAULT_DEBOUNCE_MS = 200;
  const DEFAULT_TOAST_DELAY = 30000;
  const DEFAULT_EVENT_SOURCE_INIT = { withCredentials: true };

  const noop = () => {};

  const setTimer =
    typeof global.setTimeout === "function"
      ? global.setTimeout.bind(global)
      : () => 0;
  const clearTimer =
    typeof global.clearTimeout === "function"
      ? global.clearTimeout.bind(global)
      : noop;

  const hasNativeEventSource = typeof global.EventSource === "function";

  function defaultNow() {
    return Date.now();
  }

  function resolveLogger(logger) {
    if (!logger || typeof logger !== "object") {
      if (typeof console !== "undefined") {
        return console;
      }
      return {
        log: noop,
        info: noop,
        warn: noop,
        error: noop,
      };
    }
    return {
      log: typeof logger.log === "function" ? logger.log.bind(logger) : noop,
      info: typeof logger.info === "function"
        ? logger.info.bind(logger)
        : typeof logger.log === "function"
          ? logger.log.bind(logger)
          : noop,
      warn: typeof logger.warn === "function" ? logger.warn.bind(logger) : noop,
      error: typeof logger.error === "function" ? logger.error.bind(logger) : noop,
    };
  }

  const STATUS_LABEL_FALLBACK = {
    open: "Ready",
    in_progress: "In Progress",
    blocked: "Blocked",
    closed: "Done",
  };

  function statusLabel(status) {
    const helpers = global.bdStatusHelpers || {};
    if (helpers && typeof helpers.labelForStatus === "function") {
      try {
        const label = helpers.labelForStatus(status);
        if (label) {
          return label;
        }
      } catch (error) {
        // Ignore helper failures and fall back to defaults.
      }
    }
    if (typeof status !== "string") {
      return status || "";
    }
    const normalized = status.toLowerCase();
    return STATUS_LABEL_FALLBACK[normalized] || status;
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
      } catch (error) {
        // Swallow dispatch errors to avoid breaking retry loops.
      }
    }
  }

  function defaultCreateEventSource(url, init) {
    if (!hasNativeEventSource) {
      return null;
    }
    try {
      return new global.EventSource(url, init);
    } catch (error) {
      return null;
    }
  }

  function createToastController(override) {
    if (
      override &&
      typeof override.show === "function" &&
      typeof override.hide === "function"
    ) {
      return {
        show(message) {
          try {
            override.show(message);
          } catch (error) {
            // Ignore custom toast failures.
          }
        },
        hide() {
          try {
            override.hide();
          } catch (error) {
            // Ignore custom toast failures.
          }
        },
      };
    }

    let toastEl = null;
    let messageEl = null;

    function ensureToast() {
      if (typeof document === "undefined") {
        return null;
      }
      if (toastEl && toastEl.isConnected) {
        return toastEl;
      }
      toastEl = document.querySelector("[data-role='event-toast']");
      if (!toastEl) {
        toastEl = document.createElement("div");
        toastEl.className = "ui-toast";
        toastEl.dataset.role = "event-toast";
        toastEl.setAttribute("role", "status");
        toastEl.setAttribute("aria-live", "polite");
        toastEl.hidden = true;

        messageEl = document.createElement("span");
        messageEl.dataset.role = "event-toast-message";
        toastEl.appendChild(messageEl);

        const closeButton = document.createElement("button");
        closeButton.type = "button";
        closeButton.className = "ui-toast__dismiss";
        closeButton.textContent = "Dismiss";
        closeButton.addEventListener("click", () => {
          hide();
        });
        toastEl.appendChild(closeButton);

        if (document.body && typeof document.body.appendChild === "function") {
          document.body.appendChild(toastEl);
        }
      } else {
        messageEl =
          toastEl.querySelector("[data-role='event-toast-message']") || toastEl;
      }
      return toastEl;
    }

    function show(message) {
      const element = ensureToast();
      if (!element) {
        return;
      }
      if (messageEl) {
        messageEl.textContent = message;
      } else {
        element.textContent = message;
      }
      element.hidden = false;
      element.classList.add("is-visible");
      element.setAttribute("data-state", "visible");
    }

    function hide() {
      if (!toastEl) {
        return;
      }
      toastEl.classList.remove("is-visible");
      toastEl.hidden = true;
      toastEl.setAttribute("data-state", "hidden");
    }

    return { show, hide };
  }

  function createController(options = {}) {
    const config = {
      url:
        typeof options.url === "string" && options.url.trim()
          ? options.url.trim()
          : "/events",
      debounceMs:
        typeof options.debounceMs === "number" && options.debounceMs >= 0
          ? options.debounceMs
          : DEFAULT_DEBOUNCE_MS,
      backoffInitial:
        typeof options.backoffInitial === "number" && options.backoffInitial > 0
          ? options.backoffInitial
          : DEFAULT_BACKOFF_INITIAL,
      backoffMax:
        typeof options.backoffMax === "number" && options.backoffMax > 0
          ? options.backoffMax
          : DEFAULT_BACKOFF_MAX,
      disconnectToastDelay:
        typeof options.disconnectToastDelay === "number" &&
        options.disconnectToastDelay >= 0
          ? options.disconnectToastDelay
          : DEFAULT_TOAST_DELAY,
      now: typeof options.now === "function" ? options.now : defaultNow,
      logger: resolveLogger(options.logger),
      createEventSource:
        typeof options.createEventSource === "function"
          ? options.createEventSource
          : (url, init) => defaultCreateEventSource(url, init),
      dispatch:
        typeof options.dispatch === "function"
          ? options.dispatch
          : defaultDispatch,
      onEvent: typeof options.onEvent === "function" ? options.onEvent : null,
      onStateChange:
        typeof options.onStateChange === "function"
          ? options.onStateChange
          : null,
      eventSourceInit:
        options.eventSourceInit && typeof options.eventSourceInit === "object"
          ? options.eventSourceInit
          : DEFAULT_EVENT_SOURCE_INIT,
    };

    let toast = createToastController(options.toast);
    config.toast = toast;

    let started = false;
    let source = null;
    let listeners = [];
    let pendingIssues = new Map();
    let debounceHandle = null;
    let reconnectHandle = null;
    let toastHandle = null;
    let reconnectAttempts = 0;
    let connectionLostAt = null;
    let lastState = "idle";
    let lastHeartbeatAt = null;

    function updateState(nextState) {
      if (lastState === nextState) {
        return;
      }
      lastState = nextState;
      if (config.onStateChange) {
        try {
          config.onStateChange(nextState);
        } catch (error) {
          config.logger.warn("event_stream:onStateChange failed", error);
        }
      }
    }

    function clearDebounce() {
      if (debounceHandle) {
        clearTimer(debounceHandle);
        debounceHandle = null;
      }
    }

    function clearReconnect() {
      if (reconnectHandle) {
        clearTimer(reconnectHandle);
        reconnectHandle = null;
      }
    }

    function clearToastTimer() {
      if (toastHandle) {
        clearTimer(toastHandle);
        toastHandle = null;
      }
    }

    function cleanupSource() {
      if (!source) {
        return;
      }
      if (typeof source.removeEventListener === "function") {
        listeners.forEach(({ type, handler }) => {
          try {
            source.removeEventListener(type, handler);
          } catch (error) {
            // Ignore removal issues.
          }
        });
      }
      listeners = [];
      if (typeof source.close === "function") {
        try {
          source.close();
        } catch (error) {
          // Ignore close failures.
        }
      }
      source.onopen = null;
      source.onerror = null;
      source = null;
    }

    function flushPending() {
      clearDebounce();
      if (!pendingIssues.size) {
        return;
      }
      const entries = Array.from(pendingIssues.values());
      pendingIssues.clear();

      entries.forEach((entry) => {
        const issue = entry.issue;
        if (!issue || !issue.id) {
          return;
        }
        const detail = {
          issueId: issue.id,
          source: "sse",
          issue,
          eventType: entry.type,
        };
        if (entry.type === "deleted") {
          try {
            config.dispatch("issue:deleted", detail);
          } catch (error) {
            config.logger.error("event_stream:dispatch delete failed", error);
          }
          return;
        }
        detail.status = issue.status;
        detail.label = statusLabel(issue.status);
        try {
          config.dispatch("issue:status-applied", detail);
        } catch (error) {
          config.logger.error("event_stream:dispatch status failed", error);
        }
      });

      const issueIds = entries.map((entry) => entry.issue.id);
      const eventPayload = entries.map((entry) => ({
        type: entry.type,
        issue: entry.issue,
      }));

      const updateDetail = {
        source: "sse",
        issues: issueIds,
        issueIds,
        issueId: issueIds.length ? issueIds[issueIds.length - 1] : undefined,
        events: eventPayload,
        timestamp: config.now(),
      };
      try {
        config.dispatch("events:update", updateDetail);
      } catch (error) {
        config.logger.error("event_stream:dispatch update failed", error);
      }
    }

    function scheduleFlush() {
      if (debounceHandle) {
        return;
      }
      debounceHandle = setTimer(() => {
        debounceHandle = null;
        flushPending();
      }, config.debounceMs);
    }

    function scheduleToast() {
      clearToastTimer();
      if (config.disconnectToastDelay <= 0) {
        return;
      }
      const lostAt = connectionLostAt || config.now();
      const elapsed = config.now() - lostAt;
      const remaining = Math.max(config.disconnectToastDelay - elapsed, 0);
      toastHandle = setTimer(() => {
        toastHandle = null;
        if (!started || lastState === "open") {
          return;
        }
        toast.show("Live updates paused — retrying connection…");
      }, remaining);
    }

    function handleEvent(type, rawEvent) {
      if (!rawEvent) {
        return;
      }
      let payload = null;
      if (rawEvent.data && typeof rawEvent.data === "string") {
        const trimmed = rawEvent.data.trim();
        if (!trimmed) {
          return;
        }
        try {
          payload = JSON.parse(trimmed);
        } catch (error) {
          config.logger.warn("event_stream:invalid payload", error);
          return;
        }
      } else if (rawEvent.data && typeof rawEvent.data === "object") {
        payload = rawEvent.data;
      } else if (rawEvent.detail && typeof rawEvent.detail === "object") {
        payload = rawEvent.detail;
      }
      if (!payload || !payload.issue || !payload.issue.id) {
        return;
      }

      updateState("open");

      if (config.onEvent) {
        try {
          config.onEvent({ type, payload });
        } catch (error) {
          config.logger.warn("event_stream:onEvent failed", error);
        }
      }

      pendingIssues.set(String(payload.issue.id), {
        type,
        issue: payload.issue,
      });
      scheduleFlush();
    }

    function handleOpen() {
      reconnectAttempts = 0;
      connectionLostAt = null;
      clearReconnect();
      clearToastTimer();
      toast.hide();
      lastHeartbeatAt = config.now();
      updateState("open");
    }

    function handleHeartbeat() {
      lastHeartbeatAt = config.now();
      updateState("open");
    }

    function scheduleReconnect() {
      cleanupSource();
      clearReconnect();
      if (!started) {
        return;
      }
      if (!connectionLostAt) {
        connectionLostAt = config.now();
      }
      scheduleToast();
      const attempt = reconnectAttempts++;
      const delay = Math.min(
        config.backoffInitial * Math.pow(2, attempt),
        config.backoffMax
      );
      updateState("waiting");
      reconnectHandle = setTimer(() => {
        reconnectHandle = null;
        connect();
      }, delay);
    }

    function handleError() {
      if (!started) {
        return;
      }
      config.logger.warn("event_stream:error detected, scheduling reconnect");
      updateState("error");
      scheduleReconnect();
    }

    function connect() {
      if (!started) {
        return;
      }
      cleanupSource();

      const eventSource = config.createEventSource(
        config.url,
        config.eventSourceInit
      );
      if (!eventSource) {
        config.logger.warn(
          "event_stream:failed to create EventSource, will retry"
        );
        updateState("error");
        scheduleReconnect();
        return;
      }

      source = eventSource;
      listeners = [];
      updateState("connecting");

      if (typeof source.addEventListener === "function") {
        EVENT_TYPES.forEach((type) => {
          const handler = (event) => handleEvent(type, event);
          listeners.push({ type, handler });
          source.addEventListener(type, handler);
        });
        const heartbeatHandler = () => handleHeartbeat();
        listeners.push({ type: "heartbeat", handler: heartbeatHandler });
        source.addEventListener("heartbeat", heartbeatHandler);
      } else if (typeof source.onmessage === "function") {
        const messageHandler = (event) => handleEvent("updated", event);
        source.onmessage = messageHandler;
      }

      source.onopen = handleOpen;
      source.onerror = handleError;
    }

    function start() {
      if (started) {
        return controller;
      }
      started = true;
      if (!config.createEventSource && !hasNativeEventSource) {
        updateState("unsupported");
        return controller;
      }
      connect();
      return controller;
    }

    function stop() {
      if (!started) {
        return controller;
      }
      started = false;
      clearDebounce();
      clearReconnect();
      clearToastTimer();
      cleanupSource();
      pendingIssues.clear();
      toast.hide();
      updateState("stopped");
      return controller;
    }

    function configure(nextOptions) {
      if (!nextOptions || typeof nextOptions !== "object") {
        return controller;
      }
      if (typeof nextOptions.url === "string" && nextOptions.url.trim()) {
        config.url = nextOptions.url.trim();
      }
      if (typeof nextOptions.debounceMs === "number" && nextOptions.debounceMs >= 0) {
        config.debounceMs = nextOptions.debounceMs;
      }
      if (
        typeof nextOptions.disconnectToastDelay === "number" &&
        nextOptions.disconnectToastDelay >= 0
      ) {
        config.disconnectToastDelay = nextOptions.disconnectToastDelay;
      }
      if (
        typeof nextOptions.backoffInitial === "number" &&
        nextOptions.backoffInitial > 0
      ) {
        config.backoffInitial = nextOptions.backoffInitial;
      }
      if (
        typeof nextOptions.backoffMax === "number" &&
        nextOptions.backoffMax > 0
      ) {
        config.backoffMax = nextOptions.backoffMax;
      }
      if (typeof nextOptions.dispatch === "function") {
        config.dispatch = nextOptions.dispatch;
      }
      if (typeof nextOptions.createEventSource === "function") {
        config.createEventSource = nextOptions.createEventSource;
      }
      if (
        nextOptions.eventSourceInit &&
        typeof nextOptions.eventSourceInit === "object"
      ) {
        config.eventSourceInit = nextOptions.eventSourceInit;
      }
      if (typeof nextOptions.now === "function") {
        config.now = nextOptions.now;
      }
      if (typeof nextOptions.onEvent === "function") {
        config.onEvent = nextOptions.onEvent;
      }
      if (typeof nextOptions.onStateChange === "function") {
        config.onStateChange = nextOptions.onStateChange;
      }
      if (nextOptions.logger && typeof nextOptions.logger === "object") {
        config.logger = resolveLogger(nextOptions.logger);
      }
      if (
        nextOptions.toast &&
        typeof nextOptions.toast.show === "function" &&
        typeof nextOptions.toast.hide === "function"
      ) {
        toast = createToastController(nextOptions.toast);
        config.toast = toast;
      } else if (nextOptions.toast === null) {
        toast = createToastController(null);
        config.toast = toast;
      }
      return controller;
    }

    const controller = {
      start,
      stop,
      configure,
      state() {
        return lastState;
      },
      isConnected() {
        return lastState === "open";
      },
      getLastHeartbeat() {
        return lastHeartbeatAt;
      },
      getPendingIssueCount() {
        return pendingIssues.size;
      },
    };

    return controller;
  }

  let singleton = null;

  function init(options) {
    if (!singleton) {
      singleton = createController(options);
      singleton.start();
      return singleton;
    }
    singleton.configure(options);
    singleton.start();
    return singleton;
  }

  const api = {
    create: createController,
    init,
    get() {
      return singleton;
    },
  };

  const existing = global.bdEventStream || {};
  global.bdEventStream = Object.assign({}, existing, api);

  if (typeof module !== "undefined" && module.exports) {
    module.exports = global.bdEventStream;
  }
})(typeof globalThis !== "undefined" ? globalThis : window);
