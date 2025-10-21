//go:build !windows

package rpc

import (
	"os"
	"syscall"
)

var serverSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}
