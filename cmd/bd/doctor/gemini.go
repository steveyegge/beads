package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// CheckGemini returns Gemini CLI integration verification as a DoctorCheck
func CheckGemini() DoctorCheck {
	hasHooks := hasGeminiHooks()

	if hasHooks {
		return DoctorCheck{
			Name:    "Gemini CLI Integration",
			Status:  StatusOK,
			Message: "Hooks installed",
			Detail:  "SessionStart and PreCompress hooks enabled",
		}
	}

	return DoctorCheck{
		Name:    "Gemini CLI Integration",
		Status:  StatusOK, // Not a warning - Gemini is optional
		Message: "Not configured",
		Detail:  "Run 'bd setup gemini' to enable Gemini CLI integration",
	}
}

// hasGeminiHooks checks if Gemini CLI hooks are installed
func hasGeminiHooks() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	globalSettings := filepath.Join(home, ".gemini", "settings.json")
	projectSettings := ".gemini/settings.json"

	return hasGeminiBeadsHooks(globalSettings) || hasGeminiBeadsHooks(projectSettings)
}

// hasGeminiBeadsHooks checks if a settings file has bd prime hooks for Gemini CLI
func hasGeminiBeadsHooks(settingsPath string) bool {
	data, err := os.ReadFile(settingsPath) // #nosec G304 -- settingsPath is constructed from known safe locations
	if err != nil {
		return false
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return false
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return false
	}

	// Check SessionStart and PreCompress for "bd prime"
	for _, event := range []string{"SessionStart", "PreCompress"} {
		eventHooks, ok := hooks[event].([]interface{})
		if !ok {
			continue
		}

		for _, hook := range eventHooks {
			hookMap, ok := hook.(map[string]interface{})
			if !ok {
				continue
			}
			commands, ok := hookMap["hooks"].([]interface{})
			if !ok {
				continue
			}
			for _, cmd := range commands {
				cmdMap, ok := cmd.(map[string]interface{})
				if !ok {
					continue
				}
				cmdStr := cmdMap["command"]
				if cmdStr == "bd prime" || cmdStr == "bd prime --stealth" {
					return true
				}
			}
		}
	}

	return false
}
