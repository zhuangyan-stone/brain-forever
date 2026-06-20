package zylog

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const logTimeFormat = "2006/01/02 15:04:05.000"

// ConsoleMode defines the console output mode.
type ConsoleMode int

const (
	ConsoleModeNone   ConsoleMode = iota // No console output
	ConsoleModeNormal                    // Output to console, no colors
	ConsoleModeColor                     // Output to console, with colors
)

// Level is a custom log level enum.
type Level int

const (
	LevelTrace Level = -8  // TRACE
	LevelDebug Level = -4  // DEBUG
	LevelInfo  Level = 0   // INFO
	LevelWarn  Level = 4   // WARN
	LevelError Level = 8   // ERROR
	LevelFatal Level = 12  // FATAL
	LevelOff   Level = 999 // OFF - completely disable logging
)

// Language defines the log language type.
type Language int

const (
	LanguageEN     Language = 0 // English
	LanguageCustom Language = 1 // Custom language
)

// ConsoleColorCode is a type alias for ANSI console color codes.
type ConsoleColorCode = string

// Config holds the log configuration.
type Config struct {
	Name string // Logger name

	Level    Level       // Log level
	File     string      // Log file path
	MaxSize  int         // Max file size in MB
	Console  ConsoleMode // Console output mode
	Language Language    // Log language

	// Mapping from level to color codes
	LevelColors map[Level]ConsoleColorCode

	// LevelNames maps slog.Level to [2]string where index 0 is the primary name and
	// index 1 is the custom-language name (used when Language == LanguageCustom).
	// If nil, defaultLevelNames() is used.
	LevelNames map[slog.Level][2]string
}

// ConsoleModeFromStr converts a string console mode to ConsoleMode.
func ConsoleModeFromStr(console string) ConsoleMode {
	switch console {
	case "none":
		return ConsoleModeNone
	case "normal", "":
		return ConsoleModeNormal
	case "color":
		return ConsoleModeColor
	default:
		return ConsoleModeNormal
	}
}

// LanguageFromStr converts a string language to Language.
func LanguageFromStr(language string) Language {
	switch language {
	case "custom":
		return LanguageCustom
	case "en", "":
		return LanguageEN
	default:
		return LanguageEN
	}
}

// defaultLevelNames returns the default level name mapping.
// index 0 is the primary name, index 1 is the custom-language name.
func defaultLevelNames() map[slog.Level][2]string {
	return map[slog.Level][2]string{
		slog.Level(-8):  {"TRACE", "T"}, // TRACE
		slog.LevelDebug: {"DEBUG", "D"}, // DEBUG
		slog.LevelInfo:  {"INFO", "I"},  // INFO
		slog.LevelWarn:  {"WARN", "W"},  // WARN
		slog.LevelError: {"ERROR", "E"}, // ERROR
		slog.Level(12):  {"FATAL", "F"}, // FATAL
		slog.Level(999): {"OFF", "O"},   // OFF
	}
}

// NameToLevel converts a log level name (string, supports primary/custom names) to Level. Returns LevelOff if not found.
func NameToLevel(name string) Level {
	ln := defaultLevelNames()
	for k, v := range ln {
		for _, n := range v {
			if name == n {
				return Level(k)
			}
		}
	}

	return LevelOff
}

// Logger is the logging interface for the application.
type Logger interface {
	Trace(msg string, args ...any)
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	Fatal(msg string, args ...any)

	// Formatted logging methods
	Tracef(format string, args ...any)
	Debugf(format string, args ...any)
	Warnf(format string, args ...any)
	Infof(format string, args ...any)
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)

	// With returns a new Logger containing additional context.
	With(args ...any) Logger

	// SetLevel sets the log level (thread-safe).
	SetLevel(level Level)

	// GetLevel returns the current log level (thread-safe).
	GetLevel() Level
}

// slogWithName is a wrapper around slog.Logger.
type slogWithName struct {
	logger  *slog.Logger
	name    string         // Logger name
	handler *customHandler // Pointer to underlying handler for level operations
}

// getCustomHandler attempts to extract the customHandler from slog.Logger.
func getCustomHandler(logger *slog.Logger) *customHandler {
	handler := logger.Handler()
	if ch, ok := handler.(*customHandler); ok {
		return ch
	}
	return nil
}

// newSlogLoggerWithName creates a slogLogger with the given name.
func newSlogLoggerWithName(logger *slog.Logger, name string) Logger {
	return &slogWithName{
		logger:  logger,
		name:    name,
		handler: getCustomHandler(logger),
	}
}

