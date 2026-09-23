package main

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessLog(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	h := accessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("handler read body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(append([]byte("echo:"), body...))
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("ping"))
	h.ServeHTTP(httptest.NewRecorder(), req)

	log := buf.String()
	for _, want := range []string{`"body":"ping"`, `"body":"echo:ping"`, `"status":201`} {
		if !strings.Contains(log, want) {
			t.Errorf("log entry missing %s: %s", want, log)
		}
	}
}

func TestAccessLogDefaultsStatusOK(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	h := accessLog(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !strings.Contains(buf.String(), `"status":200`) {
		t.Errorf("expected default status 200: %s", buf.String())
	}
}

func TestLimitWriterTruncates(t *testing.T) {
	w := &limitWriter{max: 4}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got, want := w.String(), "hell…"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
