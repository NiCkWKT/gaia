package main

import (
	"cmp"
	"context"
	"errors"
	"gaia/taskbroker"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/BabySid/aether"
	"github.com/go-chi/chi/v5"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := newApp(ctx, logger)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.close(shutdownCtx); err != nil {
			logger.Error("close dependencies", "err", err)
		}
	}()

	srv := &http.Server{
		Addr:              cmp.Or(os.Getenv("ADDR"), ":8080"),
		Handler:           newRouter(logger, app.engine, app.broker),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(listener) }()
	logger.Info("listening", "addr", listener.Addr().String())

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return errors.Join(err, srv.Shutdown(shutdownCtx))
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return errors.Join(err, srv.Close())
		}
		return nil
	}
}

func newRouter(logger *slog.Logger, engine *aether.Engine, broker *taskbroker.Broker) http.Handler {
	r := chi.NewRouter()
	r.Use(accessLog(logger))
	r.Get("/", handleHello)
	registerHTTP(r, logger, engine, broker)
	return r
}

func handleHello(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := w.Write([]byte("Hello, World!")); err != nil {
		slog.ErrorContext(r.Context(), "write response", "err", err)
	}
}
