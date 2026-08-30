package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	NATS     NATSConfig     `yaml:"nats"`
	APIKey   string         `yaml:"-"`
	Storage  StorageConfig  `yaml:"storage"`
	Logging  LoggingConfig  `yaml:"logging"`
	Node     NodeConfig     `yaml:"node"`
}

type ServerConfig struct {
	Addr            string        `yaml:"addr"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

type DatabaseConfig struct {
	URL             string        `yaml:"url"`
	MaxOpenConns    int           `yaml:"max_open_conns"`
	MaxIdleConns    int           `yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
}

type NATSConfig struct {
	URL           string        `yaml:"url"`
	MaxReconnects int           `yaml:"max_reconnects"`
	ReconnectWait time.Duration `yaml:"reconnect_wait"`
}

type StorageConfig struct {
	ImagesPath    string `yaml:"images_path"`
	BackupsPath   string `yaml:"backups_path"`
	MinIOEndpoint string `yaml:"minio_endpoint"`
	MinIOBucket   string `yaml:"minio_bucket"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type NodeConfig struct {
	ID           string        `yaml:"id"`
	Labels       []string      `yaml:"labels"`
	HeartbeatInt time.Duration `yaml:"heartbeat_interval"`
}

func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Addr:            envOrDefault("RYMEVISOR_SERVER_ADDR", ":8080"),
			ReadTimeout:     envDurationOrDefault("RYMEVISOR_SERVER_READ_TIMEOUT", 30*time.Second),
			WriteTimeout:    envDurationOrDefault("RYMEVISOR_SERVER_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:     envDurationOrDefault("RYMEVISOR_SERVER_IDLE_TIMEOUT", 120*time.Second),
			ShutdownTimeout: envDurationOrDefault("RYMEVISOR_SERVER_SHUTDOWN_TIMEOUT", 10*time.Second),
		},
		Database: DatabaseConfig{
			URL:             envOrDefault("RYMEVISOR_DATABASE_URL", "postgres://rymevisor:rymevisor@localhost:5432/rymevisor?sslmode=disable"),
			MaxOpenConns:    envIntOrDefault("RYMEVISOR_DATABASE_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    envIntOrDefault("RYMEVISOR_DATABASE_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: envDurationOrDefault("RYMEVISOR_DATABASE_CONN_MAX_LIFETIME", 5*time.Minute),
		},
		NATS: NATSConfig{
			URL:           envOrDefault("RYMEVISOR_NATS_URL", "nats://localhost:4222"),
			MaxReconnects: envIntOrDefault("RYMEVISOR_NATS_MAX_RECONNECTS", -1),
			ReconnectWait: envDurationOrDefault("RYMEVISOR_NATS_RECONNECT_WAIT", 2*time.Second),
		},
		APIKey: envOrDefault("RYMEVISOR_API_KEY", ""),
		Storage: StorageConfig{
			ImagesPath:    envOrDefault("RYMEVISOR_IMAGES_PATH", "/var/lib/rymevisor/images"),
			BackupsPath:   envOrDefault("RYMEVISOR_BACKUPS_PATH", "/var/lib/rymevisor/backups"),
			MinIOEndpoint: envOrDefault("RYMEVISOR_MINIO_ENDPOINT", "localhost:9000"),
			MinIOBucket:   envOrDefault("RYMEVISOR_MINIO_BUCKET", "rymevisor"),
		},
		Logging: LoggingConfig{
			Level:  envOrDefault("RYMEVISOR_LOG_LEVEL", "info"),
			Format: envOrDefault("RYMEVISOR_LOG_FORMAT", "json"),
		},
		Node: NodeConfig{
			ID:           envOrDefault("RYMEVISOR_NODE_ID", ""),
			Labels:       strings.Split(envOrDefault("RYMEVISOR_NODE_LABELS", ""), ","),
			HeartbeatInt: envDurationOrDefault("RYMEVISOR_NODE_HEARTBEAT", 10*time.Second),
		},
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}


func EnvOrDefault(key, fallback string) string {
	return envOrDefault(key, fallback)
}

func envIntOrDefault(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func envDurationOrDefault(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func (c *Config) Validate() error {
	if c.Database.URL == "" {
		return fmt.Errorf("database URL is required")
	}
	if c.NATS.URL == "" {
		return fmt.Errorf("NATS URL is required")
	}
	return nil
}
