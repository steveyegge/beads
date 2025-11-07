"use strict";

(function (global) {
  function toId(value) {
    if (typeof value === "string") {
      return value.trim();
    }
    if (value == null) {
      return "";
    }
    return String(value).trim();
  }

  function normalizeList(values) {
    if (!Array.isArray(values)) {
      return [];
    }
    const seen = new Set();
    const result = [];
    for (const value of values) {
      const id = toId(value);
      if (!id || seen.has(id)) {
        continue;
      }
      seen.add(id);
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

  function createStoreBridge(store) {
    if (!store || typeof store !== "object") {
      return {
        get() {
          return [];
        },
        set() {},
        subscribe() {
          return () => {};
        },
      };
    }

    const get =
      typeof store.getSelection === "function"
        ? () => {
            try {
              const value = store.getSelection();
              return Array.isArray(value) ? value.slice() : [];
            } catch {
              return [];
            }
          }
        : () => [];

    const set =
      typeof store.setSelection === "function"
        ? (ids) => {
            try {
              store.setSelection(Array.isArray(ids) ? ids.slice() : []);
            } catch {
              // Ignore store setter errors.
            }
          }
        : () => {};

    const subscribe =
      typeof store.subscribeSelection === "function"
        ? (listener) => {
            if (typeof listener !== "function") {
              return () => {};
            }
            try {
              const unsubscribe = store.subscribeSelection(listener);
              if (typeof unsubscribe === "function") {
                return unsubscribe;
              }
            } catch {
              // ignore subscribe failure
            }
            return () => {};
          }
        : () => () => {};

    return { get, set, subscribe };
  }

  function createModel(config = {}) {
    let items = normalizeList(config.items);
    let itemLookup = new Set(items);
    const storeBridge = createStoreBridge(config.store);
    const subscribers = new Set();

    let selectionSet = new Set();
    let anchor = "";
    let syncDepth = 0;

    function inSync() {
      return syncDepth > 0;
    }

    function beginSync() {
      syncDepth += 1;
    }

    function endSync() {
      if (syncDepth > 0) {
        syncDepth -= 1;
      }
    }

    function rebuildLookup(nextItems) {
      items = normalizeList(nextItems);
      itemLookup = new Set(items);
    }

    function currentSelection() {
      return items.filter((id) => selectionSet.has(id));
    }

    function setAnchor(candidate) {
      const id = toId(candidate);
      if (id && itemLookup.has(id)) {
        anchor = id;
        return;
      }
      const snapshot = currentSelection();
      anchor = snapshot.length > 0 ? snapshot[snapshot.length - 1] : "";
    }

    function publish(nextSelection, source) {
      if (source !== "store") {
        let ownsSync = false;
        if (!inSync()) {
          beginSync();
          ownsSync = true;
        }
        try {
          storeBridge.set(nextSelection);
        } catch {
          // ignore store bridge errors
        } finally {
          if (ownsSync) {
            endSync();
          }
        }
      }

      subscribers.forEach((listener) => {
        try {
          listener(nextSelection.slice());
        } catch (error) {
          if (console && console.error) {
            console.error("bdMultiSelect subscriber error", error);
          }
        }
      });
    }

    function applySelection(ids, options = {}) {
      const normalized = normalizeList(ids).filter((id) => itemLookup.has(id));
      const existing = currentSelection();
      if (listsEqual(existing, normalized)) {
        if (options.anchor !== undefined) {
          setAnchor(options.anchor);
        } else {
          setAnchor(anchor);
        }
        return existing;
      }

      selectionSet = new Set(normalized);
      if (options.anchor !== undefined) {
        setAnchor(options.anchor);
      } else {
        setAnchor(anchor);
      }

      const snapshot = currentSelection();
      publish(snapshot, options.source || "controller");
      return snapshot;
    }

    function toggle(id, opts = {}) {
      const issueId = toId(id);
      if (!issueId || !itemLookup.has(issueId)) {
        return currentSelection();
      }

      if (opts && opts.range) {
        if (!anchor || !itemLookup.has(anchor)) {
          anchor = issueId;
        }
        const anchorIndex = items.indexOf(anchor);
        const targetIndex = items.indexOf(issueId);
        if (anchorIndex === -1 || targetIndex === -1) {
          return applySelection([issueId], { anchor: issueId });
        }
        const start = Math.min(anchorIndex, targetIndex);
        const end = Math.max(anchorIndex, targetIndex);
        const range = items.slice(start, end + 1);
        return applySelection(range, { anchor: issueId });
      }

      const next = new Set(selectionSet);
      if (next.has(issueId)) {
        next.delete(issueId);
      } else {
        next.add(issueId);
      }
      return applySelection(Array.from(next), { anchor: issueId });
    }

    function handleKey(event, id) {
      if (!event || typeof event.key !== "string") {
        return false;
      }
      const key = event.key.length === 1 ? event.key : event.key.toLowerCase();
      if (key !== " " && key !== "space" && key !== "spacebar") {
        return false;
      }
      toggle(id);
      if (typeof event.preventDefault === "function") {
        event.preventDefault();
      }
      if (typeof event.stopPropagation === "function") {
        event.stopPropagation();
      }
      return true;
    }

    function setItems(nextItems) {
      const previous = currentSelection();
      rebuildLookup(nextItems);
      const filtered = previous.filter((id) => itemLookup.has(id));
      return applySelection(filtered, { anchor });
    }

    function clear() {
      return applySelection([], { anchor: "" });
    }

    function subscribe(listener) {
      if (typeof listener !== "function") {
        return () => {};
      }
      subscribers.add(listener);
      return () => {
        subscribers.delete(listener);
      };
    }

    const initial = config.initialSelection || storeBridge.get();
    if (Array.isArray(initial) && initial.length > 0) {
      applySelection(initial, { source: "store" });
    } else {
      selectionSet = new Set();
      setAnchor("");
    }

    const unsubscribeStore = storeBridge.subscribe((ids) => {
      if (inSync()) {
        return;
      }
      beginSync();
      try {
        applySelection(ids, { source: "store" });
      } finally {
        endSync();
      }
    });

    return {
      getSelection: currentSelection,
      toggle,
      handleKey,
      setItems,
      clear,
      subscribe,
      setSelection(ids) {
        return applySelection(ids, { anchor });
      },
      destroy() {
        subscribers.clear();
        if (typeof unsubscribeStore === "function") {
          unsubscribeStore();
        }
      },
    };
  }

  global.bdMultiSelect = Object.assign(global.bdMultiSelect || {}, {
    createModel,
  });
})(typeof window !== "undefined" ? window : globalThis);
