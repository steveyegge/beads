package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

type notifyStub struct {
	ready   chan struct{}
	cancel  context.CancelFunc
	signals []os.Signal
}

func installNotifyStub(t *testing.T) *notifyStub {
	t.Helper()
	stub := &notifyStub{ready: make(chan struct{}, 1)}
	original := notifyContext
	notifyContext = func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		stub.signals = append([]os.Signal(nil), signals...)
		ctx, cancel := context.WithCancel(parent)
		stub.cancel = cancel
		select {
		case stub.ready <- struct{}{}:
		default:
		}
		return ctx, cancel
	}
	t.Cleanup(func() { notifyContext = original })
	return stub
}

func TestReadLineWithContextReadsLine(t *testing.T) {
	stub := installNotifyStub(t)

	reader := bufio.NewReader(strings.NewReader("yes\n"))
	line, err := readLineWithContext(context.Background(), reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if line != "yes\n" {
		t.Fatalf("unexpected line: %q", line)
	}
	if len(stub.signals) != 2 || stub.signals[0] != os.Interrupt || stub.signals[1] != syscall.SIGTERM {
		t.Fatalf("unexpected signals: %v", stub.signals)
	}
}

func TestReadLineWithContextCanceled(t *testing.T) {
	stub := installNotifyStub(t)

	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})

	reader := bufio.NewReader(pr)
	done := make(chan error, 1)
	go func() {
		_, err := readLineWithContext(context.Background(), reader)
		done <- err
	}()

	select {
	case <-stub.ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for notifyContext")
	}
	if stub.cancel == nil {
		t.Fatal("expected cancel function")
	}

	stub.cancel()
	_ = pw.Close()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for readLineWithContext")
	}
}
