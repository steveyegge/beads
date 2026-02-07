// bd web dashboard — kanban.js
// kanban board view (uses lowercase JSON field names)

(function () {
  'use strict';

  var columns = [
    { status: 'open', label: 'Open' },
    { status: 'in_progress', label: 'In Progress' },
    { status: 'blocked', label: 'Blocked' },
    { status: 'deferred', label: 'Deferred' },
  ];

  window.BD.renderKanban = function () {
    var container = document.getElementById('view-kanban');
    var issues = BD.getFilteredIssues();

    // group by status
    var groups = {};
    columns.forEach(function (col) { groups[col.status] = []; });

    issues.forEach(function (issue) {
      if (groups[issue.status]) {
        groups[issue.status].push(issue);
      }
    });

    // sort within columns: priority asc, then updated desc
    Object.keys(groups).forEach(function (status) {
      groups[status].sort(function (a, b) {
        if (a.priority !== b.priority) return a.priority - b.priority;
        return new Date(b.updated_at) - new Date(a.updated_at);
      });
    });

    var html = '<div class="kanban-board">';

    columns.forEach(function (col) {
      var cards = groups[col.status] || [];
      html += '<div class="kanban-column">';
      html += '<div class="kanban-column-header">';
      html += '<span>' + col.label + '</span>';
      html += '<span class="kanban-column-count">' + cards.length + '</span>';
      html += '</div>';
      html += '<div class="kanban-column-cards">';

      if (cards.length === 0) {
        html += '<div class="empty-state" style="padding:20px"><div class="empty-state-text">no issues</div></div>';
      }

      cards.forEach(function (issue) {
        html += renderCard(issue);
      });

      html += '</div></div>';
    });

    html += '</div>';
    container.innerHTML = html;

    // attach card click handlers
    container.querySelectorAll('.kanban-card').forEach(function (card) {
      card.addEventListener('click', function () {
        if (window.BD.openDetail) BD.openDetail(card.dataset.id);
      });
    });
  };

  function renderCard(issue) {
    var html = '<div class="kanban-card" data-id="' + BD.escapeHTML(issue.id) + '">';
    html += '<div class="kanban-card-title">' + BD.escapeHTML(issue.title) + '</div>';
    html += '<div class="kanban-card-meta">';
    html += '<span class="kanban-card-id">' + BD.escapeHTML(issue.id) + '</span>';
    html += '<span class="' + BD.priorityClass(issue.priority) + '">' + BD.priorityLabel(issue.priority) + '</span>';

    if (issue.issue_type) {
      html += '<span class="' + BD.typeClass(issue.issue_type) + '">' + BD.escapeHTML(issue.issue_type) + '</span>';
    }

    if (issue.assignee) {
      html += '<span class="badge badge-assignee">' + BD.escapeHTML(issue.assignee) + '</span>';
    }

    if (issue.labels && issue.labels.length > 0) {
      issue.labels.slice(0, 2).forEach(function (label) {
        html += '<span class="badge badge-label">' + BD.escapeHTML(label) + '</span>';
      });
    }

    html += '</div></div>';
    return html;
  }
})();
