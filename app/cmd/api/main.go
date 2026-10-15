// Binario 1: la API HTTP que recibe eventos del CI.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/martincitouU/platform/app/internal/api"
	"github.com/martincitouU/platform/app/internal/config"
	"github.com/martincitouU/platform/app/internal/db"
	"github.com/martincitouU/platform/app/internal/store"
)

func main() {
	// Logs en JSON a stdout: así los recolecta Docker/Kubernetes sin configuración extra.
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("component", "api")

	if err := run(log); err != nil {
		log.Error("la API terminó con error", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.APIToken == "" {
		return errors.New("falta API_TOKEN")
	}

	// ctx se cancela al recibir SIGINT (Ctrl+C) o SIGTERM (lo que envían Docker y Kubernetes al detener).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.MigrateOnStart {
		log.Info("aplicando migraciones")
		if err := db.Migrate(cfg.DatabaseURL); err != nil {
			return err
		}
	}

	pool, err := db.Connect(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.NewServer(store.New(pool), cfg.APIToken, log).Handler(promhttp.Handler()),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// El servidor corre en su propia goroutine; main se queda esperando la señal de apagado.
	errCh := make(chan error, 1)
	go func() {
		log.Info("API escuchando", "port", cfg.Port)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err // no pudo arrancar (ej. puerto ocupado)
	case <-ctx.Done():
	}

	// Graceful shutdown: deja de aceptar conexiones y espera hasta 10 s a que terminen las que están en curso.
	log.Info("apagando la API")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
