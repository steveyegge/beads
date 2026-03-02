package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
)

func TestCheckBrokenMigrationState_NoBeadsDir(t *testing.T) {
	check := CheckBrokenMigrationState(t.TempDir())
	if check.Status != StatusOK {
		t.Errorf("expected OK for missing .beads dir, got %s: %s", check.Status, check.Message)
	}
}

func TestCheckBrokenMigrationState_SQLiteBackend(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &configfile.Config{Backend: configfile.BackendSQLite, Database: "beads.db"}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}

	check := CheckBrokenMigrationState(dir)
	if check.Status != StatusOK {
		t.Errorf("expected OK for sqlite backend, got %s: %s", check.Status, check.Message)
	}
}

func TestCheckBrokenMigrationState_DoltWithDir(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(filepath.Join(beadsDir, "dolt"), 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &configfile.Config{Backend: configfile.BackendDolt, Database: "dolt"}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}

	check := CheckBrokenMigrationState(dir)
	if check.Status != StatusOK {
		t.Errorf("expected OK when dolt dir exists, got %s: %s", check.Status, check.Message)
	}
}

func TestCheckBrokenMigrationState_DoltServerMode(t *testing.T) {
	// In server mode, no local dolt/ directory is expected. This should NOT
	// be reported as a broken migration state.
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &configfile.Config{
		Backend:        configfile.BackendDolt,
		Database:       "dolt",
		DoltMode:       configfile.DoltModeServer,
		DoltServerHost: "127.0.0.1",
		DoltServerPort: 3307,
	}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}

	check := CheckBrokenMigrationState(dir)
	if check.Status != StatusOK {
		t.Errorf("expected OK for server mode (no local dolt/ dir expected), got %s: %s", check.Status, check.Message)
	}
}

func TestCheckBrokenMigrationState_DoltWithoutDir(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Backend says dolt but no dolt/ directory
	cfg := &configfile.Config{Backend: configfile.BackendDolt, Database: "dolt"}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}

	check := CheckBrokenMigrationState(dir)
	if check.Status != StatusError {
		t.Errorf("expected ERROR when dolt dir missing, got %s: %s", check.Status, check.Message)
	}
	if check.Fix == "" {
		t.Error("expected a fix suggestion")
	}
}

func TestCheckBrokenMigrationState_DoltWithoutDirButSQLiteExists(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &configfile.Config{Backend: configfile.BackendDolt, Database: "dolt"}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}

	// Create a SQLite DB file (recoverable case)
	if err := os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte("sqlite-data"), 0600); err != nil {
		t.Fatal(err)
	}

	check := CheckBrokenMigrationState(dir)
	if check.Status != StatusError {
		t.Errorf("expected ERROR, got %s", check.Status)
	}
	if check.Detail == "" || check.Fix == "" {
		t.Error("expected detail and fix for recoverable broken state")
	}
}

func TestCheckBrokenMigrationState_BackupFilePresent(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &configfile.Config{Backend: configfile.BackendDolt, Database: "dolt"}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}

	// No beads.db, no dolt/, but a backup file exists
	if err := os.WriteFile(filepath.Join(beadsDir, "beads.backup-pre-dolt-20260226-100000.db"), []byte("backup-data"), 0600); err != nil {
		t.Fatal(err)
	}

	check := CheckBrokenMigrationState(dir)
	if check.Status != StatusError {
		t.Errorf("expected ERROR, got %s: %s", check.Status, check.Message)
	}
	if !strings.Contains(check.Detail, "backup") {
		t.Errorf("detail should mention backup, got: %s", check.Detail)
	}
}

func TestCheckBrokenMigrationState_OnlyMigratedFileExists(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &configfile.Config{Backend: configfile.BackendDolt, Database: "dolt"}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}

	// Only beads.db.migrated exists (no beads.db, no dolt/)
	if err := os.WriteFile(filepath.Join(beadsDir, "beads.db.migrated"), []byte("old-data"), 0600); err != nil {
		t.Fatal(err)
	}

	check := CheckBrokenMigrationState(dir)
	if check.Status != StatusError {
		t.Errorf("expected ERROR when only .migrated exists, got %s: %s", check.Status, check.Message)
	}
	// Detail should mention SQLite data exists
	if !strings.Contains(check.Detail, "SQLite") {
		t.Errorf("detail should mention SQLite, got: %s", check.Detail)
	}
}

// --- CheckEmbeddedModeConcurrency tests ---

func TestCheckEmbeddedModeConcurrency_NoConfig(t *testing.T) {
	check := CheckEmbeddedModeConcurrency(t.TempDir())
	if check.Status != StatusOK {
		t.Errorf("expected OK for missing config, got %s: %s", check.Status, check.Message)
	}
}

