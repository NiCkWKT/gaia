package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"gaia/expr"
	"gaia/idgen"
	"gaia/notification"
	"gaia/store"
	"gaia/taskbroker"
	"log/slog"
	"os"
	"time"

	"github.com/BabySid/aether"
	"github.com/BabySid/aether/wire"
	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

type app struct {
	engine   *aether.Engine
	broker   *taskbroker.Broker
	redis    *redis.Client
	store    *store.Store
	telegram *notification.Telegram
}

func newApp(ctx context.Context, logger *slog.Logger) (_ *app, err error) {
	dsn := os.Getenv("GAIA_MYSQL_DSN")
	redisAddr := os.Getenv("GAIA_REDIS_ADDR")
	prefix := os.Getenv("GAIA_REDIS_PREFIX")
	token := os.Getenv("GAIA_TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("GAIA_TELEGRAM_CHAT_ID")
	if dsn == "" || redisAddr == "" || prefix == "" || token == "" || chatID == "" {
		return nil, errors.New("GAIA_MYSQL_DSN, GAIA_REDIS_ADDR, GAIA_REDIS_PREFIX, GAIA_TELEGRAM_BOT_TOKEN and GAIA_TELEGRAM_CHAT_ID are required")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid MySQL DSN: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if cfg.Params == nil {
		cfg.Params = make(map[string]string)
	}
	cfg.Params["time_zone"] = "'+00:00'"
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open MySQL: %w", err)
	}
	a := &app{store: store.New(db)}
	defer func() {
		if err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err = errors.Join(err, a.close(cleanupCtx))
		}
	}()
	if err = db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping MySQL: %w", err)
	}
	a.redis = redis.NewClient(&redis.Options{Addr: redisAddr})
	if err = a.redis.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping Redis: %w", err)
	}
	if _, err = db.ExecContext(ctx, "SELECT run_id FROM workflow_runs LIMIT 0"); err != nil {
		return nil, fmt.Errorf("verify Store schema (apply migrations before startup): %w", err)
	}
	a.telegram, err = notification.NewTelegram(token, chatID, logger)
	if err != nil {
		return nil, fmt.Errorf("configure Telegram: %w", err)
	}
	var engine *aether.Engine
	a.broker, err = taskbroker.NewBroker(a.redis, prefix,
		func(ctx context.Context, id string) { engine.OnTaskStarted(ctx, id) },
		func(ctx context.Context, result *wire.TaskResult) { engine.OnTaskCompleted(ctx, result) })
	if err != nil {
		return nil, err
	}
	engine, err = aether.New(aether.WithStore(a.store), aether.WithTaskBroker(a.broker), aether.WithIDGenerator(idgen.Generator{}), aether.WithExprEvaluator(expr.Evaluator{}), aether.WithHookNotifier(a.telegram), aether.WithErrorSink(a.telegram))
	if err != nil {
		return nil, err
	}
	a.engine = engine
	if err = engine.Start(ctx); err != nil {
		return nil, fmt.Errorf("start Engine: %w", err)
	}
	return a, nil
}

func (a *app) close(ctx context.Context) error {
	if a.engine != nil {
		a.engine.Stop()
	}
	var errs []error
	if a.broker != nil {
		errs = append(errs, a.broker.Close())
	}
	if a.store != nil {
		errs = append(errs, a.store.Close())
	}
	if a.redis != nil {
		if err := a.redis.Close(); err != nil && !errors.Is(err, redis.ErrClosed) {
			errs = append(errs, err)
		}
	}
	if a.telegram != nil {
		errs = append(errs, a.telegram.Close(ctx))
	}
	return errors.Join(errs...)
}
