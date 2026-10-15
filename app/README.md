# DORA Tracker

Servicio que registra despliegues e incidentes y calcula las cuatro métricas DORA
(frecuencia de despliegue, lead time, tasa de fallos y MTTR) para Prometheus.

## Arquitectura

```
CI (GitHub Actions) ──POST──▶ API (:8080) ──▶ PostgreSQL ◀── Worker (:9091) ──▶ Prometheus
```

Son **dos binarios** del mismo módulo, con ciclos de vida distintos:

| Binario | Qué hace | Puerto | Endpoints de operación |
|---|---|---|---|
| `cmd/api` | Recibe eventos y los guarda. Aplica migraciones al arrancar. | `PORT` (8080) | `/health`, `/ready`, `/metrics` |
| `cmd/worker` | Cada `WORKER_INTERVAL` recalcula las métricas DORA. | `WORKER_PORT` (9091) | `/health`, `/metrics` |

## Mapa del código

```
app/
├── cmd/api/main.go        arranque de la API: config → migraciones → conexión → servidor HTTP
├── cmd/worker/main.go     arranque del worker: config → conexión → ciclo con ticker → gauges
├── internal/
│   ├── config/            lee variables de entorno (12-factor)
│   ├── db/                pool de conexiones con reintentos + migraciones
│   ├── store/             TODO el SQL vive aquí
│   ├── api/               rutas, validación, autenticación y métricas HTTP
│   └── dora/              cálculo de métricas: lógica pura, sin base de datos
└── migrations/            esquema SQL versionado (embebido en el binario)
```

**Cómo fluye una petición `POST /deployments`:**
`observe` (métricas + log) → `ServeMux` encuentra la ruta → `auth` revisa el token →
`createDeployment` valida el JSON → `store.CreateDeployment` hace el INSERT → respuesta 201.

## Configuración

| Variable | Obligatoria | Default | Descripción |
|---|---|---|---|
| `DATABASE_URL` | sí | — | `postgres://user:pass@host:5432/db?sslmode=disable` |
| `API_TOKEN` | sí (API) | — | Token para los endpoints de escritura |
| `PORT` | no | `8080` | Puerto de la API |
| `MIGRATE_ON_START` | no | `true` | La API aplica migraciones al arrancar |
| `WORKER_PORT` | no | `9091` | Puerto de métricas del worker |
| `WORKER_INTERVAL` | no | `1m` | Cada cuánto recalcula el worker |
| `METRICS_WINDOW` | no | `720h` | Ventana de cálculo (30 días) |

## Correr en local

Necesitas Go y Docker (solo para PostgreSQL):

```bash
# 1. PostgreSQL
docker run -d --name dora-db -p 5432:5432 \
  -e POSTGRES_USER=dora -e POSTGRES_PASSWORD=dora -e POSTGRES_DB=dora postgres:16

# 2. Dependencias (la primera vez)
go mod tidy

# 3. Variables
export DATABASE_URL="postgres://dora:dora@localhost:5432/dora?sslmode=disable"
export API_TOKEN="dev-token"
export WORKER_INTERVAL=10s

# 4. En dos terminales
go run ./cmd/api
go run ./cmd/worker
```

## Probar

```bash
# Registrar un despliegue
curl -s -X POST localhost:8080/deployments \
  -H "Authorization: Bearer dev-token" -H "Content-Type: application/json" \
  -d '{"service":"dora-tracker","version":"v0.1.0","commit_sha":"abc123",
       "commit_at":"2026-10-15T10:00:00Z","status":"success"}'

# Abrir y resolver un incidente
curl -s -X POST localhost:8080/incidents -H "Authorization: Bearer dev-token" \
  -d '{"service":"dora-tracker","deployment_id":1}'
curl -s -X POST localhost:8080/incidents/1/resolve -H "Authorization: Bearer dev-token"

# Ver las métricas DORA que calculó el worker
curl -s localhost:9091/metrics | grep ^dora_
```

## Tests

```bash
go test ./...
```

Los tests no necesitan PostgreSQL: `dora` es lógica pura y `api` usa un store falso en memoria.

## Decisiones de diseño

- **Dos binarios:** si el cálculo se pone lento, no afecta a la API que recibe eventos del CI.
- **Mediana para el lead time:** un caso extremo (un feriado) no distorsiona el resultado.
- **`/health` vs `/ready`:** `/health` solo dice que el proceso vive (liveness); `/ready` además revisa la base (readiness). Kubernetes reinicia con el primero y saca del balanceo con el segundo.
- **Migraciones al arrancar la API:** simple para la v1. Con varias réplicas conviene moverlas a un paso separado (un Job), para que no compitan entre sí.
- **Worker idempotente:** solo lee la base, así que dos workers no rompen nada; solo duplicarían las métricas en Prometheus.
- **Logs JSON a stdout y apagado ordenado con SIGTERM:** lo que esperan Docker y Kubernetes.
