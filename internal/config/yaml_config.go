package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// YamlOnlyKeys are configuration keys that must be stored in config.yaml
// rather than the database. These are "startup" settings that are
// read before the database is opened.
//
// This fixes GH#536: users were confused when `bd config set no-db true`
// appeared to succeed but had no effect (because no-db is read from yaml
// at startup, not from the database).
var YamlOnlyKeys = map[string]bool{
	// Bootstrap flags (affect how bd starts)
	"no-db": true,
	"json":  true,

	// Database and identity
	"db":       true,
	"actor":    true,
	"identity": true,

	// Git settings
	"git.author":      true,
	"git.no-gpg-sign": true,
	"no-push":         true,
	"no-git-ops":      true, // Disable git ops in bd prime session close protocol (GH#593)

	// Sync settings
	"sync.git-remote":                          true,
	"sync.require_confirmation_on_mass_delete": true,

	// Routing settings
	"routing.mode":        true,
	"routing.default":     true,
	"routing.maintainer":  true,
	"routing.contributor": true,

	// Create command settings
	"create.require-description": true,

	// Validation settings (bd-t7jq)
	// Values: "warn" | "error" | "none"
	"validation.on-create": true,
	"validation.on-sync":   true,

	// Hierarchy settings (GH#995)
	"hierarchy.max-depth": true,

	// Backup settings (must be in yaml so GetValueSource can detect overrides)
	"backup.enabled":  true,
	"backup.interval": true,
	"backup.git-push": true,
	"backup.git-repo": true,

	// Dolt server settings
	"dolt.idle-timeout": true, // Idle auto-stop timeout (default "30m", "0" disables)
}

// IsYamlOnlyKey returns true if the given key should be stored in config.yaml
// rather than the Dolt database.
func IsYamlOnlyKey(key string) bool {
	// Check exact match
	if YamlOnlyKeys[key] {
		return true
	}

	// Check prefix matches for nested keys
	prefixes := []string{"routing.", "sync.", "git.", "directory.", "repos.", "external_projects.", "validation.", "hierarchy.", "ai.", "backup.", "dolt.", "federation.", "branch_strategy."}
	for _, prefix := range prefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}

	return false
}

// keyAliases maps alternative key names to their canonical yaml form.
// This ensures consistency when users use different formats (dot vs hyphen).
var keyAliases = map[string]string{}

// normalizeYamlKey converts a key to its canonical yaml format.
// Some keys have aliases (e.g., sync.branch -> sync-branch) to handle
// different input formats consistently.
func normalizeYamlKey(key string) string {
	if canonical, ok := keyAliases[key]; ok {
		return canonical
	}
	return key
}

// SetYamlConfig sets a configuration value in the project's config.yaml file.
// It handles both adding new keys and updating existing (possibly commented) keys.
// Keys are normalized to their canonical yaml format (e.g., sync.branch -> sync-branch).
func SetYamlConfig(key, value string) error {
	// Validate specific keys (GH#995)
	if err := validateYamlConfigValue(key, value); err != nil {
		return err
	}

	configPath, err := findProjectConfigYaml()
	if err != nil {
		return err
	}

	// Normalize key to canonical yaml format
	normalizedKey := normalizeYamlKey(key)

	// Read existing config
	content, err := os.ReadFile(configPath) //nolint:gosec // configPath is from findProjectConfigYaml
	if err != nil {
		return fmt.Errorf("failed to read config.yaml: %w", err)
	}

	// Update or add the key
	newContent, err := updateYamlKey(string(content), normalizedKey, value)
	if err != nil {
		return err
	}

	// Write back
	if err := os.WriteFile(configPath, []byte(newContent), 0600); err != nil { //nolint:gosec // configPath is validated
		return fmt.Errorf("failed to write config.yaml: %w", err)
	}

	return nil
}

// GetYamlConfig gets a configuration value from config.yaml.
// Returns empty string if key is not found or is commented out.
// Keys are normalized to their canonical yaml format (e.g., sync.branch -> sync-branch).
func GetYamlConfig(key string) string {
	if v == nil {
		return ""
	}
	normalizedKey := normalizeYamlKey(key)
	return v.GetString(normalizedKey)
}

// findProjectConfigYaml finds the project's .beads/config.yaml file.
// If .beads/ exists but config.yaml doesn't, creates a minimal config.yaml
// so that bd config set works on repos initialized before config.yaml existed.
func findProjectConfigYaml() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	// Walk up parent directories to find .beads/config.yaml
	for dir := cwd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		configPath := filepath.Join(dir, ".beads", "config.yaml")
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}
		// If .beads/ exists but config.yaml doesn't, create a bare one
		beadsDir := filepath.Join(dir, ".beads")
		if _, err := os.Stat(beadsDir); err == nil {
			if err := os.WriteFile(configPath, []byte("# Beads Configuration File\n"), 0600); err != nil {
				return "", fmt.Errorf("failed to create config.yaml: %w", err)
			}
			return configPath, nil
		}
	}

	return "", fmt.Errorf("no .beads/ directory found (run 'bd init' first)")
}

