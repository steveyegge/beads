package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// WorkspaceScanResult holds the scan results
type WorkspaceScanResult struct {
	Timestamp    string              `json:"timestamp"`
	WorkspaceDir string              `json:"workspace_dir"`
	HubDir       string              `json:"hub_dir"`
	Items        []WorkspaceScanItem `json:"items"`
	Summary      WorkspaceScanSummary `json:"summary"`
}

// WorkspaceScanItem represents a discovered item
type WorkspaceScanItem struct {
	Path        string `json:"path"`
	Type        string `json:"type"` // spec, doc, blog, readme
	Project     string `json:"project"`
	Title       string `json:"title"`
	Status      string `json:"status"` // new, updated, unchanged
	Decision    string `json:"decision"` // inbox, triage, active
	HubNotePath string `json:"hub_note_path,omitempty"`
}

// WorkspaceScanSummary holds counts
type WorkspaceScanSummary struct {
	ProjectsScanned int `json:"projects_scanned"`
	ItemsFound      int `json:"items_found"`
	NewItems        int `json:"new_items"`
	UpdatedItems    int `json:"updated_items"`
	NotesCreated    int `json:"notes_created,omitempty"`
}

var workspaceCmd = &cobra.Command{
	Use:     "workspace",
	GroupID: "advanced",
	Short:   "Workspace-level commands",
	Long:    "Commands that operate across the entire workspace, not just the current project.",
}

var workspaceScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan workspace for specs and docs, write hub notes",
	Long: `Scan the entire workspace for specs, docs, and blogs.
Creates or updates notes in workspace-hub with confirmation.

Examples:
  bd workspace scan                    # Preview mode (default)
  bd workspace scan --apply            # Write hub notes with confirmation
  bd workspace scan --apply --yes      # Skip confirmation prompt
  bd workspace scan --path ~/projects  # Custom workspace path`,
	Run: runWorkspaceScan,
}

// Flags
var (
	workspacePath string
	hubPath       string
	applyChanges  bool
	skipConfirm   bool
	createBeads   bool
)

var createBeadFn = createBeadInRepo

func init() {
	// Default paths
	homeDir, _ := os.UserHomeDir()
	defaultWorkspace := filepath.Join(homeDir, "Desktop", "workspace")
	defaultHub := filepath.Join(defaultWorkspace, "workspace-hub")

	workspaceScanCmd.Flags().StringVar(&workspacePath, "path", defaultWorkspace, "Workspace root path")
	workspaceScanCmd.Flags().StringVar(&hubPath, "hub", defaultHub, "Workspace hub path")
	workspaceScanCmd.Flags().BoolVar(&applyChanges, "apply", false, "Apply changes (write hub notes)")
	workspaceScanCmd.Flags().BoolVar(&skipConfirm, "yes", false, "Skip confirmation prompt")
	workspaceScanCmd.Flags().BoolVar(&createBeads, "create-beads", false, "Create beads for actionable items")

	workspaceCmd.AddCommand(workspaceScanCmd)
	rootCmd.AddCommand(workspaceCmd)
}

func runWorkspaceScan(cmd *cobra.Command, args []string) {
	// Validate paths
	if _, err := os.Stat(workspacePath); os.IsNotExist(err) {
		FatalErrorRespectJSON("workspace path does not exist: %s", workspacePath)
	}

	result := scanWorkspace()

	if jsonOutput {
		outputJSON(result)
		return
	}

	// Print preview
	printScanPreview(result)

	if !applyChanges {
		fmt.Println("\nPreview mode. Use --apply to write hub notes.")
		return
	}

	// Confirmation
	if !skipConfirm && result.Summary.NewItems > 0 {
		fmt.Printf("\nApply %d changes? [y/N] ", result.Summary.NewItems)
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Aborted.")
			return
		}
	}

	// Apply changes
	notesCreated, err := applyHubNotes(result, createBeads)
	if err != nil {
		FatalErrorRespectJSON("workspace scan failed: %s", err)
	}
	result.Summary.NotesCreated = notesCreated

	fmt.Printf("\nCreated %d hub notes.\n", notesCreated)

	if _, err := writeScanReport(result); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write scan report: %v\n", err)
	}
}

