package jobs

import (
    "context"
    "log/slog"

    "github.com/jackc/pgx/v5/pgtype"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/riverqueue/river"

    "github.com/takamurayuki/vxpl/internal/store"
)

// ParsePaperArgs is the payload enqueued for each paper to be parsed.
type ParsePaperArgs struct {
    PaperID string `json:"paper_id"`
}

// Kind identifies this job type in the queue. River uses it for routing.
func (ParsePaperArgs) Kind() string { return "parse_paper" }

// ParsePaperWorker processes ParsePaperArgs jobs.
type ParsePaperWorker struct {
    river.WorkerDefaults[ParsePaperArgs]
    Pool *pgxpool.Pool
    Log  *slog.Logger
}

func (w *ParsePaperWorker) Work(ctx context.Context, job *river.Job[ParsePaperArgs]) error {
    q := store.New(w.Pool)
    paperID := job.Args.PaperID

    w.Log.Info("parse job started", "paper_id", paperID, "attempt", job.Attempt)

    if err := q.MarkPaperRunning(ctx, paperID); err != nil {
        return err // returning an error makes River retry with backoff
    }

    // TODO(Phase 3): fetch the raw PDF from object storage and run the parser
    // (GROBID + MinerU via parser.Engine / ParseBest), upload the resulting
    // Document JSON to object storage, and use its real key below.

    err := q.MarkPaperDone(ctx, store.MarkPaperDoneParams{
        ID:            paperID,
        SchemaVersion: pgtype.Text{String: "1.0", Valid: true},
        ResultKey:     pgtype.Text{String: "result/" + paperID + ".json", Valid: true},
    })
    if err != nil {
        return err
    }

    w.Log.Info("parse job done", "paper_id", paperID)
    return nil
}