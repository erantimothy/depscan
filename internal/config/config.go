// Package config centralizes how the dependency scanner reads it's runtime configuration.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr        string
	MaxScanWorkers  int
	ShutdownTimeout time.Duration
	LogLevel        string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:        getEnv("DEPSCAN_HTTP_ADDR", ":8080"),
		MaxScanWorkers:  8,
		ShutdownTimeout: 10 * time.Second,
		LogLevel:        getEnv("DEPSCAN_LOG_LEVEL", "info"),
	}

	if raw, ok := os.LookupEnv("DEPSCAN_MAX_SCAN_WORKERS"); ok {
		n, err := strconv.Atoi(raw) // convert string to integer
		if err != nil {
			return Config{}, fmt.Errorf("parsing DEPSCAN_MAX_SCAN_WORKERS=%q, %w", raw, err)
		}
		if n <= 0 {
			return Config{}, fmt.Errorf("DEPSCAN_MAX_SCAN_WORKERS must be positive")
		}
		cfg.MaxScanWorkers = n
	}

	if raw, ok := os.LookupEnv("DEPSCAN_SHUTDOWN_TIMEOUT"); ok {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("parsing DEPSCAN_SHUTDOWN_TIMEOUT=%q: %w", raw, err)
		}
		cfg.ShutdownTimeout = d
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