func scanWorkspace() *WorkspaceScanResult {
	result := &WorkspaceScanResult{
		Timestamp:    time.Now().Format(time.RFC3339),
		WorkspaceDir: workspacePath,
		HubDir:       hubPath,
		Items:        []WorkspaceScanItem{},
	}

	// Content scan triggers (from workspace-hub design)
	folderTriggers := []string{"specs", "docs", "blog", "blogs"}
	filePatterns := []string{"SPEC_*.md", "README.md", "*.blog.md", "*.blog.txt"}

	projectsScanned := make(map[string]bool)

	// Walk workspace
	filepath.Walk(workspacePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip hidden dirs, node_modules, etc.
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		// Get relative path and project name
		relPath, _ := filepath.Rel(workspacePath, path)
		parts := strings.Split(relPath, string(os.PathSeparator))
		if len(parts) < 2 {
			return nil
		}
		projectName := parts[0]

		// Skip workspace-hub itself
		if projectName == "workspace-hub" {
			return nil
		}

		projectsScanned[projectName] = true

		// Check folder triggers
		inTriggerFolder := false
		for _, trigger := range folderTriggers {
			if strings.Contains(relPath, string(os.PathSeparator)+trigger+string(os.PathSeparator)) ||
				strings.HasPrefix(relPath, projectName+string(os.PathSeparator)+trigger) {
				inTriggerFolder = true
				break
			}
		}

		// Check file patterns
		matchesPattern := false
		fileName := info.Name()
		for _, pattern := range filePatterns {
			if matched, _ := filepath.Match(pattern, fileName); matched {
				matchesPattern = true
				break
			}
		}

		if !inTriggerFolder && !matchesPattern {
			return nil
		}

		// Determine type
		itemType := determineItemType(path, fileName)

		// Extract title from file
		title := extractTitle(path, fileName)

		decision := decideHubBucket(itemType)
		hubNotePath := generateHubNotePath(projectName, fileName, decision)
		status := classifyItemStatus(path, hubNotePath)

		item := WorkspaceScanItem{
			Path:        path,
			Type:        itemType,
			Project:     projectName,
			Title:       title,
			Status:      status,
			Decision:    decision,
			HubNotePath: hubNotePath,
		}

		result.Items = append(result.Items, item)
		return nil
	})

	// Build summary
	result.Summary.ProjectsScanned = len(projectsScanned)
	result.Summary.ItemsFound = len(result.Items)
	for _, item := range result.Items {
		if item.Status == "new" {
			result.Summary.NewItems++
		} else if item.Status == "updated" {
			result.Summary.UpdatedItems++
		}
	}

	return result
}

func determineItemType(path, fileName string) string {
	lowerName := strings.ToLower(fileName)
	lowerPath := strings.ToLower(path)

	if strings.HasPrefix(fileName, "SPEC_") || strings.Contains(lowerPath, "/specs/") {
		return "spec"
	}
	if strings.Contains(lowerName, ".blog.") || strings.Contains(lowerPath, "/blog") {
		return "blog"
	}
	if lowerName == "readme.md" {
		return "readme"
	}
	if strings.Contains(lowerPath, "/docs/") {
		return "doc"
	}
	return "doc"
}

func extractTitle(path, fileName string) string {
	// Try to read first heading from markdown
	if strings.HasSuffix(strings.ToLower(fileName), ".md") {
		file, err := os.Open(path)
		if err != nil {
			return fileName
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "# ") {
				return strings.TrimPrefix(line, "# ")
			}
		}
	}
	return strings.TrimSuffix(fileName, filepath.Ext(fileName))
}

func classifyItemStatus(srcPath, hubNotePath string) string {
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return "new"
	}
	noteInfo, err := os.Stat(hubNotePath)
	if os.IsNotExist(err) {
		return "new"
	}
	if err != nil {
		return "new"
	}
	if srcInfo.ModTime().After(noteInfo.ModTime()) {
		return "updated"
	}
	return "unchanged"
}

func decideHubBucket(itemType string) string {
	switch itemType {
	case "doc", "blog":
		return "triage"
	default:
		return "inbox"
	}
}

func generateHubNotePath(project, fileName, decision string) string {
	noteName := fmt.Sprintf("%s_%s", project, strings.TrimSuffix(fileName, filepath.Ext(fileName)))
	noteName = strings.ReplaceAll(noteName, " ", "_")
	return filepath.Join(hubPath, decision, noteName+".md")
}

func generateHubNotePathForItem(item WorkspaceScanItem, decision string) string {
	return generateHubNotePath(item.Project, filepath.Base(item.Path), decision)
}

