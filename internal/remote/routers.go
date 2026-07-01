package remote

import (
	"encoding/json"
	"net/http"

	"BrainForever/infra/httpx"
	"BrainForever/internal/remote/agent"
)

// InitRouters registers all API routes on the given server.
func InitRouters(srv *httpx.Server) {
	// Health check
	srv.GET("/api/health", handleHealth)

	// JSON trait extraction endpoint (POST)
	srv.POST("/api/traits", agent.OnTripTraits)

	// Portrait generation endpoint (POST, streaming SSE)
	srv.POST("/api/portrait", agent.OnTripPortrait)

	// Serve demo static files
	srv.Handle("/demo/", func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/demo/", http.FileServer(http.Dir("cmd/remote-server/demo"))).ServeHTTP(w, r)
	})

	// Catch-all for unimplemented endpoints
	srv.Handle("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/demo/", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "not found",
			"path":    r.URL.Path,
			"message": "remote-server — see /demo/ for the demo page",
		})
	})
}

// handleHealth responds with a simple health check JSON.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"server":  "remote-server",
		"version": "1.0.0",
	})
}
