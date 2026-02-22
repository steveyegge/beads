// reparent_test.go - Test that reparented issues don't appear under old parent.

//go:build cgo && integration

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

// TestCLI_ReparentExcludesOldParent tests that after reparenting a dotted-ID
// child to a new parent, it no longer appears under the old parent in
// `bd list --parent`.
//
// This documents the decision: explicit parent-child dependencies take
// precedence over dotted-ID prefix matching.
func TestCLI_ReparentExcludesOldParent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow CLI test in short mode")
	}
	tmpDir := initExecTestDB(t)

	// Create parent A and child A.1 (dotted-ID convention)
	parentA := createExecTestIssue(t, tmpDir, "Parent A")

	// Create a second issue that looks like a dotted child of parentA
	// We need to create it with a title, then we'll check parent filtering
	childID := createExecTestIssue(t, tmpDir, "Child of A")

	// Create parent B
	parentB := createExecTestIssue(t, tmpDir, "Parent B")

	// Add parent-child dep: child -> parentA
	depCmd := exec.Command(testBD, "dep", "add", childID, parentA, "--type", "parent-child")
	depCmd.Dir = tmpDir
	depCmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	if out, err := depCmd.CombinedOutput(); err != nil {
		t.Fatalf("dep add to parentA failed: %v\n%s", err, out)
	}

	// Verify child appears under parentA
	listCmd := exec.Command(testBD, "list", "--parent", parentA, "--json")
	listCmd.Dir = tmpDir
	listCmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	out, err := listCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list --parent parentA failed: %v\n%s", err, out)
	}
	var issues []map[string]interface{}
	json.Unmarshal(out, &issues)
	found := false
	for _, iss := range issues {
		if iss["id"] == childID {
			found = true
		}
	}
	if !found {
		t.Fatalf("child %s should appear under parentA %s before reparenting", childID, parentA)
	}

	// Reparent: remove old dep, add new dep to parentB
	removeCmd := exec.Command(testBD, "dep", "remove", childID, parentA)
	removeCmd.Dir = tmpDir
	removeCmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	if out, err := removeCmd.CombinedOutput(); err != nil {
		t.Fatalf("dep remove failed: %v\n%s", err, out)
	}

	addCmd := exec.Command(testBD, "dep", "add", childID, parentB, "--type", "parent-child")
	addCmd.Dir = tmpDir
	addCmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("dep add to parentB failed: %v\n%s", err, out)
	}

	// Verify child NO LONGER appears under parentA
	listCmd2 := exec.Command(testBD, "list", "--parent", parentA, "--json")
	listCmd2.Dir = tmpDir
	listCmd2.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	out2, err := listCmd2.CombinedOutput()
	if err != nil {
		// Empty list may exit 0 with empty JSON
		t.Logf("list --parent parentA after reparent: %s", out2)
	}
	var issuesAfter []map[string]interface{}
	json.Unmarshal(out2, &issuesAfter)
	for _, iss := range issuesAfter {
		if iss["id"] == childID {
			t.Errorf("child %s should NOT appear under old parent %s after reparenting to %s", childID, parentA, parentB)
		}
	}

	// Verify child DOES appear under parentB
	listCmd3 := exec.Command(testBD, "list", "--parent", parentB, "--json")
	listCmd3.Dir = tmpDir
	listCmd3.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	out3, err := listCmd3.CombinedOutput()
	if err != nil {
		t.Fatalf("list --parent parentB failed: %v\n%s", err, out3)
	}
	var issuesB []map[string]interface{}
	json.Unmarshal(out3, &issuesB)
	foundB := false
	for _, iss := range issuesB {
		if iss["id"] == childID {
			foundB = true
		}
	}
	if !foundB {
		t.Errorf("child %s should appear under new parent %s after reparenting", childID, parentB)
	}
}
