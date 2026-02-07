// bd web dashboard — table.js
// sortable, filterable table view (uses lowercase JSON field names)

(function () {
  'use strict';

  var sortColumn = 'priority';
  var sortAsc = true;

  var columnDefs = [
    { key: 'status', label: 'Status', width: '100px' },
    { key: 'id', label: 'ID', width: '110px' },
    { key: 'priority', label: 'Pri', width: '50px' },
    { key: 'title', label: 'Title', width: '' },
    { key: 'issue_type', label: 'Type', width: '80px' },
    { key: 'assignee', label: 'Assignee', width: '120px' },
    { key: 'updated_at', label: 'Updated', width: '90px' },
  ];

  window.BD.renderTable = function () {
    var container = document.getElementById('view-table');
    var issues = BD.getFilteredIssues();

    // sort
    issues = issues.slice().sort(function (a, b) {
      var va = getSortValue(a, sortColumn);
      var vb = getSortValue(b, sortColumn);
      var cmp = 0;
      if (va < vb) cmp = -1;
      else if (va > vb) cmp = 1;
      return sortAsc ? cmp : -cmp;
    });

    if (issues.length === 0) {
      container.innerHTML = '<div class="empty-state"><div class="empty-state-icon">&#128269;</div><div class="empty-state-text">no issues match current filters</div></div>';
      return;
    }

    var html = '<table class="issue-table">';
    html += '<thead><tr>';
    columnDefs.forEach(function (col) {
      var arrow = '';
      if (col.key === sortColumn) {
        arrow = '<span class="sort-arrow">' + (sortAsc ? '\u25B2' : '\u25BC') + '</span>';
      }
      var style = col.width ? ' style="width:' + col.width + '"' : '';
      html += '<th data-sort="' + col.key + '"' + style + '>' + col.label + arrow + '</th>';
    });
    html += '</tr></thead><tbody>';

    issues.forEach(function (issue) {
      html += '<tr data-id="' + BD.escapeHTML(issue.id) + '">';
      html += '<td><span class="status-dot" style="background:' + BD.statusColor(issue.status) + '"></span>' + BD.escapeHTML(issue.status) + '</td>';
      html += '<td style="font-family:monospace;font-size:12px;color:var(--text-dim)">' + BD.escapeHTML(issue.id) + '</td>';
      html += '<td><span class="' + BD.priorityClass(issue.priority) + '">' + BD.priorityLabel(issue.priority) + '</span></td>';
      html += '<td>' + BD.escapeHTML(issue.title) + '</td>';
      html += '<td>';
      if (issue.issue_type) {
        html += '<span class="' + BD.typeClass(issue.issue_type) + '">' + BD.escapeHTML(issue.issue_type) + '</span>';
      }
      html += '</td>';
      html += '<td style="color:var(--text-muted)">' + BD.escapeHTML(issue.assignee || '') + '</td>';
      html += '<td style="color:var(--text-dim);font-size:12px">' + BD.formatDate(issue.updated_at) + '</td>';
      html += '</tr>';
    });

    html += '</tbody></table>';
    container.innerHTML = html;

    // sort click handlers
    container.querySelectorAll('th[data-sort]').forEach(function (th) {
      th.addEventListener('click', function () {
        var key = th.dataset.sort;
        if (sortColumn === key) {
          sortAsc = !sortAsc;
        } else {
          sortColumn = key;
          sortAsc = true;
        }
        BD.renderTable();
      });
    });

    // row click handlers
    container.querySelectorAll('tbody tr').forEach(function (row) {
      row.addEventListener('click', function () {
        if (window.BD.openDetail) BD.openDetail(row.dataset.id);
      });
    });
  };

  function getSortValue(issue, key) {
    var v = issue[key];
    if (v === undefined || v === null) return '';
    if (key === 'priority') return v || 0;
    if (typeof v === 'string') return v.toLowerCase();
    return v;
  }
})();
