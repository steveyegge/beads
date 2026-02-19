package main

import (
	"context"
	"errors"
	"testing"
)

func TestDepBatchRunCycleGuard_NoCycle_NoRollback(t *testing.T) {
	t.Helper()

	rollbackCalls := 0
	cycleCount, cycleDetected, rollbackErr, guardErr := depBatchRunCycleGuard(
		context.Background(),
		func(context.Context) (int, error) { return 0, nil },
		func(context.Context) error {
			rollbackCalls++
			return nil
		},
	)

	if guardErr != nil {
		t.Fatalf("expected nil guardErr, got %v", guardErr)
	}
	if rollbackErr != nil {
		t.Fatalf("expected nil rollbackErr, got %v", rollbackErr)
	}
	if cycleDetected {
		t.Fatalf("expected cycleDetected=false")
	}
	if cycleCount != 0 {
		t.Fatalf("expected cycleCount=0, got %d", cycleCount)
	}
	if rollbackCalls != 0 {
		t.Fatalf("expected no rollback calls, got %d", rollbackCalls)
	}
}

func TestDepBatchRunCycleGuard_CycleDetected_RollsBack(t *testing.T) {
	t.Helper()

	rollbackCalls := 0
	cycleCount, cycleDetected, rollbackErr, guardErr := depBatchRunCycleGuard(
		context.Background(),
		func(context.Context) (int, error) { return 1, nil },
		func(context.Context) error {
			rollbackCalls++
			return nil
		},
	)

	if guardErr != nil {
		t.Fatalf("expected nil guardErr, got %v", guardErr)
	}
	if rollbackErr != nil {
		t.Fatalf("expected nil rollbackErr, got %v", rollbackErr)
	}
	if !cycleDetected {
		t.Fatalf("expected cycleDetected=true")
	}
	if cycleCount != 1 {
		t.Fatalf("expected cycleCount=1, got %d", cycleCount)
	}
	if rollbackCalls != 1 {
		t.Fatalf("expected one rollback call, got %d", rollbackCalls)
	}
}

func TestDepBatchRunCycleGuard_GuardError_RollsBackAndReturnsError(t *testing.T) {
	t.Helper()

	guardProbeErr := errors.New("guard query failed")
	rollbackFailure := errors.New("rollback failed")
	rollbackCalls := 0
	cycleCount, cycleDetected, rollbackErr, guardErr := depBatchRunCycleGuard(
		context.Background(),
		func(context.Context) (int, error) { return 0, guardProbeErr },
		func(context.Context) error {
			rollbackCalls++
			return rollbackFailure
		},
	)

	if !errors.Is(guardErr, guardProbeErr) {
		t.Fatalf("expected guardErr=%v, got %v", guardProbeErr, guardErr)
	}
	if !errors.Is(rollbackErr, rollbackFailure) {
		t.Fatalf("expected rollbackErr=%v, got %v", rollbackFailure, rollbackErr)
	}
	if cycleDetected {
		t.Fatalf("expected cycleDetected=false on guard error")
	}
	if cycleCount != 0 {
		t.Fatalf("expected cycleCount=0, got %d", cycleCount)
	}
	if rollbackCalls != 1 {
		t.Fatalf("expected one rollback call, got %d", rollbackCalls)
	}
}
