package toolset

import (
	"encoding/json"
	"net/http"
)

// WriteJSONError writes a JSON error response with the given status code.
// The response body is {"error": msg}.
func WriteJSONError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
