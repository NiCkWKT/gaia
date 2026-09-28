package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowHTTPRejectsInvalidRequest(t *testing.T) {
	h := newRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)
	cases := []struct {
		method, path, body, contentType string
		code                            int
	}{
		{"POST", "/workflow", `[]`, "application/json", 1001},
		{"POST", "/workflow", `null`, "application/json", 1001},
		{"POST", "/workflow", ` { } `, "application/json", 1001},
		{"POST", "/workflow", `{"kind":"Workflow"}`, "application/json", 1001},
		{"POST", "/workflow", `{"apiVersion":"aether/v1","kind":"Workflow","metadata":{},"spec":{},"bad":1}`, "application/json", 1001},
		{"POST", "/workflow", `{"apiVersion":"aether/v1","kind":"Workflow","metadata":{},"spec":{"bogus":true}}`, "application/json", 1001},
		{"POST", "/workflow", `{"apiVersion":"aether/v1","kind":"Workflow","metadata":[],"spec":{}}`, "application/json", 1001},
		{"POST", "/workflow", `{"apiVersion":"aether/v1","kind":"Workflow","metadata":{},"spec":{}} true`, "application/json", 1001},
		{"POST", "/workflow", `{}`, "text/plain", 1001},
		{"POST", "/workflow", strings.Repeat(" ", 1<<20) + `{}`, "application/json", 1001},
		{"GET", "/workflow", "", "", 1001},
		{"GET", "/workflow?workflow_run_id=a&workflow_run_id=b", "", "", 1001},
		{"GET", "/workflow?workflow_run_id=a", "x", "", 1001},
		{"POST", "/task/fetch", `{"workerID":"invalid"}`, "application/json", 1001},
		{"POST", "/task/start", `{"workerID":"worker"}`, "application/json", 1001},
		{"POST", "/task/complete", `{"taskRunID":"task"}`, "application/json", 1001},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path+tc.body[:min(12, len(tc.body))], func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			assertReply(t, w, tc.code, "invalid request")
		})
	}
}

func assertReply(t *testing.T, w *httptest.ResponseRecorder, code int, msg string) map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	var got struct {
		ErrNo  int            `json:"errNo"`
		ErrMsg string         `json:"errMsg"`
		Data   map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, code, got.ErrNo)
	assert.Equal(t, msg, got.ErrMsg)
	require.NotNil(t, got.Data)
	return got.Data
}

