package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

func runDebtCapture() reflectDebtSummary {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("\nTechnical debt to flag? [enter to skip]: ")
	debt, _ := reader.ReadString('\n')
	debt = strings.TrimSpace(debt)
	if debt == "" {
		return reflectDebtSummary{Logged: false}
	}

	fmt.Print("Severity? [low/medium/high]: ")
	severity, _ := reader.ReadString('\n')
	severity = strings.TrimSpace(strings.ToLower(severity))
	if severity == "" {
		severity = "medium"
	}

	fmt.Print("Create bead? [y/n]: ")
	create, _ := reader.ReadString('\n')
	create = strings.TrimSpace(strings.ToLower(create))

	if create != "y" && create != "yes" {
		journalPath := getReflectJournalPath()
		entry := fmt.Sprintf("[DEBT:%s] %s", severity, debt)
		if err := appendToJournal(journalPath, entry, "debt"); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving: %v\n", err)
			return reflectDebtSummary{Logged: false}
		}
		fmt.Printf("%s Logged to %s\n", ui.RenderPassIcon(), journalPath)
		return reflectDebtSummary{Logged: true, Path: journalPath, Severity: severity}
	}

	priority := 2
	switch severity {
	case "high":
		priority = 1
	case "low":
		priority = 3
	}

	title := fmt.Sprintf("[DEBT] %s", reflectTruncate(debt, 60))
	issue := &types.Issue{
		Title:       title,
		Description: debt,
		IssueType:   types.TypeChore,
		Priority:    priority,
		Status:      types.StatusOpen,
		Labels:      []string{"tech-debt", severity},
	}

	if err := store.CreateIssue(rootCtx, issue, getActorWithGit()); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating bead: %v\n", err)
		return reflectDebtSummary{Logged: false}
	}

	info := issueToInfo(issue)
	fmt.Printf("%s Created %s: %s\n", ui.RenderPassIcon(), issue.ID, title)
	return reflectDebtSummary{Logged: true, Created: &info, Severity: severity}
}

func reflectTruncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
