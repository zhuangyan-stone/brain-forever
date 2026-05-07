package sse

import (
	"bufio"
	"io"
	"strings"
)

// ============================================================
// SSE decoder (Server-Sent Events)
// ============================================================

// SSEDecoder reads SSE streams line by line using bufio.Scanner
type SSEDecoder struct {
	scanner *bufio.Scanner
}

// NewSSEDecoder creates an SSE decoder
func NewSSEDecoder(r io.Reader) *SSEDecoder {
	return &SSEDecoder{
		scanner: bufio.NewScanner(r),
	}
}

// Decode reads the next SSE data line, returns content after "data: "
// The returned data has the "data: " prefix stripped.
// ok=false indicates the stream has ended.
func (d *SSEDecoder) Decode() (data string, ok bool) {
	for d.scanner.Scan() {
		line := d.scanner.Text()

		// Empty line indicates end of an event
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