func TestStartupRequiresConfiguration(t *testing.T) {
	for _, name := range []string{"GAIA_MYSQL_DSN", "GAIA_REDIS_ADDR", "GAIA_REDIS_PREFIX", "GAIA_TELEGRAM_BOT_TOKEN", "GAIA_TELEGRAM_CHAT_ID"} {
		t.Setenv(name, "")
	}
	_, err := newApp(t.Context(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GAIA_TELEGRAM_BOT_TOKEN")
}

func TestStartupRejectsUnavailableDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, name := range []string{"GAIA_MYSQL_DSN", "GAIA_REDIS_ADDR", "GAIA_REDIS_PREFIX", "GAIA_TELEGRAM_BOT_TOKEN", "GAIA_TELEGRAM_CHAT_ID"} {
		t.Setenv(name, "configured")
	}
	t.Setenv("GAIA_MYSQL_DSN", "root:pass@tcp(127.0.0.1:1)/gaia")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	app, err := newApp(ctx, logger)
	require.Nil(t, app)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ping MySQL")
}

func TestStartupRejectsUnavailableRedis(t *testing.T) {
	if os.Getenv("GAIA_TEST_MYSQL_DSN") == "" {
		t.Skip("set GAIA_TEST_MYSQL_DSN to an isolated MySQL database")
	}
	t.Setenv("GAIA_MYSQL_DSN", os.Getenv("GAIA_TEST_MYSQL_DSN"))
	t.Setenv("GAIA_REDIS_ADDR", "127.0.0.1:1")
	t.Setenv("GAIA_REDIS_PREFIX", "http-test")
	t.Setenv("GAIA_TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("GAIA_TELEGRAM_CHAT_ID", "123")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	app, err := newApp(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.Nil(t, app)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ping Redis")
}

func TestWorkflowHTTPHappyPath(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker required")
	}
	out, err := exec.Command("docker", "run", "-d", "--rm", "-e", "MYSQL_ROOT_PASSWORD=gaia-test", "-e", "MYSQL_DATABASE=gaia_http_test", "-p", "127.0.0.1::3306", "mysql:8.4").CombinedOutput()
	if err != nil {
		t.Skipf("isolated MySQL unavailable: %s: %v", out, err)
	}
	mysqlID := strings.TrimSpace(string(out))
	t.Cleanup(func() {
		output, stopErr := exec.Command("docker", "stop", mysqlID).CombinedOutput()
		assert.NoError(t, stopErr, string(output))
	})
	out, err = exec.Command("docker", "port", mysqlID, "3306/tcp").CombinedOutput()
	require.NoError(t, err)
	_, mysqlPort, err := net.SplitHostPort(strings.TrimSpace(string(out)))
	require.NoError(t, err)
	out, err = exec.Command("docker", "run", "-d", "--rm", "-p", "127.0.0.1::6379", "redis:7.2.16-alpine").CombinedOutput()
	require.NoError(t, err, string(out))
	redisID := strings.TrimSpace(string(out))
	t.Cleanup(func() {
		output, stopErr := exec.Command("docker", "stop", redisID).CombinedOutput()
		assert.NoError(t, stopErr, string(output))
	})
	out, err = exec.Command("docker", "port", redisID, "6379/tcp").CombinedOutput()
	require.NoError(t, err)
	_, redisPort, err := net.SplitHostPort(strings.TrimSpace(string(out)))
	require.NoError(t, err)
	dsn := fmt.Sprintf("root:gaia-test@tcp(127.0.0.1:%s)/gaia_http_test", mysqlPort)
	cfg, err := mysql.ParseDSN(dsn)
	require.NoError(t, err)
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	db, err := sql.Open("mysql", cfg.FormatDSN())
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })
	var pingErr error
	for range 120 {
		pingErr = db.PingContext(t.Context())
		if pingErr == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	require.NoError(t, pingErr)
	migration, err := os.ReadFile("store/migrations/000001_create_store.up.sql")
	require.NoError(t, err)
	for _, statement := range strings.Split(string(migration), ";") {
		if strings.TrimSpace(statement) != "" {
			_, err = db.ExecContext(t.Context(), statement)
			require.NoError(t, err)
		}
	}
	t.Setenv("GAIA_MYSQL_DSN", dsn)
	t.Setenv("GAIA_REDIS_ADDR", net.JoinHostPort("127.0.0.1", redisPort))
	t.Setenv("GAIA_REDIS_PREFIX", "http-test")
	t.Setenv("GAIA_TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("GAIA_TELEGRAM_CHAT_ID", "123")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := newApp(t.Context(), logger)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, app.close(context.Background())) })
	handler := newRouter(logger, app.engine, app.broker)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if method == "POST" {
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	assertReply(t, request("GET", "/workflow?workflow_run_id=missing", ""), 1003, "workflow not found")
	assertReply(t, request("POST", "/workflow", `{"apiVersion":"aether/v1","kind":"Workflow","metadata":{},"spec":{}}`), 1002, "invalid workflow")
	assert.Empty(t, assertReply(t, request("POST", "/task/fetch", `{"workerID":"v1::worker::echo"}`), 0, "succ"))
	submitted := assertReply(t, request("POST", "/workflow", `{"apiVersion":"aether/v1","kind":"Workflow","metadata":{"name":"greeting"},"spec":{"entrypoint":"greet","templates":[{"task":{"name":"greet","executor":{"type":"echo"}}}]}}`), 0, "succ")
	id, ok := submitted["workflowRunID"].(string)
	require.True(t, ok)
	require.NotEmpty(t, id)
	assignment := assertReply(t, request("POST", "/task/fetch", `{"workerID":"v1::worker::echo"}`), 0, "succ")
	taskID, ok := assignment["taskRunID"].(string)
	require.True(t, ok)
	require.NotEmpty(t, taskID)
	assert.Equal(t, id, assignment["workflowRunID"])
	assert.Empty(t, assertReply(t, request("POST", "/task/fetch", `{"workerID":"v1::worker::echo"}`), 0, "succ"))
	assertReply(t, request("POST", "/task/start", fmt.Sprintf(`{"workerID":"v1::worker::echo","taskRunID":%q}`, taskID)), 0, "succ")
	assertReply(t, request("POST", "/task/complete", fmt.Sprintf(`{"taskRunID":%q,"workflowRunID":%q,"parameters":[{"name":"text","type":"string","value":"hello"}]}`, taskID, id)), 0, "succ")
	execution := assertReply(t, request("GET", "/workflow?workflow_run_id="+id, ""), 0, "succ")
	assert.Equal(t, id, execution["runID"])
	assert.Equal(t, "Succeeded", execution["status"])
	tasks, ok := execution["tasks"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, tasks)
	task, ok := tasks[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Succeeded", task["status"])
	for _, tc := range []struct {
		name, result, phase string
	}{
		{"business-failure", `"code":2,"message":"rejected"`, "Failed"},
		{"suspended", `"code":1,"message":"waiting"`, "Suspended"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"apiVersion":"aether/v1","kind":"Workflow","metadata":{"name":%q},"spec":{"entrypoint":"greet","templates":[{"task":{"name":"greet","executor":{"type":"echo"}}}]}}`, tc.name)
			submitted := assertReply(t, request("POST", "/workflow", body), 0, "succ")
			id, ok := submitted["workflowRunID"].(string)
			require.True(t, ok)
			assignment := assertReply(t, request("POST", "/task/fetch", `{"workerID":"v1::worker::echo"}`), 0, "succ")
			taskID, ok := assignment["taskRunID"].(string)
			require.True(t, ok)
			assertReply(t, request("POST", "/task/start", fmt.Sprintf(`{"workerID":"v1::worker::echo","taskRunID":%q}`, taskID)), 0, "succ")
			assertReply(t, request("POST", "/task/complete", fmt.Sprintf(`{"taskRunID":%q,"workflowRunID":%q,%s}`, taskID, id, tc.result)), 0, "succ")
			execution := assertReply(t, request("GET", "/workflow?workflow_run_id="+id, ""), 0, "succ")
			tasks, ok := execution["tasks"].([]any)
			require.True(t, ok)
			require.NotEmpty(t, tasks)
			task, ok := tasks[0].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tc.phase, task["status"])
		})
	}
	assert.NoError(t, app.redis.Close())
	assertReply(t, request("POST", "/task/fetch", `{"workerID":"v1::worker::echo"}`), 2001, "dependency unavailable")
}
