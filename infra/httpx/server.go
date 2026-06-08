package httpx

import (
	"BrainForever/infra/zylog"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

type Config struct {
	Name string

	Host string
	Port uint16

	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

// Server is an API server based on HTTP protocol
type Server struct {
	name string

	wg       sync.WaitGroup
	shutdown chan struct{}

	svc        *http.Server
	mux        *http.ServeMux
	middleware func(http.Handler) http.Handler

	logger zylog.Logger
	cfg    Config
}

// NewServer creates an API server
func NewServer(cfg Config, logger zylog.Logger) *Server {
	return &Server{
		name: cfg.Name,

		shutdown: make(chan struct{}),

		svc: nil,
		mux: http.NewServeMux(),

		logger: zylog.WrapWithSubject(logger, cfg.Name),
		cfg:    cfg,
	}
}

func (s *Server) Name() string {
	return s.name
}

func (s *Server) Logger() zylog.Logger {
	return s.logger
}

// ServeHTTP makes APIServer satisfy the http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Handle registers a handler for the specified pattern.
// It sets common response headers (JSON content type, no caching) and
// invokes the handler without restricting the HTTP method.
func (s *Server) Handle(pattern string, fn http.HandlerFunc) {
	s.mux.HandleFunc(pattern, s.wrapHandler(fn))
}

// wrapHandler returns a handler func that delegates to fn.
// Each handler is responsible for setting its own Content-Type header.
// Static files served via http.FileServer auto-detect their Content-Type.
func (s *Server) wrapHandler(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fn(w, r)
	}
}

// registerMethod registers a handler restricted to the given HTTP method
// using Go 1.22+ enhanced routing (e.g. "GET /api/chat/title").
func (s *Server) registerMethod(method, pattern string, fn http.HandlerFunc) {
	fullPattern := method + " " + pattern
	s.mux.HandleFunc(fullPattern, s.wrapHandler(fn))
}

// GET registers a GET-only route (Go 1.22+ enhanced routing)
func (s *Server) GET(pattern string, fn http.HandlerFunc) {
	s.registerMethod(http.MethodGet, pattern, fn)
}

// POST registers a POST-only route (Go 1.22+ enhanced routing)
func (s *Server) POST(pattern string, fn http.HandlerFunc) {
	s.registerMethod(http.MethodPost, pattern, fn)
}

// PUT registers a PUT-only route (Go 1.22+ enhanced routing)
func (s *Server) PUT(pattern string, fn http.HandlerFunc) {
	s.registerMethod(http.MethodPut, pattern, fn)
}

// DELETE registers a DELETE-only route (Go 1.22+ enhanced routing)
func (s *Server) DELETE(pattern string, fn http.HandlerFunc) {
	s.registerMethod(http.MethodDelete, pattern, fn)
}

// HEAD registers a HEAD-only route (Go 1.22+ enhanced routing)
func (s *Server) HEAD(pattern string, fn http.HandlerFunc) {
	s.registerMethod(http.MethodHead, pattern, fn)
}

// Addr returns the listen address
func (s *Server) Addr() string {
	return net.JoinHostPort(s.cfg.Host, fmt.Sprintf("%d", s.cfg.Port))
}

// Use registers a middleware that wraps the HTTP handler chain at Start
func (s *Server) Use(mw func(http.Handler) http.Handler) {
	s.middleware = mw
}

// Start begins listening and starts serving
func (s *Server) Start() {
	handler := http.Handler(s)
	if s.middleware != nil {
		handler = s.middleware(handler)
	}

	s.svc = &http.Server{
		Addr:              s.Addr(),
		Handler:           handler,
		ReadTimeout:       s.cfg.ReadTimeout,
		WriteTimeout:      s.cfg.WriteTimeout,
		IdleTimeout:       s.cfg.IdleTimeout,
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
	}

	s.wg.Add(1)

	started := make(chan struct{})

	go func() {
		defer s.wg.Done()
		s.logger.Infof("Starting, listening on %s", s.svc.Addr)
		close(started)

		if err := s.svc.ListenAndServe(); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				s.logger.Fatalf("Failed to start. %v", err)
			}
		}
	}()

	<-started
}

// Stop stops the server
func (s *Server) Stop(reason string) {
	s.logger.Infof("%s, shutting down in 30s...", reason)

	ctx, cancle := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancle()

	s.logger.Info("Processing all pending requests...")

	if err := s.svc.Shutdown(ctx); err != nil {
		s.logger.Fatalf("Shutdown failed, will force exit. %v", err)
	} else {
		s.logger.Info("Gracefully exited")
	}
}
