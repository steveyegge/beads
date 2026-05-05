package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

var promptCmd = &cobra.Command{
	Use:     "prompt",
	GroupID: "issues",
	Short:   "Capture user prompts as traceable beads",
}

var promptCaptureCmd = &cobra.Command{
	Use:   "capture",
	Short: "Capture a raw user prompt as a bead",
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("prompt capture")

		title, _ := cmd.Flags().GetString("title")
		if strings.TrimSpace(title) == "" {
			FatalError("--title is required")
		}

		promptText := getPromptTextFlag(cmd)
		if strings.TrimSpace(promptText) == "" {
			FatalError("prompt text is required; pass --stdin or --body-file")
		}

		parentID, _ := cmd.Flags().GetString("parent")
		sessionID, _ := cmd.Flags().GetString("session")
		sourceTool, _ := cmd.Flags().GetString("source-tool")
		summary, _ := cmd.Flags().GetString("summary")
		silent, _ := cmd.Flags().GetBool("silent")
		labels, _ := cmd.Flags().GetStringSlice("labels")
		labelAlias, _ := cmd.Flags().GetStringSlice("label")
		labels = append(labels, labelAlias...)
		labels = mergePromptLabels(labels)

		if sessionID == "" {
			sessionID = os.Getenv("BEADS_SESSION_ID")
		}

		metadata, err := promptCaptureMetadata(sessionID, sourceTool, summary)
		if err != nil {
			FatalError("failed to build prompt metadata: %v", err)
		}

		auditActor := getActorWithGit()
		if parentID != "" {
			if _, err := store.GetIssue(rootCtx, parentID); err != nil {
				if errors.Is(err, storage.ErrNotFound) {
					FatalError("parent issue %s not found", parentID)
				}
				FatalError("failed to check parent issue: %v", err)
			}
		}

		issue := &types.Issue{
			Title:              title,
			Description:        promptText,
			AcceptanceCriteria: "Prompt captured; related work should be linked, parented, or noted from this bead.",
			Status:             types.StatusOpen,
			Priority:           2,
			IssueType:          types.TypeTask,
			CreatedBy:          auditActor,
			Owner:              getOwner(),
			SourceSystem:       "bd prompt capture",
			Metadata:           metadata,
		}

		if err := store.CreateIssue(rootCtx, issue, auditActor); err != nil {
			FatalError("%v", err)
		}

		postCreateWrites := false
		if parentID != "" {
			dep := &types.Dependency{
				IssueID:     issue.ID,
				DependsOnID: parentID,
				Type:        types.DepParentChild,
			}
			if err := store.AddDependency(rootCtx, dep, auditActor); err != nil {
				WarnError("failed to add parent-child dependency %s -> %s: %v", issue.ID, parentID, err)
			} else {
				postCreateWrites = true
			}
		}

		for _, label := range labels {
			if err := store.AddLabel(rootCtx, issue.ID, label, auditActor); err != nil {
				WarnError("failed to add label %s: %v", label, err)
			} else {
				postCreateWrites = true
			}
		}

		if isEmbeddedMode() || postCreateWrites {
			if err := store.Commit(rootCtx, fmt.Sprintf("bd: prompt capture %s", issue.ID)); err != nil && !isDoltNothingToCommit(err) {
				WarnError("failed to commit: %v", err)
			}
		}

		issue.Labels = labels
		if jsonOutput {
			outputJSON(issue)
		} else if silent {
			fmt.Println(issue.ID)
		} else {
			fmt.Printf("%s Captured prompt: %s\n", ui.RenderPass("✓"), formatFeedbackID(issue.ID, issue.Title))
		}
		SetLastTouchedID(issue.ID)
	},
}

func promptCaptureMetadata(sessionID, sourceTool, summary string) (json.RawMessage, error) {
	cwd, _ := os.Getwd()
	metadata := map[string]string{
		"kind":        "prompt",
		"source":      "user_prompt",
		"captured_at": time.Now().UTC().Format(time.RFC3339Nano),
		"actor":       getActorWithGit(),
		"cwd":         cwd,
	}
	if summary != "" {
		metadata["summary"] = summary
	}
	if sessionID != "" {
		metadata["session_id"] = sessionID
	}
	if sourceTool != "" {
		metadata["source_tool"] = sourceTool
	}
	if repo := gitOutput("rev-parse", "--show-toplevel"); repo != "" {
		metadata["repo"] = repo
	}
	if branch := gitOutput("rev-parse", "--abbrev-ref", "HEAD"); branch != "" {
		metadata["git_branch"] = branch
	}
	if head := gitOutput("rev-parse", "--short", "HEAD"); head != "" {
		metadata["git_head"] = head
	}

	raw, err := json.Marshal(metadata)
	return json.RawMessage(raw), err
}

func gitOutput(args ...string) string {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func mergePromptLabels(labels []string) []string {
	seen := make(map[string]bool, len(labels)+2)
	result := make([]string, 0, len(labels)+2)
	for _, label := range append([]string{"prompt", "user-request"}, labels...) {
		label = strings.TrimSpace(label)
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		result = append(result, label)
	}
	return result
}

func getPromptTextFlag(cmd *cobra.Command) string {
	stdinFlag, _ := cmd.Flags().GetBool("stdin")
	bodyFile, _ := cmd.Flags().GetString("body-file")
	description, _ := cmd.Flags().GetString("description")

	changed := 0
	if stdinFlag {
		changed++
	}
	if bodyFile != "" {
		changed++
	}
	if description != "" {
		changed++
	}
	if changed > 1 {
		FatalError("use only one of --stdin, --body-file, or --description")
	}
	if stdinFlag {
		content, err := readBodyFile("-")
		if err != nil {
			FatalError("reading from stdin: %v", err)
		}
		return content
	}
	if bodyFile != "" {
		content, err := readBodyFile(bodyFile)
		if err != nil {
			FatalError("reading body file: %v", err)
		}
		return content
	}
	return description
}

func init() {
	promptCaptureCmd.Flags().String("title", "", "Prompt bead title")
	promptCaptureCmd.Flags().String("summary", "", "Short normalized summary of the prompt")
	promptCaptureCmd.Flags().String("parent", "", "Parent issue ID for hierarchical child")
	promptCaptureCmd.Flags().String("session", "", "Agent/session identifier")
	promptCaptureCmd.Flags().String("source-tool", "", "Tool or agent that captured the prompt")
	promptCaptureCmd.Flags().StringP("description", "d", "", "Raw prompt text")
	promptCaptureCmd.Flags().String("body-file", "", "Read raw prompt text from file (use - for stdin)")
	promptCaptureCmd.Flags().Bool("stdin", false, "Read raw prompt text from stdin")
	promptCaptureCmd.Flags().StringSliceP("labels", "l", []string{}, "Additional labels")
	promptCaptureCmd.Flags().StringSlice("label", []string{}, "Alias for --labels")
	_ = promptCaptureCmd.Flags().MarkHidden("label")
	promptCaptureCmd.Flags().Bool("silent", false, "Output only the issue ID")
	promptCmd.AddCommand(promptCaptureCmd)
	rootCmd.AddCommand(promptCmd)
}
