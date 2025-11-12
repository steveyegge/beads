package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/steveyegge/beads/pkg/agentmail"
)

type Issue struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Priority    int    `json:"priority"`
	IssueType   string `json:"issue_type"`
}

type BeadsAgent struct {
	agentName      string
	projectID      string
	agentMailURL   string
	mailClient     *agentmail.Client
	maxIterations  int
}

func NewBeadsAgent(agentName, projectID, agentMailURL string, maxIterations int) *BeadsAgent {
	agent := &BeadsAgent{
		agentName:     agentName,
		projectID:     projectID,
		agentMailURL:  agentMailURL,
		maxIterations: maxIterations,
	}

	if agentMailURL != "" {
		_ = os.Setenv("BEADS_AGENT_MAIL_URL", agentMailURL)
		_ = os.Setenv("BEADS_AGENT_NAME", agentName)
		_ = os.Setenv("BEADS_PROJECT_ID", projectID)
		agent.mailClient = agentmail.NewClient(
			agentmail.WithURL(agentMailURL),
			agentmail.WithAgentName(agentName),
		)
		if agent.mailClient.Enabled {
			fmt.Printf("✨ Agent Mail enabled: %s @ %s\n", agentName, agentMailURL)
		} else {
			fmt.Printf("📝 Git-only mode: %s (Agent Mail unavailable)\n", agentName)
		}
	} else {
		fmt.Printf("📝 Git-only mode: %s\n", agentName)
	}

	return agent
}

func (a *BeadsAgent) runBD(args ...string) ([]byte, error) {
	args = append(args, "--json")
	cmd := exec.Command("bd", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "already reserved") || strings.Contains(string(output), "reservation conflict") {
			return output, fmt.Errorf("reservation_conflict")
		}
		return output, err
	}
	return output, nil
}

func (a *BeadsAgent) getReadyWork() ([]Issue, error) {
	output, err := a.runBD("ready")
	if err != nil {
		return nil, err
	}

	var issues []Issue
	if err := json.Unmarshal(output, &issues); err != nil {
		return nil, fmt.Errorf("failed to parse ready work: %w", err)
	}

	return issues, nil
}

func (a *BeadsAgent) claimIssue(issueID string) bool {
	fmt.Printf("📋 Claiming issue: %s\n", issueID)
	
	if a.mailClient != nil && a.mailClient.Enabled {
		if !a.mailClient.ReserveIssue(issueID, 3600) {
			fmt.Printf("   ⚠️  Issue %s already claimed by another agent\n", issueID)
			return false
		}
	}

	_, err := a.runBD("update", issueID, "--status", "in_progress")
	if err != nil {
		if err.Error() == "reservation_conflict" {
			fmt.Printf("   ⚠️  Issue %s already claimed by another agent\n", issueID)
			return false
		}
		fmt.Printf("   ❌ Failed to claim %s: %v\n", issueID, err)
		return false
	}

	fmt.Printf("   ✅ Successfully claimed %s\n", issueID)
	return true
}

func (a *BeadsAgent) completeIssue(issueID, reason string) bool {
	fmt.Printf("✅ Completing issue: %s\n", issueID)
	
	_, err := a.runBD("close", issueID, "--reason", reason)
	if err != nil {
		fmt.Printf("   ❌ Failed to complete %s: %v\n", issueID, err)
		return false
	}

	if a.mailClient != nil && a.mailClient.Enabled {
		a.mailClient.ReleaseIssue(issueID)
		a.mailClient.Notify("issue_completed", map[string]interface{}{
			"issue_id": issueID,
			"agent":    a.agentName,
		})
	}

	fmt.Printf("   ✅ Issue %s completed\n", issueID)
	return true
}

func (a *BeadsAgent) createDiscoveredIssue(title, parentID string, priority int, issueType string) string {
	fmt.Printf("💡 Creating discovered issue: %s\n", title)
	
	output, err := a.runBD("create", title,
		"-t", issueType,
		"-p", fmt.Sprintf("%d", priority),
		"--deps", fmt.Sprintf("discovered-from:%s", parentID),
	)
	if err != nil {
		fmt.Printf("   ❌ Failed to create issue: %v\n", err)
		return ""
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		fmt.Printf("   ❌ Failed to parse created issue: %v\n", err)
		return ""
	}

	fmt.Printf("   ✅ Created %s\n", result.ID)
	return result.ID
}

func (a *BeadsAgent) simulateWork(issue Issue) {
	fmt.Printf("🤖 Working on: %s (%s)\n", issue.Title, issue.ID)
	fmt.Printf("   Priority: %d, Type: %s\n", issue.Priority, issue.IssueType)
	time.Sleep(1 * time.Second)
}

func (a *BeadsAgent) run() {
	fmt.Printf("\n🚀 Agent '%s' starting...\n", a.agentName)
	fmt.Printf("   Project: %s\n", a.projectID)
	if a.agentMailURL != "" {
		fmt.Printf("   Agent Mail: Enabled\n\n")
	} else {
		fmt.Printf("   Agent Mail: Disabled (git-only mode)\n\n")
	}

	for iteration := 1; iteration <= a.maxIterations; iteration++ {
		fmt.Println(strings.Repeat("=", 60))
		fmt.Printf("Iteration %d/%d\n", iteration, a.maxIterations)
		fmt.Println(strings.Repeat("=", 60))

		readyIssues, err := a.getReadyWork()
		if err != nil {
			fmt.Printf("❌ Failed to get ready work: %v\n", err)
			continue
		}

		if len(readyIssues) == 0 {
			fmt.Println("📭 No ready work available. Stopping.")
			break
		}

		claimed := false
		for _, issue := range readyIssues {
			if a.claimIssue(issue.ID) {
				claimed = true

				a.simulateWork(issue)

				// 33% chance to discover new work
				if rand.Float32() < 0.33 {
					discoveredTitle := fmt.Sprintf("Follow-up work for %s", issue.Title)
					newID := a.createDiscoveredIssue(discoveredTitle, issue.ID, issue.Priority, "task")
					if newID != "" {
						fmt.Printf("🔗 Linked %s ← discovered-from ← %s\n", newID, issue.ID)
					}
				}

				a.completeIssue(issue.ID, "Implemented successfully")
				break
			}
		}

		if !claimed {
			fmt.Println("⚠️  All ready issues are reserved by other agents. Waiting...")
			time.Sleep(2 * time.Second)
		}

		fmt.Println()
	}

	fmt.Printf("🏁 Agent '%s' finished\n", a.agentName)
}

func main() {
	agentName := flag.String("agent-name", getEnv("BEADS_AGENT_NAME", fmt.Sprintf("agent-%d", os.Getpid())), "Unique agent identifier")
	projectID := flag.String("project-id", getEnv("BEADS_PROJECT_ID", "default"), "Project namespace for Agent Mail")
	agentMailURL := flag.String("agent-mail-url", os.Getenv("BEADS_AGENT_MAIL_URL"), "Agent Mail server URL")
	maxIterations := flag.Int("max-iterations", 10, "Maximum number of issues to process")
	
	flag.Parse()

	agent := NewBeadsAgent(*agentName, *projectID, *agentMailURL, *maxIterations)
	agent.run()
}

func getEnv(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}
