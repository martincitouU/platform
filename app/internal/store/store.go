// Package store contiene todas las consultas SQL. El resto de la app no escribe SQL.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/martincitouU/platform/app/internal/dora"
)

var ErrNotFound = errors.New("no encontrado")

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Ping lo usa el endpoint /ready para saber si la base responde.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// ---------- Servicios ----------

type Service struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Team      string    `json:"team"`
	CreatedAt time.Time `json:"created_at"`
}

// UpsertService crea el servicio o actualiza su equipo si ya existe.
func (s *Store) UpsertService(ctx context.Context, name, team string) (Service, error) {
	var svc Service
	err := s.pool.QueryRow(ctx, `
		INSERT INTO services (name, team) VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE SET team = EXCLUDED.team
		RETURNING id, name, team, created_at`, name, team,
	).Scan(&svc.ID, &svc.Name, &svc.Team, &svc.CreatedAt)
	return svc, err
}

func (s *Store) ListServices(ctx context.Context) ([]Service, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, team, created_at FROM services ORDER BY name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Service, error) {
		var svc Service
		err := r.Scan(&svc.ID, &svc.Name, &svc.Team, &svc.CreatedAt)
		return svc, err
	})
}

// serviceID devuelve el id del servicio, creándolo si no existe.
// Así el CI puede reportar un despliegue sin registrar el servicio antes.
func (s *Store) serviceID(ctx context.Context, name string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO services (name) VALUES ($1)
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`, name,
	).Scan(&id)
	return id, err
}

// ---------- Despliegues ----------

type Deployment struct {
	ID          int64     `json:"id"`
	Service     string    `json:"service"`
	Version     string    `json:"version"`
	CommitSHA   string    `json:"commit_sha"`
	CommitAt    time.Time `json:"commit_at"`
	DeployedAt  time.Time `json:"deployed_at"`
	Status      string    `json:"status"`
	Environment string    `json:"environment"`
}

func (s *Store) CreateDeployment(ctx context.Context, d Deployment) (Deployment, error) {
	svcID, err := s.serviceID(ctx, d.Service)
	if err != nil {
		return d, err
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO deployments (service_id, version, commit_sha, commit_at, deployed_at, status, environment)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		svcID, d.Version, d.CommitSHA, d.CommitAt, d.DeployedAt, d.Status, d.Environment,
	).Scan(&d.ID)
	return d, err
}

func (s *Store) ListDeployments(ctx context.Context, service string, limit int) ([]Deployment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id, s.name, d.version, d.commit_sha, d.commit_at, d.deployed_at, d.status, d.environment
		FROM deployments d JOIN services s ON s.id = d.service_id
		WHERE ($1::text = '' OR s.name = $1::text)
		ORDER BY d.deployed_at DESC
		LIMIT $2`, service, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Deployment, error) {
		var d Deployment
		err := r.Scan(&d.ID, &d.Service, &d.Version, &d.CommitSHA, &d.CommitAt, &d.DeployedAt, &d.Status, &d.Environment)
		return d, err
	})
}

// ---------- Incidentes ----------

type Incident struct {
	ID           int64      `json:"id"`
	Service      string     `json:"service"`
	DeploymentID *int64     `json:"deployment_id,omitempty"`
	OpenedAt     time.Time  `json:"opened_at"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
}

func (s *Store) CreateIncident(ctx context.Context, inc Incident) (Incident, error) {
	svcID, err := s.serviceID(ctx, inc.Service)
	if err != nil {
		return inc, err
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO incidents (service_id, deployment_id, opened_at)
		VALUES ($1, $2, $3)
		RETURNING id`, svcID, inc.DeploymentID, inc.OpenedAt,
	).Scan(&inc.ID)
	return inc, err
}

// ResolveIncident marca el incidente como resuelto. Si ya estaba resuelto, no cambia la fecha.
func (s *Store) ResolveIncident(ctx context.Context, id int64, at time.Time) (Incident, error) {
	var inc Incident
	err := s.pool.QueryRow(ctx, `
		UPDATE incidents i SET resolved_at = COALESCE(i.resolved_at, $2)
		FROM services s
		WHERE i.id = $1 AND s.id = i.service_id
		RETURNING i.id, s.name, i.deployment_id, i.opened_at, i.resolved_at`, id, at,
	).Scan(&inc.ID, &inc.Service, &inc.DeploymentID, &inc.OpenedAt, &inc.ResolvedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return inc, ErrNotFound
	}
	return inc, err
}

// ---------- Datos para el worker ----------

func (s *Store) DeploymentsSince(ctx context.Context, since time.Time) ([]dora.Deployment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id, s.name, d.commit_at, d.deployed_at, d.status = 'success'
		FROM deployments d JOIN services s ON s.id = d.service_id
		WHERE d.deployed_at >= $1`, since)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (dora.Deployment, error) {
		var d dora.Deployment
		err := r.Scan(&d.ID, &d.Service, &d.CommitAt, &d.DeployedAt, &d.Success)
		return d, err
	})
}

func (s *Store) IncidentsSince(ctx context.Context, since time.Time) ([]dora.Incident, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.name, i.deployment_id, i.opened_at, i.resolved_at
		FROM incidents i JOIN services s ON s.id = i.service_id
		WHERE i.opened_at >= $1`, since)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (dora.Incident, error) {
		var inc dora.Incident
		err := r.Scan(&inc.Service, &inc.DeploymentID, &inc.OpenedAt, &inc.ResolvedAt)
		return inc, err
	})
}
