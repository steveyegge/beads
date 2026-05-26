package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/timeparsing"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/validation"
)

// createInput is the shared, parsed form of every `bd create` flag.
// Both the embedded and proxied-server paths consume this struct so flag
// parsing, conflict checks, and value coercion live in one place.
type createInput struct {
	// Mode selectors. Exactly one is set; if neither, single-issue mode.
	markdownFile string
	graphFile    string

	// Single-issue identity / shape.
	title       string
	explicitID  string
	parentID    string
	issueType   string
	priority    int
	assignee    string
	externalRef string
	specID      string

	// Body fields. Description has already had --skills / --context appended.
	description        string
	design             string
	acceptanceCriteria string
	notes              string
	appendNotes        string

	// Labels & dependencies.
	labels          []string // --labels and --label aliases merged
	noInheritLabels bool
	deps            []string
	waitsFor        string
	waitsForGate    string

	// Behavior flags.
	silent   bool
	dryRun   bool
	force    bool
	validate bool

	// Lifecycle.
	ephemeral bool
	noHistory bool
	molType   types.MolType
	wispType  types.WispType

	// Event (only valid when issueType == "event").
	eventCategory string
	eventActor    string
	eventTarget   string
	eventPayload  string

	// Scheduling.
	dueAt      *time.Time
	deferUntil *time.Time

	// Metadata.
	metadata    json.RawMessage
	metadataSet bool

	estimatedMinutes *int

	// Routing.
	repoOverride    string
	repoOverrideSet bool

	// Identity (resolved from env/git/global state at gather time).
	actor     string
	createdBy string
	owner     string

	// Cross-cutting (persistent flags).
	jsonOutput bool

	// LintIssue dispatch hint. Embedded/proxied paths run validation
	// themselves; this records the user's intent.
	// Values: "" (off) | "error" | "warn".
	validationMode string
}

