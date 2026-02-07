// bd web dashboard — graph.js
// SVG dependency graph using dagre layout

(function () {
  'use strict';

  var graphData = null;
  var svgElement = null;
  var viewBox = { x: 0, y: 0, w: 1200, h: 800 };
  var isDragging = false;
  var dragStart = { x: 0, y: 0 };

  var NODE_W = 180;
  var NODE_H = 50;
  var NODE_PADDING = 20;

  var statusFills = {
    open: '#2a2f45',
    in_progress: '#1e2a4a',
    blocked: '#3a1e1e',
    deferred: '#2a1e3a',
    closed: '#1e2a22',
  };

  var statusStrokes = {
    open: '#5c6078',
    in_progress: '#6c8cff',
    blocked: '#ff6b6b',
    deferred: '#9775d4',
    closed: '#4caf7d',
  };

  window.BD.renderGraph = function () {
    var container = document.getElementById('view-graph');

    container.innerHTML =
      '<div class="graph-container" id="graph-svg-container">' +
      '<div class="graph-controls">' +
      '<button id="graph-zoom-in" title="zoom in">+</button>' +
      '<button id="graph-zoom-out" title="zoom out">&minus;</button>' +
      '<button id="graph-zoom-fit" title="fit to view">&#9633;</button>' +
      '</div>' +
      '<svg id="graph-svg"></svg>' +
      '</div>';

    svgElement = document.getElementById('graph-svg');

    // load graph data
    BD.fetchJSON('/api/graph').then(function (data) {
      if (!data || !data.nodes) {
        container.querySelector('.graph-container').innerHTML =
          '<div class="empty-state"><div class="empty-state-icon">&#128716;</div>' +
          '<div class="empty-state-text">no dependency graph available</div></div>';
        return;
      }
      graphData = data;
      layoutAndRender();
      initControls();
      initPanZoom();
    });
  };

  function layoutAndRender() {
    if (!graphData || !graphData.nodes || graphData.nodes.length === 0) return;

    // filter by current filters
    var filtered = applyFilters(graphData);
    if (filtered.nodes.length === 0) {
      svgElement.innerHTML = '<text x="50%" y="50%" text-anchor="middle" fill="#5c6078">no issues match current filters</text>';
      return;
    }

    // use dagre for layout
    var g = new dagre.graphlib.Graph();
    g.setGraph({ rankdir: 'LR', nodesep: 30, ranksep: 80, marginx: NODE_PADDING, marginy: NODE_PADDING });
    g.setDefaultEdgeLabel(function () { return {}; });

    filtered.nodes.forEach(function (node) {
      g.setNode(node.id, { width: NODE_W, height: NODE_H, label: node });
    });

    var nodeSet = {};
    filtered.nodes.forEach(function (n) { nodeSet[n.id] = true; });

    filtered.edges.forEach(function (edge) {
      if (nodeSet[edge.from] && nodeSet[edge.to]) {
        g.setEdge(edge.from, edge.to, { type: edge.type });
      }
    });

    dagre.layout(g);

    // compute viewBox from layout
    var minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    g.nodes().forEach(function (id) {
      var n = g.node(id);
      minX = Math.min(minX, n.x - NODE_W / 2);
      minY = Math.min(minY, n.y - NODE_H / 2);
      maxX = Math.max(maxX, n.x + NODE_W / 2);
      maxY = Math.max(maxY, n.y + NODE_H / 2);
    });

    var padding = 40;
    viewBox.x = minX - padding;
    viewBox.y = minY - padding;
    viewBox.w = (maxX - minX) + padding * 2;
    viewBox.h = (maxY - minY) + padding * 2;

    svgElement.setAttribute('viewBox', viewBox.x + ' ' + viewBox.y + ' ' + viewBox.w + ' ' + viewBox.h);

    // render edges
    var svg = '';

    // arrowhead marker
    svg += '<defs><marker id="arrowhead" viewBox="0 0 10 7" refX="10" refY="3.5" markerWidth="8" markerHeight="6" orient="auto">';
    svg += '<polygon points="0 0, 10 3.5, 0 7" fill="#5c6078"/>';
    svg += '</marker></defs>';

    // edges
    g.edges().forEach(function (e) {
      var edge = g.edge(e);
      var points = edge.points;
      if (points && points.length >= 2) {
        var d = 'M ' + points[0].x + ' ' + points[0].y;
        for (var i = 1; i < points.length; i++) {
          d += ' L ' + points[i].x + ' ' + points[i].y;
        }
        var edgeData = g.edge(e);
        var strokeColor = edgeData.type === 'blocks' ? '#ff6b6b' : '#5c6078';
        var dashArray = edgeData.type === 'parent-child' ? '4,4' : 'none';
        svg += '<g class="graph-edge">';
        svg += '<path d="' + d + '" stroke="' + strokeColor + '" stroke-width="1.2" fill="none" stroke-dasharray="' + dashArray + '" marker-end="url(#arrowhead)"/>';
        svg += '</g>';
      }
    });

    // nodes
    g.nodes().forEach(function (id) {
      var n = g.node(id);
      var node = n.label;
      var x = n.x - NODE_W / 2;
      var y = n.y - NODE_H / 2;
      var fill = statusFills[node.status] || statusFills.open;
      var stroke = statusStrokes[node.status] || statusStrokes.open;
      var titleText = truncate(node.title, 22);
      var idText = node.id;

      svg += '<g class="graph-node" data-id="' + escAttr(node.id) + '">';
      svg += '<rect x="' + x + '" y="' + y + '" width="' + NODE_W + '" height="' + NODE_H + '" fill="' + fill + '" stroke="' + stroke + '" rx="4" ry="4"/>';
      svg += '<text x="' + (x + 8) + '" y="' + (y + 20) + '" fill="#e2e4ed" font-size="11">' + escSvg(titleText) + '</text>';
      svg += '<text class="node-id" x="' + (x + 8) + '" y="' + (y + 36) + '">' + escSvg(idText) + '</text>';

      // priority indicator
      var priColor = ['#ff4757', '#ff8c42', '#ffd166', '#6c8cff', '#5c6078'][node.priority] || '#5c6078';
      svg += '<rect x="' + (x + NODE_W - 18) + '" y="' + (y + 4) + '" width="14" height="14" rx="2" fill="' + priColor + '" opacity="0.8"/>';
      svg += '<text x="' + (x + NODE_W - 14.5) + '" y="' + (y + 14) + '" font-size="8" fill="#fff" font-weight="600" text-anchor="middle">' + node.priority + '</text>';

      svg += '</g>';
    });

    svgElement.innerHTML = svg;

    // node click handlers
    svgElement.querySelectorAll('.graph-node').forEach(function (el) {
      el.addEventListener('click', function () {
        if (window.BD.openDetail) BD.openDetail(el.dataset.id);
      });
    });
  }

  function applyFilters(data) {
    var nodeSet = {};
    var filteredNodes = data.nodes.filter(function (node) {
      if (BD.filters.statuses.length > 0 && BD.filters.statuses.indexOf(node.status) === -1) return false;
      if (BD.filters.priority !== '' && node.priority !== parseInt(BD.filters.priority, 10)) return false;
      if (BD.filters.type !== '' && node.type !== BD.filters.type) return false;
      if (BD.filters.search) {
        var q = BD.filters.search.toLowerCase();
        if ((node.title || '').toLowerCase().indexOf(q) === -1 &&
            (node.id || '').toLowerCase().indexOf(q) === -1) return false;
      }
      return true;
    });
    filteredNodes.forEach(function (n) { nodeSet[n.id] = true; });

    var filteredEdges = data.edges.filter(function (e) {
      return nodeSet[e.from] && nodeSet[e.to];
    });

    return { nodes: filteredNodes, edges: filteredEdges };
  }

  function initControls() {
    document.getElementById('graph-zoom-in').addEventListener('click', function () {
      zoom(0.8);
    });
    document.getElementById('graph-zoom-out').addEventListener('click', function () {
      zoom(1.25);
    });
    document.getElementById('graph-zoom-fit').addEventListener('click', function () {
      layoutAndRender();
    });
  }

  function zoom(factor) {
    var cx = viewBox.x + viewBox.w / 2;
    var cy = viewBox.y + viewBox.h / 2;
    viewBox.w *= factor;
    viewBox.h *= factor;
    viewBox.x = cx - viewBox.w / 2;
    viewBox.y = cy - viewBox.h / 2;
    svgElement.setAttribute('viewBox', viewBox.x + ' ' + viewBox.y + ' ' + viewBox.w + ' ' + viewBox.h);
  }

  function initPanZoom() {
    var containerEl = document.getElementById('graph-svg-container');
    if (!containerEl) return;

    containerEl.addEventListener('mousedown', function (e) {
      if (e.target.closest('.graph-node') || e.target.closest('.graph-controls')) return;
      isDragging = true;
      dragStart.x = e.clientX;
      dragStart.y = e.clientY;
      containerEl.style.cursor = 'grabbing';
    });

    window.addEventListener('mousemove', function (e) {
      if (!isDragging) return;
      var dx = e.clientX - dragStart.x;
      var dy = e.clientY - dragStart.y;
      var rect = svgElement.getBoundingClientRect();
      var scaleX = viewBox.w / rect.width;
      var scaleY = viewBox.h / rect.height;
      viewBox.x -= dx * scaleX;
      viewBox.y -= dy * scaleY;
      svgElement.setAttribute('viewBox', viewBox.x + ' ' + viewBox.y + ' ' + viewBox.w + ' ' + viewBox.h);
      dragStart.x = e.clientX;
      dragStart.y = e.clientY;
    });

    window.addEventListener('mouseup', function () {
      isDragging = false;
      if (containerEl) containerEl.style.cursor = '';
    });

    containerEl.addEventListener('wheel', function (e) {
      e.preventDefault();
      var factor = e.deltaY > 0 ? 1.1 : 0.9;
      zoom(factor);
    }, { passive: false });
  }

  function truncate(str, max) {
    if (!str) return '';
    if (str.length <= max) return str;
    return str.slice(0, max - 1) + '\u2026';
  }

  function escAttr(s) {
    return (s || '').replace(/"/g, '&quot;').replace(/</g, '&lt;');
  }

  function escSvg(s) {
    return (s || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }
})();
