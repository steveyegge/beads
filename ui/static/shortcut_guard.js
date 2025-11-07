"use strict";

(function (global) {
  const EDITABLE_INPUT_TYPES = new Set([
    "",
    "text",
    "search",
    "email",
    "url",
    "tel",
    "password",
    "number",
    "date",
    "datetime",
    "datetime-local",
    "time",
    "month",
    "week",
  ]);

  function isElement(value) {
    return !!(value && typeof value === "object" && typeof value.tagName === "string");
  }

  function isContentEditable(node) {
    if (!node || typeof node !== "object") {
      return false;
    }
    if (node.isContentEditable) {
      return true;
    }
    if (typeof node.getAttribute === "function") {
      const attr = node.getAttribute("contenteditable");
      if (typeof attr === "string" && attr.toLowerCase() === "true") {
        return true;
      }
    }
    if (typeof node.closest === "function") {
      const editable = node.closest("[contenteditable='true']");
      if (editable) {
        return true;
      }
    }
    return false;
  }

  function isEditableTarget(target) {
    if (!isElement(target)) {
      return isContentEditable(target);
    }
    const tagName = target.tagName.toLowerCase();
    if (target.hasAttribute && target.hasAttribute("data-shortcut-allow")) {
      return false;
    }
    if (tagName === "textarea") {
      return !target.readOnly && !target.disabled;
    }
    if (tagName === "select") {
      return !target.disabled;
    }
    if (tagName === "input") {
      const type = typeof target.type === "string" ? target.type.toLowerCase() : "";
      if (target.disabled) {
        return false;
      }
      if (target.readOnly && !EDITABLE_INPUT_TYPES.has(type)) {
        return false;
      }
      return EDITABLE_INPUT_TYPES.has(type);
    }
    return isContentEditable(target);
  }

  function pathContainsEditable(event) {
    if (!event) {
      return false;
    }
    const path =
      (typeof event.composedPath === "function" && event.composedPath()) ||
      (Array.isArray(event.path) && event.path) ||
      [];
    for (let i = 0; i < path.length; i += 1) {
      if (isEditableTarget(path[i])) {
        return true;
      }
    }
    return false;
  }

  function shouldIgnoreEvent(event) {
    if (!event) {
      return false;
    }
    if (isEditableTarget(event.target)) {
      return true;
    }
    if (pathContainsEditable(event)) {
      return true;
    }
    if (typeof document !== "undefined" && isEditableTarget(document.activeElement)) {
      return true;
    }
    return false;
  }

  const utils =
    (typeof global.bdShortcutUtils === "object" && global.bdShortcutUtils !== null && global.bdShortcutUtils) || {};

  utils.isEditableTarget = isEditableTarget;
  utils.shouldIgnoreEvent = shouldIgnoreEvent;
  utils.EDITABLE_INPUT_TYPES = EDITABLE_INPUT_TYPES;

  global.bdShortcutUtils = utils;
})(typeof window !== "undefined" ? window : globalThis);
