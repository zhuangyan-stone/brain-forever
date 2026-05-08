package sse

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// ============================================================
// SSEReader — reads SSE (Server-Sent Events) data lines
//
// SSEReader reads an SSE stream line by line using bufio.Scanner,
// strips the "data: " prefix, and unmarshals the JSON payload
// into the currentChunk field.
//
// Subtypes can embed SSEReader and override Next() to provide
// typed current values (e.g. ChatCompletionChunkDecoder in llm_raw).
// ============================================================

// Reader reads SSE data lines from an io.ReadCloser.
type Reader struct {
	scanner      *bufio.Scanner
	body         io.Closer
	currentChunk any
	err          error
	done         bool
}

// NewSSEReader creates an SSEReader from an io.ReadCloser.
func NewSSEReader(body io.ReadCloser) *Reader {
	return &Reader{
		scanner: bufio.NewScanner(body),
		body:    body,
	}
}

// Decode reads the next SSE data line, returns content after "data: ".
// The returned data has the "data: " prefix stripped.
// ok=false indicates the stream has ended.
func (r *Reader) Decode() (data string, ok bool) {
	for r.scanner.Scan() {
		line := r.scanner.Text()

		// Empty line — skip
		if line == "" {
			continue
		}

		// Only process data lines
		if strings.HasPrefix(line, "data: ") {
			return line[6:], true
		}
	}
	return "", false
}

// Next advances the stream to the next SSE data line, unmarshals its JSON
// payload into currentChunk (as any), and returns true.
// Returns false when the stream is exhausted ([DONE] signal, EOF, or scanner error).
// After Next returns false, call Err() to check for errors.
func (r *Reader) Next() bool {
	if r.done {
		return false
	}

	for r.scanner.Scan() {
		line := r.scanner.Text()

		// Skip empty lines
		if line == "" {
			continue
		}

		// Skip non-data lines (e.g., "event: ...")
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := line[6:] // Strip "data: " prefix

		// Check for the stream termination signal
		if data == "[DONE]" {
			r.done = true
			return false
		}

		// Unmarshal JSON into currentChunk
		var v any
		if err := json.Unmarshal([]byte(data), &v); err != nil {
			r.err = err
			r.done = true
			return false
		}
		r.currentChunk = v
		return true
	}

	// Check for scanner error
	if err := r.scanner.Err(); err != nil {
		r.err = err
	}
	r.done = true
	return false
}

// Current returns the most recently decoded value as any.
// The value is the result of json.Unmarshal (typically map[string]any).
func (r *Reader) Current() any {
	return r.currentChunk
}

// Err returns any error encountered during streaming.
func (r *Reader) Err() error {
	return r.err
}

// SetErr sets the error state and marks the stream as done.
// This is used by embedding types (e.g. ChatCompletionChunkDecoder)
// to signal an error before streaming begins.
func (r *Reader) SetErr(err error) {
	r.err = err
	r.done = true
}

// SetDone marks the stream as finished.
// This is used by embedding types to signal stream termination.
func (r *Reader) SetDone() {
	r.done = true
}

// Close closes the underlying response body.
func (r *Reader) Close() error {
	if r.body != nil {
		return r.body.Close()
	}
	return nil
}
