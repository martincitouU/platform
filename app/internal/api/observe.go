package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Métricas HTTP de la API (las métricas DORA las expone el worker).
var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Peticiones HTTP atendidas, por ruta, método y código de estado.",
	}, []string{"route", "method", "code"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Duración de las peticiones HTTP.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route", "method"})
)

// statusRecorder guarda el código de estado que escribió el handler.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// observe registra métricas y un log por cada petición.
func (s *Server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		// r.Pattern es la ruta registrada ("POST /incidents/{id}/resolve"), no la URL real.
		// Usar la URL real ("/incidents/42/resolve") crearía una serie de métricas por cada id.
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		elapsed := time.Since(start)
		httpRequests.WithLabelValues(route, r.Method, strconv.Itoa(rec.status)).Inc()
		httpDuration.WithLabelValues(route, r.Method).Observe(elapsed.Seconds())

		// Los probes y Prometheus llaman cada pocos segundos: no ensuciamos el log con eso.
		if route != "GET /health" && route != "GET /ready" && route != "GET /metrics" {
			s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration_ms", elapsed.Milliseconds())
		}
	})
}
