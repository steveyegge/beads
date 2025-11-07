(function (global) {
  "use strict";

  const ACTION_NONE = "none";
  const ACTION_NEXT = "next";
  const ACTION_PREV = "prev";
  const ACTION_ACTIVATE = "activate";

  function isModifierPressed(event) {
    return !!(event.altKey || event.ctrlKey || event.metaKey);
  }

  function mapKeyToAction(event) {
    if (!event || typeof event.key !== "string") {
      return ACTION_NONE;
    }
    if (isModifierPressed(event)) {
      return ACTION_NONE;
    }
    const key = event.key.toLowerCase();
    switch (key) {
      case "j":
      case "arrowdown":
        return ACTION_NEXT;
      case "k":
      case "arrowup":
        return ACTION_PREV;
      case "enter":
      case " ":
        return ACTION_ACTIVATE;
      default:
        return ACTION_NONE;
    }
  }

  function focusElement(el) {
    if (!el) {
      return;
    }
    try {
      el.focus({ preventScroll: true });
    } catch {
      el.focus();
    }
  }

  function setRowState(row, isActive) {
    if (!row) {
      return;
    }
    row.classList.toggle("is-active", isActive);
    row.setAttribute("aria-selected", isActive ? "true" : "false");
    row.setAttribute("tabindex", isActive ? "0" : "-1");
  }

  function createIssueListNavigator(listEl, opts = {}) {
    if (!listEl) {
      throw new Error("listEl is required");
    }

    const options = {
      autoFocus: true,
      onActivate(row) {
        if (row && typeof row.click === "function") {
          row.click();
        }
      },
      onSelectionChange() {},
      ...opts,
    };

    const state = {
      rows: [],
      activeIndex: -1,
    };

    if (typeof listEl.dataset !== "object" || listEl.dataset === null) {
      listEl.dataset = {};
    }
    if (!Object.prototype.hasOwnProperty.call(listEl.dataset, "announceIssueIds")) {
      listEl.dataset.announceIssueIds = "true";
    }

    function buildAnnouncement(row, fallbackId) {
      if (!row) {
        return "";
      }
      const ariaLabel =
        typeof row.getAttribute === "function"
          ? (row.getAttribute("aria-label") || "").trim()
          : "";
      if (ariaLabel) {
        return ariaLabel;
      }
      const titleEl =
        typeof row.querySelector === "function"
          ? row.querySelector(".ui-issue-row-title")
          : null;
      const titleText =
        titleEl && typeof titleEl.textContent === "string"
          ? titleEl.textContent.trim()
          : "";
      const id =
        (row.dataset && typeof row.dataset.issueId === "string"
          ? row.dataset.issueId
          : "") || fallbackId || "";
      const parts = [];
      if (id) {
        parts.push(id);
      }
      if (titleText) {
        parts.push(titleText);
      }
      return parts.join(" · ").trim();
    }

    function updateDataset() {
      const activeRow =
        state.activeIndex >= 0 ? state.rows[state.activeIndex] : null;
      const activeId =
        activeRow && typeof activeRow.getAttribute === "function"
          ? activeRow.getAttribute("data-issue-id") || ""
          : "";
      listEl.dataset.activeIssueId = activeId;

      if ((listEl.dataset.announceIssueIds || "true") !== "false") {
        const announcement = buildAnnouncement(activeRow, activeId);
        if (announcement) {
          listEl.dataset.activeIssueAnnouncement = announcement;
        } else {
          delete listEl.dataset.activeIssueAnnouncement;
        }
      } else if (listEl.dataset.activeIssueAnnouncement) {
        delete listEl.dataset.activeIssueAnnouncement;
      }
    }

    function applySelection(nextIndex, { shouldFocus = true } = {}) {
      if (state.rows.length === 0) {
        state.activeIndex = -1;
        updateDataset();
        return;
      }

      const boundedIndex = Math.max(
        0,
        Math.min(state.rows.length - 1, nextIndex)
      );
      if (boundedIndex === state.activeIndex) {
        return;
      }

      state.rows.forEach((row, idx) => setRowState(row, idx === boundedIndex));
      state.activeIndex = boundedIndex;
      updateDataset();
      options.onSelectionChange(state.rows[boundedIndex], boundedIndex);

      if (shouldFocus && options.autoFocus !== false) {
        focusElement(state.rows[boundedIndex]);
      }
    }

    function refresh() {
      state.rows = Array.from(
        listEl.querySelectorAll("[data-role='issue-row']")
      );
      listEl.dataset.issueRowCount = String(state.rows.length);

      let activeIndex = state.rows.findIndex((row) =>
        row.classList.contains("is-active")
      );
      if (activeIndex === -1 && state.rows.length > 0) {
        activeIndex = 0;
        state.rows.forEach((row, idx) => setRowState(row, idx === activeIndex));
      } else {
        state.rows.forEach((row, idx) =>
          setRowState(row, idx === activeIndex)
        );
      }
      state.activeIndex = activeIndex;
      updateDataset();
    }

    function move(delta) {
      if (state.rows.length === 0) {
        return;
      }
      const nextIndex =
        state.activeIndex === -1 ? 0 : state.activeIndex + delta;
      applySelection(nextIndex);
    }

    function activate() {
      if (state.activeIndex < 0) {
        return;
      }
      const row = state.rows[state.activeIndex];
      options.onActivate(row, state.activeIndex);
    }

    function handleKey(event) {
      const action = mapKeyToAction(event);
      switch (action) {
        case ACTION_NEXT:
          move(1);
          return true;
        case ACTION_PREV:
          move(-1);
          return true;
        case ACTION_ACTIVATE:
          activate();
          return true;
        default:
          return false;
      }
    }

    refresh();

    return {
      refresh,
      handleKey,
      move,
      activate,
      getActiveIssueId() {
        return listEl.dataset.activeIssueId || "";
      },
    };
  }

  const exports = {
    mapKeyToAction,
    createIssueListNavigator,
  };

  if (typeof module !== "undefined" && module.exports) {
    module.exports = exports;
  }

  global.bdNavigation = Object.assign(global.bdNavigation || {}, exports);
})(typeof window !== "undefined" ? window : globalThis);
