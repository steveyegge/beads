package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

var (
	// WebSocket upgrader
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			// Allow all origins for simplicity (consider restricting in production)
			return true
		},
	}

	// WebSocket client management
	wsClients   = make(map[*websocket.Conn]bool)
	wsClientsMu sync.Mutex
	wsBroadcast = make(chan []byte, 256)
)

var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Start web UI for real-time issue monitoring",
	Long: `Start a web server that provides a real-time web interface for monitoring issues.

The monitor provides:
- Table view of all issues with filtering
- Click-through to detailed issue view
- Real-time updates via WebSocket (when daemon is running)
- Simple, clean UI styled with milligram.css

Example:
  bd monitor                    # Start on localhost:8080
  bd monitor --port 3000        # Start on custom port
  bd monitor --host 0.0.0.0     # Listen on all interfaces`,
	Run: runMonitor,
}

func init() {
	monitorCmd.Flags().Int("port", 8080, "Port for web server")
	monitorCmd.Flags().String("host", "localhost", "Host to bind to")
	rootCmd.AddCommand(monitorCmd)
}

func runMonitor(cmd *cobra.Command, args []string) {
	port, _ := cmd.Flags().GetInt("port")
	host, _ := cmd.Flags().GetString("host")

	// Start WebSocket broadcaster
	go handleWebSocketBroadcast()

	// Start mutation polling if daemon is available
	if daemonClient != nil {
		go pollMutations()
	}

	// Set up HTTP routes
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/issues", handleAPIIssues)
	http.HandleFunc("/api/issues/", handleAPIIssueDetail)
	http.HandleFunc("/api/ready", handleAPIReady)
	http.HandleFunc("/api/stats", handleAPIStats)
	http.HandleFunc("/ws", handleWebSocket)

	addr := fmt.Sprintf("%s:%d", host, port)
	fmt.Printf("🖥️  bd monitor starting on http://%s\n", addr)
	fmt.Printf("📊 Open your browser to view real-time issue tracking\n")
	fmt.Printf("🔌 WebSocket endpoint available at ws://%s/ws\n", addr)
	fmt.Printf("Press Ctrl+C to stop\n\n")

	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting server: %v\n", err)
		os.Exit(1)
	}
}

