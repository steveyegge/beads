// dialog-test sends test dialog requests to verify the dialog-client is working.
// Run this on EC2 after dialog-client is connected from your laptop.
//
// Usage:
//   dialog-test entry "What's your name?"
//   dialog-test choice "Pick one" "a:Option A" "b:Option B" "c:Option C"
//   dialog-test confirm "Are you sure?"
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/steveyegge/beads/internal/dialog"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  dialog-test entry \"prompt\"")
		fmt.Fprintln(os.Stderr, "  dialog-test choice \"prompt\" \"id:label\" ...")
		fmt.Fprintln(os.Stderr, "  dialog-test confirm \"prompt\"")
		os.Exit(1)
	}

	dialogType := os.Args[1]
	prompt := os.Args[2]

	client := dialog.NewClient("")
	if err := client.Connect(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect: %v\n", err)
		fmt.Fprintln(os.Stderr, "Make sure dialog-client is running on your laptop with SSH tunnel active")
		os.Exit(1)
	}
	defer client.Close()

	fmt.Println("Connected to dialog client")

	switch dialogType {
	case "entry":
		text, cancelled, err := client.ShowEntry("test-1", "Test Dialog", prompt, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if cancelled {
			fmt.Println("User cancelled")
		} else {
			fmt.Printf("User entered: %q\n", text)
		}

	case "choice":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "choice requires at least one option (id:label)")
			os.Exit(1)
		}
		var options []dialog.Option
		for _, arg := range os.Args[3:] {
			parts := strings.SplitN(arg, ":", 2)
			if len(parts) != 2 {
				fmt.Fprintf(os.Stderr, "Invalid option format %q, expected id:label\n", arg)
				os.Exit(1)
			}
			options = append(options, dialog.Option{ID: parts[0], Label: parts[1]})
		}
		selected, cancelled, err := client.ShowChoice("test-1", "Test Dialog", prompt, options)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if cancelled {
			fmt.Println("User cancelled")
		} else {
			fmt.Printf("User selected: %q\n", selected)
		}

	case "confirm":
		yes, cancelled, err := client.ShowConfirm("test-1", "Test Dialog", prompt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if cancelled {
			fmt.Println("User cancelled")
		} else if yes {
			fmt.Println("User confirmed: Yes")
		} else {
			fmt.Println("User confirmed: No")
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown dialog type: %s\n", dialogType)
		os.Exit(1)
	}
}
