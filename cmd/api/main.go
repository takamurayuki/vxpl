package main

import (
    "bytes"
    "context"
    "crypto/rand"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgtype"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"
    "github.com/riverqueue/river"
    "github.com/riverqueue/river/riverdriver/riverpgxv5"

    "github.com/takamurayuki/vxpl/internal/jobs"
    "github.com/takamurayuki/vxpl/internal/store"
)

type config struct {
    addr           string
    databaseURL    string
    grobidURL      string
    minioEndpoint  string
    minioAccessKey string
    minioSecretKey string
    minioBucket    string
}

func loadConfig() config {
    return config{
        addr:           env("VXPL_ADDR", ":8080"),
        databaseURL:    env("DATABASE_URL", "postgres://app:secret@localhost:5432/vxpl?sslmode=disable"),
        grobidURL:      env("GROBID_URL", "http://localhost:8070"),
        minioEndpoint:  env("MINIO_ENDPOINT", "localhost:9000"),
        minioAccessKey: env("MINIO_ACCESS_KEY", "minioadmin"),
        minioSecretKey: env("MINIO_SECRET_KEY", "minioadmin"),
        minioBucket:    env("MINIO_BUCKET", "vxpl"),
    }
}

func env(key, def string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return def
}

type app struct {
    cfg         config
    log         *slog.Logger
    db          *pgxpool.Pool
    minio       *minio.Client
    http        *http.Client
    queries     *store.Queries
    riverClient *river.Client[pgx.Tx]
}

func main() {
    log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
    cfg := loadConfig()

    pool, err := pgxpool.New(context.Background(), cfg.databaseURL)
    if err != nil {
        log.Error("failed to create db pool", "err", err)
        os.Exit(1)
    }
    defer pool.Close()

    mc, err := minio.New(cfg.minioEndpoint, &minio.Options{
        Creds:  credentials.NewStaticV4(cfg.minioAccessKey, cfg.minioSecretKey, ""),
        Secure: false,
    })
    if err != nil {
        log.Error("failed to create minio client", "err", err)
        os.Exit(1)
    }

    // Insert-only River client: the API enqueues jobs but does not process them.
    riverClient, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
    if err != nil {
        log.Error("failed to create river client", "err", err)
        os.Exit(1)
    }

    a := &app{
        cfg:         cfg,
        log:         log,
        db:          pool,
        minio:       mc,
        http:        &http.Client{Timeout: 5 * time.Second},
        queries:     store.New(pool),
        riverClient: riverClient,
    }

    r := chi.NewRouter()
    r.Use(middleware.RequestID)
    r.Use(middleware.Recoverer)
    r.Use(requestLogger(log))
    r.Get("/healthz", a.handleHealthz)
    r.Post("/papers", a.handleCreatePaper)
    r.Get("/papers/{id}", a.handleGetPaper)

    srv := &http.Server{
        Addr:         cfg.addr,
        Handler:      r,
        ReadTimeout:  30 * time.Second,
        WriteTimeout: 30 * time.Second,
    }

    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    go func() {
        log.Info("api listening", "addr", cfg.addr)
        if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
            log.Error("server error", "err", err)
            os.Exit(1)
        }
    }()

    <-ctx.Done()
    log.Info("shutting down")

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := srv.Shutdown(shutdownCtx); err != nil {
        log.Error("graceful shutdown failed", "err", err)
    }
}

// ---- POST /papers ----

type paperResp struct {
    ID          string `json:"id"`
    Status      string `json:"status"`
    ContentHash string `json:"content_hash"`
    ResultKey   string `json:"result_key,omitempty"`
}

func paperResponse(p store.Paper) paperResp {
    return paperResp{
        ID:          p.ID,
        Status:      p.Status,
        ContentHash: p.ContentHash,
        ResultKey:   p.ResultKey.String,
    }
}

