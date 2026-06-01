//go:build unix

package main

import "path/filepath"

func sockPath(beadsDir string) string { return filepath.Join(beadsDir, "bdd.sock") }