// handleIndex serves the main HTML page
func handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>bd monitor - Issue Tracker</title>
    <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/milligram/1.4.1/milligram.min.css">
    <style>
        body { padding: 2rem; }
        .header {
            margin-bottom: 2rem;
            display: flex;
            justify-content: space-between;
            align-items: center;
            flex-wrap: wrap;
        }
        .connection-status {
            display: inline-flex;
            align-items: center;
            gap: 0.5rem;
            padding: 0.5rem 1rem;
            border-radius: 0.4rem;
            font-size: 1.2rem;
        }
        .connection-status.connected {
            background: #d4edda;
            color: #155724;
        }
        .connection-status.disconnected {
            background: #f8d7da;
            color: #721c24;
        }
        .connection-dot {
            width: 8px;
            height: 8px;
            border-radius: 50%;
        }
        .connection-dot.connected {
            background: #28a745;
            animation: pulse 2s infinite;
        }
        .connection-dot.disconnected {
            background: #dc3545;
        }
        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
        }
        .stats { margin-bottom: 2rem; }
        .stats-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 1rem; }
        .stat-card { padding: 1rem; background: #f4f5f6; border-radius: 0.4rem; }
        .stat-value { font-size: 2.4rem; font-weight: bold; color: #9b4dca; }
        .stat-label { font-size: 1.2rem; color: #606c76; }

        /* Loading spinner */
        .spinner {
            border: 3px solid #f3f3f3;
            border-top: 3px solid #9b4dca;
            border-radius: 50%;
            width: 30px;
            height: 30px;
            animation: spin 1s linear infinite;
            margin: 2rem auto;
        }
        @keyframes spin {
            0% { transform: rotate(0deg); }
            100% { transform: rotate(360deg); }
        }
        .loading-overlay {
            display: none;
            position: fixed;
            top: 0;
            left: 0;
            width: 100%;
            height: 100%;
            background: rgba(255, 255, 255, 0.8);
            z-index: 999;
            justify-content: center;
            align-items: center;
        }
        .loading-overlay.active {
            display: flex;
        }

        /* Error message */
        .error-message {
            display: none;
            padding: 1rem;
            margin: 1rem 0;
            background: #f8d7da;
            border: 1px solid #f5c6cb;
            border-radius: 0.4rem;
            color: #721c24;
        }
        .error-message.active {
            display: block;
        }

        /* Empty state */
        .empty-state {
            text-align: center;
            padding: 4rem 2rem;
            color: #606c76;
        }
        .empty-state-icon {
            font-size: 4rem;
            margin-bottom: 1rem;
        }

        /* Table styles */
        table { width: 100%; }
        tbody tr { cursor: pointer; }
        tbody tr:hover { background: #f4f5f6; }
        .status-open { color: #0074d9; }
        .status-closed { color: #2ecc40; }
        .status-in-progress { color: #ff851b; }
        .priority-1 { color: #ff4136; font-weight: bold; }
        .priority-2 { color: #ff851b; }
        .priority-3 { color: #ffdc00; }

        /* Modal styles */
        .modal { display: none; position: fixed; z-index: 1000; left: 0; top: 0; width: 100%; height: 100%; overflow: auto; background-color: rgba(0,0,0,0.4); }
        .modal-content { background-color: #fefefe; margin: 5% auto; padding: 2rem; border-radius: 0.4rem; width: 80%; max-width: 800px; }
        .close { color: #aaa; float: right; font-size: 2.8rem; font-weight: bold; line-height: 2rem; cursor: pointer; }
        .close:hover, .close:focus { color: #000; }

        .filter-controls { margin-bottom: 2rem; }
        .filter-controls select { margin-right: 1rem; }

        /* Responsive design for mobile */
        @media screen and (max-width: 768px) {
            body { padding: 1rem; }
            .header {
                flex-direction: column;
                align-items: flex-start;
            }
            .connection-status {
                margin-top: 1rem;
            }
            .stats-grid {
                grid-template-columns: repeat(2, 1fr);
            }
            .filter-controls {
                display: flex;
                flex-direction: column;
            }
            .filter-controls select {
                margin-right: 0;
                margin-bottom: 1rem;
            }

            /* Hide table, show card view on mobile */
            table { display: none; }
            .issues-card-view { display: block; }

            .issue-card {
                background: #fff;
                border: 1px solid #d1d1d1;
                border-radius: 0.4rem;
                padding: 1.5rem;
                margin-bottom: 1rem;
                cursor: pointer;
                transition: box-shadow 0.2s;
            }
            .issue-card:hover {
                box-shadow: 0 2px 8px rgba(0,0,0,0.1);
            }
            .issue-card-header {
                display: flex;
                justify-content: space-between;
                align-items: start;
                margin-bottom: 1rem;
            }
            .issue-card-id {
                font-weight: bold;
                color: #9b4dca;
            }
            .issue-card-title {
                font-size: 1.6rem;
                margin: 0.5rem 0;
            }
            .issue-card-meta {
                display: flex;
                flex-wrap: wrap;
                gap: 1rem;
                font-size: 1.2rem;
            }
            .modal-content {
                width: 95%;
                margin: 10% auto;
            }
        }

        @media screen and (min-width: 769px) {
            .issues-card-view { display: none; }
        }
    </style>
</head>
<body>
    <div class="loading-overlay" id="loading-overlay">
        <div class="spinner"></div>
    </div>

    <div class="header">
        <div>
            <h1>bd monitor</h1>
            <p>Real-time issue tracking dashboard</p>
        </div>
        <div class="connection-status disconnected" id="connection-status">
            <span class="connection-dot disconnected" id="connection-dot"></span>
            <span id="connection-text">Connecting...</span>
        </div>
    </div>

    <div class="error-message" id="error-message"></div>

    <div class="stats">
        <h2>Statistics</h2>
        <div class="stats-grid" id="stats-grid">
            <div class="stat-card">
                <div class="stat-value" id="stat-total">-</div>
                <div class="stat-label">Total Issues</div>
            </div>
            <div class="stat-card">
                <div class="stat-value" id="stat-open">-</div>
                <div class="stat-label">Open</div>
            </div>
            <div class="stat-card">
                <div class="stat-value" id="stat-ready">-</div>
                <div class="stat-label">Ready to Work</div>
            </div>
            <div class="stat-card">
                <div class="stat-value" id="stat-closed">-</div>
                <div class="stat-label">Closed</div>
            </div>
        </div>
    </div>

    <div class="filter-controls">
        <label>
            Status:
            <select id="filter-status">
                <option value="">All</option>
                <option value="open">Open</option>
                <option value="in-progress">In Progress</option>
                <option value="closed">Closed</option>
            </select>
        </label>
        <label>
            Priority:
            <select id="filter-priority">
                <option value="">All</option>
                <option value="1">P1</option>
                <option value="2">P2</option>
                <option value="3">P3</option>
            </select>
        </label>
    </div>

    <h2>Issues</h2>
    <table id="issues-table">
        <thead>
            <tr>
                <th>ID</th>
                <th>Title</th>
                <th>Status</th>
                <th>Priority</th>
                <th>Type</th>
                <th>Assignee</th>
            </tr>
        </thead>
        <tbody id="issues-tbody">
            <tr><td colspan="6"><div class="spinner"></div></td></tr>
        </tbody>
    </table>

    <!-- Mobile card view -->
    <div class="issues-card-view" id="issues-card-view">
        <div class="spinner"></div>
    </div>

    <!-- Modal for issue details -->
    <div id="issue-modal" class="modal">
        <div class="modal-content">
            <span class="close">&times;</span>
            <h2 id="modal-title">Issue Details</h2>
            <div id="modal-body">
                <p>Loading...</p>
            </div>
        </div>
    </div>

    <script>
        let allIssues = [];
        let ws = null;
        let wsConnected = false;

        // WebSocket connection
        function connectWebSocket() {
            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            const wsUrl = protocol + '//' + window.location.host + '/ws';

            ws = new WebSocket(wsUrl);

            ws.onopen = function() {
                console.log('WebSocket connected');
                wsConnected = true;
                updateConnectionStatus(true);
            };

            ws.onmessage = function(event) {
                console.log('WebSocket message:', event.data);
                const mutation = JSON.parse(event.data);
                handleMutation(mutation);
            };

            ws.onerror = function(error) {
                console.error('WebSocket error:', error);
                wsConnected = false;
                updateConnectionStatus(false);
            };

            ws.onclose = function() {
                console.log('WebSocket disconnected');
                wsConnected = false;
                updateConnectionStatus(false);
                // Reconnect after 5 seconds
                setTimeout(connectWebSocket, 5000);
            };
        }

        // Update connection status indicator
        function updateConnectionStatus(connected) {
            const statusEl = document.getElementById('connection-status');
            const dotEl = document.getElementById('connection-dot');
            const textEl = document.getElementById('connection-text');

            if (connected) {
                statusEl.className = 'connection-status connected';
                dotEl.className = 'connection-dot connected';
                textEl.textContent = 'Connected';
            } else {
                statusEl.className = 'connection-status disconnected';
                dotEl.className = 'connection-dot disconnected';
                textEl.textContent = 'Disconnected';
            }
        }

        // Show/hide loading overlay
        function setLoading(isLoading) {
            const overlay = document.getElementById('loading-overlay');
            if (isLoading) {
                overlay.classList.add('active');
            } else {
                overlay.classList.remove('active');
            }
        }

        // Show error message
        function showError(message) {
            const errorEl = document.getElementById('error-message');
            errorEl.textContent = message;
            errorEl.classList.add('active');
            setTimeout(() => {
                errorEl.classList.remove('active');
            }, 5000);
        }

        // Handle mutation event
        function handleMutation(mutation) {
            console.log('Mutation:', mutation.type, mutation.issue_id);
            // Refresh data on mutation
            loadStats();
            loadIssues();
        }

        // Load statistics
        async function loadStats() {
            try {
                const response = await fetch('/api/stats');
                if (!response.ok) throw new Error('Failed to load statistics');
                const stats = await response.json();
                document.getElementById('stat-total').textContent = stats.total || 0;
                document.getElementById('stat-open').textContent = stats.by_status?.open || 0;
                document.getElementById('stat-closed').textContent = stats.by_status?.closed || 0;

                // Load ready count separately
                const readyResponse = await fetch('/api/ready');
                if (!readyResponse.ok) throw new Error('Failed to load ready count');
                const readyIssues = await readyResponse.json();
                document.getElementById('stat-ready').textContent = readyIssues.length;
            } catch (error) {
                console.error('Error loading statistics:', error);
                showError('Failed to load statistics: ' + error.message);
            }
        }

        // Load all issues
        async function loadIssues() {
            try {
                const response = await fetch('/api/issues');
                if (!response.ok) throw new Error('Failed to load issues');
                allIssues = await response.json();
                renderIssues(allIssues);
            } catch (error) {
                console.error('Error loading issues:', error);
                showError('Failed to load issues: ' + error.message);
                document.getElementById('issues-tbody').innerHTML = '<tr><td colspan="6" style="text-align: center; color: #721c24;">Error loading issues</td></tr>';
                document.getElementById('issues-card-view').innerHTML = '<div class="empty-state"><div class="empty-state-icon">⚠️</div><p>Error loading issues</p></div>';
            }
        }

        // Render issues table
        function renderIssues(issues) {
            const tbody = document.getElementById('issues-tbody');
            const cardView = document.getElementById('issues-card-view');

            if (!issues || issues.length === 0) {
                const emptyState = '<div class="empty-state"><div class="empty-state-icon">📋</div><h3>No issues found</h3><p>Create your first issue to get started!</p></div>';
                tbody.innerHTML = '<tr><td colspan="6">' + emptyState + '</td></tr>';
                cardView.innerHTML = emptyState;
                return;
            }

            // Render table view
            tbody.innerHTML = issues.map(issue => {
                const statusClass = 'status-' + (issue.status || 'open').toLowerCase().replace('_', '-');
                const priorityClass = 'priority-' + (issue.priority || 2);
                return '<tr onclick="showIssueDetail(\'' + issue.id + '\')"><td>' + issue.id + '</td><td>' + issue.title + '</td><td class="' + statusClass + '">' + (issue.status || 'open') + '</td><td class="' + priorityClass + '">P' + (issue.priority || 2) + '</td><td>' + (issue.issue_type || 'task') + '</td><td>' + (issue.assignee || '-') + '</td></tr>';
            }).join('');

            // Render card view for mobile
            cardView.innerHTML = issues.map(issue => {
                const statusClass = 'status-' + (issue.status || 'open').toLowerCase().replace('_', '-');
                const priorityClass = 'priority-' + (issue.priority || 2);
                let html = '<div class="issue-card" onclick="showIssueDetail(\'' + issue.id + '\')">';
                html += '<div class="issue-card-header">';
                html += '<span class="issue-card-id">' + issue.id + '</span>';
                html += '<span class="' + priorityClass + '">P' + (issue.priority || 2) + '</span>';
                html += '</div>';
                html += '<h3 class="issue-card-title">' + issue.title + '</h3>';
                html += '<div class="issue-card-meta">';
                html += '<span class="' + statusClass + '">● ' + (issue.status || 'open') + '</span>';
                html += '<span>Type: ' + (issue.issue_type || 'task') + '</span>';
                if (issue.assignee) html += '<span>👤 ' + issue.assignee + '</span>';
                html += '</div>';
                html += '</div>';
                return html;
            }).join('');
        }

        // Filter issues
        function filterIssues() {
            const statusFilter = document.getElementById('filter-status').value;
            const priorityFilter = document.getElementById('filter-priority').value;

            const filtered = allIssues.filter(issue => {
                if (statusFilter && issue.status !== statusFilter) return false;
                if (priorityFilter && issue.priority !== parseInt(priorityFilter)) return false;
                return true;
            });

            renderIssues(filtered);
        }

        // Show issue detail modal
        async function showIssueDetail(issueId) {
            const modal = document.getElementById('issue-modal');
            const modalTitle = document.getElementById('modal-title');
            const modalBody = document.getElementById('modal-body');

            modal.style.display = 'block';
            modalTitle.textContent = 'Loading...';
            modalBody.innerHTML = '<div class="spinner"></div>';

            try {
                const response = await fetch('/api/issues/' + issueId);
                if (!response.ok) throw new Error('Issue not found');
                const issue = await response.json();

                modalTitle.textContent = issue.id + ': ' + issue.title;
                let html = '<p><strong>Status:</strong> ' + issue.status + '</p>';
                html += '<p><strong>Priority:</strong> P' + issue.priority + '</p>';
                html += '<p><strong>Type:</strong> ' + issue.issue_type + '</p>';
                html += '<p><strong>Assignee:</strong> ' + (issue.assignee || 'Unassigned') + '</p>';
                html += '<p><strong>Created:</strong> ' + new Date(issue.created_at).toLocaleString() + '</p>';
                html += '<p><strong>Updated:</strong> ' + new Date(issue.updated_at).toLocaleString() + '</p>';
                if (issue.description) html += '<h3>Description</h3><pre>' + issue.description + '</pre>';
                if (issue.design) html += '<h3>Design</h3><pre>' + issue.design + '</pre>';
                if (issue.notes) html += '<h3>Notes</h3><pre>' + issue.notes + '</pre>';
                if (issue.labels && issue.labels.length > 0) html += '<p><strong>Labels:</strong> ' + issue.labels.join(', ') + '</p>';
                modalBody.innerHTML = html;
            } catch (error) {
                console.error('Error loading issue details:', error);
                showError('Failed to load issue details: ' + error.message);
                modalBody.innerHTML = '<div class="empty-state"><div class="empty-state-icon">⚠️</div><p>Error loading issue details</p></div>';
            }
        }

        // Close modal
        document.querySelector('.close').onclick = function() {
            document.getElementById('issue-modal').style.display = 'none';
        };

        window.onclick = function(event) {
            const modal = document.getElementById('issue-modal');
            if (event.target == modal) {
                modal.style.display = 'none';
            }
        };

        // Filter event listeners
        document.getElementById('filter-status').addEventListener('change', filterIssues);
        document.getElementById('filter-priority').addEventListener('change', filterIssues);

        // Initial load
        connectWebSocket();
        loadStats();
        loadIssues();

        // Fallback: Refresh every 30 seconds (WebSocket should handle real-time updates)
        setInterval(() => {
            if (!wsConnected) {
                loadStats();
                loadIssues();
            }
        }, 30000);
    </script>
</body>
</html>`;
	fmt.Fprint(w, html)
}

// handleAPIIssues returns all issues as JSON
func handleAPIIssues(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	var issues []*types.Issue
	var err error

	// Support both daemon mode (RPC) and direct mode (SQLite)
	if daemonClient != nil {
		// RPC mode: use daemon
		resp, err := daemonClient.List(&rpc.ListArgs{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Error fetching issues via RPC: %v", err), http.StatusInternalServerError)
			return
		}

		if err := json.Unmarshal(resp.Data, &issues); err != nil {
			http.Error(w, fmt.Sprintf("Error unmarshaling issues: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		// Direct mode: query storage directly
		if store == nil {
			http.Error(w, "Storage not initialized", http.StatusInternalServerError)
			return
		}

		issues, err = store.SearchIssues(ctx, "", types.IssueFilter{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Error fetching issues: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(issues)
}

// handleAPIIssueDetail returns a single issue's details
func handleAPIIssueDetail(w http.ResponseWriter, r *http.Request) {
	// Extract issue ID from URL path (e.g., /api/issues/bd-1)
	issueID := r.URL.Path[len("/api/issues/"):]
	if issueID == "" {
		http.Error(w, "Issue ID required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	var issue *types.Issue
	var err error

	// Support both daemon mode (RPC) and direct mode (SQLite)
	if daemonClient != nil {
		// RPC mode: use daemon
		resp, err := daemonClient.Show(&rpc.ShowArgs{ID: issueID})
		if err != nil {
			http.Error(w, fmt.Sprintf("Issue not found: %v", err), http.StatusNotFound)
			return
		}

		if err := json.Unmarshal(resp.Data, &issue); err != nil {
			http.Error(w, fmt.Sprintf("Error unmarshaling issue: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		// Direct mode: query storage directly
		if store == nil {
			http.Error(w, "Storage not initialized", http.StatusInternalServerError)
			return
		}

		issue, err = store.GetIssue(ctx, issueID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Issue not found: %v", err), http.StatusNotFound)
			return
		}

		// Enrich with labels
		labels, _ := store.GetLabels(ctx, issueID)
		issue.Labels = labels
	}

	// Note: Dependencies could be added via custom response type if needed

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(issue)
}

// handleAPIReady returns ready work (no blockers)
func handleAPIReady(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	var issues []*types.Issue
	var err error

	// Support both daemon mode (RPC) and direct mode (SQLite)
	if daemonClient != nil {
		// RPC mode: use daemon
		resp, err := daemonClient.Ready(&rpc.ReadyArgs{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Error fetching ready work via RPC: %v", err), http.StatusInternalServerError)
			return
		}

		if err := json.Unmarshal(resp.Data, &issues); err != nil {
			http.Error(w, fmt.Sprintf("Error unmarshaling issues: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		// Direct mode: query storage directly
		if store == nil {
			http.Error(w, "Storage not initialized", http.StatusInternalServerError)
			return
		}

		issues, err = store.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusOpen,
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Error fetching ready work: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(issues)
}

// handleAPIStats returns issue statistics
func handleAPIStats(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	var stats *types.Statistics
	var err error

	// Support both daemon mode (RPC) and direct mode (SQLite)
	if daemonClient != nil {
		// RPC mode: use daemon
		resp, err := daemonClient.Stats()
		if err != nil {
			http.Error(w, fmt.Sprintf("Error fetching statistics via RPC: %v", err), http.StatusInternalServerError)
			return
		}

		if err := json.Unmarshal(resp.Data, &stats); err != nil {
			http.Error(w, fmt.Sprintf("Error unmarshaling statistics: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		// Direct mode: query storage directly
		if store == nil {
			http.Error(w, "Storage not initialized", http.StatusInternalServerError)
			return
		}

		stats, err = store.GetStatistics(ctx)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error fetching statistics: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleWebSocket upgrades HTTP connection to WebSocket and manages client lifecycle
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error upgrading to WebSocket: %v\n", err)
		return
	}

	// Register client
	wsClientsMu.Lock()
	wsClients[conn] = true
	wsClientsMu.Unlock()

	fmt.Printf("WebSocket client connected (total: %d)\n", len(wsClients))

	// Handle client disconnection
	defer func() {
		wsClientsMu.Lock()
		delete(wsClients, conn)
		wsClientsMu.Unlock()
		conn.Close()
		fmt.Printf("WebSocket client disconnected (total: %d)\n", len(wsClients))
	}()

	// Keep connection alive and handle client messages
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// handleWebSocketBroadcast sends messages to all connected WebSocket clients
func handleWebSocketBroadcast() {
	for {
		// Wait for message to broadcast
		message := <-wsBroadcast

		// Send to all connected clients
		wsClientsMu.Lock()
		for client := range wsClients {
			err := client.WriteMessage(websocket.TextMessage, message)
			if err != nil {
				// Client disconnected, will be cleaned up by handleWebSocket
				fmt.Fprintf(os.Stderr, "Error writing to WebSocket client: %v\n", err)
				client.Close()
				delete(wsClients, client)
			}
		}
		wsClientsMu.Unlock()
	}
}

// pollMutations polls the daemon for mutations and broadcasts them to WebSocket clients
func pollMutations() {
	lastPollTime := int64(0) // Start from beginning

	ticker := time.NewTicker(2 * time.Second) // Poll every 2 seconds
	defer ticker.Stop()

	for range ticker.C {
		if daemonClient == nil {
			continue
		}

		// Call GetMutations RPC
		resp, err := daemonClient.GetMutations(&rpc.GetMutationsArgs{
			Since: lastPollTime,
		})
		if err != nil {
			// Daemon might be down or restarting, just skip this poll
			continue
		}

		var mutations []rpc.MutationEvent
		if err := json.Unmarshal(resp.Data, &mutations); err != nil {
			fmt.Fprintf(os.Stderr, "Error unmarshaling mutations: %v\n", err)
			continue
		}

		// Broadcast each mutation to WebSocket clients
		for _, mutation := range mutations {
			data, _ := json.Marshal(mutation)
			wsBroadcast <- data

			// Update last poll time to this mutation's timestamp
			mutationTimeMillis := mutation.Timestamp.UnixMilli()
			if mutationTimeMillis > lastPollTime {
				lastPollTime = mutationTimeMillis
			}
		}
	}
}
