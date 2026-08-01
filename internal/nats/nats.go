package nats

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rymelabs/rymevisor/internal/config"
)

func Connect(ctx context.Context, cfg config.NATSConfig) (jetstream.JetStream, error) {
	nc, err := nats.Connect(cfg.URL,
		nats.MaxReconnects(cfg.MaxReconnects),
		nats.ReconnectWait(cfg.ReconnectWait),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			fmt.Printf("NATS disconnected: %v\n", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			fmt.Printf("NATS reconnected to %s\n", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			fmt.Printf("NATS connection closed\n")
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("nats: connect: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("nats: jetstream: %w", err)
	}

	// Ensure required streams exist
	if err := ensureStreams(ctx, js); err != nil {
		nc.Close()
		return nil, fmt.Errorf("nats: ensure streams: %w", err)
	}

	return js, nil
}

func ensureStreams(ctx context.Context, js jetstream.JetStream) error {
	streams := []jetstream.StreamConfig{
		{
			Name:      "EVENTS",
			Subjects:  []string{"events.>"},
			Storage:   jetstream.FileStorage,
			Retention: jetstream.LimitsPolicy,
			MaxAge:    24 * time.Hour,
		},
		{
			Name:      "COMMANDS",
			Subjects:  []string{"commands.>"},
			Storage:   jetstream.FileStorage,
			Retention: jetstream.WorkQueuePolicy,
			MaxAge:    1 * time.Hour,
		},
		{
			Name:      "HEARTBEATS",
			Subjects:  []string{"heartbeats.>"},
			Storage:   jetstream.MemoryStorage,
			Retention: jetstream.LimitsPolicy,
			MaxAge:    30 * time.Second,
			MaxMsgs:   1000,
		},
	}

	for _, cfg := range streams {
		if _, err := js.CreateOrUpdateStream(ctx, cfg); err != nil {
			return fmt.Errorf("stream %s: %w", cfg.Name, err)
		}
	}

	return nil
}
