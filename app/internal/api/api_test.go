package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/martincitouU/platform/app/internal/store"
)

// fakeStore implementa Storage en memoria: los tests no necesitan PostgreSQL.
type fakeStore struct{ deployments []store.Deployment }

func (f *fakeStore) Ping(context.Context) error { return nil }
func (f *fakeStore) UpsertService(_ context.Context, name, team string) (store.Service, error) {
	return store.Service{ID: 1, Name: name, Team: team}, nil
}
func (f *fakeStore) ListServices(context.Context) ([]store.Service, error) { return nil, nil }
func (f *fakeStore) CreateDeployment(_ context.Context, d store.Deployment) (store.Deployment, error) {
	d.ID = int64(len(f.deployments) + 1)
	f.deployments = append(f.deployments, d)
	return d, nil
}
func (f *fakeStore) ListDeployments(context.Context, string, int) ([]store.Deployment, error) {
	return f.deployments, nil
}
func (f *fakeStore) CreateIncident(_ context.Context, inc store.Incident) (store.Incident, error) {
	return inc, nil
}
func (f *fakeStore) ResolveIncident(context.Context, int64, time.Time) (store.Incident, error) {
	return store.Incident{}, store.ErrNotFound
}

func newTestServer() (http.Handler, *fakeStore) {
	fs := &fakeStore{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(fs, "secret", log).Handler(http.NotFoundHandler()), fs
}

func do(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	h, _ := newTestServer()
	if rec := do(h, "GET", "/health", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /health = %d, quería 200", rec.Code)
	}
	if rec := do(h, "POST", "/health", "", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /health = %d, quería 405", rec.Code)
	}
}

func TestCreateDeployment(t *testing.T) {
	valid := `{"service":"api","version":"v1","commit_sha":"abc","commit_at":"2026-10-15T12:00:00Z","status":"success"}`

	tests := []struct {
		name  string
		token string
		body  string
		want  int
	}{
		{"sin token", "", valid, http.StatusUnauthorized},
		{"token incorrecto", "otro", valid, http.StatusUnauthorized},
		{"válido", "secret", valid, http.StatusCreated},
		{"status inválido", "secret", strings.Replace(valid, "success", "ok", 1), http.StatusBadRequest},
		{"campo desconocido", "secret", `{"service":"api","foo":1}`, http.StatusBadRequest},
		{"falta commit_at", "secret", `{"service":"api","version":"v1","commit_sha":"abc","status":"success"}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newTestServer()
			rec := do(h, "POST", "/deployments", tt.token, tt.body)
			if rec.Code != tt.want {
				t.Errorf("código = %d, quería %d (body: %s)", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func TestResolveUnknownIncident(t *testing.T) {
	h, _ := newTestServer()
	if rec := do(h, "POST", "/incidents/99/resolve", "secret", ""); rec.Code != http.StatusNotFound {
		t.Errorf("código = %d, quería 404", rec.Code)
	}
}
