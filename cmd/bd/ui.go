package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
)

//go:embed templates/*.html templates/*.css templates/*.js
var embedFS embed.FS

// Pre-parse templates at package init for performance
var (
	tmplIndex       *template.Template
	tmplDetail      *template.Template
	tmplGraph       *template.Template
	tmplReady       *template.Template
	tmplBlocked     *template.Template
	tmplIssuesTbody *template.Template
)

func init() {
	tmplIndex = template.Must(template.ParseFS(embedFS, "templates/index.html"))
	tmplDetail = template.Must(template.ParseFS(embedFS, "templates/detail.html"))
	tmplGraph = template.Must(template.ParseFS(embedFS, "templates/graph.html"))
	tmplReady = template.Must(template.ParseFS(embedFS, "templates/ready.html"))
	tmplBlocked = template.Must(template.ParseFS(embedFS, "templates/blocked.html"))
	tmplIssuesTbody = template.Must(template.ParseFS(embedFS, "templates/issues_tbody.html"))
}

var serveCmd = &cobra.Command{
	Use:   "ui [port]",
	Short: "Start web UI server",
	Long: `Start a local web server for browsing issues in a graphical interface.

The web UI provides:
- Issue list with real-time filtering (search, status, priority)
- Issue detail pages with dependencies and activity
- Dependency graphs visualized with Graphviz
- Ready work view (unblocked issues)
- Blocked issues view with blocker details

Server binds to 127.0.0.1 (localhost only) by default.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		port := "8080"
		if len(args) > 0 {
			port = args[0]
		}

		addr := net.JoinHostPort("127.0.0.1", port)
		
		mux := http.NewServeMux()
		mux.HandleFunc("/", handleIndex)
		mux.HandleFunc("/ready", handleReady)
		mux.HandleFunc("/blocked", handleBlocked)
		mux.HandleFunc("/issue/", handleIssueDetail)
		mux.HandleFunc("/graph/", handleGraph)
		mux.HandleFunc("/api/issues", handleAPIIssues)
		mux.HandleFunc("/api/issue/", handleAPIIssue)
		mux.HandleFunc("/api/stats", handleAPIStats)
		mux.HandleFunc("/static/", handleStatic)

		srv := &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  60 * time.Second,
		}

		fmt.Printf("Starting beads web UI at http://%s\n", addr)
		fmt.Printf("Press Ctrl+C to stop\n")
		
		if err := srv.ListenAndServe(); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting server: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{Limit: 100})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	issuesWithLabels := enrichIssuesWithLabels(ctx, issues)

	stats, err := store.GetStatistics(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Issues": issuesWithLabels,
		"Stats":  stats,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmplIndex.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleIssueDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	issueID := strings.TrimPrefix(r.URL.Path, "/issue/")
	if issueID == "" {
		http.Error(w, "Issue ID required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	issue, err := store.GetIssue(ctx, issueID)
	if err != nil {
		http.Error(w, "Issue not found", http.StatusNotFound)
		return
	}

	deps, _ := store.GetDependencies(ctx, issueID)
	dependents, _ := store.GetDependents(ctx, issueID)
	labels, _ := store.GetLabels(ctx, issueID)
	events, _ := store.GetEvents(ctx, issueID, 50)

	data := map[string]interface{}{
		"Issue":      issue,
		"Deps":       deps,
		"Dependents": dependents,
		"Labels":     labels,
		"Events":     events,
		"HasDeps":    len(deps) > 0 || len(dependents) > 0,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmplDetail.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	issueID := strings.TrimPrefix(r.URL.Path, "/graph/")
	if issueID == "" {
		http.Error(w, "Issue ID required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	issue, err := store.GetIssue(ctx, issueID)
	if err != nil {
		http.Error(w, "Issue not found", http.StatusNotFound)
		return
	}

	dotGraph := generateDotGraph(ctx, issue)

	data := map[string]interface{}{
		"Issue":    issue,
		"DotGraph": dotGraph,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmplGraph.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	
	filter := types.WorkFilter{}
	ready, err := store.GetReadyWork(ctx, filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter out issues with excluded labels
	excludeLabel := r.URL.Query().Get("exclude")
	var filtered []*types.Issue
	if excludeLabel != "" {
		for _, issue := range ready {
			labels, _ := store.GetLabels(ctx, issue.ID)
			hasExcluded := false
			for _, label := range labels {
				if label == excludeLabel {
					hasExcluded = true
					break
				}
			}
			if !hasExcluded {
				filtered = append(filtered, issue)
			}
		}
	} else {
		filtered = ready
	}

	issuesWithLabels := enrichIssuesWithLabels(ctx, filtered)
	stats, _ := store.GetStatistics(ctx)

	data := map[string]interface{}{
		"Issues":       issuesWithLabels,
		"Stats":        stats,
		"ExcludeLabel": excludeLabel,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmplReady.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleBlocked(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	
	blocked, err := store.GetBlockedIssues(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stats, _ := store.GetStatistics(ctx)

	data := map[string]interface{}{
		"Blocked": blocked,
		"Stats":   stats,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmplBlocked.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleAPIIssues(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	
	filter := types.IssueFilter{Limit: 1000}
	searchQuery := ""
	
	if status := r.URL.Query().Get("status"); status != "" {
		s := types.Status(status)
		filter.Status = &s
	}
	
	if priority := r.URL.Query().Get("priority"); priority != "" {
		p, err := strconv.Atoi(priority)
		if err != nil {
			http.Error(w, "Invalid priority", http.StatusBadRequest)
			return
		}
		filter.Priority = &p
	}
	
	if search := r.URL.Query().Get("search"); search != "" {
		searchQuery = search
	}

	issues, err := store.SearchIssues(ctx, searchQuery, filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Check if htmx request (return partial HTML)
	if r.Header.Get("HX-Request") == "true" {
		issuesWithLabels := enrichIssuesWithLabels(ctx, issues)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmplIssuesTbody.Execute(w, issuesWithLabels); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Regular JSON response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(issues); err != nil {
		http.Error(w, "Failed to encode JSON", http.StatusInternalServerError)
	}
}

func handleAPIIssue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	issueID := strings.TrimPrefix(r.URL.Path, "/api/issue/")
	
	ctx := r.Context()
	issue, err := store.GetIssue(ctx, issueID)
	if err != nil {
		http.Error(w, "Issue not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(issue); err != nil {
		http.Error(w, "Failed to encode JSON", http.StatusInternalServerError)
	}
}

func handleAPIStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	stats, err := store.GetStatistics(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		http.Error(w, "Failed to encode JSON", http.StatusInternalServerError)
	}
}

type IssueWithLabels struct {
	*types.Issue
	Labels        []string
	DepsCount     int
	BlockersCount int
}

func enrichIssuesWithLabels(ctx context.Context, issues []*types.Issue) []*IssueWithLabels {
	result := make([]*IssueWithLabels, len(issues))
	for i, issue := range issues {
		labels, _ := store.GetLabels(ctx, issue.ID)
		deps, _ := store.GetDependencies(ctx, issue.ID)
		dependents, _ := store.GetDependents(ctx, issue.ID)
		result[i] = &IssueWithLabels{
			Issue:         issue,
			Labels:        labels,
			DepsCount:     len(deps),
			BlockersCount: len(dependents),
		}
	}
	return result
}

func generateDotGraph(ctx context.Context, root *types.Issue) string {
	var sb strings.Builder
	sb.WriteString("digraph G {\n")
	sb.WriteString("  rankdir=TB;\n")
	sb.WriteString("  node [shape=box, style=filled];\n\n")

	// Build node and edge maps to avoid duplicates
	nodes := make(map[string]*types.Issue)
	edges := make(map[string]bool)
	
	// Add root
	nodes[root.ID] = root
	
	// Get dependencies and dependents to build relationships
	deps, _ := store.GetDependencies(ctx, root.ID)
	dependents, _ := store.GetDependents(ctx, root.ID)
	
	// Add all dependencies as nodes and edges
	for _, dep := range deps {
		nodes[dep.ID] = dep
		edgeKey := fmt.Sprintf("%s->%s", root.ID, dep.ID)
		edges[edgeKey] = true
	}
	
	// Add all dependents as nodes and edges
	for _, dependent := range dependents {
		nodes[dependent.ID] = dependent
		edgeKey := fmt.Sprintf("%s->%s", dependent.ID, root.ID)
		edges[edgeKey] = true
	}
	
	// Render all nodes
	for _, issue := range nodes {
		color := "#7b9e87" // open
		if issue.Status == types.StatusClosed {
			color = "#8a8175"
		} else if issue.Status == types.StatusInProgress {
			color = "#c17a3c"
		}
		
		// Escape title for DOT format
		title := strings.ReplaceAll(issue.Title, "\\", "\\\\")
		title = strings.ReplaceAll(title, "\"", "'")
		
		label := fmt.Sprintf("%s\\n%s\\nP%d", issue.ID, title, issue.Priority)
		
		sb.WriteString(fmt.Sprintf("  \"%s\" [label=\"%s\", fillcolor=\"%s\", fontcolor=\"white\"];\n",
			issue.ID, label, color))
	}
	
	sb.WriteString("\n")
	
	// Render all edges
	for edge := range edges {
		parts := strings.Split(edge, "->")
		sb.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\";\n", parts[0], parts[1]))
	}

	sb.WriteString("}\n")
	return sb.String()
}

func handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/static/")
	
	var contentType string
	if strings.HasSuffix(path, ".css") {
		contentType = "text/css; charset=utf-8"
	} else if strings.HasSuffix(path, ".js") {
		contentType = "application/javascript; charset=utf-8"
	}

	content, err := embedFS.ReadFile("templates/" + path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Write(content)
}
