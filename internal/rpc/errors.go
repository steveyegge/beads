package rpc

import "errors"

// ErrDaemonUnavailable indicates that the Beads daemon could not be reached.
var ErrDaemonUnavailable = errors.New("daemon unavailable")
