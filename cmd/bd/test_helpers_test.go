package main

import (
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/testutil/teststore"
)

func newTestStore(t *testing.T, _ string) storage.Storage {
	t.Helper()
	return teststore.New(t)
}
