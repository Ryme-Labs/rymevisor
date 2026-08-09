package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/rymelabs/rymevisor/internal/config"
	"github.com/rymelabs/rymevisor/internal/database"
	"github.com/rymelabs/rymevisor/internal/health"
	"github.com/rymelabs/rymevisor/internal/logging"
	"github.com/rymelabs/rymevisor/internal/middleware"
	"github.com/rymelabs/rymevisor/services/auth"
	"github.com/rymelabs/rymevisor/services/auth/handler"
	"github.com/rymelabs/rymevisor/services/auth/postgres"
	"go.uber.org/zap"
)

func main() {
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
	defer logger.Sync()

	ctx := context.Background()

	pool, err := database.Connect(ctx, cfg.Database)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}
	defer pool.Close()

	userRepo := postgres.NewUserRepository(pool)
	orgRepo := postgres.NewOrganizationRepository(pool)
	keyRepo := postgres.NewAPIKeyRepository(pool)
	sessionRepo := postgres.NewSessionRepository(pool)

	svc := auth.NewService(
		userRepo, orgRepo, keyRepo, sessionRepo,
		cfg.Auth.JWTSecret, cfg.Auth.BcryptCost,
		cfg.Auth.JWTExpiry, cfg.Auth.RefreshTokenExpiry,
	)

	authHandler := handler.NewHandler(svc)
	authRouter := authHandler.Routes()

	healthHandler := health.NewHandler()
	healthHandler.Register("database", func(ctx context.Context) error {
		return pool.Ping(ctx)
	})

	mux := chi.NewRouter()
	mux.Mount("/api/v1/auth", authRouter)
	mux.Handle("/health/live", healthHandler.Liveness())
	mux.Handle("/health/ready", healthHandler.Readiness())

	var httpHandler http.Handler = mux
	httpHandler = middleware.RequestTracing(httpHandler)
	httpHandler = middleware.CORS()(httpHandler)
	httpHandler = middleware.Logger(httpHandler)
	httpHandler = middleware.Recoverer(httpHandler)

	server := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      httpHandler,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	go func() {
		logger.Info("auth-service starting", zap.String("addr", cfg.Server.Addr))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down auth-service")
	shutdownCtx, cancel := context.WithTimeout(ctx, cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", zap.Error(err))
	}

	logger.Info("auth-service stopped")
}
