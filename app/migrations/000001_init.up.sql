-- Esquema inicial del DORA tracker.
-- Las migraciones son inmutables: si hay que cambiar algo, se crea una nueva (000002_...).

CREATE TABLE services (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT        NOT NULL UNIQUE,
    team        TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE deployments (
    id           BIGSERIAL PRIMARY KEY,
    service_id   BIGINT      NOT NULL REFERENCES services(id),
    version      TEXT        NOT NULL,
    commit_sha   TEXT        NOT NULL,
    commit_at    TIMESTAMPTZ NOT NULL,                 -- cuándo se hizo el commit
    deployed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),   -- cuándo terminó el despliegue
    status       TEXT        NOT NULL CHECK (status IN ('success', 'failed')),
    environment  TEXT        NOT NULL DEFAULT 'production'
);

-- El worker filtra por fecha de despliegue en cada ejecución.
CREATE INDEX idx_deployments_deployed_at ON deployments (deployed_at);

CREATE TABLE incidents (
    id             BIGSERIAL PRIMARY KEY,
    service_id     BIGINT      NOT NULL REFERENCES services(id),
    deployment_id  BIGINT      REFERENCES deployments(id),  -- opcional: qué despliegue lo causó
    opened_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at    TIMESTAMPTZ                               -- NULL mientras está abierto
);

CREATE INDEX idx_incidents_opened_at ON incidents (opened_at);
