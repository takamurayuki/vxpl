DROP TRIGGER IF EXISTS trg_papers_set_updated_at ON papers;
DROP FUNCTION IF EXISTS set_updated_at();
DROP TABLE IF EXISTS papers;