package toolset

import (
	"net/http"
)

// WriteError writes a plain-text error response with the given status code.
// The response body is the error message.
func WriteError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(msg))
}
