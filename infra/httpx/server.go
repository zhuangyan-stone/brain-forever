package httpx

import (
	"BrainForever/infra/zylog"
	"context"
	"errors"
	"fmt"
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

// Handle registers a handler for the specified HTTP method and pattern
func (s *Server) Handle(method, pattern string, fn http.HandlerFunc) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		// Disable caching
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")

		fn(w, r)
	})
}

// HandleFunc registers a route without restricting the HTTP method;
// the handler determines allowed methods internally.
// Suitable for scenarios where a single path needs to handle multiple HTTP methods.
func (s *Server) HandleFunc(pattern string, fn http.HandlerFunc) {
	s.mux.HandleFunc(pattern, fn)
}

// GET registers a GET route
func (s *Server) GET(pattern string, fn http.HandlerFunc) {
	s.Handle(http.MethodGet, pattern, fn)
}

// POST registers a POST route
func (s *Server) POST(pattern string, fn http.HandlerFunc) {
	s.Handle(http.MethodPost, pattern, fn)
}

// PUT registers a PUT route
func (s *Server) PUT(pattern string, fn http.HandlerFunc) {
	s.Handle(http.MethodPut, pattern, fn)
}

// DELETE registers a DELETE route
func (s *Server) DELETE(pattern string, fn http.HandlerFunc) {
	s.Handle(http.MethodDelete, pattern, fn)
}

// HEAD registers a HEAD route
func (s *Server) HEAD(pattern string, fn http.HandlerFunc) {
	s.Handle(http.MethodHead, pattern, fn)
}

// Addr returns the listen address
func (s *Server) Addr() string {
	return fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
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

	go func() {
		defer s.wg.Done()
		s.logger.Infof("Starting, listening on %s", s.svc.Addr)

		if err := s.svc.ListenAndServe(); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				s.logger.Fatalf("Failed to start. %v", err)
			}
		}
	}()
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
