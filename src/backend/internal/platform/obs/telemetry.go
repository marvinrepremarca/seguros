package obs

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TelemetryOptions configura SetupTelemetry.
type TelemetryOptions struct {
	Service string
	Version string
	Env     string
	// Endpoint OTLP/HTTP (ej. http://otel-collector:4318). Vacío = sin exportación: se generan
	// trazas para correlacionar logs, pero no se envían.
	Endpoint    string
	SampleRatio float64
}

// ShutdownFunc vacía y cierra los proveedores de telemetría.
type ShutdownFunc func(context.Context) error

// SetupTelemetry instala los proveedores globales de OpenTelemetry (trazas, métricas y
// propagación W3C). El llamador debe invocar el ShutdownFunc al terminar el proceso.
func SetupTelemetry(ctx context.Context, opts TelemetryOptions) (ShutdownFunc, error) {
	res := resource.NewWithAttributes("",
		attribute.String("service.name", opts.Service),
		attribute.String("service.version", opts.Version),
		attribute.String("deployment.environment.name", opts.Env),
	)

	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(opts.SampleRatio))),
	}
	mpOpts := []sdkmetric.Option{sdkmetric.WithResource(res)}

	if endpoint := strings.TrimRight(opts.Endpoint, "/"); endpoint != "" {
		traceExp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint+"/v1/traces"))
		if err != nil {
			return nil, fmt.Errorf("exportador OTLP de trazas: %w", err)
		}
		metricExp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint+"/v1/metrics"))
		if err != nil {
			return nil, errors.Join(fmt.Errorf("exportador OTLP de métricas: %w", err), traceExp.Shutdown(ctx))
		}
		tpOpts = append(tpOpts, sdktrace.WithBatcher(traceExp))
		mpOpts = append(mpOpts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)))
	}

	tp := sdktrace.NewTracerProvider(tpOpts...)
	mp := sdkmetric.NewMeterProvider(mpOpts...)
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}
