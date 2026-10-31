-- +goose Up
-- A new table: the previous release does not know it and is not affected
-- (expand/contract, see README.md).
CREATE TABLE monitors (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id               UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name                  TEXT NOT NULL,
    url                   TEXT NOT NULL,
    method                TEXT NOT NULL DEFAULT 'GET' CHECK (method IN ('GET','HEAD')),
    interval_seconds      INT  NOT NULL DEFAULT 60 CHECK (interval_seconds BETWEEN 30 AND 86400),
    timeout_seconds       INT  NOT NULL DEFAULT 10 CHECK (timeout_seconds BETWEEN 1 AND 30),
    expected_status       INT,
    keyword               TEXT,
    follow_redirects      BOOLEAN NOT NULL DEFAULT TRUE,
    failure_threshold     INT  NOT NULL DEFAULT 3 CHECK (failure_threshold BETWEEN 1 AND 10),
    cert_expiry_warn_days INT  NOT NULL DEFAULT 14 CHECK (cert_expiry_warn_days BETWEEN 1 AND 90),
    is_active             BOOLEAN NOT NULL DEFAULT TRUE,
    status                TEXT NOT NULL DEFAULT 'unknown' CHECK (status IN ('unknown','up','down')),
    consecutive_failures  INT  NOT NULL DEFAULT 0,
    -- Expiry of the last seen leaf certificate.
    tls_not_after         TIMESTAMPTZ,
    -- The tls_not_after value an expiry warning was already sent for.
    cert_alerted_for      TIMESTAMPTZ,
    next_check_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_checked_at       TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Every API query filters by owner.
CREATE INDEX idx_monitors_user ON monitors(user_id);
-- The scheduler looks for active monitors that are due. Partial: paused
-- monitors are never scanned and do not bloat the index.
CREATE INDEX idx_monitors_due ON monitors(next_check_at) WHERE is_active;

-- +goose Down
DROP TABLE monitors;
