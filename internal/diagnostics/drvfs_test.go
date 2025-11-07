package diagnostics

import (
	"runtime"
	"testing"
)

func TestIsDrvFSPathNonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("linux DrvFS detection requires WSL environment")
	}
	mounted, err := IsDrvFSPath("C:/Users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mounted {
		t.Fatalf("expected non-DrvFS path on %s", runtime.GOOS)
	}
}
