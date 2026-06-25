package httpx

import (
	"BrainForever/infra/zylog"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
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

	// Charset is the default charset to append to response Content-Type headers
	// when no charset is specified (e.g., "utf-8"). If empty, no charset is added.
	Charset string
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
// If Config.Charset is set, the handler will automatically append
// "; charset=<charset>" to the Content-Type header when no charset is
// already specified.
func (s *Server) wrapHandler(fn http.HandlerFunc) http.HandlerFunc {
	charset := s.cfg.Charset
	if charset == "" {
		// No charset configured, pass through directly
		return func(w http.ResponseWriter, r *http.Request) {
			fn(w, r)
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		fn(&charsetResponseWriter{
			ResponseWriter: w,
			charset:        charset,
		}, r)
	}
}

// charsetResponseWriter wraps http.ResponseWriter to inject charset
// into the Content-Type header on first write, if no charset is set.
type charsetResponseWriter struct {
	http.ResponseWriter
	charset     string
	wroteHeader bool
}

// hasTextContentType reports whether the given MIME type supports charset.
// Binary types like image/*, audio/*, video/*, font/*, application/octet-stream,
// application/pdf, multipart/* should not have charset appended.
func hasTextContentType(ct string) bool {
	lower := strings.ToLower(ct)

	// XML-based and JSON-based structured formats support charset,
	// regardless of top-level media type (e.g. image/svg+xml).
	if strings.HasSuffix(lower, "+xml") || strings.HasSuffix(lower, "+json") {
		return true
	}

	switch {
	case strings.HasPrefix(lower, "text/"):
		return true
	case strings.HasPrefix(lower, "application/"):
		// Only specific text-based application subtypes support charset
		switch {
		case strings.Contains(lower, "json"),
			strings.Contains(lower, "xml"),
			strings.Contains(lower, "javascript"),
			strings.Contains(lower, "ecmascript"),
			strings.Contains(lower, "x-www-form-urlencoded"):
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func (w *charsetResponseWriter) WriteHeader(statusCode int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		ct := w.Header().Get("Content-Type")
		if ct != "" && !strings.Contains(strings.ToLower(ct), "charset=") && hasTextContentType(ct) {
			w.Header().Set("Content-Type", ct+"; charset="+w.charset)
		}
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *charsetResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Flush implements http.Flusher so that SSE streaming handlers
// (e.g. OnPortrait) can flush chunks to the client through this wrapper.
// It delegates to the underlying ResponseWriter's Flush method if available.
func (w *charsetResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
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
