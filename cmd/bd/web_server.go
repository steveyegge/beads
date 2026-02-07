package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

//go:embed webui
var webFiles embed.FS

// sseClient represents a connected SSE client.
type sseClient struct {
	ch     chan []byte
	closed chan struct{}
}

// sseBroadcaster manages connected SSE clients and mutation polling.
type sseBroadcaster struct {
	mu      sync.Mutex
	clients map[*sseClient]struct{}
}

func newSSEBroadcaster() *sseBroadcaster {
	return &sseBroadcaster{
		clients: make(map[*sseClient]struct{}),
	}
}

func (b *sseBroadcaster) addClient() *sseClient {
	c := &sseClient{
		ch:     make(chan []byte, 64),
		closed: make(chan struct{}),
	}
	b.mu.Lock()
	b.clients[c] = struct{}{}
	b.mu.Unlock()
	return c
}

func (b *sseBroadcaster) removeClient(c *sseClient) {
	b.mu.Lock()
	delete(b.clients, c)
	b.mu.Unlock()
	close(c.closed)
}

func (b *sseBroadcaster) broadcast(data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for c := range b.clients {
		select {
		case c.ch <- data:
		default:
			// client buffer full, skip
		}
	}
}

// pollMutations polls the daemon for mutations and broadcasts them to SSE clients.
func (b *sseBroadcaster) pollMutations(ctx context.Context, client *rpc.Client) {
	lastPollTime := time.Now().UnixMilli()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if client == nil {
				continue
			}

			resp, err := client.GetMutations(&rpc.GetMutationsArgs{
				Since: lastPollTime,
			})
			if err != nil {
				continue
			}

			var mutations []json.RawMessage
			if err := json.Unmarshal(resp.Data, &mutations); err != nil {
				continue
			}

			for _, m := range mutations {
				b.broadcast(m)

				// extract timestamp to advance cursor
				var ev struct {
					Timestamp time.Time `json:"Timestamp"`
				}
				if json.Unmarshal(m, &ev) == nil && !ev.Timestamp.IsZero() {
					if t := ev.Timestamp.UnixMilli(); t > lastPollTime {
						lastPollTime = t
					}
				}
			}
		}
	}
}

// webGraphNode is a node in the JSON graph response.
type webGraphNode struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
	Type     string `json:"type"`
	Assignee string `json:"assignee,omitempty"`
}

// webGraphEdge is an edge in the JSON graph response.
type webGraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

// webGraphResponse is the JSON response for /api/graph.
type webGraphResponse struct {
	Nodes []webGraphNode `json:"nodes"`
	Edges []webGraphEdge `json:"edges"`
}

// buildWebMux constructs the HTTP mux for the web dashboard.
func buildWebMux(client *rpc.Client, graphStore storage.Storage, devMode bool) *http.ServeMux {
	mux := http.NewServeMux()

	// set up file system for static assets
	var webFS fs.FS
	if devMode {
		fmt.Fprintln(os.Stderr, "dev mode: serving web files from disk")
		webFS = os.DirFS("cmd/bd/webui")
	} else {
		var err error
		webFS, err = fs.Sub(webFiles, "webui")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error accessing embedded web files: %v\n", err)
			os.Exit(1)
		}
	}

	// SSE broadcaster
	broadcaster := newSSEBroadcaster()
	go broadcaster.pollMutations(rootCtx, client)

	// serve index.html at root
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			// serve static files
			http.FileServer(http.FS(webFS)).ServeHTTP(w, r)
			return
		}
		data, err := fs.ReadFile(webFS, "index.html")
		if err != nil {
			http.Error(w, "error reading index.html", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})

	// API: list issues
	mux.HandleFunc("/api/issues", func(w http.ResponseWriter, r *http.Request) {
		if client == nil {
			httpJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "daemon not connected"})
			return
		}

		args := &rpc.ListArgs{}

		// apply query params as filters
		if s := r.URL.Query().Get("status"); s != "" {
			args.Status = s
		}
		if t := r.URL.Query().Get("type"); t != "" {
			args.IssueType = t
		}
		if a := r.URL.Query().Get("assignee"); a != "" {
			args.Assignee = a
		}
		if q := r.URL.Query().Get("q"); q != "" {
			args.TitleContains = q
		}

		resp, err := client.List(args)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp.Data)
	})

	// API: show single issue
	mux.HandleFunc("/api/issues/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/issues/")
		if id == "" {
			http.Error(w, "issue id required", http.StatusBadRequest)
			return
		}
		if client == nil {
			httpJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "daemon not connected"})
			return
		}

		resp, err := client.Show(&rpc.ShowArgs{ID: id})
		if err != nil {
			httpJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp.Data)
	})

	// API: stats
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		if client == nil {
			httpJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "daemon not connected"})
			return
		}

		resp, err := client.Stats()
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp.Data)
	})

	// API: ready issues
	mux.HandleFunc("/api/ready", func(w http.ResponseWriter, r *http.Request) {
		if client == nil {
			httpJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "daemon not connected"})
			return
		}

		resp, err := client.Ready(&rpc.ReadyArgs{})
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp.Data)
	})

	// API: blocked issues
	mux.HandleFunc("/api/blocked", func(w http.ResponseWriter, r *http.Request) {
		if client == nil {
			httpJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "daemon not connected"})
			return
		}

		resp, err := client.Blocked(&rpc.BlockedArgs{})
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp.Data)
	})

	// API: dependency graph (all open issues)
	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		if graphStore == nil {
			httpJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "graph store not available"})
			return
		}

		includeClosed := r.URL.Query().Get("include_closed") == "true"
		graph := buildGraphData(rootCtx, graphStore, "", includeClosed)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(graph)
	})

	// API: dependency graph for specific issue
	mux.HandleFunc("/api/graph/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/graph/")
		if id == "" {
			httpJSON(w, http.StatusBadRequest, map[string]string{"error": "issue id required"})
			return
		}
		if graphStore == nil {
			httpJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "graph store not available"})
			return
		}

		graph := buildGraphData(rootCtx, graphStore, id, true)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(graph)
	})

	// API: SSE event stream
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		c := broadcaster.addClient()
		defer broadcaster.removeClient(c)

		// send initial connected event
		_, _ = fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
		flusher.Flush()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-c.closed:
				return
			case data := <-c.ch:
				_, _ = fmt.Fprintf(w, "event: mutation\ndata: %s\n\n", data)
				flusher.Flush()
			}
		}
	})

	return mux
}

