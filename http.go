package main

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"gaia/store"
	"gaia/taskbroker"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/BabySid/aether"
	"github.com/BabySid/aether/model"
	aetherstore "github.com/BabySid/aether/store"
	"github.com/BabySid/aether/wire"
	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

const maxJSONBody = 1 << 20

type httpAPI struct {
	engine *aether.Engine
	broker *taskbroker.Broker
	logger *slog.Logger
}

type reply struct {
	ErrNo  int    `json:"errNo"`
	ErrMsg string `json:"errMsg"`
	Data   any    `json:"data"`
}

func registerHTTP(r chi.Router, logger *slog.Logger, engine *aether.Engine, broker *taskbroker.Broker) {
	api := httpAPI{engine: engine, broker: broker, logger: logger}
	r.Post("/workflow", api.submit)
	r.Get("/workflow", api.get)
	r.Post("/task/fetch", api.fetch)
	r.Post("/task/start", api.start)
	r.Post("/task/complete", api.complete)
}

func (a httpAPI) respond(w http.ResponseWriter, r *http.Request, code int, msg string, data any) {
	if data == nil {
		data = struct{}{}
	}
	body, err := json.Marshal(reply{code, msg, data})
	if err != nil {
		a.logger.ErrorContext(r.Context(), "encode response", "err", err)
		body = []byte(`{"errNo":2002,"errMsg":"internal error","data":{}}`)
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(append(body, '\n')); err != nil {
		a.logger.ErrorContext(r.Context(), "write response", "err", err)
	}
}

func (a httpAPI) failure(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.ErrorContext(r.Context(), "request failed", "err", err)
	switch {
	case errors.Is(err, aether.ErrValidation):
		a.respond(w, r, 1002, "invalid workflow", nil)
	case errors.Is(err, aetherstore.ErrNotFound):
		a.respond(w, r, 1003, "workflow not found", nil)
	case errors.Is(err, taskbroker.ErrInvalidArgument), errors.Is(err, store.ErrInvalidArgument):
		a.respond(w, r, 1001, "invalid request", nil)
	case errors.Is(err, redis.ErrClosed), errors.Is(err, sql.ErrConnDone), errors.Is(err, driver.ErrBadConn), errors.Is(err, redis.ErrPoolTimeout):
		a.respond(w, r, 2001, "dependency unavailable", nil)
	default:
		var netErr *net.OpError
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &netErr) || errors.As(err, &mysqlErr) || errors.Is(err, context.DeadlineExceeded) {
			a.respond(w, r, 2001, "dependency unavailable", nil)
		} else {
			a.respond(w, r, 2002, "internal error", nil)
		}
	}
}

func decodeRequest(w http.ResponseWriter, r *http.Request, dst any) error {
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(params) > 1 || len(params) == 1 && !strings.EqualFold(params["charset"], "utf-8") {
		return errors.New("invalid media type")
	}
	if r.ContentLength > maxJSONBody {
		return errors.New("JSON body exceeds limit")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	body, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(body) || len(bytes.TrimSpace(body)) < 2 || bytes.TrimSpace(body)[0] != '{' {
		return errors.New("invalid JSON body")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if object, ok := dst.(*map[string]json.RawMessage); ok && *object == nil {
		return errors.New("null JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func (a httpAPI) submit(w http.ResponseWriter, r *http.Request) {
	var raw map[string]json.RawMessage
	if err := decodeRequest(w, r, &raw); err != nil {
		a.respond(w, r, 1001, "invalid request", nil)
		return
	}
	for _, key := range []string{"apiVersion", "kind", "metadata", "spec"} {
		v, ok := raw[key]
		if !ok || len(v) == 0 || string(v) == "null" || key == "metadata" && !jsonObject(v) || key == "spec" && !jsonObject(v) {
			a.respond(w, r, 1001, "invalid request", nil)
			return
		}
	}
	var workflow model.Workflow
	if err := decodeRequestBody(raw, &workflow); err != nil || workflow.APIVersion != "aether/v1" || workflow.Kind != "Workflow" {
		a.respond(w, r, 1001, "invalid request", nil)
		return
	}
	id, err := a.engine.Submit(r.Context(), &workflow)
	if err != nil {
		a.failure(w, r, err)
		return
	}
	a.respond(w, r, 0, "succ", struct {
		WorkflowRunID string `json:"workflowRunID"`
	}{id})
}

func jsonObject(v []byte) bool {
	v = bytes.TrimSpace(v)
	return len(v) >= 2 && v[0] == '{' && v[len(v)-1] == '}'
}

func decodeRequestBody(raw map[string]json.RawMessage, dst any) error {
	body, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	return d.Decode(dst)
}

func (a httpAPI) get(w http.ResponseWriter, r *http.Request) {
	ids := r.URL.Query()["workflow_run_id"]
	if len(ids) != 1 || strings.TrimSpace(ids[0]) == "" || len(r.URL.Query()) != 1 {
		a.respond(w, r, 1001, "invalid request", nil)
		return
	}
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(body) != 0 {
			a.respond(w, r, 1001, "invalid request", nil)
			return
		}
	}
	execution, err := a.engine.Get(r.Context(), ids[0])
	if err != nil {
		a.failure(w, r, err)
		return
	}
	a.respond(w, r, 0, "succ", execution)
}

func validWorkerID(id string) bool {
	parts := strings.Split(id, "::")
	return len(parts) == 3 && parts[0] == "v1" && strings.TrimSpace(parts[1]) != "" && strings.TrimSpace(parts[2]) != ""
}

func (a httpAPI) fetch(w http.ResponseWriter, r *http.Request) {
	var input struct {
		WorkerID string `json:"workerID"`
	}
	if err := decodeRequest(w, r, &input); err != nil || !validWorkerID(input.WorkerID) {
		a.respond(w, r, 1001, "invalid request", nil)
		return
	}
	assignment, err := a.broker.FetchTask(r.Context(), input.WorkerID)
	if errors.Is(err, taskbroker.ErrNoTaskAvailable) {
		a.respond(w, r, 0, "succ", nil)
		return
	}
	if err != nil {
		a.failure(w, r, err)
		return
	}
	a.respond(w, r, 0, "succ", assignment)
}

func (a httpAPI) start(w http.ResponseWriter, r *http.Request) {
	var input struct {
		WorkerID  string `json:"workerID"`
		TaskRunID string `json:"taskRunID"`
	}
	if err := decodeRequest(w, r, &input); err != nil || !validWorkerID(input.WorkerID) || strings.TrimSpace(input.TaskRunID) == "" {
		a.respond(w, r, 1001, "invalid request", nil)
		return
	}
	if err := a.broker.StartTask(r.Context(), input.TaskRunID, input.WorkerID); err != nil {
		a.failure(w, r, err)
		return
	}
	a.respond(w, r, 0, "succ", nil)
}

func (a httpAPI) complete(w http.ResponseWriter, r *http.Request) {
	var result wire.TaskResult
	if err := decodeRequest(w, r, &result); err != nil || strings.TrimSpace(result.TaskRunID) == "" || strings.TrimSpace(result.WorkflowRunID) == "" {
		a.respond(w, r, 1001, "invalid request", nil)
		return
	}
	if err := a.broker.CompleteTask(r.Context(), &result); err != nil {
		a.failure(w, r, err)
		return
	}
	a.respond(w, r, 0, "succ", nil)
}
