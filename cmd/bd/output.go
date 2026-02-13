package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	toon "github.com/toon-format/toon-go"
)

// resolveOutputFormat checks BD_OUTPUT_FORMAT env var.
// Returns "toon" or "json" (default).
func resolveOutputFormat() string {
	if env := os.Getenv("BD_OUTPUT_FORMAT"); env != "" {
		if strings.EqualFold(env, "toon") {
			return "toon"
		}
	}
	return "json"
}

// outputJSON outputs data as pretty-printed JSON or TOON to stdout.
// Format is determined by BD_OUTPUT_FORMAT env var (default: json).
func outputJSON(v interface{}) {
	if resolveOutputFormat() == "toon" {
		// Round-trip through JSON to handle custom types (e.g. types.Status)
		// that implement json.Marshaler but not toon struct tags.
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠ toon: json marshal failed, falling back to JSON: %v\n", err)
			outputJSONRaw(v)
			return
		}
		var generic interface{}
		if err := json.Unmarshal(jsonBytes, &generic); err != nil {
			fmt.Fprintf(os.Stderr, "⚠ toon: json unmarshal failed, falling back to JSON: %v\n", err)
			outputJSONRaw(v)
			return
		}
		data, err := toon.Marshal(generic)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠ toon encoding failed, falling back to JSON: %v\n", err)
			outputJSONRaw(v)
			return
		}
		fmt.Fprintln(os.Stdout, string(data))
		return
	}
	outputJSONRaw(v)
}

// outputJSONRaw always outputs as JSON regardless of format setting.
func outputJSONRaw(v interface{}) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
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
	_ = encoder.Encode(errObj)
	os.Exit(1)
}
