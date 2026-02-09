package main

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
)

// notifyContext is overridden in tests to avoid sending real signals.
var notifyContext = signal.NotifyContext

// readLineWithContext returns a line from reader or ctx.Err() if canceled.
func readLineWithContext(ctx context.Context, reader *bufio.Reader) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	sigCtx, stop := notifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := sigCtx.Err(); err != nil {
		return "", err
	}

	type result struct {
		line string
		err  error
	}

	resultCh := make(chan result, 1)
	go func() {
		line, err := reader.ReadString('\n')
		resultCh <- result{line: line, err: err}
	}()

	select {
	case <-sigCtx.Done():
		return "", sigCtx.Err()
	case res := <-resultCh:
		return res.line, res.err
	}
}

func isCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
}
