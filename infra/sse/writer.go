package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// ============================================================
// SSE writer — wraps Server-Sent Events streaming writes
// ============================================================

// SSEWriter wraps SSE streaming writes, automatically sets response headers and supports Flush
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter creates an SSE writer
// Sets necessary SSE response headers and obtains the Flusher interface
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		// If ResponseWriter doesn't support Flush, can still write but without real-time push
		flusher = &noopFlusher{}
	}

	return &SSEWriter{w: w, flusher: flusher}
}

// WriteEvent serializes any value to JSON and writes it in SSE format
// Format: data: <json>\n\n
func (s *SSEWriter) WriteEvent(event any) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to serialize SSE event. %w", err)
	}

	_, err = fmt.Fprintf(s.w, "data: %s\n\n", data)
	if err != nil {
		return err
	}

	s.flusher.Flush()
	return nil
}

// WriteRaw writes raw SSE data lines (for special scenarios)
func (s *SSEWriter) WriteRaw(raw string) error {
	_, err := fmt.Fprintf(s.w, "data: %s\n\n", raw)
	if err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// noopFlusher is a no-op implementation when ResponseWriter doesn't support Flush
type noopFlusher struct{}

func (f *noopFlusher) Flush() {}
