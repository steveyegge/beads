package main

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

type wobbleCascadeResult struct {
	Skill      string   `json:"skill"`
	Dependents []string `json:"dependents,omitempty"`
}

var wobbleCascadeCmd = &cobra.Command{
	Use:     "cascade [skill]",
	Short:   "Show wobble cascade impact",
	Args:    cobra.ExactArgs(1),
	GroupID: GroupMaintenance,
	Run:     runWobbleCascade,
}

func init() {
	rootCmd.AddCommand(wobbleCascadeCmd)
}

func runWobbleCascade(_ *cobra.Command, args []string) {
	skillID := args[0]
	skillsPath, historyPath, err := wobbleStorePaths()
	if err != nil {
		FatalErrorRespectJSON("wobble store: %v", err)
	}
	storeSnapshot, _, err := loadWobbleStore(skillsPath, historyPath)
	if err != nil {
		FatalErrorRespectJSON("wobble store: %v", err)
	}

	var dependents []string
	for _, skill := range storeSnapshot.Skills {
		if skill.ID == skillID {
			dependents = append([]string{}, skill.Dependents...)
			break
		}
	}
	sort.Strings(dependents)

	if jsonOutput {
		outputJSON(wobbleCascadeResult{Skill: skillID, Dependents: dependents})
		return
	}

	if len(dependents) == 0 {
		fmt.Printf("Dependents for %s: none\n", skillID)
		return
	}

	fmt.Printf("Dependents for %s (%d):\n", skillID, len(dependents))
	for _, dep := range dependents {
		fmt.Printf("  - %s\n", dep)
	}
}