// updateYamlKey updates a key in yaml content, handling commented-out keys.
// If the key exists (commented or not), it updates it in place.
// If the key doesn't exist, it appends it at the end.
//
// For dotted keys (e.g., "branch_strategy.prompt"), this function produces
// proper nested YAML structure rather than flat dotted keys:
//
//	branch_strategy:
//	  prompt: true
//
// This ensures viper's GetStringMap works correctly for parent sections.
//
//nolint:unparam // error return kept for future validation
func updateYamlKey(content, key, value string) (string, error) {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) == 2 {
		return updateNestedYamlKey(content, parts[0], parts[1], value)
	}
	return updateFlatYamlKey(content, key, value)
}

// updateFlatYamlKey handles simple (non-dotted) keys like "no-db" or "json".
func updateFlatYamlKey(content, key, value string) (string, error) {
	formattedValue := formatYamlValue(value)
	newLine := fmt.Sprintf("%s: %s", key, formattedValue)

	// Build regex to match the key (commented or not)
	// Matches: "key: value" or "# key: value" with optional leading whitespace
	keyPattern := regexp.MustCompile(`^(\s*)(#\s*)?` + regexp.QuoteMeta(key) + `\s*:`)

	found := false
	var result []string

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if keyPattern.MatchString(line) {
			// Found the key - replace with new value (uncommented)
			// Preserve leading whitespace
			matches := keyPattern.FindStringSubmatch(line)
			indent := ""
			if len(matches) > 1 {
				indent = matches[1]
			}
			result = append(result, indent+newLine)
			found = true
		} else {
			result = append(result, line)
		}
	}

	if !found {
		// Key not found - append at end
		// Add blank line before if content doesn't end with one
		if len(result) > 0 && result[len(result)-1] != "" {
			result = append(result, "")
		}
		result = append(result, newLine)
	}

	return strings.Join(result, "\n"), nil
}

// updateNestedYamlKey handles dotted keys (e.g., "branch_strategy.prompt") by
// producing proper nested YAML. The childKey may itself contain dots for deeper
// nesting (e.g., "defaults.reset_dolt_with_git").
func updateNestedYamlKey(content, parentKey, childKey, value string) (string, error) {
	formattedValue := formatYamlValue(value)

	// Split childKey further for deeper nesting (e.g., "defaults.reset_dolt_with_git")
	childParts := strings.Split(childKey, ".")
	leafKey := childParts[len(childParts)-1]
	intermediateParts := childParts[:len(childParts)-1]

	lines := strings.Split(content, "\n")

	// First, check for and remove any flat dotted key (e.g., "branch_strategy.prompt: value")
	flatKey := parentKey + "." + childKey
	flatPattern := regexp.MustCompile(`^(\s*)(#\s*)?` + regexp.QuoteMeta(flatKey) + `\s*:`)
	var cleaned []string
	for _, line := range lines {
		if !flatPattern.MatchString(line) {
			cleaned = append(cleaned, line)
		}
	}
	lines = cleaned

	// Look for the parent section header (e.g., "branch_strategy:" or "# branch_strategy:")
	parentPattern := regexp.MustCompile(`^(\s*)(#\s*)?` + regexp.QuoteMeta(parentKey) + `\s*:\s*$`)

	parentIdx := -1
	parentCommented := false
	for i, line := range lines {
		if parentPattern.MatchString(line) {
			parentIdx = i
			matches := parentPattern.FindStringSubmatch(line)
			parentCommented = len(matches) > 2 && matches[2] != ""
			break
		}
	}

	if parentIdx == -1 {
		// No parent section — append nested structure at end
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		lines = append(lines, parentKey+":")
		indent := "  "
		for _, part := range intermediateParts {
			lines = append(lines, indent+part+":")
			indent += "  "
		}
		lines = append(lines, indent+leafKey+": "+formattedValue)
		return strings.Join(lines, "\n"), nil
	}

	// Parent section found — uncomment it if needed
	if parentCommented {
		matches := parentPattern.FindStringSubmatch(lines[parentIdx])
		indent := ""
		if len(matches) > 1 {
			indent = matches[1]
		}
		lines[parentIdx] = indent + parentKey + ":"
	}

	// Determine the indentation level for children under this parent
	parentIndent := ""
	if m := parentPattern.FindStringSubmatch(lines[parentIdx]); len(m) > 1 {
		parentIndent = m[1]
	}
	childIndent := parentIndent + "  "

	// Build the target indent for the leaf key (accounting for intermediate parts)
	targetIndent := childIndent
	for range intermediateParts {
		targetIndent += "  "
	}

	// Scan the children of this parent section to find the target key
	// Children are lines with indent > parentIndent, or commented lines at child indent
	childFound := false
	intermediateHandled := make([]bool, len(intermediateParts))

	for i := parentIdx + 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Skip empty lines within the section
		if trimmed == "" {
			continue
		}

		// Check if we've left the parent section (line at same or lesser indent, uncommented)
		lineIndent := len(line) - len(strings.TrimLeft(line, " \t"))
		commentedLine := strings.HasPrefix(trimmed, "#")
		if !commentedLine && lineIndent <= len(parentIndent) && trimmed != "" {
			break
		}

		// Check for intermediate parts (e.g., "defaults:" under "branch_strategy:")
		for j, part := range intermediateParts {
			interIndent := childIndent
			for k := 0; k < j; k++ {
				interIndent += "  "
			}
			interPattern := regexp.MustCompile(`^(\s*)(#\s*)?` + regexp.QuoteMeta(part) + `\s*:\s*$`)
			if interPattern.MatchString(line) {
				intermediateHandled[j] = true
				// Uncomment if needed
				if m := interPattern.FindStringSubmatch(line); len(m) > 2 && m[2] != "" {
					lines[i] = interIndent + part + ":"
				}
			}
		}

		// Check for the leaf key (commented or not, at any indent within this section)
		leafPattern := regexp.MustCompile(`^(\s*)(#\s*)?` + regexp.QuoteMeta(leafKey) + `\s*:`)
		if leafPattern.MatchString(line) {
			lines[i] = targetIndent + leafKey + ": " + formattedValue
			childFound = true
		}
	}

	if !childFound {
		// Insert the child key after the parent section header
		// Find the end of the parent section to insert before the next section
		insertIdx := parentIdx + 1
		for insertIdx < len(lines) {
			trimmed := strings.TrimSpace(lines[insertIdx])
			if trimmed == "" {
				insertIdx++
				continue
			}
			lineIndent := len(lines[insertIdx]) - len(strings.TrimLeft(lines[insertIdx], " \t"))
			commentedLine := strings.HasPrefix(trimmed, "#")
			if !commentedLine && lineIndent <= len(parentIndent) {
				break
			}
			insertIdx++
		}

		// Build lines to insert
		var newLines []string
		indent := childIndent
		for j, part := range intermediateParts {
			if !intermediateHandled[j] {
				newLines = append(newLines, indent+part+":")
			}
			indent += "  "
		}
		newLines = append(newLines, targetIndent+leafKey+": "+formattedValue)

		// Insert at position
		tail := make([]string, len(lines[insertIdx:]))
		copy(tail, lines[insertIdx:])
		lines = append(lines[:insertIdx], newLines...)
		lines = append(lines, tail...)
	}

	return strings.Join(lines, "\n"), nil
}

