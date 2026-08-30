package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rymelabs/rymevisor/internal/config"
)

func Connect(ctx context.Context, cfg config.DatabaseConfig) (*pgxpool.Pool, error) {

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("database: parse config: %w", err)
	}

	poolCfg.MaxConns = int32(cfg.MaxOpenConns)


	minConns := int32(cfg.MaxIdleConns / 2)
	if minConns < 2 {
		minConns = 2
	}
	if minConns >= poolCfg.MaxConns {
		minConns = poolCfg.MaxConns / 2
	}

	if minConns > poolCfg.MaxConns {
		minConns = poolCfg.MaxConns / 2
	}
	if minConns < 0 {
		minConns = 0
	}
	poolCfg.MinConns = minConns
	poolCfg.MaxConnLifetime = cfg.ConnMaxLifetime
	poolCfg.MaxConnLifetimeJitter = 30 * time.Second
	poolCfg.MaxConnIdleTime = 5 * time.Minute
	poolCfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(connectCtx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("database: connect: %w", err)
	}

	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}

	return pool, nil
}