func printScanPreview(result *WorkspaceScanResult) {
	fmt.Printf("Workspace Scan: %s\n", result.WorkspaceDir)
	fmt.Printf("Hub: %s\n", result.HubDir)
	fmt.Printf("Scanned: %d projects, Found: %d items\n\n", result.Summary.ProjectsScanned, result.Summary.ItemsFound)

	if len(result.Items) == 0 {
		fmt.Println("No items found.")
		return
	}

	// Group by status
	newItems := []WorkspaceScanItem{}
	updatedItems := []WorkspaceScanItem{}
	unchangedItems := []WorkspaceScanItem{}

	for _, item := range result.Items {
		switch item.Status {
		case "new":
			newItems = append(newItems, item)
		case "updated":
			updatedItems = append(updatedItems, item)
		default:
			unchangedItems = append(unchangedItems, item)
		}
	}

	if len(newItems) > 0 {
		fmt.Printf("New items (%d):\n", len(newItems))
		for _, item := range newItems {
			fmt.Printf("  + [%s] %s/%s → %s\n", item.Type, item.Project, item.Title, item.Decision)
		}
	}

	if len(updatedItems) > 0 {
		fmt.Printf("\nUpdated items (%d):\n", len(updatedItems))
		for _, item := range updatedItems {
			fmt.Printf("  ~ [%s] %s/%s → %s\n", item.Type, item.Project, item.Title, item.Decision)
		}
	}

	if len(unchangedItems) > 0 {
		fmt.Printf("\nUnchanged items (%d):\n", len(unchangedItems))
		for _, item := range unchangedItems {
			fmt.Printf("  = [%s] %s/%s\n", item.Type, item.Project, item.Title)
		}
	}
}

func applyHubNotes(result *WorkspaceScanResult, createBeads bool) (int, error) {
	notesCreated := 0

	// Ensure hub directories exist
	for _, dir := range []string{"inbox", "triage", "active", "reports"} {
		if err := os.MkdirAll(filepath.Join(hubPath, dir), 0755); err != nil {
			return notesCreated, fmt.Errorf("failed to create hub dir %s: %w", dir, err)
		}
	}

	for _, item := range result.Items {
		if item.Status == "unchanged" {
			continue
		}

		beadID := ""
		if createBeads && item.Status == "new" && shouldCreateBead(item) {
			var err error
			beadID, err = createBeadFn(item)
			if err != nil {
				return notesCreated, err
			}
			if beadID != "" {
				item.Decision = "active"
				item.HubNotePath = generateHubNotePathForItem(item, item.Decision)
			}
		}

		// Create hub note
		noteContent := generateHubNote(item, beadID)
		err := os.WriteFile(item.HubNotePath, []byte(noteContent), 0644)
		if err != nil {
			fmt.Printf("Error creating note for %s: %v\n", item.Title, err)
			continue
		}
		notesCreated++
	}

	return notesCreated, nil
}

func generateHubNote(item WorkspaceScanItem, beadID string) string {
	beadLine := ""
	if beadID != "" {
		beadLine = fmt.Sprintf("**Bead:** %s\n", beadID)
	}
	return fmt.Sprintf(`# %s

**Project:** %s
**Type:** %s
**Source:** %s
**Created:** %s
**Status:** %s
%s

## Summary

[Auto-generated hub note. Add summary here.]

## Links

- Source: [%s](%s)
`, item.Title, item.Project, item.Type, item.Path, time.Now().Format("2006-01-02"), item.Decision, beadLine, item.Title, item.Path)
}

func shouldCreateBead(item WorkspaceScanItem) bool {
	return item.Type == "spec"
}

func createBeadInRepo(item WorkspaceScanItem) (string, error) {
	projectRoot := filepath.Join(workspacePath, item.Project)
	cmd := exec.Command(os.Args[0], "create", item.Title, "--spec-id", item.Path, "--json")
	cmd.Dir = projectRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("bd create failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", fmt.Errorf("failed to parse bd create output: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return resp.ID, nil
}

func writeScanReport(result *WorkspaceScanResult) (string, error) {
	timestamp := time.Now().Format("2006-01-02_150405")
	reportPath := filepath.Join(hubPath, "reports", fmt.Sprintf("scan_content_%s.md", timestamp))
	var b strings.Builder
	b.WriteString("# Workspace Scan Report\n\n")
	b.WriteString(fmt.Sprintf("**Workspace:** %s\n", result.WorkspaceDir))
	b.WriteString(fmt.Sprintf("**Hub:** %s\n", result.HubDir))
	b.WriteString(fmt.Sprintf("**Timestamp:** %s\n\n", result.Timestamp))
	b.WriteString(fmt.Sprintf("- Projects: %d\n", result.Summary.ProjectsScanned))
	b.WriteString(fmt.Sprintf("- Items: %d\n", result.Summary.ItemsFound))
	b.WriteString(fmt.Sprintf("- New: %d\n", result.Summary.NewItems))
	b.WriteString(fmt.Sprintf("- Updated: %d\n\n", result.Summary.UpdatedItems))
	if len(result.Items) > 0 {
		b.WriteString("## Items\n\n")
		for _, item := range result.Items {
			b.WriteString(fmt.Sprintf("- [%s] %s/%s (%s)\n", item.Type, item.Project, item.Title, item.Status))
		}
	}
	if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(reportPath, []byte(b.String()), 0644); err != nil {
		return "", err
	}
	return reportPath, nil
}
