package main

import (
    "context"
    "log/slog"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/riverqueue/river"
    "github.com/riverqueue/river/riverdriver/riverpgxv5"

    "github.com/takamurayuki/vxpl/internal/jobs"
)

func main() {
    log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
    databaseURL := env("DATABASE_URL", "postgres://app:secret@localhost:5432/vxpl?sslmode=disable")

    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    pool, err := pgxpool.New(ctx, databaseURL)
    if err != nil {
        log.Error("failed to create db pool", "err", err)
        os.Exit(1)
    }
    defer pool.Close()

    workers := river.NewWorkers()
    river.AddWorker(workers, &jobs.ParsePaperWorker{Pool: pool, Log: log})

    client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
        Queues: map[string]river.QueueConfig{
            river.QueueDefault: {MaxWorkers: 5},
        },
        Workers: workers,
    })
    if err != nil {
        log.Error("failed to create river client", "err", err)
        os.Exit(1)
    }

    if err := client.Start(ctx); err != nil {
        log.Error("failed to start river client", "err", err)
        os.Exit(1)
    }
    log.Info("worker started")

    <-ctx.Done()
    log.Info("worker shutting down")

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    if err := client.Stop(shutdownCtx); err != nil {
        log.Error("river stop failed", "err", err)
    }
}

func env(key, def string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return def
}