// gatherCreateInput pulls every flag value out of cobra, parses each into
// its strongly-typed form, and applies the validation that does not require
// database access. Callers downstream (runCreateEmbedded, runCreateProxied*)
// receive a fully-validated input or this function FatalError's.
func gatherCreateInput(cmd *cobra.Command, args []string) createInput {
	in := createInput{}

	in.markdownFile, _ = cmd.Flags().GetString("file")
	in.graphFile, _ = cmd.Flags().GetString("graph")
	in.dryRun, _ = cmd.Flags().GetBool("dry-run")

	// Mode dispatch validation.
	if in.markdownFile != "" && in.graphFile != "" {
		FatalError("cannot specify both --file and --graph")
	}
	if in.markdownFile != "" {
		if len(args) > 0 {
			FatalError("cannot specify both title and --file flag")
		}
		if in.dryRun {
			FatalError("--dry-run is not supported with --file flag")
		}
		rejectSingleIssueFlagsForMarkdown(cmd)
	}
	if in.graphFile != "" {
		if len(args) > 0 {
			FatalError("cannot specify both title and --graph flag")
		}
		rejectSingleIssueFlagsForGraph(cmd)
	}

	in.silent, _ = cmd.Flags().GetBool("silent")
	in.force, _ = cmd.Flags().GetBool("force")
	in.validate, _ = cmd.Flags().GetBool("validate")
	in.noInheritLabels, _ = cmd.Flags().GetBool("no-inherit-labels")
	in.ephemeral, _ = cmd.Flags().GetBool("ephemeral")
	in.noHistory, _ = cmd.Flags().GetBool("no-history")

	if in.ephemeral && in.noHistory {
		FatalError("--ephemeral and --no-history are mutually exclusive")
	}

	titleFlag, _ := cmd.Flags().GetString("title")
	in.title = resolveTitle(args, titleFlag, in.markdownFile, in.graphFile)

	// Body fields. Description is composed from getDescriptionFlag plus
	// --skills / --context concatenation; design via getDesignFlag.
	in.description, _ = getDescriptionFlag(cmd)
	skills, _ := cmd.Flags().GetString("skills")
	if skills != "" {
		if in.description != "" {
			in.description += "\n\n"
		}
		in.description += "## Required Skills\n" + skills
	}
	ctxStr, _ := cmd.Flags().GetString("context")
	if ctxStr != "" {
		if in.description != "" {
			in.description += "\n\n"
		}
		in.description += "## Context\n" + ctxStr
	}

	in.design, _ = getDesignFlag(cmd)
	in.acceptanceCriteria, _ = cmd.Flags().GetString("acceptance")
	in.notes, _ = cmd.Flags().GetString("notes")
	in.appendNotes, _ = cmd.Flags().GetString("append-notes")
	in.specID, _ = cmd.Flags().GetString("spec-id")

	// Description-required check only fires in single-issue mode. Markdown
	// templates supply their own descriptions; graph nodes likewise.
	if in.markdownFile == "" && in.graphFile == "" {
		if in.description == "" && !isTestIssue(in.title) {
			if config.GetBool("create.require-description") {
				FatalError("description is required (set create.require-description: false in config.yaml to disable)")
			}
		}
	}

	// Priority — supports both "1" and "P1" formats.
	priorityStr, _ := cmd.Flags().GetString("priority")
	priority, err := validation.ValidatePriority(priorityStr)
	if err != nil {
		FatalError("%v", err)
	}
	in.priority = priority

	in.issueType, _ = cmd.Flags().GetString("type")
	in.assignee, _ = cmd.Flags().GetString("assignee")
	in.externalRef, _ = cmd.Flags().GetString("external-ref")
	in.explicitID, _ = cmd.Flags().GetString("id")
	in.parentID, _ = cmd.Flags().GetString("parent")
	in.waitsFor, _ = cmd.Flags().GetString("waits-for")
	in.waitsForGate, _ = cmd.Flags().GetString("waits-for-gate")

	if in.explicitID != "" && in.parentID != "" {
		FatalError("cannot specify both --id and --parent flags")
	}

	in.labels, _ = cmd.Flags().GetStringSlice("labels")
	labelAlias, _ := cmd.Flags().GetStringSlice("label")
	if len(labelAlias) > 0 {
		in.labels = append(in.labels, labelAlias...)
	}
	in.deps, _ = cmd.Flags().GetStringSlice("deps")

	in.repoOverride, _ = cmd.Flags().GetString("repo")
	in.repoOverrideSet = cmd.Flags().Changed("repo")

	// MOL / wisp type validation.
	if molTypeStr, _ := cmd.Flags().GetString("mol-type"); molTypeStr != "" {
		mt := types.MolType(molTypeStr)
		if !mt.IsValid() {
			FatalError("invalid mol-type %q (must be swarm, patrol, or work)", molTypeStr)
		}
		in.molType = mt
	}
	if wispTypeStr, _ := cmd.Flags().GetString("wisp-type"); wispTypeStr != "" {
		wt := types.WispType(wispTypeStr)
		if !wt.IsValid() {
			FatalError("invalid wisp-type %q (must be heartbeat, ping, patrol, gc_report, recovery, error, or escalation)", wispTypeStr)
		}
		in.wispType = wt
	}

	// Event flags require --type=event.
	in.eventCategory, _ = cmd.Flags().GetString("event-category")
	in.eventActor, _ = cmd.Flags().GetString("event-actor")
	in.eventTarget, _ = cmd.Flags().GetString("event-target")
	in.eventPayload, _ = cmd.Flags().GetString("event-payload")
	if (in.eventCategory != "" || in.eventActor != "" || in.eventTarget != "" || in.eventPayload != "") && in.issueType != "event" {
		FatalError("--event-category, --event-actor, --event-target, and --event-payload flags require --type=event")
	}

	// --due
	if dueStr, _ := cmd.Flags().GetString("due"); dueStr != "" {
		t, err := timeparsing.ParseRelativeTime(dueStr, time.Now())
		if err != nil {
			FatalError("invalid --due format %q. Examples: +6h, tomorrow, next monday, 2025-01-15", dueStr)
		}
		in.dueAt = &t
	}

	// --defer (warns on past dates).
	if deferStr, _ := cmd.Flags().GetString("defer"); deferStr != "" {
		t, err := timeparsing.ParseRelativeTime(deferStr, time.Now())
		if err != nil {
			FatalError("invalid --defer format %q. Examples: +1h, tomorrow, next monday, 2025-01-15", deferStr)
		}
		if t.Before(time.Now()) && !in.silent && !debug.IsQuiet() {
			fmt.Fprintf(os.Stderr, "%s Defer date %q is in the past. Issue will appear in bd ready immediately.\n",
				ui.RenderWarn("!"), t.Format("2006-01-02 15:04"))
			fmt.Fprintf(os.Stderr, "  Did you mean a future date? Use --defer=+1h or --defer=tomorrow\n")
		}
		in.deferUntil = &t
	}

	// --metadata (inline JSON or @file.json).
	if cmd.Flags().Changed("metadata") {
		metadataValue, _ := cmd.Flags().GetString("metadata")
		var metadataJSON string
		if strings.HasPrefix(metadataValue, "@") {
			filePath := metadataValue[1:]
			// #nosec G304 -- user explicitly provides file path via @file.json syntax
			data, err := os.ReadFile(filePath)
			if err != nil {
				FatalError("failed to read metadata file %s: %v", filePath, err)
			}
			metadataJSON = string(data)
		} else {
			metadataJSON = metadataValue
		}
		if !json.Valid([]byte(metadataJSON)) {
			FatalError("invalid JSON in --metadata: must be valid JSON")
		}
		in.metadata = json.RawMessage(metadataJSON)
		in.metadataSet = true
	}

	// --estimate
	if cmd.Flags().Changed("estimate") {
		est, _ := cmd.Flags().GetInt("estimate")
		if est < 0 {
			FatalError("estimate must be a non-negative number of minutes")
		}
		in.estimatedMinutes = &est
	}

	// Identity helpers.
	in.actor = actor
	in.createdBy = getActorWithGit()
	in.owner = getOwner()

	// Output mode.
	in.jsonOutput = jsonOutput

	// Validation mode hint. --validate forces error; otherwise use the
	// validation.on-create config key.
	in.validationMode = config.GetString("validation.on-create")
	if in.validate {
		in.validationMode = "error"
	}

	return in
}

