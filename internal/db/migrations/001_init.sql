CREATE TABLE IF NOT EXISTS repos (
    id          SERIAL PRIMARY KEY,
    owner       TEXT NOT NULL,
    name        TEXT NOT NULL,
    full_name   TEXT UNIQUE NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    stars       INTEGER NOT NULL DEFAULT 0,
    forks       INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One snapshot per repo. Always stores 90 days of raw data.
-- timeframe column is always "90d" in MVP.
CREATE TABLE IF NOT EXISTS snapshots (
    id          SERIAL PRIMARY KEY,
    repo_id     INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    timeframe   TEXT NOT NULL DEFAULT '90d',
    data        JSONB NOT NULL,
    fetched_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(repo_id, timeframe)
);

-- Prevents duplicate concurrent fetches for the same repo.
CREATE TABLE IF NOT EXISTS fetch_locks (
    repo_id     INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    timeframe   TEXT NOT NULL DEFAULT '90d',
    locked_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_id, timeframe)
);

CREATE INDEX IF NOT EXISTS idx_snapshots_repo ON snapshots(repo_id);
CREATE INDEX IF NOT EXISTS idx_snapshots_fetched ON snapshots(fetched_at);
