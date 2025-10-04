package metrics

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// Reporter uses the OpenTelemetry SDK to create and increment metrics.
type Reporter struct {
	meter metric.Meter
}

// NewReporter creates a new OTel-based metrics reporter.
func NewReporter(jobName string) (*Reporter, error) {
	// Get a meter from the global MeterProvider.
	// The provider is responsible for the entire metrics pipeline.
	meter := otel.GetMeterProvider().Meter(jobName)
	return &Reporter{meter: meter}, nil
}

// Increment finds or creates a counter and increments it by 1.
// The underlying OTel Meter handles caching instruments, so it's efficient
// to call this repeatedly for the same counter name.
func (r *Reporter) Increment(ctx context.Context, name string) {
	// Create an Int64Counter instrument. If one with the same name
	// already exists, the Meter will return the existing instance.
	counter, err := r.meter.Int64Counter(name)
	if err != nil {
		slog.Error("Failed to create/get OTel counter", "name", name, "error", err)
		return
	}

	// Add 1 to the counter.
	counter.Add(ctx, 1)
}

// Close is a no-op for this reporter implementation because the lifecycle
// of the underlying MeterProvider is managed globally in the main application setup.
func (r *Reporter) Close() {}