// singleIssueOnlyFlags lists the flag names that only make sense for
// single-issue mode. Both markdown templates (--file) and graph plans
// (--graph) supply per-issue fields; passing any of these alongside the
// batch flag is meaningless and likely a user error.
var singleIssueOnlyFlags = []string{
	"title",
	"id", "parent", "no-inherit-labels",
	"deps", "waits-for", "waits-for-gate",
	"type", "priority", "assignee", "external-ref", "spec-id",
	"description", "body", "message", "body-file", "description-file", "stdin",
	"design", "design-file", "acceptance", "notes", "append-notes",
	"labels", "label", "skills", "context",
	"event-category", "event-actor", "event-target", "event-payload",
	"due", "defer",
	"metadata", "estimate", "force", "wisp-type",
}

// rejectSingleIssueFlagsForMarkdown FatalErrors when any single-issue-only
// flag has been explicitly set alongside --file. Per the proxied-server
// create spec, markdown mode supplies every per-issue field via templates;
// passing CLI fields for these is silently ignored on the embedded path
// today and is treated as an error here for clarity. --mol-type is allowed
// because markdown mode propagates it to every template.
func rejectSingleIssueFlagsForMarkdown(cmd *cobra.Command) {
	for _, name := range singleIssueOnlyFlags {
		if cmd.Flags().Changed(name) {
			FatalError("--%s is not valid with --file (markdown templates supply per-issue fields)", name)
		}
	}
}

// rejectSingleIssueFlagsForGraph FatalErrors when any single-issue-only
// flag has been explicitly set alongside --graph. Per the spec, graph mode
// honors only --dry-run, --ephemeral, --no-history, --silent, --json, and
// --repo; --mol-type is also rejected here (graph plans don't carry
// molecule semantics today).
func rejectSingleIssueFlagsForGraph(cmd *cobra.Command) {
	for _, name := range singleIssueOnlyFlags {
		if cmd.Flags().Changed(name) {
			FatalError("--%s is not valid with --graph (graph plans supply per-node fields)", name)
		}
	}
	if cmd.Flags().Changed("mol-type") {
		FatalError("--mol-type is not valid with --graph (graph plans don't carry molecule semantics)")
	}
}

// resolveTitle implements the single-issue title resolution rules. Returns
// "" in markdown/graph mode (no positional title allowed there).
func resolveTitle(args []string, titleFlag, markdownFile, graphFile string) string {
	if markdownFile != "" || graphFile != "" {
		return ""
	}

	switch {
	case len(args) > 0 && titleFlag != "":
		if args[0] != titleFlag {
			FatalError("cannot specify different titles as both positional argument and --title flag\n  Positional: %q\n  --title:    %q", args[0], titleFlag)
		}
		return args[0]
	case len(args) > 0:
		// Guard: reject positional args that look like flags (bd-2c0).
		// When --help or other flags bypass Cobra's flag parsing (e.g.,
		// programmatic invocation), they end up here as positional args.
		if strings.HasPrefix(args[0], "-") {
			FatalError("title %q looks like a flag (starts with '-').\n  Run 'bd create --help' for available options.\n  To use this title anyway, pass it explicitly: bd create --title=%q", args[0], args[0])
		}
		return args[0]
	case titleFlag != "":
		return titleFlag
	default:
		FatalError("title required (or use --file to create from markdown)")
		return "" // unreachable
	}
}
