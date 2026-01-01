package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	sandboxDir      string
	sandboxKeep     bool
	sandboxWith     string
	sandboxIssues   int
	sandboxPrefix   string
)

var sandboxCmd = &cobra.Command{
	Use:   "sandbox",
	Short: "Create an isolated sandbox environment for testing",
	Long: `Create a temporary beads project for safe script testing.

The sandbox creates:
  - A temp directory with initialized beads project
  - Sample issues with various types and dependencies
  - Optionally copies example scripts for testing

After testing, the sandbox is automatically cleaned up unless --keep is specified.

Examples:
  bd-examples sandbox                    # Create sandbox with 5 issues
  bd-examples sandbox --issues 20        # Create with more issues
  bd-examples sandbox --with bash-agent  # Copy bash-agent scripts
  bd-examples sandbox --keep             # Don't auto-cleanup`,
	RunE: runSandbox,
}

func init() {
	sandboxCmd.Flags().StringVar(&sandboxDir, "dir", "", "Directory for sandbox (default: temp dir)")
	sandboxCmd.Flags().BoolVar(&sandboxKeep, "keep", false, "Don't cleanup sandbox after exit")
	sandboxCmd.Flags().StringVar(&sandboxWith, "with", "", "Copy scripts from this example folder")
	sandboxCmd.Flags().IntVar(&sandboxIssues, "issues", 5, "Number of sample issues to create")
	sandboxCmd.Flags().StringVar(&sandboxPrefix, "prefix", "test", "Issue ID prefix")
}

func runSandbox(cmd *cobra.Command, args []string) error {
	// Create sandbox directory
	var dir string
	var err error

	if sandboxDir != "" {
		dir = sandboxDir
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create sandbox directory: %v", err)
		}
	} else {
		dir, err = os.MkdirTemp("", "bd-sandbox-")
		if err != nil {
			return fmt.Errorf("failed to create temp directory: %v", err)
		}
	}

	fmt.Printf("Creating sandbox at %s...\n\n", accentStyle.Render(dir))

	// Initialize git repo
	if verbose {
		fmt.Println(mutedStyle.Render("Initializing git repository..."))
	}
	gitInit := exec.Command("git", "init")
	gitInit.Dir = dir
	if err := gitInit.Run(); err != nil {
		return fmt.Errorf("failed to initialize git: %v", err)
	}

	// Initialize beads
	if verbose {
		fmt.Println(mutedStyle.Render("Initializing beads project..."))
	}
	bdInit := exec.Command("bd", "init", "--prefix="+sandboxPrefix)
	bdInit.Dir = dir
	if err := bdInit.Run(); err != nil {
		return fmt.Errorf("failed to initialize beads: %v", err)
	}

	// Create sample issues
	fmt.Printf("Creating %d sample issues...\n", sandboxIssues)
	issueIDs, err := createSampleIssues(dir, sandboxIssues)
	if err != nil {
		return fmt.Errorf("failed to create sample issues: %v", err)
	}

	// Create some dependencies
	if len(issueIDs) >= 3 {
		// Make some issues depend on the first one
		for i := 2; i < len(issueIDs) && i < 5; i++ {
			depCmd := exec.Command("bd", "dep", "add", issueIDs[i], issueIDs[0])
			depCmd.Dir = dir
			_ = depCmd.Run() // Ignore errors
		}
	}

	// Copy example scripts if requested
	if sandboxWith != "" {
		if err := copyExampleScripts(dir, sandboxWith); err != nil {
			fmt.Printf("%s Failed to copy scripts: %v\n", warnStyle.Render("Warning:"), err)
		}
	}

	// Sync beads
	bdSync := exec.Command("bd", "sync")
	bdSync.Dir = dir
	_ = bdSync.Run()

	// Print summary
	fmt.Println()
	fmt.Printf("%s\n", passStyle.Render("Sandbox created successfully!"))
	fmt.Println()

	// Show stats
	bdStats := exec.Command("bd", "stats")
	bdStats.Dir = dir
	bdStats.Stdout = os.Stdout
	_ = bdStats.Run()

	fmt.Println()
	fmt.Println(boldStyle.Render("To use the sandbox:"))
	fmt.Printf("  cd %s\n", dir)
	fmt.Println("  bd ready")
	if sandboxWith != "" {
		scripts := GetScriptsByFolder(sandboxWith)
		if len(scripts) > 0 {
			fmt.Printf("  ./%s\n", scripts[0].Path)
		}
	}
	fmt.Println()

	if !sandboxKeep {
		fmt.Println(mutedStyle.Render("Cleanup:"))
		fmt.Printf("  rm -rf %s\n", dir)
		fmt.Println()
		fmt.Println(warnStyle.Render("Note: Use --keep to preserve the sandbox"))
	}

	if jsonOutput {
		out := map[string]interface{}{
			"directory": dir,
			"issues":    issueIDs,
			"keep":      sandboxKeep,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	return nil
}

func createSampleIssues(dir string, count int) ([]string, error) {
	issueTypes := []string{"bug", "feature", "task", "chore"}
	priorities := []string{"0", "1", "2", "3"}
	sampleTitles := []string{
		"Fix login authentication",
		"Add user profile page",
		"Update database schema",
		"Refactor API endpoints",
		"Add unit tests",
		"Fix memory leak",
		"Implement caching",
		"Update documentation",
		"Add error handling",
		"Optimize queries",
		"Add logging",
		"Fix race condition",
		"Add rate limiting",
		"Implement retry logic",
		"Add health check",
	}

	var ids []string
	for i := 0; i < count; i++ {
		title := sampleTitles[i%len(sampleTitles)]
		issueType := issueTypes[rand.Intn(len(issueTypes))]
		priority := priorities[rand.Intn(len(priorities))]

		bdCreate := exec.Command("bd", "create",
			"--title", fmt.Sprintf("%s #%d", title, i+1),
			"--type", issueType,
			"--priority", priority,
			"--json",
		)
		bdCreate.Dir = dir
		output, err := bdCreate.Output()
		if err != nil {
			continue // Skip on error
		}

		// Extract ID from JSON output
		var result map[string]interface{}
		if err := json.Unmarshal(output, &result); err == nil {
			if id, ok := result["id"].(string); ok {
				ids = append(ids, id)
				if verbose {
					fmt.Printf("  Created: %s\n", id)
				}
			}
		}
	}

	return ids, nil
}

func copyExampleScripts(sandboxDir, folder string) error {
	exDir, err := findExamplesDir()
	if err != nil {
		return err
	}

	srcDir := filepath.Join(exDir, folder)
	if _, err := os.Stat(srcDir); err != nil {
		return fmt.Errorf("example folder not found: %s", folder)
	}

	// Create destination directory
	dstDir := filepath.Join(sandboxDir, folder)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}

	// Copy files
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())

		content, err := os.ReadFile(srcPath)
		if err != nil {
			continue
		}

		if err := os.WriteFile(dstPath, content, 0755); err != nil {
			continue
		}

		if verbose {
			fmt.Printf("  Copied: %s\n", entry.Name())
		}
	}

	return nil
}
