package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rymelabs/rymevisor/internal/config"
	"github.com/rymelabs/rymevisor/internal/logging"
	natspkg "github.com/rymelabs/rymevisor/internal/nats"
	"github.com/rymelabs/rymevisor/services/nodeagent"
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
	defer func() { _ = logger.Sync() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	js, err := natspkg.Connect(ctx, cfg.NATS)
	if err != nil {
		logger.Fatal("failed to connect to NATS", zap.Error(err))
	}

	nodeID := cfg.Node.ID
	if nodeID == "" {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "node-1"
		}
		nodeID = hostname
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "localhost"
	}

	agent := nodeagent.NewAgent(nodeID, hostname, js, logger)

	if err := agent.SubscribeCommands(ctx); err != nil {
		logger.Fatal("failed to subscribe to commands", zap.Error(err))
	}

	heartbeatInterval := cfg.Node.HeartbeatInt
	if heartbeatInterval == 0 {
		heartbeatInterval = 10 * time.Second
	}

	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()

		if err := agent.SendHeartbeat(ctx); err != nil {
			logger.Error("failed to send initial heartbeat", zap.Error(err))
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := agent.SendHeartbeat(ctx); err != nil {
					logger.Error("failed to send heartbeat", zap.Error(err))
				}
			}
		}
	}()

	logger.Info("node-agent started",
		zap.String("node_id", nodeID),
		zap.String("hostname", hostname),
		zap.Duration("heartbeat_interval", heartbeatInterval),
	)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("node-agent stopping")
	cancel()
	time.Sleep(1 * time.Second)
	logger.Info("node-agent stopped")
}
