package rpc

import (
	"errors"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
)

// TestDecodeRPCError_AllSentinels verifies that each known sentinel kind
// round-trips through decodeRPCError so errors.Is returns true for the
// correct sentinel. This guards the daemon RPC boundary: an error that crosses
// the boundary must remain matchable by callers using errors.Is.
func TestDecodeRPCError_AllSentinels(t *testing.T) {
	cases := []struct {
		kind     string
		sentinel error
	}{
		{"ErrAlreadyClaimed", storage.ErrAlreadyClaimed},
		{"ErrNotClaimable", storage.ErrNotClaimable},
		{"ErrNotFound", storage.ErrNotFound},
		{"ErrNotInitialized", storage.ErrNotInitialized},
		{"ErrPrefixMismatch", storage.ErrPrefixMismatch},
		{"ErrTooManyIterators", storage.ErrTooManyIterators},
		{"ErrIterSessionNotFound", storage.ErrIterSessionNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			rpcErr := &RPCError{Kind: tc.kind, Msg: "test: " + tc.kind}
			got := decodeRPCError(rpcErr)
			if got == nil {
				t.Fatalf("decodeRPCError returned nil for kind %q", tc.kind)
			}
			if !errors.Is(got, tc.sentinel) {
				t.Errorf("errors.Is(got, %v) = false; got type/msg: %T %v", tc.sentinel, got, got)
			}
		})
	}
}

// TestDecodeRPCError_OpaqueError verifies that an unknown Kind is passed
// through as a plain error with the original message, not wrapped with any
// sentinel. This ensures unknown future error kinds don't silently match a
// sentinel.
func TestDecodeRPCError_OpaqueError(t *testing.T) {
	rpcErr := &RPCError{Kind: "", Msg: "something went wrong"}
	got := decodeRPCError(rpcErr)
	if got == nil {
		t.Fatal("expected non-nil error for opaque RPCError")
	}
	if got.Error() != "something went wrong" {
		t.Errorf("expected message %q, got %q", "something went wrong", got.Error())
	}
	// Must not match any sentinel.
	sentinels := []error{
		storage.ErrAlreadyClaimed,
		storage.ErrNotClaimable,
		storage.ErrNotFound,
		storage.ErrNotInitialized,
		storage.ErrPrefixMismatch,
		storage.ErrTooManyIterators,
		storage.ErrIterSessionNotFound,
	}
	for _, s := range sentinels {
		if errors.Is(got, s) {
			t.Errorf("opaque error should not match sentinel %v", s)
		}
	}
}

// TestDecodeRPCError_Nil verifies that a nil RPCError returns nil.
func TestDecodeRPCError_Nil(t *testing.T) {
	if got := decodeRPCError(nil); got != nil {
		t.Errorf("expected nil for nil RPCError, got %v", got)
	}
}
