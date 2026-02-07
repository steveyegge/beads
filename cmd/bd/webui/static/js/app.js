// bd web dashboard — app.js
// entry point, state store, SSE client, router
//
// JSON field mapping (Go struct tags → JS):
//   issue.id, issue.title, issue.status, issue.priority,
//   issue.issue_type, issue.assignee, issue.created_at,
//   issue.updated_at, issue.closed_at, issue.created_by,
//   issue.description, issue.close_reason, issue.labels,
//   issue.dependencies, issue.comments, issue.acceptance_criteria

(function () {
  'use strict';

  // ── state store ──
  window.BD = {
    issues: [],
    stats: null,
    currentView: 'kanban',
    filters: {
      search: '',
      statuses: ['open', 'in_progress', 'blocked'],
      priority: '',
      type: '',
    },
    detail: null,
  };

  // ── api helpers ──
  async function fetchJSON(url) {
    var resp = await fetch(url);
    if (!resp.ok) return null;
    return resp.json();
  }

  window.BD.fetchJSON = fetchJSON;

  async function loadIssues() {
    var data = await fetchJSON('/api/issues');
    if (data) {
      BD.issues = Array.isArray(data) ? data : [];
      renderCurrentView();
    }
  }

  async function loadStats() {
    var data = await fetchJSON('/api/stats');
    if (data) {
      BD.stats = data;
      renderStats();
    }
  }

  function renderStats() {
    var el = document.getElementById('stats-bar');
    if (!BD.stats || !el) return;
    var s = BD.stats;
    el.innerHTML = [
      statItem('total', s.total_issues, '--text-muted'),
      statItem('open', s.open_issues, '--status-open'),
      statItem('active', s.in_progress_issues, '--status-in-progress'),
      statItem('blocked', s.blocked_issues, '--status-blocked'),
      statItem('closed', s.closed_issues, '--status-closed'),
    ].join('');
  }

  function statItem(label, count, colorVar) {
    return '<span class="stat-item">' +
      '<span class="stat-dot" style="background:var(' + colorVar + ')"></span>' +
      count + ' ' + label +
      '</span>';
  }

  // ── SSE client ──
  function connectSSE() {
    var indicator = document.getElementById('sse-status');
    var es = new EventSource('/api/events');

    es.addEventListener('connected', function () {
      indicator.className = 'sse-status connected';
      indicator.title = 'connected';
    });

    es.addEventListener('mutation', function () {
      loadIssues();
      loadStats();
    });

    es.onerror = function () {
      indicator.className = 'sse-status reconnecting';
      indicator.title = 'reconnecting...';
    };

    es.onopen = function () {
      indicator.className = 'sse-status connected';
      indicator.title = 'connected';
    };
  }

  // ── filtering ──
  window.BD.getFilteredIssues = function () {
    return BD.issues.filter(function (issue) {
      if (BD.filters.statuses.length > 0) {
        if (BD.filters.statuses.indexOf(issue.status) === -1) return false;
      }
      if (BD.filters.priority !== '') {
        if (issue.priority !== parseInt(BD.filters.priority, 10)) return false;
      }
      if (BD.filters.type !== '') {
        if (issue.issue_type !== BD.filters.type) return false;
      }
      if (BD.filters.search) {
        var q = BD.filters.search.toLowerCase();
        var inTitle = (issue.title || '').toLowerCase().indexOf(q) !== -1;
        var inID = (issue.id || '').toLowerCase().indexOf(q) !== -1;
        var inAssignee = (issue.assignee || '').toLowerCase().indexOf(q) !== -1;
        if (!inTitle && !inID && !inAssignee) return false;
      }
      return true;
    });
  };

  // ── view routing ──
  function switchView(view) {
    BD.currentView = view;
    document.querySelectorAll('.tab').forEach(function (t) {
      t.classList.toggle('active', t.dataset.view === view);
    });
    document.querySelectorAll('.view').forEach(function (v) {
      v.classList.toggle('active', v.id === 'view-' + view);
    });
    renderCurrentView();
  }

  function renderCurrentView() {
    switch (BD.currentView) {
      case 'kanban':
        if (window.BD.renderKanban) BD.renderKanban();
        break;
      case 'table':
        if (window.BD.renderTable) BD.renderTable();
        break;
      case 'graph':
        if (window.BD.renderGraph) BD.renderGraph();
        break;
    }
  }

  window.BD.renderCurrentView = renderCurrentView;

  // ── status filter as toggle buttons ──
  function initFilters() {
    var searchInput = document.getElementById('filter-search');
    var statusContainer = document.getElementById('filter-status');
    var prioritySelect = document.getElementById('filter-priority');
    var typeSelect = document.getElementById('filter-type');

    // render status toggle buttons
    renderStatusToggles(statusContainer);

    var debounceTimer;
    searchInput.addEventListener('input', function () {
      clearTimeout(debounceTimer);
      debounceTimer = setTimeout(function () {
        BD.filters.search = searchInput.value;
        renderCurrentView();
      }, 200);
    });

    prioritySelect.addEventListener('change', function () {
      BD.filters.priority = prioritySelect.value;
      renderCurrentView();
    });

    typeSelect.addEventListener('change', function () {
      BD.filters.type = typeSelect.value;
      renderCurrentView();
    });
  }

  function renderStatusToggles(container) {
    var statuses = [
      { value: 'open', label: 'open' },
      { value: 'in_progress', label: 'active' },
      { value: 'blocked', label: 'blocked' },
      { value: 'deferred', label: 'deferred' },
      { value: 'closed', label: 'closed' },
    ];

    var html = '';
    statuses.forEach(function (s) {
      var active = BD.filters.statuses.indexOf(s.value) !== -1;
      html += '<button class="status-toggle' + (active ? ' active' : '') + '" data-status="' + s.value + '">' + s.label + '</button>';
    });
    container.innerHTML = html;

    container.querySelectorAll('.status-toggle').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var status = btn.dataset.status;
        var idx = BD.filters.statuses.indexOf(status);
        if (idx === -1) {
          BD.filters.statuses.push(status);
          btn.classList.add('active');
        } else {
          BD.filters.statuses.splice(idx, 1);
          btn.classList.remove('active');
        }
        renderCurrentView();
      });
    });
  }

  // ── tab click handlers ──
  function initTabs() {
    document.querySelectorAll('.tab').forEach(function (tab) {
      tab.addEventListener('click', function () {
        switchView(tab.dataset.view);
      });
    });
  }

  // ── helpers (all use lowercase JSON field names) ──
  window.BD.priorityClass = function (p) {
    return 'badge badge-priority badge-p' + p;
  };

  window.BD.priorityLabel = function (p) {
    return 'P' + p;
  };

  window.BD.typeClass = function (t) {
    return 'badge badge-type badge-type-' + (t || 'task');
  };

  window.BD.statusColor = function (status) {
    var map = {
      open: 'var(--status-open)',
      in_progress: 'var(--status-in-progress)',
      blocked: 'var(--status-blocked)',
      deferred: 'var(--status-deferred)',
      closed: 'var(--status-closed)',
    };
    return map[status] || 'var(--text-dim)';
  };

  window.BD.escapeHTML = function (s) {
    if (!s) return '';
    var div = document.createElement('div');
    div.textContent = s;
    return div.innerHTML;
  };

  window.BD.formatDate = function (dateStr) {
    if (!dateStr) return '-';
    var d = new Date(dateStr);
    if (isNaN(d.getTime())) return '-';
    return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  };

  // ── init ──
  document.addEventListener('DOMContentLoaded', function () {
    initTabs();
    initFilters();
    connectSSE();
    loadIssues();
    loadStats();
  });
})();