func (l *slogWithName) Trace(msg string, args ...any) {
	// LevelTrace has value -8
	l.logger.Log(context.Background(), slog.Level(-8), msg, args...)
}

func (l *slogWithName) Debug(msg string, args ...any) {
	l.logger.Debug(msg, args...)
}

func (l *slogWithName) Info(msg string, args ...any) {
	l.logger.Info(msg, args...)
}

func (l *slogWithName) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)
}

func (l *slogWithName) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)
}

func (l *slogWithName) Fatal(msg string, args ...any) {
	// Fatal level is higher than Error, using Level(12)
	l.logger.Log(context.Background(), slog.Level(12), msg, args...)
}

func (l *slogWithName) With(args ...any) Logger {
	return &slogWithName{logger: l.logger.With(args...), handler: l.handler}
}

// SetLevel sets the log level.
func (l *slogWithName) SetLevel(level Level) {
	if l.handler == nil {
		return
	}
	l.handler.mu.Lock()
	defer l.handler.mu.Unlock()
	l.handler.cfg.Level = level
}

// GetLevel returns the current log level.
func (l *slogWithName) GetLevel() Level {
	if l.handler == nil {
		return LevelInfo // Default level
	}
	l.handler.mu.RLock()
	defer l.handler.mu.RUnlock()
	return l.handler.cfg.Level
}

// Tracef logs a formatted Trace-level log.
func (l *slogWithName) Tracef(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Trace(msg)
}

// Debugf logs a formatted Debug-level log.
func (l *slogWithName) Debugf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Debug(msg)
}

// Infof logs a formatted Info-level log.
func (l *slogWithName) Infof(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.logger.Info(msg)
}

// Warnf logs a formatted Warn-level log.
func (l *slogWithName) Warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Warn(msg)
}

// Errorf logs a formatted Error-level log.
func (l *slogWithName) Errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.logger.Error(msg)
}

// Fatalf logs a formatted Fatal-level log.
func (l *slogWithName) Fatalf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Fatal(msg)
}

// logLevelToSlog converts a custom LogLevel to slog.Level.
func logLevelToSlog(level Level) slog.Level {
	return slog.Level(level)
}

// slogToLogLevel converts slog.Level to a custom LogLevel.
func slogToLogLevel(level slog.Level) Level {
	return Level(level)
}

// ANSI color codes
// These constants are used to output different colors for log levels in the console.
// For example, ColorRed represents red, ColorGreen represents green, ColorBoldRed represents bold red.
// These escape sequences wrap the text when logging to achieve colored output.
// ColorReset is used to reset the color and prevent affecting subsequent output.
const (
	ColorReset ConsoleColorCode = "\033[0m" // Reset all attributes

	ColorBlack   = "\033[30m" // Black
	ColorRed     = "\033[31m" // Red
	ColorGreen   = "\033[32m" // Green
	ColorYellow  = "\033[33m" // Yellow
	ColorBlue    = "\033[34m" // Blue
	ColorMagenta = "\033[35m" // Magenta
	ColorCyan    = "\033[36m" // Cyan
	ColorWhite   = "\033[37m" // White
	ColorGray    = "\033[90m" // Gray (bright black)

	ColorBrightRed     = "\033[91m" // Bright red
	ColorBrightGreen   = "\033[92m" // Bright green
	ColorBrightYellow  = "\033[93m" // Bright yellow
	ColorBrightBlue    = "\033[94m" // Bright blue
	ColorBrightMagenta = "\033[95m" // Bright magenta
	ColorBrightCyan    = "\033[96m" // Bright cyan
	ColorBrightWhite   = "\033[97m" // Bright white

	ColorBoldRed     = "\033[1;31m" // Bold red
	ColorBoldGreen   = "\033[1;32m" // Bold green
	ColorBoldYellow  = "\033[1;33m" // Bold yellow
	ColorBoldBlue    = "\033[1;34m" // Bold blue
	ColorBoldMagenta = "\033[1;35m" // Bold magenta
	ColorBoldCyan    = "\033[1;36m" // Bold cyan
	ColorBoldWhite   = "\033[1;37m" // Bold white
)

// isTerminal checks whether the writer points to a terminal.
func isTerminal(w io.Writer) bool {
	// Try type assertion to *os.File
	if f, ok := w.(*os.File); ok {
		// Check if it's stdout or stderr
		return f == os.Stdout || f == os.Stderr
	}
	return false
}

