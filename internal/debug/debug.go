package debug

import (
	"fmt"
	"os"
)

var (
	enabled     = os.Getenv("BD_DEBUG") != ""
	verboseMode = false
	quietMode   = false
	jsonMode    = false
)

func Enabled() bool {
	return enabled || verboseMode
}

// SetVerbose enables verbose/debug output
func SetVerbose(verbose bool) {
	verboseMode = verbose
}

// SetQuiet enables quiet mode (suppress non-essential output)
func SetQuiet(quiet bool) {
	quietMode = quiet
}

// IsQuiet returns true if quiet mode is enabled
func IsQuiet() bool {
	return quietMode
}

// SetJSON marks that the command's output is machine-readable (--json or
// --format json). Callers outside the normal output path (e.g. an advisory
// note on a fallback) use IsJSON to stay out of a machine-readable stream.
func SetJSON(json bool) {
	jsonMode = json
}

// IsJSON returns true if JSON output mode is enabled.
func IsJSON() bool {
	return jsonMode
}

func Logf(format string, args ...interface{}) {
	if enabled || verboseMode {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}

func Printf(format string, args ...interface{}) {
	if enabled || verboseMode {
		fmt.Printf(format, args...)
	}
}

// PrintNormal prints output unless quiet mode is enabled.
// Use this for normal informational output that should be suppressed in quiet mode.
func PrintNormal(format string, args ...interface{}) {
	if !quietMode {
		fmt.Printf(format, args...)
	}
}

// PrintlnNormal prints a line unless quiet mode is enabled.
func PrintlnNormal(args ...interface{}) {
	if !quietMode {
		fmt.Println(args...)
	}
}
