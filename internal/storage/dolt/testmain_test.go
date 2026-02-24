package dolt

import (
	"fmt"
	"os"
	"testing"

	"github.com/steveyegge/beads/internal/testutil"
)

// testServerPort is the port of the shared test Dolt server (0 = not running).
// Set by TestMain before tests run, used implicitly via BEADS_DOLT_PORT env var
// which applyConfigDefaults reads when ServerPort is 0.
var testServerPort int

func TestMain(m *testing.M) {
	os.Exit(testMainInner(m))
}

func testMainInner(m *testing.M) int {
	srv, cleanup := testutil.StartTestDoltServer("dolt-pkg-test-*")
	defer cleanup()

	if srv != nil {
		testServerPort = srv.Port
		os.Setenv("BEADS_DOLT_PORT", fmt.Sprintf("%d", srv.Port))
		os.Setenv("BEADS_TEST_MODE", "1")
	}

	code := m.Run()

	testServerPort = 0
	os.Unsetenv("BEADS_DOLT_PORT")
	os.Unsetenv("BEADS_TEST_MODE")
	return code
}
