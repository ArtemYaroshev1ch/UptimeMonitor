-- +goose Up
-- A new table only: the previous release does not know it and keeps working.
CREATE TABLE incidents (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    monitor_id  UUID NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    -- Time of the first failed check of the streak that opened it.
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Time of the first successful check afterwards; NULL while open.
    resolved_at TIMESTAMPTZ,
    -- error_kind of the check that opened it.
    cause_kind  TEXT
);

-- At most one open incident per monitor, enforced by the database: even if
-- two results of the same monitor were evaluated at once, the second insert
-- would conflict instead of opening a duplicate.
CREATE UNIQUE INDEX uniq_open_incident_per_monitor ON incidents(monitor_id) WHERE resolved_at IS NULL;
CREATE INDEX idx_incidents_monitor ON incidents(monitor_id, started_at DESC);

-- +goose Down
DROP TABLE incidents;
