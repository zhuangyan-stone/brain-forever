package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// ============================================================
// main — remote-server stub
//
// 当前为 Hello-World 级实现，仅提供健康检查端点。
// TODO: 后续扩展为接收 local-server 请求的后端 AI 服务。
// ============================================================

func main() {
	// ============================================================
	// Create a signal-aware context
	// ============================================================
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ============================================================
	// Setup routes
	// ============================================================
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"server":  "remote-server",
			"version": "1.0.0",
		})
	})

	// Catch-all for unimplemented endpoints
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "not found",
			"path":    r.URL.Path,
			"message": "remote-server stub — not yet implemented",
		})
	})

	// ============================================================
	// Start HTTP Server
	// ============================================================
	addr := ":9090"
	if envAddr := os.Getenv("REMOTE_ADDR"); envAddr != "" {
		addr = envAddr
	}

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// ============================================================
	// Graceful shutdown
	// ============================================================
	go func() {
		<-ctx.Done()
		fmt.Println("\nShutting down remote-server...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("server shutdown timed out or errored: %v", err)
			server.Close()
		}
	}()

	fmt.Printf("remote-server listening on: http://%s\n", addr)
	fmt.Println("press Ctrl+C to stop the server")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "server failed to start: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("remote-server shut down gracefully")
}
