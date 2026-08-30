package main

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rymelabs/rymevisor/internal/bootstrap"
	"github.com/rymelabs/rymevisor/internal/config"
	"github.com/rymelabs/rymevisor/internal/health"
	"github.com/rymelabs/rymevisor/internal/middleware"
	"github.com/rymelabs/rymevisor/services/scheduler"
	"github.com/rymelabs/rymevisor/services/scheduler/handler"
	schedulerpostgres "github.com/rymelabs/rymevisor/services/scheduler/postgres"
	"go.uber.org/zap"
)

func main() {
	bootstrap.Run(context.Background(), bootstrap.Options{ServiceName: "scheduler"}, func(ctx context.Context, cfg *config.Config, logger *zap.Logger, _ *pgxpool.Pool, _ jetstream.JetStream) error {
		repo := schedulerpostgres.NewSchedulerRepository()
		svc := scheduler.NewService(repo, nil)
		h := handler.NewHandler(svc)
		hh := health.NewHandler()
		r := chi.NewRouter()
		h.Register(r)
		r.Handle("/health/live", hh.Liveness())
		r.Handle("/health/ready", hh.Readiness())
		var httpHandler http.Handler = r
		httpHandler = middleware.RequestTracing(httpHandler)
		httpHandler = middleware.CORS()(httpHandler)
		httpHandler = middleware.Logger(httpHandler)
		httpHandler = middleware.Recoverer(httpHandler)
		return bootstrap.RunHTTP(ctx, logger, cfg.Server, httpHandler)
	})
}