// buildGraphData constructs the graph response from storage.
// If focusID is non-empty, only the subgraph connected to that issue is returned.
func buildGraphData(ctx context.Context, s storage.Storage, focusID string, includeClosed bool) webGraphResponse {
	result := webGraphResponse{
		Nodes: []webGraphNode{},
		Edges: []webGraphEdge{},
	}

	if s == nil {
		return result
	}

	// load all dependency records in bulk
	allDeps, err := s.GetAllDependencyRecords(ctx)
	if err != nil {
		return result
	}

	// load issues — for focus mode, load the subgraph; otherwise load all open
	issueMap := make(map[string]*types.Issue)

	if focusID != "" {
		// BFS from focus issue
		issue, err := s.GetIssue(ctx, focusID)
		if err != nil || issue == nil {
			return result
		}
		issueMap[issue.ID] = issue

		queue := []string{issue.ID}
		visited := map[string]bool{issue.ID: true}

		// build adjacency from deps
		adj := make(map[string][]string)
		for issueID, deps := range allDeps {
			for _, dep := range deps {
				adj[issueID] = append(adj[issueID], dep.DependsOnID)
				adj[dep.DependsOnID] = append(adj[dep.DependsOnID], issueID)
			}
		}

		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, neighbor := range adj[cur] {
				if !visited[neighbor] {
					visited[neighbor] = true
					queue = append(queue, neighbor)
					if ni, err := s.GetIssue(ctx, neighbor); err == nil && ni != nil {
						issueMap[ni.ID] = ni
					}
				}
			}
		}
	} else {
		// load all open issues
		for _, status := range []types.Status{types.StatusOpen, types.StatusInProgress, types.StatusBlocked, types.StatusDeferred} {
			statusCopy := status
			issues, err := s.SearchIssues(ctx, "", types.IssueFilter{
				Status: &statusCopy,
			})
			if err != nil {
				continue
			}
			for _, issue := range issues {
				issueMap[issue.ID] = issue
			}
		}

		if includeClosed {
			closedStatus := types.StatusClosed
			issues, err := s.SearchIssues(ctx, "", types.IssueFilter{
				Status: &closedStatus,
			})
			if err == nil {
				for _, issue := range issues {
					issueMap[issue.ID] = issue
				}
			}
		}
	}

	// build nodes
	for _, issue := range issueMap {
		result.Nodes = append(result.Nodes, webGraphNode{
			ID:       issue.ID,
			Title:    issue.Title,
			Status:   string(issue.Status),
			Priority: issue.Priority,
			Type:     string(issue.IssueType),
			Assignee: issue.Assignee,
		})
	}

	// build edges (only between issues in our set)
	for issueID, deps := range allDeps {
		if _, ok := issueMap[issueID]; !ok {
			continue
		}
		for _, dep := range deps {
			if _, ok := issueMap[dep.DependsOnID]; ok {
				result.Edges = append(result.Edges, webGraphEdge{
					From: issueID,
					To:   dep.DependsOnID,
					Type: string(dep.Type),
				})
			}
		}
	}

	return result
}

// httpJSON writes a JSON response with the given status code.
func httpJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