// formatYamlValue formats a value appropriately for YAML.
func formatYamlValue(value string) string {
	// Boolean values
	lower := strings.ToLower(value)
	if lower == "true" || lower == "false" {
		return lower
	}

	// Numeric values - return as-is
	if isNumeric(value) {
		return value
	}

	// Duration values (like "30s", "5m") - return as-is
	if isDuration(value) {
		return value
	}

	// For all other string-like values, quote to preserve YAML string semantics
	return fmt.Sprintf("%q", value)
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if c == '-' && i == 0 {
			continue
		}
		if c == '.' {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func isDuration(s string) bool {
	if len(s) < 2 {
		return false
	}
	suffix := s[len(s)-1]
	if suffix != 's' && suffix != 'm' && suffix != 'h' {
		return false
	}
	return isNumeric(s[:len(s)-1])
}

func needsQuoting(s string) bool {
	// Quote if contains special YAML characters
	special := []string{":", "#", "[", "]", "{", "}", ",", "&", "*", "!", "|", ">", "'", "\"", "%", "@", "`"}
	for _, c := range special {
		if strings.Contains(s, c) {
			return true
		}
	}
	// Quote if starts/ends with whitespace
	if strings.TrimSpace(s) != s {
		return true
	}
	return false
}

// validateYamlConfigValue validates a configuration value before setting.
// Returns an error if the value is invalid for the given key.
func validateYamlConfigValue(key, value string) error {
	switch key {
	case "hierarchy.max-depth":
		// Must be a positive integer >= 1 (GH#995)
		depth, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("hierarchy.max-depth must be a positive integer, got %q", value)
		}
		if depth < 1 {
			return fmt.Errorf("hierarchy.max-depth must be at least 1, got %d", depth)
		}
	case "dolt.idle-timeout":
		// "0" disables, otherwise must be a valid Go duration
		if value != "0" {
			if _, err := time.ParseDuration(value); err != nil {
				return fmt.Errorf("dolt.idle-timeout must be a duration (e.g. \"30m\", \"1h\") or \"0\" to disable, got %q", value)
			}
		}
	}
	return nil
}
