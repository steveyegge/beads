package main

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/telemetry"
)

// commandSpanAttrs builds the initial attribute set for the bd.command.<name>
// span. Pulled out so tests can assert the attribute set without touching
// global OTel state. The bd.actor attribute is stamped later via SetAttributes
// in main.go once getActorWithGit() resolves.
func commandSpanAttrs(cmdName, version string, args []string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("bd.command", cmdName),
		attribute.String("bd.version", version),
		attribute.String("bd.args", strings.Join(args, " ")),
	}
}

// startCommandTelemetry initializes OTel and starts the root bd.command.<name>
// span. Returns the new context (carrying the span) and the span itself for
// End().
//
// Telemetry init failures are non-fatal — they are logged via debug and
// telemetry falls back to noop providers, matching the rest of bd's
// "telemetry is opt-in, never blocks the user" stance.
//
// Extracted so that the call from main.go's PreRunE is a single testable
// line. The original PR-3475 bug (telemetry.WrapStorage implemented but
// never called) taught us that wiring inside cobra PreRunE is exactly the
// kind of code that decays silently.
func startCommandTelemetry(ctx context.Context, cmdName, version string, args []string) (context.Context, oteltrace.Span) {
	if err := telemetry.Init(ctx, "bd", version); err != nil {
		debug.Logf("warning: telemetry init failed: %v", err)
	}
	return telemetry.Tracer("bd").Start(ctx, "bd.command."+cmdName,
		oteltrace.WithAttributes(commandSpanAttrs(cmdName, version, args)...),
	)
}
