CREATE TABLE IF NOT EXISTS resources
(
	id            TEXT PRIMARY KEY,
	account_id    TEXT           NOT NULL,
	service       TEXT           NOT NULL,
	resource_type TEXT           NOT NULL,
	region        TEXT           NOT NULL,
	monthly_cost  NUMERIC(10, 2) NOT NULL,
	usage_metric  NUMERIC(10, 2) NOT NULL,
	created_at    TIMESTAMPTZ    NOT NULL,
	updated_at    TIMESTAMPTZ    NOT NULL
);

CREATE INDEX idx_resources_service ON resources (service);
CREATE INDEX idx_resources_account ON resources (account_id);
