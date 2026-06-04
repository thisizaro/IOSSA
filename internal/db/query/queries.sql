-- name: GetRepoByFullName :one
SELECT * FROM repos WHERE full_name = $1 LIMIT 1;

-- name: GetOrCreateRepo :one
INSERT INTO repos (owner, name, full_name, description, stars, forks)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (full_name) DO UPDATE
    SET description = EXCLUDED.description,
        stars       = EXCLUDED.stars,
        forks       = EXCLUDED.forks,
        updated_at  = now()
RETURNING *;

-- name: UpdateRepo :exec
UPDATE repos SET description = $2, stars = $3, forks = $4, updated_at = now()
WHERE id = $1;

-- name: GetSnapshot :one
SELECT * FROM snapshots
WHERE repo_id = $1 AND timeframe = $2
LIMIT 1;

-- name: UpsertSnapshot :exec
INSERT INTO snapshots (repo_id, timeframe, data, fetched_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (repo_id, timeframe) DO UPDATE
    SET data       = EXCLUDED.data,
        fetched_at = now();

-- name: AcquireFetchLock :exec
INSERT INTO fetch_locks (repo_id, timeframe)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ReleaseFetchLock :exec
DELETE FROM fetch_locks
WHERE repo_id = $1 AND timeframe = $2;

-- name: IsFetchLocked :one
SELECT EXISTS(
    SELECT 1 FROM fetch_locks
    WHERE repo_id = $1 AND timeframe = $2
      AND locked_at > now() - INTERVAL '5 minutes'
) AS locked;
