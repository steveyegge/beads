//go:build windows

package rpc

import (
	"os"
	"syscall"
)

var serverSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
