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
		http.HandleFunc("/issue/", handleIssueDetail)
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

	stats, err := store.GetStatistics(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Issues": issues,
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
	}

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleAPIIssues(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	
	filter := types.IssueFilter{Limit: 1000}
	
	if status := r.URL.Query().Get("status"); status != "" {
		s := types.Status(status)
		filter.Status = &s
	}
	
	if priority := r.URL.Query().Get("priority"); priority != "" {
		var p int
		fmt.Sscanf(priority, "%d", &p)
		filter.Priority = &p
	}

	issues, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

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
