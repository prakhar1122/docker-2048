package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/extra/redisotel/v9"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// alloyOTLPEndpoint is the in-cluster Alloy OTLP gRPC receiver. Hardcoded for
// the trace-correlation walkthrough — set it to the cluster's monitoring stack.
const alloyOTLPEndpoint = "alloy.monitoring.svc.cluster.local:4317"

var (
	rdb    *redis.Client
	tracer = otel.Tracer("docker-2048/api")
	logger *slog.Logger
)

// initTracer wires up the OTLP gRPC exporter pointed at Alloy and installs the
// resulting TracerProvider as the global tracer.
func initTracer(ctx context.Context) (func(context.Context) error, error) {
	conn, err := grpc.NewClient(
		alloyOTLPEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, err
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("docker-2048-api"),
			semconv.ServiceNamespace("docker-2048"),
		),
	)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	tracer = tp.Tracer("docker-2048/api")
	return tp.Shutdown, nil
}

// traceContextHandler is a slog.Handler that pulls the active span's trace_id +
// span_id off the context and adds them as attributes on every log record. Two
// keys are emitted — `trace_id` and `traceId` — so the Inframan console picks
// up the click-to-open-trace button regardless of which casing your other
// services use.
type traceContextHandler struct{ slog.Handler }

func (h *traceContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		sc := span.SpanContext()
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("traceId", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, r)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	gameURL := os.Getenv("GAME_URL")

	// Structured JSON logger. Every record that's emitted with a context carrying
	// an active span will get trace_id / traceId / span_id attached automatically.
	logger = slog.New(&traceContextHandler{slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})})
	slog.SetDefault(logger)

	// Tracing.
	bootCtx := context.Background()
	shutdownTracer, err := initTracer(bootCtx)
	if err != nil {
		logger.Error("tracer init failed", "error", err)
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdownTracer(ctx)
		}()
		logger.Info("tracer initialised", "endpoint", alloyOTLPEndpoint)
	}

	logger.Info("startup env", "TEST_HAHA", os.Getenv("TEST_HAHA"))

	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		opts, err := redis.ParseURL(redisURL)
		if err != nil {
			logger.Error("redis: invalid REDIS_URL", "error", err)
		} else {
			rdb = redis.NewClient(opts)
			// Instrument Redis commands as spans on the active trace.
			if err := redisotel.InstrumentTracing(rdb); err != nil {
				logger.Warn("redis: failed to instrument tracing", "error", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := rdb.Ping(ctx).Err(); err != nil {
				logger.Error("redis: ping failed", "error", err)
			} else {
				logger.Info("redis: connected")
			}
		}
	} else {
		logger.Info("redis: REDIS_URL not set, skipping connection")
	}

	// otelhttp.DefaultClient propagates the current span context on outbound calls.
	httpClient := http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}

	mux := http.NewServeMux()

	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		logger.InfoContext(r.Context(), "ping", "path", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service":  "api",
			"status":   "pong",
			"time":     time.Now().Format(time.RFC3339),
			"game_url": gameURL,
		})
	})

	mux.HandleFunc("/roundtrip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		result := map[string]any{
			"service":  "api",
			"step":     "api received roundtrip request",
			"game_url": gameURL,
		}

		if gameURL == "" {
			result["error"] = "GAME_URL not set — no connection env var configured"
			logger.WarnContext(r.Context(), "roundtrip: no GAME_URL configured")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(result)
			return
		}

		// Child span around the outbound call so the trace shows api → game.
		ctx, span := tracer.Start(r.Context(), "call game service")
		defer span.End()

		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, gameURL, nil)
		resp, err := httpClient.Do(req)
		if err != nil {
			result["error"] = "failed to reach game service: " + err.Error()
			result["game_reachable"] = false
			logger.ErrorContext(ctx, "roundtrip: game unreachable", "error", err)
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(result)
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		result["game_reachable"] = true
		result["game_status"] = resp.StatusCode
		result["game_response_size"] = len(body)
		result["roundtrip"] = "success"

		logger.InfoContext(ctx, "roundtrip: ok",
			"game_status", resp.StatusCode,
			"game_response_size", len(body),
		)
		_ = json.NewEncoder(w).Encode(result)
	})

	mux.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var ev struct {
			Type  string `json:"type"`
			Score int    `json:"score"`
			Ts    int64  `json:"ts"`
		}
		if err := json.Unmarshal(body, &ev); err != nil || ev.Type == "" {
			logger.WarnContext(r.Context(), "event: bad body")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		logger.InfoContext(r.Context(), "event received",
			"type", ev.Type, "score", ev.Score, "ts", ev.Ts,
		)
		if rdb != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			rdb.Incr(ctx, "events:count:"+ev.Type)
			rdb.Set(ctx, "events:last:"+ev.Type, string(body), 0)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if rdb == nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"redis": "not_configured"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		stats := map[string]any{}
		for _, t := range []string{"score_update", "game_start", "game_over"} {
			count, _ := rdb.Get(ctx, "events:count:"+t).Int64()
			last, _ := rdb.Get(ctx, "events:last:"+t).Result()
			stats[t] = map[string]any{"count": count, "last": last}
		}
		_ = json.NewEncoder(w).Encode(stats)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		result := map[string]any{"status": "healthy"}

		if rdb == nil {
			result["redis"] = "not_configured"
		} else {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := rdb.Ping(ctx).Err(); err != nil {
				result["redis"] = "unreachable"
				result["redis_error"] = err.Error()
				result["status"] = "degraded"
				w.WriteHeader(http.StatusServiceUnavailable)
			} else {
				result["redis"] = "ok"
			}
		}

		_ = json.NewEncoder(w).Encode(result)
	})

	// Wrap the whole mux so every request gets a parent server span.
	handler := otelhttp.NewHandler(mux, "http.server",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)

	logger.Info("api service starting", "port", port, "game_url", gameURL)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}
