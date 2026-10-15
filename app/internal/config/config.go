// Package config lee toda la configuración desde variables de entorno (12-factor).
// Nada de URLs, puertos ni secretos escritos en el código.
package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL    string        // DATABASE_URL (obligatoria)
	APIToken       string        // API_TOKEN (obligatoria para la API)
	Port           string        // PORT: puerto de la API (default 8080)
	MigrateOnStart bool          // MIGRATE_ON_START: la API aplica migraciones al arrancar (default true)
	WorkerPort     string        // WORKER_PORT: puerto de /metrics del worker (default 9091)
	WorkerInterval time.Duration // WORKER_INTERVAL: cada cuánto recalcula (default 1m)
	MetricsWindow  time.Duration // METRICS_WINDOW: ventana de cálculo DORA (default 720h = 30 días)
}

func Load() (Config, error) {
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		APIToken:    os.Getenv("API_TOKEN"),
		Port:        getenv("PORT", "8080"),
		WorkerPort:  getenv("WORKER_PORT", "9091"),
	}

	var err error
	if cfg.MigrateOnStart, err = strconv.ParseBool(getenv("MIGRATE_ON_START", "true")); err != nil {
		return cfg, errors.New("MIGRATE_ON_START debe ser true o false")
	}
	if cfg.WorkerInterval, err = time.ParseDuration(getenv("WORKER_INTERVAL", "1m")); err != nil {
		return cfg, errors.New("WORKER_INTERVAL inválido (ejemplo: 30s, 1m)")
	}
	if cfg.MetricsWindow, err = time.ParseDuration(getenv("METRICS_WINDOW", "720h")); err != nil {
		return cfg, errors.New("METRICS_WINDOW inválido (ejemplo: 720h)")
	}

	if cfg.DatabaseURL == "" {
		return cfg, errors.New("falta DATABASE_URL")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
