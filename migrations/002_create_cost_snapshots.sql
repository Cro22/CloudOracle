CREATE TABLE IF NOT EXISTS cost_snapshots
(
    id                 SERIAL PRIMARY KEY,
    taken_at           TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    account_id         TEXT           NOT NULL,
    service            TEXT           NOT NULL,
    resource_count     INT            NOT NULL,
    total_monthly_cost NUMERIC(12, 2) NOT NULL
);

CREATE INDEX idx_snapshots_taken_at ON cost_snapshots (taken_at);
CREATE INDEX idx_snapshots_account ON cost_snapshots (account_id);
