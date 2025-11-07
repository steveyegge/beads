//go:build !integration

package main

import (
    "os/exec"
    "testing"
)

// initTestGitRepo initializes a git repository in the provided directory with
// basic user configuration so CLI/UI tests can run git commands safely. The
// integration test suite defines its own version behind the integration build
// tag, so this helper is only included for the default (non-integration) test
// runs.
func initTestGitRepo(t testing.TB, dir string) {
    t.Helper()

    cmd := exec.Command("git", "init")
    cmd.Dir = dir
    if err := cmd.Run(); err != nil {
        t.Fatalf("failed to init git repo: %v", err)
    }

    configCmds := [][]string{
        {"git", "config", "user.email", "test@example.com"},
        {"git", "config", "user.name", "Test User"},
    }
    for _, args := range configCmds {
        cmd := exec.Command(args[0], args[1:]...)
        cmd.Dir = dir
        if err := cmd.Run(); err != nil {
            t.Logf("warning: git config failed: %v", err)
        }
    }
}
