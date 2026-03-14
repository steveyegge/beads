//go:build embeddeddolt

package embeddeddolt

import (
	"context"
	"errors"
	"testing"
)

func TestRetryOpenSQLPanicsRetriesRecoveredPanics(t *testing.T) {
	attempts := 0

	err := retryOpenSQLPanics(t.Context(), func() error {
		attempts++
		if attempts < 3 {
			panic("boom")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryOpenSQLPanics returned error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestRetryOpenSQLPanicsDoesNotRetryNormalErrors(t *testing.T) {
	attempts := 0
	wantErr := errors.New("boom")

	err := retryOpenSQLPanics(t.Context(), func() error {
		attempts++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("retryOpenSQLPanics error = %v, want %v", err, wantErr)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRetryOpenSQLPanicsStopsWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	attempts := 0
	err := retryOpenSQLPanics(ctx, func() error {
		attempts++
		panic("boom")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retryOpenSQLPanics error = %v, want context canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}