func (a *app) handleCreatePaper(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    // 1. Cap the upload size, then pull the file out of the multipart form.
    const maxUpload = 50 << 20 // 50 MiB
    r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
    file, header, err := r.FormFile("file")
    if err != nil {
        writeError(w, http.StatusBadRequest, "expected a PDF in form field 'file' (max 50MB)")
        return
    }
    defer file.Close()

    data, err := io.ReadAll(file)
    if err != nil {
        writeError(w, http.StatusBadRequest, "could not read uploaded file")
        return
    }

    // 2. Validate it really is a PDF by checking the magic bytes.
    if len(data) < 5 || string(data[:5]) != "%PDF-" {
        writeError(w, http.StatusBadRequest, "file is not a PDF")
        return
    }

    // 3. Fingerprint the file: same bytes -> same hash. This is our dedup key.
    sum := sha256.Sum256(data)
    hash := hex.EncodeToString(sum[:])

    // 4. Dedup: if we've seen this exact paper, return the existing record.
    existing, err := a.queries.GetPaperByHash(ctx, hash)
    if err == nil {
        writeJSON(w, http.StatusOK, paperResponse(existing))
        return
    }
    if !errors.Is(err, pgx.ErrNoRows) {
        a.log.Error("dedup lookup failed", "err", err)
        writeError(w, http.StatusInternalServerError, "internal error")
        return
    }

    // 5. Store the raw PDF in object storage at raw/{hash}.pdf.
    rawKey := "raw/" + hash + ".pdf"
    _, err = a.minio.PutObject(ctx, a.cfg.minioBucket, rawKey,
        bytes.NewReader(data), int64(len(data)),
        minio.PutObjectOptions{ContentType: "application/pdf"})
    if err != nil {
        a.log.Error("object store put failed", "err", err)
        writeError(w, http.StatusInternalServerError, "failed to store file")
        return
    }

    // 6. Write the DB row AND enqueue the parse job in ONE transaction.
    id := newID()
    tx, err := a.db.Begin(ctx)
    if err != nil {
        a.log.Error("begin tx failed", "err", err)
        writeError(w, http.StatusInternalServerError, "internal error")
        return
    }
    defer tx.Rollback(ctx) // no-op if we already committed

    qtx := a.queries.WithTx(tx)
    paper, err := qtx.CreatePaper(ctx, store.CreatePaperParams{
        ID:          id,
        ContentHash: hash,
        SourceType:  "upload",
        SourceRef:   pgtype.Text{String: header.Filename, Valid: true},
        RawKey:      rawKey,
    })
    if err != nil {
        a.log.Error("insert paper failed", "err", err)
        writeError(w, http.StatusInternalServerError, "internal error")
        return
    }

    _, err = a.riverClient.InsertTx(ctx, tx, jobs.ParsePaperArgs{PaperID: id}, nil)
    if err != nil {
        a.log.Error("enqueue job failed", "err", err)
        writeError(w, http.StatusInternalServerError, "internal error")
        return
    }

    if err := tx.Commit(ctx); err != nil {
        a.log.Error("commit failed", "err", err)
        writeError(w, http.StatusInternalServerError, "internal error")
        return
    }

    a.log.Info("paper accepted", "paper_id", id, "filename", header.Filename)
    writeJSON(w, http.StatusAccepted, paperResponse(paper))
}

// ---- GET /papers/{id} ----

func (a *app) handleGetPaper(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    id := chi.URLParam(r, "id")

    paper, err := a.queries.GetPaper(ctx, id)
    if errors.Is(err, pgx.ErrNoRows) {
        writeError(w, http.StatusNotFound, "paper not found")
        return
    }
    if err != nil {
        a.log.Error("get paper failed", "err", err)
        writeError(w, http.StatusInternalServerError, "internal error")
        return
    }
    writeJSON(w, http.StatusOK, paperResponse(paper))
}

// ---- small helpers ----

func newID() string {
    b := make([]byte, 16)
    _, _ = rand.Read(b)
    return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(code)
    _ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
    writeJSON(w, code, map[string]string{"error": msg})
}

// ---- healthz (unchanged from Phase 1) ----

type depStatus struct {
    OK    bool   `json:"ok"`
    Error string `json:"error,omitempty"`
}

type healthResponse struct {
    Status string               `json:"status"`
    Deps   map[string]depStatus `json:"deps"`
}

func (a *app) handleHealthz(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
    defer cancel()

    deps := map[string]depStatus{
        "postgres": check(a.pingPostgres(ctx)),
        "grobid":   check(a.pingGrobid(ctx)),
        "minio":    check(a.pingMinio(ctx)),
    }

    allOK := true
    for _, d := range deps {
        if !d.OK {
            allOK = false
        }
    }

    resp := healthResponse{Status: "ok", Deps: deps}
    code := http.StatusOK
    if !allOK {
        resp.Status = "degraded"
        code = http.StatusServiceUnavailable
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(code)
    _ = json.NewEncoder(w).Encode(resp)
}

func check(err error) depStatus {
    if err != nil {
        return depStatus{OK: false, Error: err.Error()}
    }
    return depStatus{OK: true}
}

func (a *app) pingPostgres(ctx context.Context) error {
    return a.db.Ping(ctx)
}

func (a *app) pingGrobid(ctx context.Context) error {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.grobidURL+"/api/isalive", nil)
    if err != nil {
        return err
    }
    resp, err := a.http.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("grobid status %d", resp.StatusCode)
    }
    return nil
}

func (a *app) pingMinio(ctx context.Context) error {
    _, err := a.minio.BucketExists(ctx, a.cfg.minioBucket)
    return err
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()
            ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
            next.ServeHTTP(ww, r)
            log.Info("http request",
                "method", r.Method,
                "path", r.URL.Path,
                "status", ww.Status(),
                "duration_ms", time.Since(start).Milliseconds(),
                "request_id", middleware.GetReqID(r.Context()),
            )
        })
    }
}