func TestCheckEmbeddedModeConcurrency_SQLiteBackend(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := &configfile.Config{Backend: configfile.BackendSQLite}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}

	check := CheckEmbeddedModeConcurrency(dir)
	if check.Status != StatusOK {
		t.Errorf("expected OK for sqlite backend, got %s", check.Status)
	}
}

func TestCheckEmbeddedModeConcurrency_ServerMode(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := &configfile.Config{
		Backend:  configfile.BackendDolt,
		DoltMode: configfile.DoltModeServer,
	}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}

	check := CheckEmbeddedModeConcurrency(dir)
	if check.Status != StatusOK {
		t.Errorf("expected OK for server mode, got %s: %s", check.Status, check.Message)
	}
	if !strings.Contains(check.Message, "server mode") {
		t.Errorf("message should mention server mode, got: %s", check.Message)
	}
}

func TestCheckEmbeddedModeConcurrency_EmbeddedNoLocks(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := &configfile.Config{Backend: configfile.BackendDolt}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}

	check := CheckEmbeddedModeConcurrency(dir)
	if check.Status != StatusOK {
		t.Errorf("expected OK for embedded mode with no locks, got %s", check.Status)
	}
}

func TestCheckEmbeddedModeConcurrency_EmbeddedWithAccessLock(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := &configfile.Config{Backend: configfile.BackendDolt}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}

	// Create dolt-access.lock (sign of concurrent embedded access)
	if err := os.WriteFile(filepath.Join(beadsDir, "dolt-access.lock"), []byte("locked"), 0600); err != nil {
		t.Fatal(err)
	}

	check := CheckEmbeddedModeConcurrency(dir)
	if check.Status != StatusWarning {
		t.Errorf("expected WARNING for embedded mode with access lock, got %s: %s", check.Status, check.Message)
	}
	if !strings.Contains(check.Fix, "bd dolt start") {
		t.Errorf("fix should recommend starting Dolt server, got: %s", check.Fix)
	}
}

func TestCheckEmbeddedModeConcurrency_EmbeddedWithNomsLock(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	// Create dolt database dir with noms LOCK
	nomsDir := filepath.Join(beadsDir, "dolt", "mydb", ".dolt", "noms")
	if err := os.MkdirAll(nomsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nomsDir, "LOCK"), []byte("locked"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := &configfile.Config{Backend: configfile.BackendDolt}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}

	check := CheckEmbeddedModeConcurrency(dir)
	if check.Status != StatusWarning {
		t.Errorf("expected WARNING for embedded mode with noms LOCK, got %s: %s", check.Status, check.Message)
	}
	if !strings.Contains(check.Detail, "noms LOCK") {
		t.Errorf("detail should mention noms LOCK, got: %s", check.Detail)
	}
}

// --- CheckSQLiteResidue tests ---

func TestCheckSQLiteResidue_NoBeadsDir(t *testing.T) {
	check := CheckSQLiteResidue(t.TempDir())
	if check.Status != StatusOK {
		t.Errorf("expected OK for missing .beads dir, got %s: %s", check.Status, check.Message)
	}
}

func TestCheckSQLiteResidue_SQLiteBackend(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := &configfile.Config{Backend: configfile.BackendSQLite, Database: "beads.db"}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}
	// Create beads.db — should NOT be reported as residue when backend=sqlite
	if err := os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte("real-data"), 0600); err != nil {
		t.Fatal(err)
	}
	check := CheckSQLiteResidue(dir)
	if check.Status != StatusOK {
		t.Errorf("expected OK for sqlite backend (beads.db is not residue), got %s: %s", check.Status, check.Message)
	}
}

func TestCheckSQLiteResidue_ZeroByteFile(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := &configfile.Config{Backend: configfile.BackendDolt, Database: "dolt"}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}
	// Zero-byte beads.db should NOT be reported as residue
	if err := os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	check := CheckSQLiteResidue(dir)
	if check.Status != StatusOK {
		t.Errorf("expected OK for zero-byte beads.db, got %s: %s", check.Status, check.Message)
	}
}

func TestCheckSQLiteResidue_NoResidue(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &configfile.Config{Backend: configfile.BackendDolt, Database: "dolt"}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}

	check := CheckSQLiteResidue(dir)
	if check.Status != StatusOK {
		t.Errorf("expected OK when no beads.db, got %s: %s", check.Status, check.Message)
	}
}

func TestCheckSQLiteResidue_HasResidue(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &configfile.Config{Backend: configfile.BackendDolt, Database: "dolt"}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}

	// Create leftover beads.db
	if err := os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte("leftover-data"), 0600); err != nil {
		t.Fatal(err)
	}

	check := CheckSQLiteResidue(dir)
	if check.Status != StatusWarning {
		t.Errorf("expected WARNING for leftover beads.db, got %s: %s", check.Status, check.Message)
	}
}
