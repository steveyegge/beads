package api

import (
	"errors"

	"github.com/steveyegge/beads/internal/rpc"
)

func isDaemonUnavailable(err error) bool {
	return errors.Is(err, rpc.ErrDaemonUnavailable)
}
