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
	cpHandler "github.com/rymelabs/rymevisor/services/controlplane/handler"
	cpNats "github.com/rymelabs/rymevisor/services/controlplane/nats"
	"github.com/rymelabs/rymevisor/services/controlplane/postgres"
	"github.com/rymelabs/rymevisor/services/controlplane"
	"go.uber.org/zap"
)

func main() {
	bootstrap.Run(context.Background(), bootstrap.Options{ServiceName: "control-plane", NeedDB: true, NeedNATS: true}, func(ctx context.Context, cfg *config.Config, logger *zap.Logger, pool *pgxpool.Pool, js jetstream.JetStream) error {
		vmRepo := postgres.NewVMRepository(pool)
		nodeRepo := postgres.NewNodeRepository(pool)
		imageRepo := postgres.NewImageRepository(pool)
		backupRepo := postgres.NewBackupRepository(pool)
		snapRepo := postgres.NewSnapshotRepository(pool)
		flavorRepo := postgres.NewFlavorRepository(pool)
		keypairRepo := postgres.NewKeypairRepository(pool)
		ipamRepo := postgres.NewIPAMRepository(pool)

		var publisher controlplane.EventPublisher
		if js != nil {
			publisher = cpNats.NewEventPublisher(js)
		}
		svc := controlplane.NewService(vmRepo, nodeRepo, imageRepo, backupRepo, snapRepo, publisher)
		svc.SetLogger(logger)
		svc.InitPuller(cfg.Storage.ImagesPath, logger)
		svc.SetFlavorRepository(flavorRepo)
		svc.SetKeypairRepository(keypairRepo)
		svc.SetIPAMAllocator(ipamRepo)

		recovery := controlplane.NewRecovery(svc, logger)
		if err := recovery.Recover(ctx); err != nil {
			logger.Warn("initial recovery failed", zap.Error(err))
		}
		h := cpHandler.NewHandler(svc)
		hh := health.NewHandler()
		hh.Register("database", func(ctx context.Context) error { return pool.Ping(ctx) })
		r := chi.NewRouter()
		r.Route("/api/v1", func(r chi.Router) { r.Mount("/", h.Routes()) })
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
