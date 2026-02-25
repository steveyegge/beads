package main

import (
	"fmt"
	"strings"
)

type doltAutoPushMode string

const (
	doltAutoPushOff doltAutoPushMode = "off"
	doltAutoPushOn  doltAutoPushMode = "on"
)

func getDoltAutoPushMode() (doltAutoPushMode, error) {
	mode := strings.TrimSpace(strings.ToLower(doltAutoPush))
	if mode == "" {
		mode = string(doltAutoPushOff)
	}
	switch doltAutoPushMode(mode) {
	case doltAutoPushOff:
		return doltAutoPushOff, nil
	case doltAutoPushOn:
		return doltAutoPushOn, nil
	default:
		return "", fmt.Errorf("invalid --dolt-auto-push=%q (valid: off, on)", doltAutoPush)
	}
}
