// Package telemetry provides OpenTelemetry integration for beads.
//
// Telemetry is disabled by default (zero runtime overhead when off).
//
// # Configuration
//
//	BD_OTEL_ENABLED=true              enable telemetry (default: off)
//	BD_OTEL_STDOUT=true               write spans/metrics to stdout (dev mode)
//	OTEL_EXPORTER_OTLP_ENDPOINT=...  OTLP gRPC endpoint (e.g. localhost:4317)
//	OTEL_SERVICE_NAME=bd              override service name
//
// # Supported exporters
//
//   - stdout: pretty-prints spans/metrics to stderr (BD_OTEL_STDOUT=true)
//   - OTLP/gRPC: Jaeger, Grafana Tempo, Honeycomb, Datadog, etc.
//     (OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317)
package telemetry

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/sdk/resource"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

const instrumentationScope = "github.com/steveyegge/beads"

var shutdownFns []func(context.Context) error

// Enabled reports whether telemetry is active (BD_OTEL_ENABLED=true).
func Enabled() bool {
	return os.Getenv("BD_OTEL_ENABLED") == "true"
}

// Init configures OTel providers. When BD_OTEL_ENABLED is not "true" this
// installs no-op providers and returns immediately (zero overhead path).
func Init(ctx context.Context, serviceName, version string) error {
	if !Enabled() {
		otel.SetTracerProvider(tracenoop.NewTracerProvider())
		otel.SetMeterProvider(metricnoop.NewMeterProvider())
		return nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(version),
		),
		resource.WithHost(),
		resource.WithProcess(),
	)
	if err != nil {
		return fmt.Errorf("telemetry: resource: %w", err)
	}

	tp, err := buildTraceProvider(ctx, res)
	if err != nil {
		return fmt.Errorf("telemetry: trace provider: %w", err)
	}
	otel.SetTracerProvider(tp)
	shutdownFns = append(shutdownFns, tp.Shutdown)

	mp, err := buildMetricProvider(ctx, res)
	if err != nil {
		return fmt.Errorf("telemetry: metric provider: %w", err)
	}
	otel.SetMeterProvider(mp)
	shutdownFns = append(shutdownFns, mp.Shutdown)

	return nil
}

func buildTraceProvider(ctx context.Context, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	var exporters []sdktrace.SpanExporter

	if os.Getenv("BD_OTEL_STDOUT") == "true" {
		exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, err
		}
		exporters = append(exporters, exp)
	}

	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		exp, err := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithInsecure(),
		)
		if err != nil {
			return nil, fmt.Errorf("otlp trace exporter: %w", err)
		}
		exporters = append(exporters, exp)
	}

	// Default to stdout when enabled but no exporter is configured.
	if len(exporters) == 0 {
		exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, err
		}
		exporters = append(exporters, exp)
	}

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	}
	for _, exp := range exporters {
		opts = append(opts, sdktrace.WithBatcher(exp))
	}
	return sdktrace.NewTracerProvider(opts...), nil
}

func buildMetricProvider(ctx context.Context, res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	opts := []sdkmetric.Option{sdkmetric.WithResource(res)}

	if os.Getenv("BD_OTEL_STDOUT") == "true" {
		exp, err := stdoutmetric.New()
		if err != nil {
			return nil, err
		}
		opts = append(opts, sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(15*time.Second)),
		))
	}

	// OTLP metric export via gRPC — uses the same endpoint env var as traces.
	// Users wanting metrics-only can set OTEL_EXPORTER_OTLP_METRICS_ENDPOINT.
	if endpoint := firstNonEmpty(
		os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"),
		os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
	); endpoint != "" {
		exp, err := buildOTLPMetricExporter(ctx, endpoint)
		if err != nil {
			return nil, fmt.Errorf("otlp metric exporter: %w", err)
		}
		opts = append(opts, sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(30*time.Second)),
		))
	}

	return sdkmetric.NewMeterProvider(opts...), nil
}

// Tracer returns a tracer with the given instrumentation name (or the global scope).
func Tracer(name string) trace.Tracer {
	if name == "" {
		name = instrumentationScope
	}
	return otel.Tracer(name)
}

// Meter returns a meter with the given instrumentation name (or the global scope).
func Meter(name string) metric.Meter {
	if name == "" {
		name = instrumentationScope
	}
	return otel.Meter(name)
}

// Shutdown flushes all spans/metrics and shuts down OTel providers.
// Should be deferred in PersistentPostRun with a short-lived context.
func Shutdown(ctx context.Context) {
	for _, fn := range shutdownFns {
		_ = fn(ctx)
	}
	shutdownFns = nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
