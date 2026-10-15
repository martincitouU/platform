// Package api define las rutas HTTP, los handlers y los middlewares.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/martincitouU/platform/app/internal/store"
)

// Storage es lo que la API necesita de la base. Al ser una interfaz,
// los tests pueden usar una implementación falsa sin PostgreSQL.
type Storage interface {
	Ping(ctx context.Context) error
	UpsertService(ctx context.Context, name, team string) (store.Service, error)
	ListServices(ctx context.Context) ([]store.Service, error)
	CreateDeployment(ctx context.Context, d store.Deployment) (store.Deployment, error)
	ListDeployments(ctx context.Context, service string, limit int) ([]store.Deployment, error)
	CreateIncident(ctx context.Context, inc store.Incident) (store.Incident, error)
	ResolveIncident(ctx context.Context, id int64, at time.Time) (store.Incident, error)
}

type Server struct {
	store Storage
	token string
	log   *slog.Logger
}

func NewServer(s Storage, token string, log *slog.Logger) *Server {
	return &Server{store: s, token: token, log: log}
}

// Handler arma el router completo con sus middlewares.
func (s *Server) Handler(metrics http.Handler) http.Handler {
	mux := http.NewServeMux()

	// Operación: sin autenticación (los usan probes y Prometheus).
	mux.HandleFunc("GET /health", s.health) // liveness: ¿el proceso está vivo?
	mux.HandleFunc("GET /ready", s.ready)   // readiness: ¿puede atender? (incluye la base)
	mux.Handle("GET /metrics", metrics)

	// Lectura.
	mux.HandleFunc("GET /services", s.listServices)
	mux.HandleFunc("GET /deployments", s.listDeployments)

	// Escritura: requieren token.
	mux.Handle("POST /services", s.auth(http.HandlerFunc(s.createService)))
	mux.Handle("POST /deployments", s.auth(http.HandlerFunc(s.createDeployment)))
	mux.Handle("POST /incidents", s.auth(http.HandlerFunc(s.createIncident)))
	mux.Handle("POST /incidents/{id}/resolve", s.auth(http.HandlerFunc(s.resolveIncident)))

	return s.observe(mux)
}

// ---------- Operación ----------

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "reason": "database"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// ---------- Servicios ----------

func (s *Server) createService(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		Team string `json:"team"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeError(w, http.StatusBadRequest, "name es obligatorio")
		return
	}
	svc, err := s.store.UpsertService(r.Context(), in.Name, in.Team)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, svc)
}

func (s *Server) listServices(w http.ResponseWriter, r *http.Request) {
	svcs, err := s.store.ListServices(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, svcs)
}

// ---------- Despliegues ----------

func (s *Server) createDeployment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Service     string     `json:"service"`
		Version     string     `json:"version"`
		CommitSHA   string     `json:"commit_sha"`
		CommitAt    *time.Time `json:"commit_at"`
		DeployedAt  *time.Time `json:"deployed_at"`
		Status      string     `json:"status"`
		Environment string     `json:"environment"`
	}
	if !s.decode(w, r, &in) {
		return
	}

	var problems []string
	if in.Service == "" {
		problems = append(problems, "service es obligatorio")
	}
	if in.Version == "" {
		problems = append(problems, "version es obligatorio")
	}
	if in.CommitSHA == "" {
		problems = append(problems, "commit_sha es obligatorio")
	}
	if in.CommitAt == nil {
		problems = append(problems, "commit_at es obligatorio (RFC3339, ej: 2026-10-15T12:00:00Z)")
	}
	if in.Status != "success" && in.Status != "failed" {
		problems = append(problems, "status debe ser 'success' o 'failed'")
	}
	if len(problems) > 0 {
		writeError(w, http.StatusBadRequest, strings.Join(problems, "; "))
		return
	}

	d := store.Deployment{
		Service:     in.Service,
		Version:     in.Version,
		CommitSHA:   in.CommitSHA,
		CommitAt:    *in.CommitAt,
		DeployedAt:  time.Now().UTC(),
		Status:      in.Status,
		Environment: "production",
	}
	if in.DeployedAt != nil {
		d.DeployedAt = *in.DeployedAt
	}
	if in.Environment != "" {
		d.Environment = in.Environment
	}

	created, err := s.store.CreateDeployment(r.Context(), d)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.log.Info("despliegue registrado", "service", created.Service, "version", created.Version, "status", created.Status)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) listDeployments(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			writeError(w, http.StatusBadRequest, "limit debe estar entre 1 y 500")
			return
		}
		limit = n
	}
	deps, err := s.store.ListDeployments(r.Context(), r.URL.Query().Get("service"), limit)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, deps)
}

// ---------- Incidentes ----------

func (s *Server) createIncident(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Service      string     `json:"service"`
		DeploymentID *int64     `json:"deployment_id"`
		OpenedAt     *time.Time `json:"opened_at"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	if in.Service == "" {
		writeError(w, http.StatusBadRequest, "service es obligatorio")
		return
	}
	inc := store.Incident{Service: in.Service, DeploymentID: in.DeploymentID, OpenedAt: time.Now().UTC()}
	if in.OpenedAt != nil {
		inc.OpenedAt = *in.OpenedAt
	}
	created, err := s.store.CreateIncident(r.Context(), inc)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.log.Warn("incidente abierto", "service", created.Service, "id", created.ID)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) resolveIncident(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id inválido")
		return
	}
	inc, err := s.store.ResolveIncident(r.Context(), id, time.Now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "incidente no existe")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.log.Info("incidente resuelto", "service", inc.Service, "id", inc.ID)
	writeJSON(w, http.StatusOK, inc)
}

// ---------- Middleware de autenticación ----------

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		// ConstantTimeCompare evita filtrar el token midiendo cuánto tarda la comparación.
		if !ok || s.token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "token inválido o ausente")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- Helpers ----------

func (s *Server) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // máximo 1 MB por petición
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return false
	}
	return true
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	// Al cliente no se le muestra el error real (podría filtrar detalles internos).
	s.log.Error("error interno", "method", r.Method, "path", r.URL.Path, "error", err)
	writeError(w, http.StatusInternalServerError, "error interno")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
