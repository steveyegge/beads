package main

import (
	"encoding/json"
	"os"
)

// outputJSON outputs data as pretty-printed JSON to stdout.
func outputJSON(v interface{}) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		FatalError("encoding JSON: %v", err)
	}
}

// outputJSONError outputs an error as JSON to stderr and exits with code 1.
func outputJSONError(err error, code string) {
	errObj := map[string]string{"error": err.Error()}
	if code != "" {
		errObj["code"] = code
	}
	encoder := json.NewEncoder(os.Stderr)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(errObj) // Best effort: if JSON encoding fails, error is already printed to stderr
	os.Exit(1)
}
