package main

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccessLog(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	h := accessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err, "handler read body")
		w.WriteHeader(http.StatusCreated)
		_, err = w.Write(append([]byte("echo:"), body...))
		require.NoError(t, err, "handler write response")
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("ping"))
	h.ServeHTTP(httptest.NewRecorder(), req)

	log := buf.String()
	for _, want := range []string{`"body":"ping"`, `"body":"echo:ping"`, `"status":201`} {
		assert.Contains(t, log, want)
	}
}

func TestAccessLogDefaultsStatusOK(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	h := accessLog(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Contains(t, buf.String(), `"status":200`)
}

func TestLimitWriterTruncates(t *testing.T) {
	w := &limitWriter{max: 4}
	_, err := w.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, "hell…", w.String())
}
