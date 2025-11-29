package main

import (
	"fmt"
	"os"
)

// FatalError writes an error message to stderr and exits with code 1.
// Use this for fatal errors that prevent the command from completing.
//
// Pattern A from ERROR_HANDLING.md:
// - User input validation failures
// - Critical preconditions not met
// - Unrecoverable system errors
//
// Example:
//
//	if err := store.CreateIssue(ctx, issue, actor); err != nil {
//	    FatalError("%v", err)
//	}
func FatalError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}

// FatalErrorWithHint writes an error message with a hint to stderr and exits.
// Use this when you can provide an actionable suggestion to fix the error.
//
// Example:
//
//	FatalErrorWithHint("database not found", "Run 'bd init' to create a database")
func FatalErrorWithHint(message, hint string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	fmt.Fprintf(os.Stderr, "Hint: %s\n", hint)
	os.Exit(1)
}

// WarnError writes a warning message to stderr and returns.
// Use this for optional operations that enhance functionality but aren't required.
//
// Pattern B from ERROR_HANDLING.md:
// - Metadata operations
// - Cleanup operations
// - Auxiliary features (git hooks, merge drivers)
//
// Example:
//
//	if err := createConfigYaml(beadsDir, false); err != nil {
//	    WarnError("failed to create config.yaml: %v", err)
//	}
func WarnError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Warning: "+format+"\n", args...)
}
