package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"BrainForever/infra/httpx"
	"BrainForever/infra/i18n"
	"BrainForever/infra/zylog"
	"BrainForever/internal/remote"
)

// ============================================================
// main -remote-server trait extraction service
//
// Listens on :8088 and provides:
//   - GET  /api/health       -health check
//   - POST /api/traits       -trait extraction (JSON in/out)
//   - /demo/                 -static files (demo page)
// ============================================================

func main() {
	// ============================================================
	// Initialize i18n with remote language resources
	// ============================================================
	i18n.Init("lang/remote")

	// ============================================================
	// Create a signal-aware context
	// ============================================================
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ============================================================
	// Create the logger
	// ============================================================
	theLogger, err := zylog.NewLogger(zylog.Config{
		Name:    "remote-server",
		Level:   zylog.LevelInfo,
		Console: zylog.ConsoleModeColor,
	})
	if err != nil {
		log.Fatalf("create logger fail: %v", err)
	}

	// ============================================================
	// Setup routes using httpx.Server
	// ============================================================

	// Parse server address from environment variable
	host := "::"
	port := uint16(8088)

	if envAddr := os.Getenv("REMOTE_ADDR"); envAddr != "" {
		if h, p, err := net.SplitHostPort(envAddr); err == nil {
			if h != "" {
				host = h
			}
			if pn, err := strconv.ParseUint(p, 10, 16); err == nil {
				port = uint16(pn)
			}
		}
	}

	srv := httpx.NewServer(httpx.Config{
		Name:              "remote-server",
		Host:              host,
		Port:              port,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0, // 0 = disabled -trait extraction may take time
		IdleTimeout:       60 * time.Second,
		Charset:           "utf-8",
	}, theLogger)

	// CORS middleware
	srv.Use(httpx.UseCORSMiddleware)

	// Initialize all API routes
	remote.InitRouters(srv)

	// ============================================================
	// Start server & wait for shutdown signal
	// ============================================================

	srv.Start()
	theLogger.Infof("demo page: http://%s/demo/", srv.Addr())
	theLogger.Infof("press Ctrl+C to stop the server")

	<-ctx.Done()
	theLogger.Info("Shutting down remote-server...")
	srv.Stop("received shutdown signal")
	theLogger.Info("remote-server shut down gracefully")
}
