package fix

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/configfile"
)

var repoFingerprintReadLine = readLineUnbuffered
var repoFingerprintGetBdBinary = getBdBinary
var repoFingerprintNewBdCmd = newBdCmd

// readLineUnbuffered reads a line from stdin without buffering.
// This ensures subprocess stdin isn't consumed by our buffered reader.
func readLineUnbuffered() (string, error) {
	var result []byte
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return string(result), err
		}
		if n == 1 {
			c := buf[0] // #nosec G602 -- n==1 guarantees buf has 1 byte
			if c == '\n' {
				return string(result), nil
			}
			result = append(result, c)
		}
	}
}

func runUpdateRepoID(path, bdBinary string, autoYes bool) error {
	args := []string{"migrate", "--update-repo-id"}
	if autoYes {
		args = append(args, "--yes")
		fmt.Println("  → Auto mode (--yes): running 'bd migrate --update-repo-id --yes'...")
	} else {
		fmt.Println("  → Running 'bd migrate --update-repo-id'...")
	}

	cmd := repoFingerprintNewBdCmd(bdBinary, args...)
	cmd.Dir = path
	if !autoYes {
		// Allow interactive confirmation prompt when running without --yes.
		cmd.Stdin = os.Stdin
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to update repo ID: %w", err)
	}
	return nil
}

// RepoFingerprint fixes repo fingerprint mismatches by prompting the user
// for which action to take. This is interactive because the consequences
// differ significantly between options:
//  1. Update repo ID (if URL changed or bd upgraded)
//  2. Reinitialize database (if wrong database was copied)
//  3. Skip (do nothing)
func RepoFingerprint(path string, autoYes bool) error {
	// Validate workspace
	if err := validateBeadsWorkspace(path); err != nil {
		return err
	}

	// Get bd binary path
	bdBinary, err := repoFingerprintGetBdBinary()
	if err != nil {
		return err
	}

	// In --yes mode, auto-select the recommended safe action [1].
	if autoYes {
		return runUpdateRepoID(path, bdBinary, true)
	}

	// Prompt user for action
	fmt.Println("\n  Repo fingerprint mismatch detected. Choose an action:")
	fmt.Println()
	fmt.Println("    [1] Update repo ID (if git remote URL changed or bd was upgraded)")
	fmt.Println("    [2] Reinitialize database (if wrong .beads was copied here)")
	fmt.Println("    [s] Skip (do nothing)")
	fmt.Println()
	fmt.Print("  Choice [1/2/s]: ")

	// Read single character without buffering to avoid consuming input meant for subprocesses
	response, err := repoFingerprintReadLine()
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	response = strings.TrimSpace(strings.ToLower(response))

	switch response {
	case "1":
		return runUpdateRepoID(path, bdBinary, false)

	case "2":
		// Detect backend to determine what to remove
		beadsDir := filepath.Join(path, ".beads")
		cfg, cfgErr := configfile.Load(beadsDir)
		if cfgErr != nil || cfg == nil {
			cfg = configfile.DefaultConfig()
		}
		dbPath := cfg.DatabasePath(beadsDir)
		isDolt := cfg.GetBackend() == configfile.BackendDolt

		// Confirm before destructive action
		fmt.Printf("  ⚠️  This will DELETE %s. Continue? [y/N]: ", dbPath)
		confirm, err := repoFingerprintReadLine()
		if err != nil {
			return fmt.Errorf("failed to read confirmation: %w", err)
		}
		confirm = strings.TrimSpace(strings.ToLower(confirm))
		if confirm != "y" && confirm != "yes" {
			fmt.Println("  → Skipped (canceled)")
			return nil
		}

		// Remove database and reinitialize
		fmt.Printf("  → Removing %s...\n", dbPath)
		if isDolt {
			// Dolt uses a directory
			if err := os.RemoveAll(dbPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove Dolt database: %w", err)
			}
		} else {
			// SQLite uses a file
			if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove database: %w", err)
			}
			// Also remove WAL and SHM files if they exist
			_ = os.Remove(dbPath + "-wal")
			_ = os.Remove(dbPath + "-shm")
		}

		fmt.Println("  → Running 'bd init'...")
		cmd := repoFingerprintNewBdCmd(bdBinary, "init", "--quiet")
		cmd.Dir = path
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to initialize database: %w", err)
		}
		return nil

	case "s", "":
		fmt.Println("  → Skipped")
		return nil

	default:
		fmt.Printf("  → Unrecognized input '%s', skipping\n", response)
		return nil
	}
}
