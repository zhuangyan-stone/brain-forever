package theme

import (
	"net/http"
	"os"
)

// manifestPath is the path to the theme manifest file.
// It is relative to the server working directory.
const manifestPath = "./frontend/themes/manifest.json"

// Handler provides HTTP handlers for theme management.
type Handler struct{}

// NewHandler creates a new theme Handler.
// Deprecated: theme Handler no longer requires session manager or cookie name,
// as user theme operations have been moved to internal/user package.
// This function is kept for backward compatibility.
func NewHandler(_ ...interface{}) *Handler {
	return &Handler{}
}

// GetThemeMainfes handles GET /api/themes/mainfes.
// It reads the manifest.json file and returns its contents as JSON.
// No authentication required.
func (h *Handler) GetThemeMainfes(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		http.Error(w, `{"error":"cannot read theme manifest"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
