package main

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rymelabs/rymevisor/internal/bootstrap"
	"github.com/rymelabs/rymevisor/internal/config"
	"github.com/rymelabs/rymevisor/internal/health"
	"github.com/rymelabs/rymevisor/internal/middleware"
	handler "github.com/rymelabs/rymevisor/services/gateway/handler"
	"go.uber.org/zap"
)

func main() {
	bootstrap.Run(context.Background(), bootstrap.Options{ServiceName: "api-gateway"}, func(ctx context.Context, cfg *config.Config, logger *zap.Logger, _ *pgxpool.Pool, _ jetstream.JetStream) error {
		svcCfg := handler.ServiceConfig{
			ControlPlaneURL: config.EnvOrDefault("RYMEVISOR_CONTROL_PLANE_URL", "localhost:8081"),
			NetworkURL:      config.EnvOrDefault("RYMEVISOR_NETWORK_URL", "localhost:8083"),
			StorageURL:      config.EnvOrDefault("RYMEVISOR_STORAGE_URL", "localhost:8084"),
			SchedulerURL:    config.EnvOrDefault("RYMEVISOR_SCHEDULER_URL", "localhost:8085"),
		}
		gw := handler.NewGateway(svcCfg)
		hh := health.NewHandler()
		mux := http.NewServeMux()
		mux.Handle("/", gw.ServeHTTP())
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})
		mux.Handle("/health/live", hh.Liveness())
		mux.Handle("/health/ready", hh.Readiness())
		var h http.Handler = mux
		h = middleware.RequestID(h)
		h = middleware.RealIP(h)
		h = middleware.RequestTracing(h)
		h = middleware.CORS()(h)
		h = middleware.RequireAPIKey(h)
		h = middleware.Logger(h)
		h = middleware.Recoverer(h)
		h = middleware.RateLimit(100, 200)(h)
		return bootstrap.RunHTTP(ctx, logger, cfg.Server, h)
	})
}
