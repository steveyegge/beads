package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
)

var milestoneCmd = &cobra.Command{
	Use:     "milestone",
	GroupID: "issues",
	Short:   "Manage milestones",
}

var milestoneCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a milestone",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("milestone create")
		name := args[0]
		targetStr, _ := cmd.Flags().GetString("target")
		description, _ := cmd.Flags().GetString("description")

		if err := withStorage(rootCtx, store, dbPath, func(s *dolt.DoltStore) error {
			ms := &types.Milestone{
				Name:        name,
				Description: description,
			}
			if targetStr != "" {
				t, err := time.Parse("2006-01-02", targetStr)
				if err != nil {
					FatalErrorRespectJSON("invalid date: %v (use YYYY-MM-DD)", err)
				}
				ms.TargetDate = &t
			}

			if err := s.CreateMilestone(rootCtx, ms, actor); err != nil {
				FatalErrorRespectJSON("%v", err)
			}
			fmt.Printf("Created milestone %q\n", name)
			return nil
		}); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
	},
}

var milestoneListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all milestones",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if err := withStorage(rootCtx, store, dbPath, func(s *dolt.DoltStore) error {
			milestones, err := s.ListMilestones(rootCtx)
			if err != nil {
				FatalErrorRespectJSON("%v", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(milestones)
			}

			if len(milestones) == 0 {
				fmt.Println("No milestones.")
				return nil
			}

			for _, ms := range milestones {
				target := "no date"
				if ms.TargetDate != nil {
					target = ms.TargetDate.Format("2006-01-02")
				}
				fmt.Printf("  %s  target: %s", ms.Name, target)
				if ms.Description != "" {
					fmt.Printf("  %q", ms.Description)
				}
				fmt.Println()
			}
			return nil
		}); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
	},
}

var milestoneStatusCmd = &cobra.Command{
	Use:   "status <name>",
	Short: "Show milestone detail with linked issues",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]

		if err := withStorage(rootCtx, store, dbPath, func(s *dolt.DoltStore) error {
			ms, err := s.GetMilestone(rootCtx, name)
			if err != nil {
				FatalErrorRespectJSON("%v", err)
			}

			msName := name
			filter := types.IssueFilter{Milestone: &msName}
			issues, err := s.SearchIssues(rootCtx, "", filter)
			if err != nil {
				FatalErrorRespectJSON("%v", err)
			}

			if jsonOutput {
				result := map[string]interface{}{
					"milestone": ms,
					"issues":    issues,
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			target := "no date"
			if ms.TargetDate != nil {
				target = ms.TargetDate.Format("2006-01-02")
			}
			fmt.Printf("Milestone: %s\n", ms.Name)
			fmt.Printf("Target: %s\n", target)
			if ms.Description != "" {
				fmt.Printf("Description: %s\n", ms.Description)
			}

			closed := 0
			for _, issue := range issues {
				if issue.Status == types.StatusClosed {
					closed++
				}
			}
			if len(issues) > 0 {
				pct := float64(closed) / float64(len(issues)) * 100
				fmt.Printf("Progress: %d/%d (%.0f%%)\n\n", closed, len(issues), pct)
			}

			for _, issue := range issues {
				check := " "
				if issue.Status == types.StatusClosed {
					check = "x"
				}
				fmt.Printf("  [%s] %s  %q  %s\n", check, issue.ID, issue.Title, issue.Status)
			}
			return nil
		}); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
	},
}

var milestoneDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a milestone",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("milestone delete")
		name := args[0]

		if err := withStorage(rootCtx, store, dbPath, func(s *dolt.DoltStore) error {
			if err := s.DeleteMilestone(rootCtx, name, actor); err != nil {
				FatalErrorRespectJSON("%v", err)
			}
			fmt.Printf("Deleted milestone %q\n", name)
			return nil
		}); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(milestoneCmd)
	milestoneCmd.AddCommand(milestoneCreateCmd)
	milestoneCmd.AddCommand(milestoneListCmd)
	milestoneCmd.AddCommand(milestoneStatusCmd)
	milestoneCmd.AddCommand(milestoneDeleteCmd)

	milestoneCreateCmd.Flags().String("target", "", "Target date (YYYY-MM-DD)")
	milestoneCreateCmd.Flags().String("description", "", "Milestone description")
}
