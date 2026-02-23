package config

// ConfigKeyDef describes a single configuration key with its metadata.
// Used by "bd config schema" and "bd config describe" for agent discoverability.
type ConfigKeyDef struct {
	Key         string   `json:"key"`
	Description string   `json:"description"`
	Type        string   `json:"type"`             // "string", "int", "bool"
	Default     string   `json:"default"`          // default value (empty string if none)
	Values      []string `json:"values,omitempty"` // valid values for enum-style keys
	Namespace   string   `json:"namespace"`        // grouping: "core", "linear", "jira", etc.
	StoredIn    string   `json:"stored_in"`        // "database", "config.yaml", "git config"
}

// Schema is the canonical list of all known configuration keys.
// Agents can discover available keys by running "bd config schema --json".
var Schema = []ConfigKeyDef{
	// ---- Core ----------------------------------------------------------------
	{
		Key:         "issue_prefix",
		Description: "Short identifier prefix for all issues in this repository (e.g. \"bd\" produces IDs like bd-a3f2). Set automatically by bd init.",
		Type:        "string",
		Default:     "",
		Namespace:   "core",
		StoredIn:    "database",
	},
	{
		Key:         "issue_id_mode",
		Description: "Controls how issue IDs are generated. \"hash\" uses content-based short hashes (e.g. bd-a3f2). \"counter\" uses sequential integers (e.g. bd-1, bd-2).",
		Type:        "string",
		Default:     "hash",
		Values:      []string{"hash", "counter"},
		Namespace:   "core",
		StoredIn:    "database",
	},
	{
		Key:         "allowed_prefixes",
		Description: "Comma-separated list of issue prefixes allowed in this repository. Empty means any prefix is accepted.",
		Type:        "string",
		Default:     "",
		Namespace:   "core",
		StoredIn:    "database",
	},
	{
		Key:         "status.custom",
		Description: "Comma-separated list of custom issue status values. Supplements the built-in statuses: open, in_progress, blocked, deferred, closed.",
		Type:        "string",
		Default:     "",
		Namespace:   "core",
		StoredIn:    "database",
	},
	{
		Key:         "types.custom",
		Description: "Comma-separated list of custom issue type values. Supplements the built-in types (bug, feature, task, etc.).",
		Type:        "string",
		Default:     "",
		Namespace:   "core",
		StoredIn:    "database",
	},
	// ---- Team ----------------------------------------------------------------
	{
		Key:         "team.enabled",
		Description: "Enables team workflow features such as sync branches. Set to \"true\" by bd init --team.",
		Type:        "bool",
		Default:     "false",
		Namespace:   "team",
		StoredIn:    "database",
	},
	{
		Key:         "team.sync_branch",
		Description: "Git branch used for team synchronisation of issue data.",
		Type:        "string",
		Default:     "",
		Namespace:   "team",
		StoredIn:    "database",
	},
	// ---- Sync ----------------------------------------------------------------
	{
		Key:         "sync.mode",
		Description: "Synchronisation mode. Only \"dolt-native\" is currently supported.",
		Type:        "string",
		Default:     "dolt-native",
		Values:      []string{"dolt-native"},
		Namespace:   "sync",
		StoredIn:    "database",
	},
	{
		Key:         "sync.branch",
		Description: "Git branch name used for syncing issue data between maintainer and contributor repositories.",
		Type:        "string",
		Default:     "",
		Namespace:   "sync",
		StoredIn:    "database",
	},
	{
		Key:         "sync.remote",
		Description: "Git remote name to push/pull sync data to/from (e.g. \"upstream\").",
		Type:        "string",
		Default:     "",
		Namespace:   "sync",
		StoredIn:    "database",
	},
	// ---- Routing -------------------------------------------------------------
	{
		Key:         "routing.mode",
		Description: "Controls where issues are stored. \"auto\" routes contributor issues to a separate planning repo.",
		Type:        "string",
		Default:     "",
		Values:      []string{"auto", "maintainer", "contributor", "explicit"},
		Namespace:   "routing",
		StoredIn:    "database",
	},
	{
		Key:         "routing.contributor",
		Description: "Path to the contributor planning repository used when routing.mode is \"auto\" or \"contributor\".",
		Type:        "string",
		Default:     "~/.beads-planning",
		Namespace:   "routing",
		StoredIn:    "database",
	},
	// ---- Mail ----------------------------------------------------------------
	{
		Key:         "mail.delegate",
		Description: "Issue ID to use as a mail delegate. When set, mail commands route through that issue.",
		Type:        "string",
		Default:     "",
		Namespace:   "mail",
		StoredIn:    "database",
	},
	// ---- Jira ----------------------------------------------------------------
	{
		Key:         "jira.url",
		Description: "Base URL of the Jira instance (e.g. \"https://company.atlassian.net\").",
		Type:        "string",
		Default:     "",
		Namespace:   "jira",
		StoredIn:    "database",
	},
	{
		Key:         "jira.project",
		Description: "Jira project key to sync with (e.g. \"PROJ\").",
		Type:        "string",
		Default:     "",
		Namespace:   "jira",
		StoredIn:    "database",
	},
	{
		Key:         "jira.api_token",
		Description: "Jira API token for authentication. Treat as a secret.",
		Type:        "string",
		Default:     "",
		Namespace:   "jira",
		StoredIn:    "database",
	},
	{
		Key:         "jira.push_prefix",
		Description: "Issue prefix to use when pushing issues to Jira.",
		Type:        "string",
		Default:     "",
		Namespace:   "jira",
		StoredIn:    "database",
	},
	{
		Key:         "jira.last_sync",
		Description: "Timestamp of the last successful Jira sync (managed automatically).",
		Type:        "string",
		Default:     "",
		Namespace:   "jira",
		StoredIn:    "database",
	},
	// ---- Linear --------------------------------------------------------------
	{
		Key:         "linear.api_key",
		Description: "Linear API key for authentication. Treat as a secret.",
		Type:        "string",
		Default:     "",
		Namespace:   "linear",
		StoredIn:    "database",
	},
	{
		Key:         "linear.team_id",
		Description: "Linear team UUID to sync with (e.g. \"12345678-1234-1234-1234-123456789abc\").",
		Type:        "string",
		Default:     "",
		Namespace:   "linear",
		StoredIn:    "database",
	},
	{
		Key:         "linear.project_id",
		Description: "Linear project ID to filter issues to a specific project (optional).",
		Type:        "string",
		Default:     "",
		Namespace:   "linear",
		StoredIn:    "database",
	},
	{
		Key:         "linear.api_endpoint",
		Description: "Custom Linear API endpoint URL (optional; defaults to the public Linear API).",
		Type:        "string",
		Default:     "",
		Namespace:   "linear",
		StoredIn:    "database",
	},
	{
		Key:         "linear.push_prefix",
		Description: "Issue prefix to use when pushing issues to Linear.",
		Type:        "string",
		Default:     "",
		Namespace:   "linear",
		StoredIn:    "database",
	},
	{
		Key:         "linear.last_sync",
		Description: "Timestamp of the last successful Linear sync (managed automatically).",
		Type:        "string",
		Default:     "",
		Namespace:   "linear",
		StoredIn:    "database",
	},
	{
		Key:         "linear.id_mode",
		Description: "Controls how Linear issue IDs map to beads IDs.",
		Type:        "string",
		Default:     "",
		Namespace:   "linear",
		StoredIn:    "database",
	},
	{
		Key:         "linear.hash_length",
		Description: "Length of the hash portion of Linear-derived issue IDs.",
		Type:        "int",
		Default:     "",
		Namespace:   "linear",
		StoredIn:    "database",
	},
	// ---- GitLab --------------------------------------------------------------
	{
		Key:         "gitlab.url",
		Description: "Base URL of the GitLab instance (e.g. \"https://gitlab.com\"). Must use HTTPS.",
		Type:        "string",
		Default:     "",
		Namespace:   "gitlab",
		StoredIn:    "database",
	},
	{
		Key:         "gitlab.token",
		Description: "GitLab personal access token. Treat as a secret.",
		Type:        "string",
		Default:     "",
		Namespace:   "gitlab",
		StoredIn:    "database",
	},
	{
		Key:         "gitlab.project_id",
		Description: "GitLab numeric project ID to sync with.",
		Type:        "string",
		Default:     "",
		Namespace:   "gitlab",
		StoredIn:    "database",
	},
	// ---- YAML-only keys (stored in .beads/config.yaml) ----------------------
	{
		Key:         "no-db",
		Description: "Run without a database (no-op for most commands; useful for quick queries from JSONL).",
		Type:        "bool",
		Default:     "false",
		Namespace:   "core",
		StoredIn:    "config.yaml",
	},
	{
		Key:         "dolt.auto-commit",
		Description: "Whether bd automatically creates a Dolt commit after write commands. Values: on, off.",
		Type:        "string",
		Default:     "on",
		Values:      []string{"on", "off"},
		Namespace:   "core",
		StoredIn:    "config.yaml",
	},
	{
		Key:         "conflict.strategy",
		Description: "Default conflict resolution strategy when merging Dolt branches. \"newest\" keeps the last-written value.",
		Type:        "string",
		Default:     "newest",
		Values:      []string{"newest", "ours", "theirs", "manual"},
		Namespace:   "sync",
		StoredIn:    "config.yaml",
	},
	{
		Key:         "federation.remote",
		Description: "Dolt remote URL for federation sync (e.g. \"dolthub://org/repo\", \"gs://bucket/path\", \"s3://bucket/path\").",
		Type:        "string",
		Default:     "",
		Namespace:   "sync",
		StoredIn:    "config.yaml",
	},
	{
		Key:         "federation.sovereignty",
		Description: "Data sovereignty tier restricting where issue data may be stored. T1=public, T2=org, T3=pseudonymous, T4=anonymous.",
		Type:        "string",
		Default:     "",
		Values:      []string{"T1", "T2", "T3", "T4"},
		Namespace:   "sync",
		StoredIn:    "config.yaml",
	},
	{
		Key:         "create.require-description",
		Description: "When true, bd create requires a non-empty description before saving an issue.",
		Type:        "bool",
		Default:     "false",
		Namespace:   "core",
		StoredIn:    "config.yaml",
	},
	{
		Key:         "validation.on-create",
		Description: "Validation behaviour when creating issues. \"none\" skips, \"warn\" prints warnings, \"error\" aborts on missing required sections.",
		Type:        "string",
		Default:     "none",
		Values:      []string{"none", "warn", "error"},
		Namespace:   "core",
		StoredIn:    "config.yaml",
	},
	{
		Key:         "validation.on-sync",
		Description: "Validation behaviour during sync. \"none\" skips, \"warn\" prints warnings, \"error\" aborts on invalid issues.",
		Type:        "string",
		Default:     "none",
		Values:      []string{"none", "warn", "error"},
		Namespace:   "core",
		StoredIn:    "config.yaml",
	},
	{
		Key:         "hierarchy.max-depth",
		Description: "Maximum nesting depth for hierarchical issue IDs (e.g. bd-abc.1.2.3). Must be a positive integer >= 1.",
		Type:        "int",
		Default:     "3",
		Namespace:   "core",
		StoredIn:    "config.yaml",
	},
	{
		Key:         "git.author",
		Description: "Override the git commit author for beads-generated commits (format: \"Name <email>\").",
		Type:        "string",
		Default:     "",
		Namespace:   "core",
		StoredIn:    "config.yaml",
	},
	{
		Key:         "git.no-gpg-sign",
		Description: "Disable GPG signing for beads-generated git commits.",
		Type:        "bool",
		Default:     "false",
		Namespace:   "core",
		StoredIn:    "config.yaml",
	},
	{
		Key:         "ai.model",
		Description: "AI model identifier used by bd AI features.",
		Type:        "string",
		Default:     "claude-haiku-4-5-20251001",
		Namespace:   "core",
		StoredIn:    "config.yaml",
	},
	{
		Key:         "sync.require_confirmation_on_mass_delete",
		Description: "When true, prompt for confirmation before a sync operation that would delete many issues at once.",
		Type:        "bool",
		Default:     "false",
		Namespace:   "sync",
		StoredIn:    "config.yaml",
	},
	// ---- Git config (per-user, not per-repo) ---------------------------------
	{
		Key:         "beads.role",
		Description: "Role of this git user: \"maintainer\" owns the canonical issue DB; \"contributor\" works in a fork or planning repo. Stored in local git config, not shared.",
		Type:        "string",
		Default:     "maintainer",
		Values:      []string{"maintainer", "contributor"},
		Namespace:   "core",
		StoredIn:    "git config",
	},
}

// SchemaByKey returns a map from key name to ConfigKeyDef for O(1) lookup.
func SchemaByKey() map[string]ConfigKeyDef {
	m := make(map[string]ConfigKeyDef, len(Schema))
	for _, def := range Schema {
		m[def.Key] = def
	}
	return m
}
