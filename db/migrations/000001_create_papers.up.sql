-- papers: one row per submitted paper, keyed by content hash for dedup.
CREATE TABLE papers (
    id             TEXT PRIMARY KEY,
    content_hash   TEXT NOT NULL UNIQUE,
    source_type    TEXT NOT NULL CHECK (source_type IN ('upload', 'url', 'doi')),
    source_ref     TEXT,
    status         TEXT NOT NULL DEFAULT 'queued'
                       CHECK (status IN ('queued', 'running', 'done', 'failed')),
    raw_key        TEXT NOT NULL,
    schema_version TEXT,
    result_key     TEXT,
    error          TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_papers_status ON papers (status);

-- Auto-touch updated_at on every UPDATE.
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_papers_set_updated_at
    BEFORE UPDATE ON papers
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();