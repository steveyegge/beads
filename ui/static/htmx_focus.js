(function (global) {
  "use strict";

  function datasetRole(target) {
    if (!target || !target.dataset) {
      return "";
    }
    const value = target.dataset.role;
    return typeof value === "string" ? value.trim().toLowerCase() : "";
  }

  function shouldRestoreIssueListFocus(target) {
    return datasetRole(target) === "issue-list";
  }

  const helpers = {
    datasetRole,
    shouldRestoreIssueListFocus,
  };

  const existing =
    typeof global.bdHtmxFocusHelpers === "object" &&
    global.bdHtmxFocusHelpers !== null
      ? global.bdHtmxFocusHelpers
      : {};
  global.bdHtmxFocusHelpers = Object.assign({}, existing, helpers);
})(typeof globalThis !== "undefined" ? globalThis : window);
