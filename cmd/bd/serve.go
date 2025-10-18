package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
)

//go:embed templates/*.html templates/*.css templates/*.js
var embedFS embed.FS

var serveCmd = &cobra.Command{
	Use:   "serve [port]",
	Short: "Start web UI server",
	Long:  `Start a local web server for browsing issues in a graphical interface.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		port := "8080"
		if len(args) > 0 {
			port = args[0]
		}

		addr := fmt.Sprintf("localhost:%s", port)
		
		http.HandleFunc("/", handleIndex)
		http.HandleFunc("/ready", handleReady)
		http.HandleFunc("/blocked", handleBlocked)
		http.HandleFunc("/issue/", handleIssueDetail)
		http.HandleFunc("/graph/", handleGraph)
		http.HandleFunc("/api/issues", handleAPIIssues)
		http.HandleFunc("/api/issue/", handleAPIIssue)
		http.HandleFunc("/api/stats", handleAPIStats)
		http.HandleFunc("/static/", handleStatic)

		fmt.Printf("Starting beads web UI at http://%s\n", addr)
		fmt.Printf("Press Ctrl+C to stop\n")
		
		if err := http.ListenAndServe(addr, nil); err != nil {
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

	tmpl, err := template.ParseFS(embedFS, "templates/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ctx := context.Background()
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{Limit: 100})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch labels for each issue
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

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleIssueDetail(w http.ResponseWriter, r *http.Request) {
	issueID := strings.TrimPrefix(r.URL.Path, "/issue/")
	if issueID == "" {
		http.Error(w, "Issue ID required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	issue, err := store.GetIssue(ctx, issueID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	deps, _ := store.GetDependencies(ctx, issueID)
	dependents, _ := store.GetDependents(ctx, issueID)
	labels, _ := store.GetLabels(ctx, issueID)
	events, _ := store.GetEvents(ctx, issueID, 50)

	tmpl, err := template.ParseFS(embedFS, "templates/detail.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Issue":      issue,
		"Deps":       deps,
		"Dependents": dependents,
		"Labels":     labels,
		"Events":     events,
		"HasDeps":    len(deps) > 0 || len(dependents) > 0,
	}

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleGraph(w http.ResponseWriter, r *http.Request) {
	issueID := strings.TrimPrefix(r.URL.Path, "/graph/")
	if issueID == "" {
		http.Error(w, "Issue ID required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	issue, err := store.GetIssue(ctx, issueID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	tree, err := store.GetDependencyTree(ctx, issueID, 10)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	dotGraph := generateDotGraph(issue, tree)

	tmpl, err := template.ParseFS(embedFS, "templates/graph.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Issue":    issue,
		"DotGraph": dotGraph,
	}

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func generateDotGraph(root *types.Issue, tree []*types.TreeNode) string {
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
	ctx := context.Background()
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
		if issue.Status == "closed" {
			color = "#8a8175"
		} else if issue.Status == "in_progress" {
			color = "#c17a3c"
		}
		
		label := fmt.Sprintf("%s\\n%s\\nP%d", issue.ID, 
			strings.ReplaceAll(issue.Title, "\"", "'"), 
			issue.Priority)
		
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

func handleAPIIssues(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	
	filter := types.IssueFilter{Limit: 1000}
	searchQuery := ""
	
	if status := r.URL.Query().Get("status"); status != "" {
		s := types.Status(status)
		filter.Status = &s
	}
	
	if priority := r.URL.Query().Get("priority"); priority != "" {
		var p int
		fmt.Sscanf(priority, "%d", &p)
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
		tmpl, err := template.ParseFS(embedFS, "templates/issues_tbody.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if err := tmpl.Execute(w, issuesWithLabels); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Regular JSON response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(issues)
}

func handleAPIIssue(w http.ResponseWriter, r *http.Request) {
	issueID := strings.TrimPrefix(r.URL.Path, "/api/issue/")
	
	ctx := context.Background()
	issue, err := store.GetIssue(ctx, issueID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(issue)
}

func handleAPIStats(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	stats, err := store.GetStatistics(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func handleReady(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	
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

	tmpl, err := template.ParseFS(embedFS, "templates/ready.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Issues":       issuesWithLabels,
		"Stats":        stats,
		"ExcludeLabel": excludeLabel,
	}

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleBlocked(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	
	blocked, err := store.GetBlockedIssues(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stats, _ := store.GetStatistics(ctx)

	tmpl, err := template.ParseFS(embedFS, "templates/blocked.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Blocked": blocked,
		"Stats":   stats,
	}

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type IssueWithLabels struct {
	*types.Issue
	Labels []string
}

func enrichIssuesWithLabels(ctx context.Context, issues []*types.Issue) []*IssueWithLabels {
	result := make([]*IssueWithLabels, len(issues))
	for i, issue := range issues {
		labels, _ := store.GetLabels(ctx, issue.ID)
		result[i] = &IssueWithLabels{
			Issue:  issue,
			Labels: labels,
		}
	}
	return result
}

func handleStatic(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/static/")
	
	var contentType string
	if strings.HasSuffix(path, ".css") {
		contentType = "text/css"
	} else if strings.HasSuffix(path, ".js") {
		contentType = "application/javascript"
	}

	content, err := embedFS.ReadFile("templates/" + path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Write(content)
}
