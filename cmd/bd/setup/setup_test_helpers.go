package setup

import "testing"

type exitCapture struct {
	called bool
	code   int
}

func stubSetupExit(t *testing.T) *exitCapture {
	t.Helper()
	cap := &exitCapture{}
	orig := setupExit
	setupExit = func(code int) {
		cap.called = true
		cap.code = code
	}
	t.Cleanup(func() {
		setupExit = orig
	})
	return cap
}
