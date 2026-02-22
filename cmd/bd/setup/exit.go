package setup

import (
	"fmt"
	"os"
)

// setupExit is used by setup commands to exit the process. Tests can stub this.
var setupExit = os.Exit

// FatalError writes an error message to stderr and exits with code 1.
// This mirrors the main package's FatalError but uses the testable setupExit.
func FatalError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	setupExit(1)
}

// FatalErrorWithHint writes an error message with a hint to stderr and exits.
// This mirrors the main package's FatalErrorWithHint but uses the testable setupExit.
func FatalErrorWithHint(message, hint string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	fmt.Fprintf(os.Stderr, "Hint: %s\n", hint)
	setupExit(1)
}