// getLevelColor returns the color code for the given log level.
// If cfg is nil or LevelColors is empty, or no color is configured for the level, returns empty strings.
func getLevelColor(level slog.Level, cfg *Config) (colorCode, resetCode ConsoleColorCode) {
	resetCode = ColorReset

	// First check the color mapping in the config
	if cfg != nil && len(cfg.LevelColors) > 0 {
		// Convert slog.Level to LogLevel
		logLevel := Level(level)
		if colorCode, ok := cfg.LevelColors[logLevel]; ok && colorCode != "" {
			// LevelColors already stores ANSI color codes
			return colorCode, resetCode
		}
	}

	// If no color configured, return empty strings
	return "", ""
}

// customHandler is a custom log handler that supports name display.
// It now supports multiple writers and can handle colors individually for each writer.
type customHandler struct {
	handler            slog.Handler
	cfg                Config       // Log configuration
	terminalWriters    []io.Writer  // Terminal output targets (may support colors)
	nonTerminalWriters []io.Writer  // Non-terminal output targets (no color support)
	mu                 sync.RWMutex // Read-write mutex protecting runtime-configurable fields
}

func (h *customHandler) Enabled(ctx context.Context, level slog.Level) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// If the log level is OFF (999), always return false -do not output any logs
	slogLevel := logLevelToSlog(h.cfg.Level)
	if slogLevel == slog.Level(999) {
		return false
	}
	return level >= slogLevel
}

