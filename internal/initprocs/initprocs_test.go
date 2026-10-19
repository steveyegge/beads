package initprocs

import (
	"runtime"
	"testing"
)

func TestDoneRestoresGOMAXPROCS(t *testing.T) {
	want := restore
	if want == 0 {
		t.Skipf("GOMAXPROCS was already at most %d; init left it alone", initProcs)
	}
	if got := runtime.GOMAXPROCS(0); got != initProcs {
		t.Fatalf("GOMAXPROCS before Done = %d, want the init cap %d", got, initProcs)
	}
	Done()
	if got := runtime.GOMAXPROCS(0); got != want {
		t.Fatalf("GOMAXPROCS after Done = %d, want %d", got, want)
	}
	Done()
	if got := runtime.GOMAXPROCS(0); got != want {
		t.Fatalf("a second Done changed GOMAXPROCS to %d", got)
	}
}
