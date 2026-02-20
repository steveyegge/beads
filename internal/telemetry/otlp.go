package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func buildOTLPMetricExporter(ctx context.Context, endpoint string) (sdkmetric.Exporter, error) {
	return otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(endpoint),
		otlpmetricgrpc.WithInsecure(),
	)
}
