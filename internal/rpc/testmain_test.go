package rpc

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// Dolt background goroutines that persist after store.Close().
		// These are internal to Dolt's embedded engine and not controllable
		// from the application layer.
		goleak.IgnoreTopFunction("github.com/dolthub/dolt/go/libraries/doltcore/doltdb.(*BranchActivityTracker).processEvents"),
		goleak.IgnoreTopFunction("github.com/dolthub/dolt/go/libraries/doltcore/sqle/binlogreplication.newBinlogStreamerManager.func1"),
		goleak.IgnoreTopFunction("github.com/dolthub/dolt/go/libraries/doltcore/sqle.RunAsyncReplicationThreads.func2"),
		goleak.IgnoreTopFunction("database/sql.(*Rows).awaitDone"),
		// Dolt event collector and telemetry goroutines
		goleak.IgnoreTopFunction("github.com/dolthub/dolt/go/libraries/events.(*sendingThread).run"),
		goleak.IgnoreTopFunction("github.com/dolthub/dolt/go/libraries/events.NewCollector.func1"),
		// Dolt async index lookup worker goroutines
		goleak.IgnoreTopFunction("github.com/dolthub/dolt/go/libraries/doltcore/sqle/index.(*asyncLookups).start.func1.(*asyncLookups).workerFunc.1"),
	)
}
