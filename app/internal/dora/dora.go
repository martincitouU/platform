// Package dora calcula las cuatro métricas DORA.
// Es lógica pura: no sabe de HTTP ni de PostgreSQL, por eso es fácil de testear.
package dora

import (
	"sort"
	"time"
)

type Deployment struct {
	ID         int64
	Service    string
	CommitAt   time.Time
	DeployedAt time.Time
	Success    bool
}

type Incident struct {
	Service      string
	DeploymentID *int64 // nil si no se asoció a un despliegue
	OpenedAt     time.Time
	ResolvedAt   *time.Time // nil si sigue abierto
}

type Metrics struct {
	Deployments           int           // total de despliegues en la ventana
	SuccessfulDeployments int           // frecuencia de despliegue
	LeadTimeMedian        time.Duration // mediana commit → producción
	HasLeadTime           bool          // false si no hay datos (no confundir con 0)
	ChangeFailureRate     float64       // 0.0 a 1.0
	MTTR                  time.Duration // tiempo medio de recuperación
	HasMTTR               bool
}

// Compute agrupa por servicio y calcula las métricas.
// Recibe datos ya filtrados por la ventana de tiempo.
func Compute(deployments []Deployment, incidents []Incident) map[string]Metrics {
	result := map[string]Metrics{}

	// Despliegues que causaron al menos un incidente.
	causedIncident := map[int64]bool{}
	for _, inc := range incidents {
		if inc.DeploymentID != nil {
			causedIncident[*inc.DeploymentID] = true
		}
	}

	leadTimes := map[string][]time.Duration{}
	failures := map[string]int{}

	for _, d := range deployments {
		m := result[d.Service]
		m.Deployments++
		if d.Success {
			m.SuccessfulDeployments++
			if lt := d.DeployedAt.Sub(d.CommitAt); lt >= 0 {
				leadTimes[d.Service] = append(leadTimes[d.Service], lt)
			}
		}
		if !d.Success || causedIncident[d.ID] {
			failures[d.Service]++
		}
		result[d.Service] = m
	}

	for svc, m := range result {
		if m.Deployments > 0 {
			m.ChangeFailureRate = float64(failures[svc]) / float64(m.Deployments)
		}
		if lts := leadTimes[svc]; len(lts) > 0 {
			m.LeadTimeMedian = median(lts)
			m.HasLeadTime = true
		}
		result[svc] = m
	}

	// MTTR: promedio de duración de los incidentes resueltos.
	recoverySum := map[string]time.Duration{}
	recoveryCount := map[string]int{}
	for _, inc := range incidents {
		if inc.ResolvedAt == nil {
			continue
		}
		recoverySum[inc.Service] += inc.ResolvedAt.Sub(inc.OpenedAt)
		recoveryCount[inc.Service]++
	}
	for svc, n := range recoveryCount {
		m := result[svc] // puede ser un servicio con incidentes pero sin despliegues en la ventana
		m.MTTR = recoverySum[svc] / time.Duration(n)
		m.HasMTTR = true
		result[svc] = m
	}

	return result
}

// median usa la mediana y no el promedio: un despliegue atrasado por un feriado
// no distorsiona el resultado.
func median(values []time.Duration) time.Duration {
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}
