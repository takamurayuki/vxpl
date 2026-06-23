-- name: CreatePaper :one
INSERT INTO papers (id, content_hash, source_type, source_ref, raw_key)
VALUES (@id, @content_hash, @source_type, @source_ref, @raw_key)
RETURNING *;

-- name: GetPaper :one
SELECT * FROM papers WHERE id = @id;

-- name: GetPaperByHash :one
SELECT * FROM papers WHERE content_hash = @content_hash;

-- name: MarkPaperRunning :exec
UPDATE papers SET status = 'running' WHERE id = @id;

-- name: MarkPaperDone :exec
UPDATE papers
SET status = 'done', schema_version = @schema_version, result_key = @result_key, error = NULL
WHERE id = @id;

-- name: MarkPaperFailed :exec
UPDATE papers
SET status = 'failed', error = @error
WHERE id = @id;