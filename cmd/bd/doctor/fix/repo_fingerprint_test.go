package fix

import (
	"os/exec"
	"reflect"
	"testing"
)

func TestRepoFingerprint_AutoYesSkipsPromptAndPassesYesToMigrate(t *testing.T) {
	dir := setupTestWorkspace(t)

	oldGetBinary := repoFingerprintGetBdBinary
	oldReadLine := repoFingerprintReadLine
	oldNewCmd := repoFingerprintNewBdCmd
	defer func() {
		repoFingerprintGetBdBinary = oldGetBinary
		repoFingerprintReadLine = oldReadLine
		repoFingerprintNewBdCmd = oldNewCmd
	}()

	var gotArgs []string
	readCalled := false

	repoFingerprintGetBdBinary = func() (string, error) { return "bd", nil }
	repoFingerprintReadLine = func() (string, error) {
		readCalled = true
		return "", nil
	}
	repoFingerprintNewBdCmd = func(_ string, args ...string) *exec.Cmd {
		gotArgs = append([]string{}, args...)
		return exec.Command("go", "version")
	}

	if err := RepoFingerprint(dir, true); err != nil {
		t.Fatalf("RepoFingerprint(autoYes=true) returned error: %v", err)
	}

	if readCalled {
		t.Fatal("expected autoYes path to skip interactive stdin read")
	}

	wantArgs := []string{"migrate", "--update-repo-id", "--yes"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected command args: got %v, want %v", gotArgs, wantArgs)
	}
}

func TestRepoFingerprint_ChoiceOneRunsUpdateRepoIDWithoutYes(t *testing.T) {
	dir := setupTestWorkspace(t)

	oldGetBinary := repoFingerprintGetBdBinary
	oldReadLine := repoFingerprintReadLine
	oldNewCmd := repoFingerprintNewBdCmd
	defer func() {
		repoFingerprintGetBdBinary = oldGetBinary
		repoFingerprintReadLine = oldReadLine
		repoFingerprintNewBdCmd = oldNewCmd
	}()

	var gotArgs []string
	repoFingerprintGetBdBinary = func() (string, error) { return "bd", nil }
	repoFingerprintReadLine = func() (string, error) { return "1", nil }
	repoFingerprintNewBdCmd = func(_ string, args ...string) *exec.Cmd {
		gotArgs = append([]string{}, args...)
		return exec.Command("go", "version")
	}

	if err := RepoFingerprint(dir, false); err != nil {
		t.Fatalf("RepoFingerprint(autoYes=false, choice=1) returned error: %v", err)
	}

	wantArgs := []string{"migrate", "--update-repo-id"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected command args: got %v, want %v", gotArgs, wantArgs)
	}
}
