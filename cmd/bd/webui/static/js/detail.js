// bd web dashboard — detail.js
// issue detail slide-out panel (uses lowercase JSON field names)

(function () {
  'use strict';

  var overlay = null;
  var panel = null;
  var content = null;

  function init() {
    overlay = document.getElementById('detail-overlay');
    panel = document.getElementById('detail-panel');
    content = document.getElementById('detail-content');

    document.getElementById('detail-close').addEventListener('click', closeDetail);
    overlay.addEventListener('click', closeDetail);

    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') closeDetail();
    });
  }

  window.BD.openDetail = function (issueID) {
    if (!overlay) init();

    overlay.classList.add('open');
    panel.classList.add('open');

    content.innerHTML = '<div style="color:var(--text-dim);padding:20px">loading...</div>';

    BD.fetchJSON('/api/issues/' + encodeURIComponent(issueID)).then(function (issue) {
      if (!issue) {
        content.innerHTML = '<div style="color:var(--text-dim);padding:20px">issue not found</div>';
        return;
      }
      renderDetail(issue);
    });
  };

  function closeDetail() {
    if (overlay) overlay.classList.remove('open');
    if (panel) panel.classList.remove('open');
    BD.detail = null;
  }

  function renderDetail(issue) {
    BD.detail = issue;
    var esc = BD.escapeHTML;

    var html = '';
    html += '<div class="detail-title">' + esc(issue.title) + '</div>';
    html += '<div class="detail-id">' + esc(issue.id) + '</div>';

    // meta grid
    html += '<div class="detail-section">';
    html += '<div class="detail-meta-grid">';
    html += metaItem('Status', '<span class="status-dot" style="background:' + BD.statusColor(issue.status) + '"></span>' + esc(issue.status));
    html += metaItem('Priority', '<span class="' + BD.priorityClass(issue.priority) + '">' + BD.priorityLabel(issue.priority) + '</span>');
    if (issue.issue_type) {
      html += metaItem('Type', '<span class="' + BD.typeClass(issue.issue_type) + '">' + esc(issue.issue_type) + '</span>');
    }
    if (issue.assignee) {
      html += metaItem('Assignee', esc(issue.assignee));
    }
    html += metaItem('Created', BD.formatDate(issue.created_at));
    html += metaItem('Updated', BD.formatDate(issue.updated_at));
    if (issue.closed_at) {
      html += metaItem('Closed', BD.formatDate(issue.closed_at));
    }
    if (issue.created_by) {
      html += metaItem('Created by', esc(issue.created_by));
    }
    html += '</div></div>';

    // labels
    if (issue.labels && issue.labels.length > 0) {
      html += '<div class="detail-section">';
      html += '<div class="detail-section-title">Labels</div>';
      html += '<div style="display:flex;gap:4px;flex-wrap:wrap">';
      issue.labels.forEach(function (label) {
        html += '<span class="badge badge-label">' + esc(label) + '</span>';
      });
      html += '</div></div>';
    }

    // description
    if (issue.description) {
      html += '<div class="detail-section">';
      html += '<div class="detail-section-title">Description</div>';
      html += '<div class="detail-description">' + esc(issue.description) + '</div>';
      html += '</div>';
    }

    // acceptance criteria
    if (issue.acceptance_criteria) {
      html += '<div class="detail-section">';
      html += '<div class="detail-section-title">Acceptance Criteria</div>';
      html += '<div class="detail-description">' + esc(issue.acceptance_criteria) + '</div>';
      html += '</div>';
    }

    // close reason
    if (issue.close_reason) {
      html += '<div class="detail-section">';
      html += '<div class="detail-section-title">Close Reason</div>';
      html += '<div class="detail-description">' + esc(issue.close_reason) + '</div>';
      html += '</div>';
    }

    // dependencies
    if (issue.dependencies && issue.dependencies.length > 0) {
      var blocks = [];
      var blockedBy = [];
      var other = [];

      issue.dependencies.forEach(function (dep) {
        if (dep.type === 'blocks' && dep.depends_on_id === issue.id) {
          blocks.push(dep);
        } else if (dep.type === 'blocks' && dep.issue_id === issue.id) {
          blockedBy.push(dep);
        } else {
          other.push(dep);
        }
      });

      if (blockedBy.length > 0) {
        html += '<div class="detail-section">';
        html += '<div class="detail-section-title">Depends On</div>';
        html += '<ul class="detail-dep-list">';
        blockedBy.forEach(function (dep) {
          html += '<li><span class="dep-id" data-id="' + esc(dep.depends_on_id) + '">' + esc(dep.depends_on_id) + '</span><span style="color:var(--text-dim)">' + esc(dep.type) + '</span></li>';
        });
        html += '</ul></div>';
      }

      if (blocks.length > 0) {
        html += '<div class="detail-section">';
        html += '<div class="detail-section-title">Blocks</div>';
        html += '<ul class="detail-dep-list">';
        blocks.forEach(function (dep) {
          html += '<li><span class="dep-id" data-id="' + esc(dep.issue_id) + '">' + esc(dep.issue_id) + '</span><span style="color:var(--text-dim)">' + esc(dep.type) + '</span></li>';
        });
        html += '</ul></div>';
      }

      if (other.length > 0) {
        html += '<div class="detail-section">';
        html += '<div class="detail-section-title">Related</div>';
        html += '<ul class="detail-dep-list">';
        other.forEach(function (dep) {
          var targetID = dep.issue_id === issue.id ? dep.depends_on_id : dep.issue_id;
          html += '<li><span class="dep-id" data-id="' + esc(targetID) + '">' + esc(targetID) + '</span><span style="color:var(--text-dim)">' + esc(dep.type) + '</span></li>';
        });
        html += '</ul></div>';
      }
    }

    // comments
    if (issue.comments && issue.comments.length > 0) {
      html += '<div class="detail-section">';
      html += '<div class="detail-section-title">Comments (' + issue.comments.length + ')</div>';
      issue.comments.forEach(function (comment) {
        html += '<div class="detail-comment">';
        html += '<div class="detail-comment-header">' + esc(comment.author || 'unknown') + ' &middot; ' + BD.formatDate(comment.created_at) + '</div>';
        html += '<div class="detail-comment-body">' + esc(comment.content || comment.body || '') + '</div>';
        html += '</div>';
      });
      html += '</div>';
    }

    content.innerHTML = html;

    // dep link click handlers
    content.querySelectorAll('.dep-id').forEach(function (el) {
      el.addEventListener('click', function (e) {
        e.stopPropagation();
        BD.openDetail(el.dataset.id);
      });
    });
  }

  function metaItem(label, value) {
    return '<div class="detail-meta-item">' +
      '<span class="detail-meta-label">' + label + '</span>' +
      '<span class="detail-meta-value">' + value + '</span>' +
      '</div>';
  }

  document.addEventListener('DOMContentLoaded', init);
})();
