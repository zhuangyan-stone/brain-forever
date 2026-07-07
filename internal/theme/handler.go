package theme

import (
	"encoding/json"
	"net/http"
	"os"
)

// manifestPath is the path to the theme manifest file.
// It is relative to the server working directory.
const manifestPath = "./frontend/themes/manifest.json"

// Handler provides HTTP handlers for theme management (GET/POST /api/themes).
type Handler struct{}

// NewHandler creates a new theme Handler.
func NewHandler() *Handler {
	return &Handler{}
}

// GetThemes handles GET /api/themes.
// It reads the manifest.json file and returns its contents as JSON.
func (h *Handler) GetThemes(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		http.Error(w, `{"error":"cannot read theme manifest"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// SetThemes handles POST /api/themes.
// It updates the actived, actived-light, and actived-dark fields in manifest.json
// while preserving the existing theme list.
func (h *Handler) SetThemes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Actived      string `json:"actived"`
		ActivedLight string `json:"actived-light"`
		ActivedDark  string `json:"actived-dark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	// Read the existing manifest, preserving themes[]
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		http.Error(w, `{"error":"cannot read theme manifest"}`, http.StatusInternalServerError)
		return
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		http.Error(w, `{"error":"invalid manifest format"}`, http.StatusInternalServerError)
		return
	}

	m["actived"] = req.Actived
	m["actived-light"] = req.ActivedLight
	m["actived-dark"] = req.ActivedDark

	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		http.Error(w, `{"error":"failed to serialize manifest"}`, http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(manifestPath, out, 0644); err != nil {
		http.Error(w, `{"error":"failed to write theme manifest"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
