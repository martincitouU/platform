// Binario 2: el worker que calcula las métricas DORA y las expone para Prometheus.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/martincitouU/platform/app/internal/config"
	"github.com/martincitouU/platform/app/internal/db"
	"github.com/martincitouU/platform/app/internal/dora"
	"github.com/martincitouU/platform/app/internal/store"
)

var (
	deploymentsTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dora_deployments",
		Help: "Despliegues en la ventana, por servicio y resultado.",
	}, []string{"service", "status"})

	leadTime = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dora_lead_time_seconds",
		Help: "Mediana del lead time (commit → producción) en la ventana.",
	}, []string{"service"})

	changeFailureRate = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dora_change_failure_rate",
		Help: "Proporción de despliegues fallidos o que causaron un incidente (0 a 1).",
	}, []string{"service"})

	mttr = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dora_mttr_seconds",
		Help: "Tiempo medio de recuperación de incidentes resueltos en la ventana.",
	}, []string{"service"})

	windowSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dora_window_seconds",
		Help: "Tamaño de la ventana de cálculo.",
	})

	runs = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dora_worker_runs_total",
		Help: "Ejecuciones del cálculo, por resultado.",
	}, []string{"result"})

	lastSuccess = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "dora_worker_last_success_timestamp_seconds",
		Help: "Momento del último cálculo exitoso (sirve para alertar si el worker se detiene).",
	})
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("component", "worker")
	if err := run(log); err != nil {
		log.Error("el worker terminó con error", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer pool.Close()
	st := store.New(pool)

	// El worker no atiende peticiones de negocio, pero expone /metrics y /health.
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	srv := &http.Server{Addr: ":" + cfg.WorkerPort, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Info("worker exponiendo métricas", "port", cfg.WorkerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("servidor de métricas falló", "error", err)
			stop()
		}
	}()

	windowSeconds.Set(cfg.MetricsWindow.Seconds())
	log.Info("worker iniciado", "interval", cfg.WorkerInterval.String(), "window", cfg.MetricsWindow.String())

	// Calcula una vez al arrancar y después cada WORKER_INTERVAL.
	ticker := time.NewTicker(cfg.WorkerInterval)
	defer ticker.Stop()
	for {
		compute(ctx, st, cfg.MetricsWindow, log)
		select {
		case <-ctx.Done():
			log.Info("apagando el worker")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return srv.Shutdown(shutdownCtx)
		case <-ticker.C:
		}
	}
}

func compute(ctx context.Context, st *store.Store, window time.Duration, log *slog.Logger) {
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	since := time.Now().Add(-window)
	deployments, err := st.DeploymentsSince(queryCtx, since)
	if err != nil {
		runs.WithLabelValues("error").Inc()
		log.Error("no se pudieron leer despliegues", "error", err)
		return
	}
	incidents, err := st.IncidentsSince(queryCtx, since)
	if err != nil {
		runs.WithLabelValues("error").Inc()
		log.Error("no se pudieron leer incidentes", "error", err)
		return
	}

	results := dora.Compute(deployments, incidents)

	// Reset borra servicios que ya no tienen datos en la ventana.
	deploymentsTotal.Reset()
	leadTime.Reset()
	changeFailureRate.Reset()
	mttr.Reset()

	for svc, m := range results {
		deploymentsTotal.WithLabelValues(svc, "success").Set(float64(m.SuccessfulDeployments))
		deploymentsTotal.WithLabelValues(svc, "failed").Set(float64(m.Deployments - m.SuccessfulDeployments))
		changeFailureRate.WithLabelValues(svc).Set(m.ChangeFailureRate)
		// Si no hay datos, no publicamos la serie: "sin datos" no es lo mismo que "0 segundos".
		if m.HasLeadTime {
			leadTime.WithLabelValues(svc).Set(m.LeadTimeMedian.Seconds())
		}
		if m.HasMTTR {
			mttr.WithLabelValues(svc).Set(m.MTTR.Seconds())
		}
	}

	runs.WithLabelValues("success").Inc()
	lastSuccess.SetToCurrentTime()
	log.Info("métricas calculadas", "services", len(results), "deployments", len(deployments), "incidents", len(incidents))
}
