package dora

import (
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func TestCompute(t *testing.T) {
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	deployments := []Deployment{
		// lead times exitosos de "api": 1h, 2h, 4h  → mediana 2h
		{ID: 1, Service: "api", CommitAt: base, DeployedAt: base.Add(1 * time.Hour), Success: true},
		{ID: 2, Service: "api", CommitAt: base, DeployedAt: base.Add(3 * time.Hour), Success: true},
		{ID: 3, Service: "api", CommitAt: base, DeployedAt: base.Add(4 * time.Hour), Success: true},
		{ID: 4, Service: "api", CommitAt: base, DeployedAt: base.Add(1 * time.Hour), Success: false},
		{ID: 5, Service: "web", CommitAt: base, DeployedAt: base.Add(30 * time.Minute), Success: true},
	}
	incidents := []Incident{
		// El despliegue 3 causó un incidente que duró 20 minutos.
		{Service: "api", DeploymentID: ptr[int64](3), OpenedAt: base, ResolvedAt: ptr(base.Add(20 * time.Minute))},
		// Otro de 40 minutos, sin despliegue asociado → MTTR = 30m.
		{Service: "api", OpenedAt: base, ResolvedAt: ptr(base.Add(40 * time.Minute))},
		// Incidente abierto: no cuenta para el MTTR.
		{Service: "api", OpenedAt: base},
	}

	got := Compute(deployments, incidents)

	api := got["api"]
	if api.Deployments != 4 || api.SuccessfulDeployments != 3 {
		t.Errorf("api: deployments=%d successful=%d, quería 4 y 3", api.Deployments, api.SuccessfulDeployments)
	}
	if api.LeadTimeMedian != 2*time.Hour {
		t.Errorf("api: lead time = %v, quería 2h", api.LeadTimeMedian)
	}
	// Fallos: el #4 (failed) + el #3 (causó incidente) = 2 de 4.
	if api.ChangeFailureRate != 0.5 {
		t.Errorf("api: change failure rate = %v, quería 0.5", api.ChangeFailureRate)
	}
	if !api.HasMTTR || api.MTTR != 30*time.Minute {
		t.Errorf("api: MTTR = %v, quería 30m", api.MTTR)
	}

	web := got["web"]
	if web.ChangeFailureRate != 0 || web.HasMTTR {
		t.Errorf("web: no debería tener fallos ni MTTR, got %+v", web)
	}
}

func TestComputeEmpty(t *testing.T) {
	if got := Compute(nil, nil); len(got) != 0 {
		t.Errorf("sin datos debería devolver un mapa vacío, got %v", got)
	}
}

func TestMedianEven(t *testing.T) {
	got := median([]time.Duration{4, 1, 3, 2})
	if got != 2 { // (2+3)/2 con división entera
		t.Errorf("median = %v, quería 2", got)
	}
}
