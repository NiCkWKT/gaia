package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/BabySid/aether/executor"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("worker stopped", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	base := cmp.Or(os.Getenv("GAIA_URL"), "http://localhost:8080")
	parsed, err := url.Parse(base)
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return fmt.Errorf("invalid GAIA_URL")
	}
	id := cmp.Or(os.Getenv("WORKER_ID"), "mock-worker")
	if strings.TrimSpace(id) != id || id == "" || strings.Contains(id, "::") {
		return errors.New("WORKER_ID must be a non-empty identifier without '::'")
	}
	concurrency := 1
	if raw := os.Getenv("WORKER_CONCURRENCY"); raw != "" {
		concurrency, err = strconv.Atoi(raw)
		if err != nil || concurrency <= 0 {
			return errors.New("WORKER_CONCURRENCY must be a positive integer")
		}
	}
	registry := executor.NewRegistry()
	if err := registry.Register(imageExecutor{}); err != nil {
		return err
	}
	if err := registry.Register(promptExecutor{}); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	w := worker{
		client:   client{baseURL: base, http: &http.Client{Timeout: 10 * time.Second}},
		registry: registry, id: id, concurrency: concurrency, interval: time.Second, logger: logger,
	}
	logger.Info("worker started", "id", id, "concurrencyPerExecutor", concurrency)
	w.run(ctx)
	return nil
}
