-- +goose Up
-- A new table only: the previous release does not know it and keeps working.
CREATE TABLE checks (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    monitor_id       UUID NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    checked_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    success          BOOLEAN NOT NULL,
    status_code      INT,
    response_time_ms INT NOT NULL,
    -- Phase timings of the final request (after redirects). NULL when the
    -- phase did not happen: no DNS for an IP literal, no TLS for http://,
    -- nothing after a failed connect.
    dns_ms           INT,
    connect_ms       INT,
    tls_ms           INT,
    ttfb_ms          INT,
    http_proto       TEXT,                        -- 'HTTP/1.1', 'HTTP/2.0'
    tls_version      TEXT,                        -- 'TLS1.2', 'TLS1.3'
    tls_not_after    TIMESTAMPTZ,
    error_kind       TEXT CHECK (error_kind IN ('dns','timeout','connection','tls',
                                                'unexpected_status','keyword_mismatch',
                                                'blocked_target','unknown')),
    error_message    TEXT
);

-- History and statistics of one monitor over a time range.
CREATE INDEX idx_checks_monitor_time ON checks(monitor_id, checked_at DESC);
-- Retention deletes by age across all monitors.
CREATE INDEX idx_checks_checked_at   ON checks(checked_at);

-- +goose Down
DROP TABLE checks;
