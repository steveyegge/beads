package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/httpclient/wire"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/issueops"
)

// sqlStateError is a driver error carrying a SQLSTATE, the shape pgconn.PgError
// and every other driver error uow.IsSerializationError reads.
type sqlStateError struct{ state string }

func (e *sqlStateError) Error() string {
	return "ERROR: could not serialize access (SQLSTATE " + e.state + ")"
}
func (e *sqlStateError) SQLState() string { return e.state }

// contendedGateRoles fails the first len(errs) updates with errs, then
// behaves as fakeGateRoles. A failed update writes nothing unless
// applyBeforeErr is set: then it lands first and is answered with the error
// anyway, as a gateway's 503 can answer a write the server committed.
type contendedGateRoles struct {
	*fakeGateRoles
	errs           []error
	applyBeforeErr bool
}

func (c *contendedGateRoles) Update(ctx context.Context, req issueops.UpdateRequest) (issueops.UpdateResult, error) {
	if len(c.errs) > 0 {
		err := c.errs[0]
		c.errs = c.errs[1:]
		if c.applyBeforeErr {
			if _, applyErr := c.fakeGateRoles.Update(ctx, req); applyErr != nil {
				return issueops.UpdateResult{}, applyErr
			}
			return issueops.UpdateResult{}, err
		}
		c.updates = append(c.updates, req)
		return issueops.UpdateResult{}, err
	}
	return c.fakeGateRoles.Update(ctx, req)
}

