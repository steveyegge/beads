package main

import (
	"fmt"
	"os"
	"time"
)

func checkPauseSignal() {
	pause, err := readPacmanPause()
	if err != nil || pause == nil {
		return
	}
	ageStr := ""
	if pause.TS != "" {
		if ts, err := time.Parse(time.RFC3339, pause.TS); err == nil {
			age := time.Since(ts).Round(time.Minute)
			ageStr = fmt.Sprintf(" (%s ago)", age)
		}
	}
	reason := pause.Reason
	if reason == "" {
		reason = "(no reason provided)"
	}
	fmt.Fprintf(os.Stderr, "\nPAUSED by %s%s: %s\n", pause.From, ageStr, reason)
	fmt.Fprintf(os.Stderr, "Run: bd pacman --resume\n\n")
}
