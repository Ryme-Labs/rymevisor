package main

import (
	"context"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rymelabs/rymevisor/internal/bootstrap"
	"github.com/rymelabs/rymevisor/internal/config"
	"github.com/rymelabs/rymevisor/services/nodeagent"
	"go.uber.org/zap"
)

func main() {
	bootstrap.Run(context.Background(), bootstrap.Options{ServiceName: "node-agent", NeedNATS: true, NATSRequired: true}, func(ctx context.Context, cfg *config.Config, logger *zap.Logger, _ *pgxpool.Pool, js jetstream.JetStream) error {
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
		if err := agent.RecoverVMs(ctx); err != nil {
			logger.Error("VM recovery failed", zap.Error(err))
		}
		interval := cfg.Node.HeartbeatInt
		if interval == 0 {
			interval = 10 * time.Second
		}
		go func() {
			ticker := time.NewTicker(interval)
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
		logger.Info("node-agent started", zap.String("node_id", nodeID), zap.String("hostname", hostname), zap.Duration("heartbeat_interval", interval))
		<-ctx.Done()
		logger.Info("node-agent stopping")
		time.Sleep(1 * time.Second)
		return nil
	})
}
