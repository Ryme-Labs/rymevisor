package main

import (
	"context"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rymelabs/rymevisor/internal/bootstrap"
	"github.com/rymelabs/rymevisor/internal/config"
	"github.com/rymelabs/rymevisor/internal/health"
	"github.com/rymelabs/rymevisor/internal/middleware"
	"github.com/rymelabs/rymevisor/services/network"
	networkhandler "github.com/rymelabs/rymevisor/services/network/handler"
	"github.com/rymelabs/rymevisor/services/network/postgres"
	"go.uber.org/zap"
)

func main() {
	bootstrap.Run(context.Background(), bootstrap.Options{ServiceName: "networking-engine", NeedDB: true}, func(ctx context.Context, cfg *config.Config, logger *zap.Logger, pool *pgxpool.Pool, _ jetstream.JetStream) error {
		networkRepo := postgres.NewNetworkRepository(pool)
		subnetRepo := postgres.NewSubnetRepository(pool)
		firewallRepo := postgres.NewFirewallRepository(pool)
		floatingIPRepo := postgres.NewFloatingIPRepository(pool)
		svc := network.NewService(networkRepo, subnetRepo, firewallRepo, floatingIPRepo)
		h := networkhandler.NewHandler(svc)
		hh := health.NewHandler()
		hh.Register("database", func(ctx context.Context) error { return pool.Ping(ctx) })
		r := chi.NewRouter()
		r.Use(middleware.RequestID)
		r.Use(middleware.RealIP)
		r.Use(middleware.Logger)
		r.Use(middleware.Recoverer)
		r.Use(middleware.CORS())
		h.RegisterRoutes(r)
		r.Handle("/health/live", hh.Liveness())
		r.Handle("/health/ready", hh.Readiness())
		return bootstrap.RunHTTP(ctx, logger, cfg.Server, r)
	})
}
