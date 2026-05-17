package schema

import (
	"errors"
	"strings"
	"testing"
)

func TestMigrationLockNameUsesRawNameWhenBounded(t *testing.T) {
	got := MigrationLockName("testdb_short")
	want := migrationLockPrefix + "testdb_short"
	if got != want {
		t.Fatalf("MigrationLockName() = %q, want %q", got, want)
	}
}

func TestMigrationLockNameHashesLongNames(t *testing.T) {
	dbName := strings.Repeat("a", 64)
	got := MigrationLockName(dbName)
	if len(got) > migrationLockNameMaxLength {
		t.Fatalf("MigrationLockName() length = %d, want <= %d", len(got), migrationLockNameMaxLength)
	}
	if got == migrationLockPrefix+dbName {
		t.Fatalf("MigrationLockName() used over-limit raw name %q", got)
	}
	if got != MigrationLockName(dbName) {
		t.Fatal("MigrationLockName() is not deterministic")
	}
}

func TestIsMigrationLockError(t *testing.T) {
	err := errors.Join(ErrMigrationLockUnavailable, errors.New("timeout"))
	if !IsMigrationLockError(err) {
		t.Fatal("IsMigrationLockError() = false, want true")
	}
}