func (h *customHandler) Handle(ctx context.Context, r slog.Record) error {
	// Defensive check: if the log level is OFF, return immediately without processing any logs
	// Note: Normally the Enabled method already filters OFF-level logs; this is an extra safety measure
	slogLevel := logLevelToSlog(h.cfg.Level)
	if slogLevel == slog.Level(999) {
		return nil
	}

	// Format the timestamp
	timeStr := r.Time.Format(logTimeFormat)

	// Get the level string
	var levelStr string
	ln := h.cfg.LevelNames
	if ln == nil {
		ln = defaultLevelNames()
	}
	if names, ok := ln[r.Level]; ok {
		// Select the name based on language
		if h.cfg.Language == LanguageCustom {
			levelStr = names[1] // Custom-language name
		} else {
			levelStr = names[0] // Primary (English) name
		}
	} else {
		// Unknown level, use the default string representation
		levelStr = r.Level.String()
	}

	// Collect attributes
	var attrs []string
	r.Attrs(func(attr slog.Attr) bool {
		// Skip the "name" attribute since it's already shown in the prefix
		if attr.Key == "name" {
			return true
		}
		attrs = append(attrs, fmt.Sprintf("%s=%v", attr.Key, attr.Value.Any()))
		return true
	})

	// Build the message prefix (without color distinction for now)
	prefix := fmt.Sprintf("%s [%s]", timeStr, levelStr)
	// If the app name is set, prepend it
	if h.cfg.Name != "" {
		prefix = fmt.Sprintf("%s %s>", prefix, h.cfg.Name)
	}

	// Build the message body (message and attributes part)
	var body string
	if len(attrs) > 0 {
		body = fmt.Sprintf(" %s %s\n", r.Message, strings.Join(attrs, " "))
	} else {
		body = fmt.Sprintf(" %s\n", r.Message)
	}

	// Write to non-terminal output
	var firstErr error
	if len(h.nonTerminalWriters) > 0 {
		plainLogLine := prefix + body
		for _, w := range h.nonTerminalWriters {
			if _, err := w.Write([]byte(plainLogLine)); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	// Write to terminal output
	if len(h.terminalWriters) > 0 {
		colorCode, resetCode := getLevelColor(r.Level, &h.cfg)

		var coloredLogLine string

		if colorCode == "" {
			coloredLogLine = prefix + body
		} else {
			coloredLogLine = fmt.Sprintf("%s%s%s%s", colorCode, prefix, resetCode, body)
		}

		for _, w := range h.terminalWriters {
			if _, err := w.Write([]byte(coloredLogLine)); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

func (h *customHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Create a new base handler and add attributes
	newHandler := h.handler.WithAttrs(attrs)
	return &customHandler{
		handler:            newHandler,
		cfg:                h.cfg,
		terminalWriters:    h.terminalWriters,
		nonTerminalWriters: h.nonTerminalWriters,
	}
}

func (h *customHandler) WithGroup(name string) slog.Handler {
	newHandler := h.handler.WithGroup(name)
	return &customHandler{
		handler:            newHandler,
		cfg:                h.cfg,
		terminalWriters:    h.terminalWriters,
		nonTerminalWriters: h.nonTerminalWriters,
	}
}

// NewLogger creates a new logger based on the configuration.
// It supports multiple output targets:
// 1. If File is configured, output to file
// 2. If Console=true or no file output is configured, output to console
// 3. Can output to both file and console simultaneously (using io.MultiWriter)
func NewLogger(cfg Config) (Logger, error) {
	var writers []io.Writer

	// If file output is configured, add a file writer
	if cfg.File != "" {
		// Ensure the log directory exists
		dir := filepath.Dir(cfg.File)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create log directory: %w", err)
		}

		// Open the log file (append mode)
		file, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open log file: %w", err)
		}

		// If log rotation is configured, use the custom rotating writer
		if cfg.MaxSize > 0 {
			writers = append(writers, newRotatingWriter(file, cfg))
		} else {
			writers = append(writers, file)
		}
	}

	// cfg.Console is already of ConsoleMode type, no need to parse
	consoleMode := cfg.Console

	// If console output is configured, add a console writer
	if consoleMode != ConsoleModeNone || len(writers) == 0 {
		writers = append(writers, os.Stdout)
	}

	// Split writers into terminal and non-terminal categories
	var terminalWriters, nonTerminalWriters []io.Writer
	for _, w := range writers {
		if isTerminal(w) {
			terminalWriters = append(terminalWriters, w)
		} else {
			nonTerminalWriters = append(nonTerminalWriters, w)
		}
	}

	// Ensure LevelNames has defaults
	if cfg.LevelNames == nil {
		cfg.LevelNames = defaultLevelNames()
	}

	// Create a custom handler with the configuration
	customHandler := &customHandler{
		handler:            nil, // We no longer use a base handler
		cfg:                cfg,
		terminalWriters:    terminalWriters,
		nonTerminalWriters: nonTerminalWriters,
	}

	// Create the slog.Logger wrapper, passing the name
	logger := newSlogLoggerWithName(slog.New(customHandler), cfg.Name)

	ln := cfg.LevelNames
	levelName := ln[slog.Level(cfg.Level)][0] + "/" + ln[slog.Level(cfg.Level)][1]

	// Log the initialization info
	logger.Tracef("Logger created. Level=%s, ConsoleMode=%d, File=%s, MaxSize=%dM",
		levelName, cfg.Console, cfg.File, cfg.MaxSize)

	return logger, nil
}

// rotatingWriter implements simple log rotation.
type rotatingWriter struct {
	file        *os.File
	cfg         Config
	currentSize int64
}

func newRotatingWriter(file *os.File, cfg Config) io.Writer {
	info, err := file.Stat()
	if err != nil {
		return file
	}

	return &rotatingWriter{
		file:        file,
		cfg:         cfg,
		currentSize: info.Size(),
	}
}

func (w *rotatingWriter) Write(p []byte) (n int, err error) {
	// Check if rotation is needed
	if w.currentSize+int64(len(p)) > int64(w.cfg.MaxSize)*1024*1024 {
		if err := w.rotate(); err != nil {
			// If rotation fails, continue writing to the current file
			log.Printf("Log rotation failed: %v", err)
		}
	}

	n, err = w.file.Write(p)
	if err == nil {
		w.currentSize += int64(n)
	}
	return n, err
}

func (w *rotatingWriter) rotate() error {
	// Close the current file
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("failed to close log file: %w", err)
	}

	// Rename the current file
	timestamp := time.Now().Format("2006_01_02-15_04_05")
	backupPath := w.cfg.File + "." + timestamp
	if err := os.Rename(w.cfg.File, backupPath); err != nil {
		return fmt.Errorf("failed to rename log file: %w", err)
	}

	// Create a new file
	file, err := os.OpenFile(w.cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to create new log file: %w", err)
	}

	w.file = file
	w.currentSize = 0

	return nil
}

// Context-related functions
func FromContext(ctx context.Context, defaultLogger Logger) Logger {
	if logger, ok := ctx.Value(loggerKey).(Logger); ok {
		return logger
	}
	return defaultLogger
}

func WithContext(ctx context.Context, logger Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

type contextKey string

const loggerKey contextKey = "logger"
