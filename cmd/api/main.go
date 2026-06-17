package main

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"
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
    cfg   config
    log   *slog.Logger
    db    *pgxpool.Pool
    minio *minio.Client
    http  *http.Client
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

    a := &app{
        cfg:   cfg,
        log:   log,
        db:    pool,
        minio: mc,
        http:  &http.Client{Timeout: 5 * time.Second},
    }

    r := chi.NewRouter()
    r.Use(middleware.RequestID)
    r.Use(middleware.Recoverer)
    r.Use(requestLogger(log))
    r.Get("/healthz", a.handleHealthz)

    srv := &http.Server{
        Addr:         cfg.addr,
        Handler:      r,
        ReadTimeout:  10 * time.Second,
        WriteTimeout: 15 * time.Second,
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