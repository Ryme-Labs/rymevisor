package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rymelabs/rymevisor/internal/config"
	"github.com/rymelabs/rymevisor/internal/database"
	"github.com/rymelabs/rymevisor/internal/logging"
	natspkg "github.com/rymelabs/rymevisor/internal/nats"
	"go.uber.org/zap"
)


type Options struct {
	ServiceName  string
	NeedDB       bool
	NeedNATS     bool
	NATSRequired bool
}















func Run(ctx context.Context, opts Options, fn func(ctx context.Context, cfg *config.Config, logger *zap.Logger, pool *pgxpool.Pool, js jetstream.JetStream) error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger, err := logging.New(cfg.Logging.Level, cfg.Logging.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()


	sigCtx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var pool *pgxpool.Pool
	if opts.NeedDB {

		connectCtx, ccancel := context.WithTimeout(sigCtx, 15*time.Second)
		p, err := database.Connect(connectCtx, cfg.Database)
		ccancel()
		if err != nil {
			logger.Fatal("failed to connect to database", zap.Error(err))
		}
		pool = p
		defer pool.Close()
	}

	var js jetstream.JetStream
	if opts.NeedNATS {
		j, err := natspkg.Connect(sigCtx, cfg.NATS)
		if err != nil {
			if opts.NATSRequired {
				logger.Fatal("failed to connect to NATS", zap.Error(err))
			} else {
				logger.Warn("failed to connect to NATS, running without", zap.Error(err))
			}
		} else {
			js = j
		}
	}

	if opts.ServiceName != "" {
		logger.Info(fmt.Sprintf("%s starting", opts.ServiceName))
	}

	if err := fn(sigCtx, cfg, logger, pool, js); err != nil {
		logger.Fatal("service failed", zap.Error(err))
	}

	if opts.ServiceName != "" {
		logger.Info(fmt.Sprintf("%s stopped", opts.ServiceName))
	}
}



func RunHTTP(ctx context.Context, logger *zap.Logger, srvCfg config.ServerConfig, handler http.Handler) error {
	srv := &http.Server{
		Addr:         srvCfg.Addr,
		Handler:      handler,
		ReadTimeout:  srvCfg.ReadTimeout,
		WriteTimeout: srvCfg.WriteTimeout,
		IdleTimeout:  srvCfg.IdleTimeout,
	}
	go func() {
		logger.Info("http server starting", zap.String("addr", srvCfg.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("http server failed", zap.Error(err))
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down http server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), srvCfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", zap.Error(err))
		return err
	}
	return nil
}


func MustLoadConfig() *config.Config {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
	return cfg
}


func MustNewLogger(cfg config.LoggingConfig) *zap.Logger {
	l, err := logging.New(cfg.Level, cfg.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger: %v\n", err)
		os.Exit(1)
	}
	return l
}


func ConnectDB(ctx context.Context, cfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	return database.Connect(ctx, cfg)
}


func ConnectNATS(ctx context.Context, cfg config.NATSConfig) (jetstream.JetStream, error) {
	return natspkg.Connect(ctx, cfg)
}
