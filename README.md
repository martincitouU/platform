<div align="center">

# 📊 DORA Tracker

### Mide qué tan bien entrega software tu equipo, sin hojas de cálculo ni adivinanzas.

Registra cada despliegue e incidente desde tu CI y convierte esos eventos en las
**cuatro métricas DORA**, listas para Prometheus y Grafana.

[![CI](https://github.com/martincitouU/platform/actions/workflows/ci.yaml/badge.svg)](https://github.com/martincitouU/platform/actions/workflows/ci.yaml)
![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)
![Prometheus](https://img.shields.io/badge/Prometheus-ready-E6522C?logo=prometheus&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-distroless-2496ED?logo=docker&logoColor=white)

[Inicio rápido](#-inicio-rápido) ·
[Arquitectura](#️-arquitectura) ·
[API](#-referencia-de-la-api) ·
[Métricas](#-métricas-expuestas) ·
[Decisiones de diseño](#-decisiones-de-diseño)

</div>

---

## ✨ ¿Qué es esto?

Las [métricas DORA](https://dora.dev/guides/dora-metrics-four-keys/) son el estándar de
la industria para medir el rendimiento de entrega de software. **DORA Tracker** las calcula
automáticamente a partir de dos tipos de eventos que tu pipeline ya conoce: *"desplegué"* y
*"algo se rompió"*.

| | Métrica | Pregunta que responde | Cómo se calcula |
|---|---|---|---|
| 🚀 | **Frecuencia de despliegue** | ¿Cada cuánto llevamos valor a producción? | Despliegues exitosos en la ventana |
| ⏱️ | **Lead time de cambios** | ¿Cuánto tarda un commit en llegar a producción? | **Mediana** de `deployed_at − commit_at` |
| 💥 | **Tasa de fallos de cambio** | ¿Qué proporción de despliegues sale mal? | (fallidos + los que causaron incidentes) / total |
| 🩹 | **MTTR** | ¿Cuánto tardamos en recuperarnos? | Promedio de `resolved_at − opened_at` |

Todo se calcula **por servicio** sobre una ventana deslizante (30 días por defecto).

### Lo que trae de serie

- 🧩 **Dos binarios desacoplados**: una API que ingiere eventos y un worker que calcula.
- 🔐 **Escrituras protegidas** con token Bearer (comparación en tiempo constante).
- 📈 **Observabilidad incluida**: métricas HTTP, métricas DORA y logs JSON estructurados.
- 🩺 **Probes listos para Kubernetes**: `/health` (liveness) y `/ready` (readiness).
- 🗄️ **Migraciones embebidas** en el binario y aplicadas al arrancar.
- 🐳 **Imágenes distroless, sin root**, escaneadas con Trivy y publicadas en GHCR.
- 🧪 **Tests sin base de datos**: lógica pura y un store en memoria.

---

## 🏗️ Arquitectura

```mermaid
flowchart LR
    subgraph CI["⚙️ Tu CI/CD"]
        GH["GitHub Actions<br/>u otro pipeline"]
    end

    subgraph DT["📊 DORA Tracker"]
        API["🟦 API<br/><code>cmd/api</code><br/>:8080"]
        WK["🟩 Worker<br/><code>cmd/worker</code><br/>:9091"]
        DB[("🐘 PostgreSQL<br/>services · deployments · incidents")]
    end

    subgraph OBS["🔭 Observabilidad"]
        PROM["Prometheus"]
        GRAF["Grafana"]
    end

    GH -- "POST /deployments<br/>POST /incidents" --> API
    API -- "INSERT" --> DB
    WK -- "SELECT cada<br/>WORKER_INTERVAL" --> DB
    PROM -- "scrape /metrics<br/>(dora_*)" --> WK
    PROM -- "scrape /metrics<br/>(http_*)" --> API
    GRAF --> PROM
```

Son **dos binarios** del mismo módulo Go, con ciclos de vida distintos:

| Binario | Responsabilidad | Puerto | Endpoints de operación |
|---|---|---|---|
| 🟦 `cmd/api` | Recibe eventos, los valida y los guarda. Aplica migraciones al arrancar. | `PORT` (8080) | `/health`, `/ready`, `/metrics` |
| 🟩 `cmd/worker` | Cada `WORKER_INTERVAL` lee la ventana, recalcula las métricas DORA y las publica como gauges. | `WORKER_PORT` (9091) | `/health`, `/metrics` |

> 💡 La API **escribe** y el worker **solo lee**. Si el cálculo se vuelve pesado, nunca
> bloquea la ingesta de eventos del CI.

### 🔄 El viaje de una petición

Así recorre el sistema un `POST /deployments`, desde el CI hasta el gráfico en Grafana:

```mermaid
sequenceDiagram
    autonumber
    participant CI as ⚙️ CI
    participant OBS as observe<br/>(middleware)
    participant MUX as ServeMux
    participant AUTH as auth<br/>(middleware)
    participant H as createDeployment
    participant ST as store
    participant DB as 🐘 PostgreSQL
    participant WK as 🟩 Worker
    participant P as Prometheus

    CI->>OBS: POST /deployments + Bearer token
    OBS->>MUX: inicia cronómetro
    MUX->>AUTH: ruta encontrada
    alt token inválido
        AUTH-->>CI: 401 Unauthorized
    end
    AUTH->>H: token OK
    H->>H: decodifica JSON (máx 1 MB, sin campos desconocidos)<br/>y valida campos
    alt datos inválidos
        H-->>CI: 400 Bad Request + lista de problemas
    end
    H->>ST: CreateDeployment()
    ST->>DB: INSERT (upsert del servicio)
    DB-->>ST: fila creada
    ST-->>H: Deployment
    H-->>OBS: 201 Created
    OBS->>OBS: http_requests_total++<br/>http_request_duration_seconds<br/>log JSON
    OBS-->>CI: 201 + JSON del despliegue

    Note over WK,DB: Más tarde, en el siguiente tick…
    WK->>DB: despliegues e incidentes de la ventana
    WK->>WK: dora.Compute() → métricas por servicio
    P->>WK: GET /metrics
    WK-->>P: dora_lead_time_seconds, dora_mttr_seconds, …
```

### 🗂️ Mapa del código

```
platform/
├── app/
│   ├── cmd/
│   │   ├── api/main.go        🟦 arranque: config → migraciones → conexión → servidor HTTP
│   │   └── worker/main.go     🟩 arranque: config → conexión → ticker → gauges
│   ├── internal/
│   │   ├── config/            lee variables de entorno (12-factor)
│   │   ├── db/                pool de conexiones con reintentos + migraciones
│   │   ├── store/             TODO el SQL vive aquí
│   │   ├── api/               rutas, validación, autenticación y métricas HTTP
│   │   └── dora/              cálculo de métricas: lógica pura, sin base de datos
│   ├── migrations/            esquema SQL versionado (embebido en el binario)
│   └── Dockerfile             build multi-etapa → distroless nonroot
├── compose.yaml               producción: imágenes publicadas en GHCR
├── compose.dev.yaml           desarrollo: build local de las imágenes
├── .env.example               plantilla de variables
└── .github/workflows/ci.yaml  tests → build → Trivy → publicación en GHCR
```

### 🧬 Modelo de datos

```mermaid
erDiagram
    services ||--o{ deployments : "tiene"
    services ||--o{ incidents : "sufre"
    deployments |o--o{ incidents : "puede causar"

    services {
        bigserial id PK
        text name UK
        text team
        timestamptz created_at
    }
    deployments {
        bigserial id PK
        bigint service_id FK
        text version
        text commit_sha
        timestamptz commit_at
        timestamptz deployed_at
        text status "success | failed"
        text environment
    }
    incidents {
        bigserial id PK
        bigint service_id FK
        bigint deployment_id FK "opcional"
        timestamptz opened_at
        timestamptz resolved_at "NULL = abierto"
    }
```

---

## 🚀 Inicio rápido

### Opción A · Docker Compose (recomendada)

Levanta PostgreSQL, la API y el worker con un solo comando.

```bash
cp .env.example .env   # edita las contraseñas y el token
```

**Construyendo las imágenes en tu máquina** (desarrollo):

```bash
docker compose -f compose.yaml -f compose.dev.yaml up --build
```

**Usando las imágenes publicadas en GHCR** (producción): pon en `IMAGE_TAG` el SHA del
commit que quieres desplegar y luego:

```bash
docker compose up -d
```

> 🔒 Los puertos se publican solo en `127.0.0.1`. Para exponerlos, pon un reverse proxy delante.

### Opción B · Go en local

Necesitas **Go 1.26+** y Docker (solo para PostgreSQL).

```bash
# 1. PostgreSQL
docker run -d --name dora-db -p 5432:5432 \
  -e POSTGRES_USER=dora -e POSTGRES_PASSWORD=dora -e POSTGRES_DB=dora postgres:16

# 2. Variables
export DATABASE_URL="postgres://dora:dora@localhost:5432/dora?sslmode=disable"
export API_TOKEN="dev-token"
export WORKER_INTERVAL=10s

# 3. En dos terminales, dentro de app/
go run ./cmd/api
go run ./cmd/worker
```

### ✅ Pruébalo en 30 segundos

> Si usas Docker Compose, cambia `dev-token` por el `API_TOKEN` de tu `.env`.

```bash
# 1. Registra un despliegue
curl -s -X POST localhost:8080/deployments \
  -H "Authorization: Bearer dev-token" -H "Content-Type: application/json" \
  -d '{"service":"checkout","version":"v1.4.0","commit_sha":"abc123",
       "commit_at":"2026-10-15T10:00:00Z","status":"success"}'

# 2. Algo se rompió: abre un incidente ligado a ese despliegue…
curl -s -X POST localhost:8080/incidents \
  -H "Authorization: Bearer dev-token" -H "Content-Type: application/json" \
  -d '{"service":"checkout","deployment_id":1}'

# 3. …y resuélvelo
curl -s -X POST localhost:8080/incidents/1/resolve -H "Authorization: Bearer dev-token"

# 4. Mira las métricas DORA que calculó el worker
curl -s localhost:9091/metrics | grep ^dora_
```

---

## 🔌 Integración con tu CI

Añade un paso al final de tu workflow de despliegue y DORA Tracker hace el resto:

```yaml
- name: Registrar despliegue en DORA Tracker
  if: always()
  run: |
    curl -sf -X POST "$DORA_URL/deployments" \
      -H "Authorization: Bearer ${{ secrets.DORA_API_TOKEN }}" \
      -H "Content-Type: application/json" \
      -d '{
        "service":    "${{ github.event.repository.name }}",
        "version":    "${{ github.sha }}",
        "commit_sha": "${{ github.sha }}",
        "commit_at":  "${{ github.event.head_commit.timestamp }}",
        "status":     "${{ job.status == 'success' && 'success' || 'failed' }}"
      }'
```

---

## 📖 Referencia de la API

| Método | Ruta | Auth | Descripción |
|---|---|:---:|---|
| `GET` | `/health` | — | Liveness: el proceso está vivo |
| `GET` | `/ready` | — | Readiness: el proceso **y** la base responden |
| `GET` | `/metrics` | — | Métricas HTTP en formato Prometheus |
| `GET` | `/services` | — | Lista los servicios registrados |
| `GET` | `/deployments?service=&limit=` | — | Últimos despliegues (`limit` 1–500, por defecto 50) |
| `POST` | `/services` | 🔐 | Crea o actualiza un servicio (`name`, `team`) |
| `POST` | `/deployments` | 🔐 | Registra un despliegue |
| `POST` | `/incidents` | 🔐 | Abre un incidente |
| `POST` | `/incidents/{id}/resolve` | 🔐 | Marca un incidente como resuelto |

<details>
<summary><b>Cuerpo de <code>POST /deployments</code></b></summary>

| Campo | Tipo | Obligatorio | Notas |
|---|---|:---:|---|
| `service` | string | ✅ | Nombre del servicio |
| `version` | string | ✅ | Tag, semver o SHA |
| `commit_sha` | string | ✅ | |
| `commit_at` | RFC3339 | ✅ | Cuándo se hizo el commit; base del lead time |
| `status` | `success` \| `failed` | ✅ | |
| `deployed_at` | RFC3339 | | Por defecto: ahora |
| `environment` | string | | Por defecto: `production` |

</details>

<details>
<summary><b>Cuerpo de <code>POST /incidents</code></b></summary>

| Campo | Tipo | Obligatorio | Notas |
|---|---|:---:|---|
| `service` | string | ✅ | |
| `deployment_id` | int | | Despliegue que lo causó (cuenta para la tasa de fallos) |
| `opened_at` | RFC3339 | | Por defecto: ahora |

</details>

Los errores siempre vuelven como JSON: `{"error": "…"}`. Los campos desconocidos se
rechazan y el cuerpo tiene un máximo de 1 MB.

---

## 📈 Métricas expuestas

**Worker** (`:9091/metrics`), todas con la etiqueta `service`:

| Métrica | Tipo | Descripción |
|---|---|---|
| `dora_deployments{status}` | gauge | Despliegues en la ventana (`success` / `failed`) |
| `dora_lead_time_seconds` | gauge | Mediana del lead time |
| `dora_change_failure_rate` | gauge | Proporción de 0 a 1 |
| `dora_mttr_seconds` | gauge | Tiempo medio de recuperación |
| `dora_window_seconds` | gauge | Tamaño de la ventana de cálculo |
| `dora_worker_runs_total{result}` | counter | Ejecuciones del cálculo |
| `dora_worker_last_success_timestamp_seconds` | gauge | Último cálculo exitoso; úsala para alertar |

**API** (`:8080/metrics`):

| Métrica | Tipo | Descripción |
|---|---|---|
| `http_requests_total{route,method,code}` | counter | Peticiones atendidas |
| `http_request_duration_seconds{route,method}` | histogram | Latencia por ruta |

<details>
<summary><b>🔎 Consultas PromQL de ejemplo</b></summary>

```promql
# Despliegues exitosos por día (ventana de 30 días)
dora_deployments{status="success"} / (dora_window_seconds / 86400)

# Lead time en horas
dora_lead_time_seconds / 3600

# Alerta: el worker lleva más de 10 minutos sin calcular
time() - dora_worker_last_success_timestamp_seconds > 600

# p95 de latencia de la API
histogram_quantile(0.95, sum by (le, route) (rate(http_request_duration_seconds_bucket[5m])))
```

</details>

---

## ⚙️ Configuración

Todo se configura por variables de entorno ([12-factor](https://12factor.net/config)).

| Variable | Obligatoria | Default | Descripción |
|---|:---:|---|---|
| `DATABASE_URL` | ✅ | — | `postgres://user:pass@host:5432/db?sslmode=disable` |
| `API_TOKEN` | ✅ (API) | — | Token para los endpoints de escritura |
| `PORT` | | `8080` | Puerto de la API |
| `MIGRATE_ON_START` | | `true` | La API aplica migraciones al arrancar |
| `WORKER_PORT` | | `9091` | Puerto de métricas del worker |
| `WORKER_INTERVAL` | | `1m` | Cada cuánto recalcula el worker |
| `METRICS_WINDOW` | | `720h` | Ventana de cálculo (30 días) |

Para Docker Compose, en `.env`: `POSTGRES_PASSWORD`, `API_TOKEN` e `IMAGE_TAG`.

---

## 🧪 Tests

```bash
cd app && go test ./...
```

No necesitan PostgreSQL: `dora` es lógica pura y `api` usa un store falso en memoria.

## 🤖 Pipeline de CI/CD

```mermaid
flowchart LR
    A["📥 push / PR"] --> B["🧪 go vet<br/>go test"]
    B --> C1["🐳 build api"]
    B --> C2["🐳 build worker"]
    C1 --> D1["🛡️ Trivy<br/>CRITICAL/HIGH"]
    C2 --> D2["🛡️ Trivy<br/>CRITICAL/HIGH"]
    D1 -- "solo main" --> E1["📦 ghcr.io/…/dora-api:SHA"]
    D2 -- "solo main" --> E2["📦 ghcr.io/…/dora-worker:SHA"]
```

Cada imagen se etiqueta con el SHA del commit: sabes exactamente qué código corre en
producción y volver atrás es cambiar `IMAGE_TAG`.

---

## 🧠 Decisiones de diseño

- **Dos binarios.** Si el cálculo se pone lento, no afecta a la API que recibe eventos del CI.
- **Mediana para el lead time.** Un caso extremo (un feriado) no distorsiona el resultado.
- **"Sin datos" ≠ "cero".** Si un servicio no tiene lead time o MTTR, la serie no se publica
  en vez de reportar un engañoso `0`.
- **`/health` vs `/ready`.** `/health` solo dice que el proceso vive (liveness); `/ready`
  además revisa la base (readiness). Kubernetes reinicia con el primero y saca del balanceo
  con el segundo.
- **Etiquetas con la ruta, no con la URL.** Las métricas HTTP usan el patrón
  (`/incidents/{id}/resolve`), así no se crea una serie nueva por cada id.
- **Migraciones al arrancar la API.** Simple para la v1. Con varias réplicas conviene
  moverlas a un paso separado (un Job) para que no compitan entre sí.
- **Worker idempotente.** Solo lee la base: dos workers no rompen nada, solo duplicarían
  las métricas en Prometheus.
- **Logs JSON a stdout y apagado ordenado con SIGTERM.** Lo que esperan Docker y Kubernetes.
- **Imagen distroless y sin root.** Sin shell ni gestor de paquetes: menos superficie de ataque.

## 🗺️ Próximos pasos

- [ ] Infraestructura como código con **Terraform** (`infra/terraform/`)
- [ ] Aprovisionamiento con **Ansible** (`ansible/`)
- [ ] Dashboard de Grafana listo para importar
- [ ] Migraciones como Job separado para múltiples réplicas

---

<div align="center">

Hecho con ☕ y Go por [Martín](https://github.com/martincitouU)

</div>
