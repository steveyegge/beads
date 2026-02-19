package main

import (
	"github.com/spf13/cobra"
)

var (
	reasonLintReason             string
	reasonLintAllowFailureReason bool
)

var reasonCmd = &cobra.Command{
	Use:     "reason",
	GroupID: "views",
	Short:   "Reason and close-policy utilities",
}

var reasonLintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Lint close reason for conditional-block safety",
	Run: func(cmd *cobra.Command, args []string) {
		if err := lintCloseReason(reasonLintReason, reasonLintAllowFailureReason); err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "reason lint",
				Result:  "policy_violation",
				Details: map[string]interface{}{
					"message": err.Error(),
					"reason":  reasonLintReason,
				},
				Events: []string{"reason_invalid"},
			}, exitCodePolicyViolation)
			return
		}

		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "reason lint",
			Result:  "ok",
			Details: map[string]interface{}{
				"reason": reasonLintReason,
			},
			Events: []string{"reason_valid"},
		}, 0)
	},
}

func init() {
	reasonLintCmd.Flags().StringVar(&reasonLintReason, "reason", "", "Close reason to lint")
	reasonLintCmd.Flags().BoolVar(&reasonLintAllowFailureReason, "allow-failure-reason", false, "Allow failed: close reasons")
	reasonLintCmd.ValidArgsFunction = noCompletions

	reasonCmd.AddCommand(reasonLintCmd)
	rootCmd.AddCommand(reasonCmd)
}
