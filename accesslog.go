package main

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// maxBodyLog caps how many bytes of a request or response body are logged.
const maxBodyLog = 4 << 10

// accessLog emits one structured entry per request, including capped bodies.
func accessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqBody := drainBody(r)

			respBody := &limitWriter{max: maxBodyLog}
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			ww.Tee(respBody)

			start := time.Now()
			next.ServeHTTP(ww, r)

			logger.LogAttrs(r.Context(), slog.LevelInfo, "access",
				slog.Group("request",
					slog.String("method", r.Method),
					slog.String("url", r.URL.String()),
					slog.String("remote_addr", r.RemoteAddr),
					slog.String("body", reqBody),
				),
				slog.Group("response",
					slog.Int("status", status(ww)),
					slog.Int("bytes", ww.BytesWritten()),
					slog.String("body", respBody.String()),
				),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			)
		})
	}
}

// status defaults to 200 when the handler wrote no explicit status.
func status(ww middleware.WrapResponseWriter) int {
	if code := ww.Status(); code != 0 {
		return code
	}
	return http.StatusOK
}

// drainBody reads up to maxBodyLog bytes and restores the body for the handler.
func drainBody(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyLog))
	r.Body = readCloser{io.MultiReader(bytes.NewReader(body), r.Body), r.Body}
	if err != nil {
		return ""
	}
	return truncate(string(body), len(body) == maxBodyLog)
}

// readCloser reattaches the original body so handlers can still close it.
type readCloser struct {
	io.Reader
	io.Closer
}

// limitWriter buffers response bytes up to max and silently discards the rest.
type limitWriter struct {
	buf bytes.Buffer
	max int
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if n := w.max - w.buf.Len(); n > 0 {
		w.buf.Write(p[:min(len(p), n)])
	}
	return len(p), nil
}

func (w *limitWriter) String() string {
	return truncate(w.buf.String(), w.buf.Len() == w.max)
}

func truncate(s string, capped bool) string {
	if capped {
		return s + "…"
	}
	return s
}