// TestAddGateWaiterRetriesTransientContention: contention the store gave up
// replaying is one more lost round, not a failure. On Postgres the role
// envelope replays a serialization failure (40001) or deadlock (40P01) a
// bounded number of times and then returns it; over HTTP bd serve answers
// that spent budget, or a refused admission, as 503 busy (wire.ErrBusy). The
// loop retries them under the same backoff and attempt bound as a version
// miss. What makes that safe is the round itself (re-read, deduplicate,
// version-guarded whole-list write), which the busy-after-commit case pins: a
// 503 is not proof that nothing was written.
func TestAddGateWaiterRetriesTransientContention(t *testing.T) {
	setGateTestActor(t)
	origSleep := gateAddWaiterSleep
	t.Cleanup(func() { gateAddWaiterSleep = origSleep })
	var sleeps []time.Duration
	gateAddWaiterSleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}
	gate := func() *types.Issue {
		return &types.Issue{ID: "bd-g", IssueType: "gate", Waiters: []string{"a"}, RowVersion: 7}
	}

	for _, tt := range []struct {
		name string
		err  error
	}{
		{"serialization failure", fmt.Errorf("update: %w", &sqlStateError{state: "40001"})},
		{"deadlock", fmt.Errorf("update: %w", &sqlStateError{state: "40P01"})},
		{"bd serve busy", fmt.Errorf("updateIssue: %w", wire.ErrBusy)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sleeps = nil
			roles := &contendedGateRoles{fakeGateRoles: newFakeGateRoles(gate()), errs: []error{tt.err, tt.err}}
			added, err := addGateWaiter(context.Background(), roles, roles, "bd-g", "b")
			if err != nil || !added {
				t.Fatalf("addGateWaiter = %v, %v; want added after two contended rounds", added, err)
			}
			if len(roles.updates) != 3 || len(sleeps) != 2 {
				t.Fatalf("updates = %d, sleeps = %d; want 3 writes with a backoff before each retry", len(roles.updates), len(sleeps))
			}
			if got := roles.issues["bd-g"].Waiters; !reflect.DeepEqual(got, []string{"a", "b"}) {
				t.Errorf("waiters = %v, want [a b]", got)
			}
		})
	}

	t.Run("bounded like a version miss", func(t *testing.T) {
		roles := &contendedGateRoles{fakeGateRoles: newFakeGateRoles(gate())}
		for i := 0; i < gateAddWaiterMaxAttempts+1; i++ {
			roles.errs = append(roles.errs, wire.ErrBusy)
		}
		_, err := addGateWaiter(context.Background(), roles, roles, "bd-g", "b")
		if !errors.Is(err, wire.ErrBusy) {
			t.Fatalf("addGateWaiter error = %v, want the last busy answer", err)
		}
		if len(roles.updates) != gateAddWaiterMaxAttempts {
			t.Fatalf("updates = %d, want %d attempts", len(roles.updates), gateAddWaiterMaxAttempts)
		}
	})

	t.Run("a busy answer to a write that landed adds the waiter once", func(t *testing.T) {
		roles := &contendedGateRoles{fakeGateRoles: newFakeGateRoles(gate()), errs: []error{wire.ErrBusy}, applyBeforeErr: true}
		added, err := addGateWaiter(context.Background(), roles, roles, "bd-g", "b")
		if err != nil {
			t.Fatalf("addGateWaiter error = %v, want the retry's re-read to find the landed waiter", err)
		}
		if added {
			t.Errorf("addGateWaiter reported a second add; the re-read must find b already registered")
		}
		if len(roles.updates) != 1 {
			t.Errorf("updates = %d, want only the write that landed", len(roles.updates))
		}
		if got := roles.issues["bd-g"].Waiters; !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Errorf("waiters = %v, want [a b] with b exactly once", got)
		}
	})

	for _, tt := range []struct {
		name string
		err  error
	}{
		{"a server fault (500, an indeterminate commit)", fmt.Errorf("updateIssue: %w", wire.ErrServerFault)},
		{"an unreachable database", fmt.Errorf("updateIssue: %w", wire.ErrDBUnavailable)},
	} {
		t.Run(tt.name+" is not retried", func(t *testing.T) {
			roles := &contendedGateRoles{fakeGateRoles: newFakeGateRoles(gate()), errs: []error{tt.err}}
			_, err := addGateWaiter(context.Background(), roles, roles, "bd-g", "b")
			if !errors.Is(err, tt.err) || len(roles.updates) != 1 {
				t.Fatalf("addGateWaiter = %v after %d updates, want %v after one write", err, len(roles.updates), tt.err)
			}
		})
	}

	t.Run("a busy answer's Retry-After raises the wait, capped", func(t *testing.T) {
		busy := func(after time.Duration) error {
			return fmt.Errorf("updateIssue: %w", &wire.ProblemError{Status: 503, Code: "busy", RetryAfter: after, Err: wire.ErrBusy})
		}
		sleeps = nil
		roles := &contendedGateRoles{fakeGateRoles: newFakeGateRoles(gate()), errs: []error{busy(time.Second), busy(time.Minute), wire.ErrBusy}}
		added, err := addGateWaiter(context.Background(), roles, roles, "bd-g", "b")
		if err != nil || !added {
			t.Fatalf("addGateWaiter = %v, %v; want added after three busy rounds", added, err)
		}
		if len(sleeps) != 3 {
			t.Fatalf("sleeps = %v, want one per retry", sleeps)
		}
		if sleeps[0] != time.Second {
			t.Errorf("wait after Retry-After 1s = %s, want 1s", sleeps[0])
		}
		if sleeps[1] != gateAddWaiterRetryAfterCap {
			t.Errorf("wait after Retry-After 1m = %s, want the %s cap", sleeps[1], gateAddWaiterRetryAfterCap)
		}
		if sleeps[2] >= gateAddWaiterBackoffCap {
			t.Errorf("wait after a busy answer with no Retry-After = %s, want the plain backoff", sleeps[2])
		}
	})

	t.Run("another SQLSTATE is not retried", func(t *testing.T) {
		roles := &contendedGateRoles{fakeGateRoles: newFakeGateRoles(gate()), errs: []error{&sqlStateError{state: "23505"}}}
		_, err := addGateWaiter(context.Background(), roles, roles, "bd-g", "b")
		if err == nil || len(roles.updates) != 1 {
			t.Fatalf("addGateWaiter = %v after %d updates, want one failed write", err, len(roles.updates))
		}
	})
}
