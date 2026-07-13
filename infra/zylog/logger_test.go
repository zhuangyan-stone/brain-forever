package zylog

import (
	"context"
	"log/slog"
	"sync"
	"testing"
)

func TestLogLevelEnum(t *testing.T) {
	// Test LogLevel enum values
	testCases := []struct {
		level    Level
		expected int
		name     string
	}{
		{LevelTrace, -8, "TRACE"},
		{LevelDebug, -4, "DEBUG"},
		{LevelInfo, 0, "INFO"},
		{LevelWarn, 4, "WARN"},
		{LevelError, 8, "ERROR"},
		{LevelFatal, 12, "FATAL"},
		{LevelOff, 999, "OFF"},
	}

	for _, tc := range testCases {
		if int(tc.level) != tc.expected {
			t.Errorf("LogLevel %s value is incorrect: expected %d, got %d", tc.name, tc.expected, tc.level)
		}
	}
}

func TestLevelConversion(t *testing.T) {
	// Test conversion between slog.Level and LogLevel
	testCases := []struct {
		logLevel  Level
		slogLevel slog.Level
	}{
		{LevelTrace, slog.Level(-8)},
		{LevelDebug, slog.LevelDebug},
		{LevelInfo, slog.LevelInfo},
		{LevelWarn, slog.LevelWarn},
		{LevelError, slog.LevelError},
		{LevelFatal, slog.Level(12)},
		{LevelOff, slog.Level(999)},
	}

	for _, tc := range testCases {
		// Test LogLevel -> slog.Level
		converted := logLevelToSlog(tc.logLevel)
		if converted != tc.slogLevel {
			t.Errorf("logLevelToSlog conversion error: input %v, expected %v, got %v", tc.logLevel, tc.slogLevel, converted)
		}

		// Test slog.Level -> LogLevel
		back := slogToLogLevel(tc.slogLevel)
		if back != tc.logLevel {
			t.Errorf("slogToLogLevel conversion error: input %v, expected %v, got %v", tc.slogLevel, tc.logLevel, back)
		}
	}
}

func TestSetGetLevel(t *testing.T) {
	// Create a test logger
	cfg := Config{
		Name:        "test",
		Level:       LevelInfo,
		File:        "", // No file output
		Console:     ConsoleModeNormal,
		Language:    LanguageEN,
		LevelColors: map[Level]string{},
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("failed to create logger. %v", err)
	}

	// Test getting the initial level
	initialLevel := logger.GetLevel()
	if initialLevel != LevelInfo {
		t.Errorf("initial level is incorrect: expected INFO(%d), got %v(%d)", LevelInfo, initialLevel, initialLevel)
	}

	// Test setting and getting DEBUG level
	logger.SetLevel(LevelDebug)
	debugLevel := logger.GetLevel()
	if debugLevel != LevelDebug {
		t.Errorf("DEBUG level set failed: expected DEBUG(%d), got %v(%d)", LevelDebug, debugLevel, debugLevel)
	}

	// Test setting and getting ERROR level
	logger.SetLevel(LevelError)
	errorLevel := logger.GetLevel()
	if errorLevel != LevelError {
		t.Errorf("ERROR level set failed: expected ERROR(%d), got %v(%d)", LevelError, errorLevel, errorLevel)
	}

	// Test setting and getting TRACE level
	logger.SetLevel(LevelTrace)
	traceLevel := logger.GetLevel()
	if traceLevel != LevelTrace {
		t.Errorf("TRACE level set failed: expected TRACE(%d), got %v(%d)", LevelTrace, traceLevel, traceLevel)
	}

	// Test setting and getting OFF level
	logger.SetLevel(LevelOff)
	offLevel := logger.GetLevel()
	if offLevel != LevelOff {
		t.Errorf("OFF level set failed: expected OFF(%d), got %v(%d)", LevelOff, offLevel, offLevel)
	}
}

func TestLevelAffectsLogging(t *testing.T) {
	// Since we cannot directly modify customHandler's writers, we create a simple test
	// Here we mainly test whether level settings affect the Enabled method

	// Create a customHandler for testing
	handler := &customHandler{
		cfg: Config{
			Level: LevelInfo,
		},
		mu: sync.RWMutex{},
	}

	// Test whether different levels are enabled
	testCases := []struct {
		level    slog.Level
		expected bool
		desc     string
	}{
		{slog.Level(-8), false, "TRACE should be filtered by INFO level"},
		{slog.LevelDebug, false, "DEBUG should be filtered by INFO level"},
		{slog.LevelInfo, true, "INFO should be enabled"},
		{slog.LevelWarn, true, "WARN should be enabled"},
		{slog.LevelError, true, "ERROR should be enabled"},
	}

	for _, tc := range testCases {
		enabled := handler.Enabled(context.Background(), tc.level)
		if enabled != tc.expected {
			t.Errorf("%s: level %v, expected %v, got %v", tc.desc, tc.level, tc.expected, enabled)
		}
	}

	// Change level to DEBUG
	handler.mu.Lock()
	handler.cfg.Level = LevelDebug
	handler.mu.Unlock()

	// Test again
	debugTestCases := []struct {
		level    slog.Level
		expected bool
		desc     string
	}{
		{slog.Level(-8), false, "TRACE should be filtered by DEBUG level"},
		{slog.LevelDebug, true, "DEBUG should be enabled"},
		{slog.LevelInfo, true, "INFO should be enabled"},
	}

	for _, tc := range debugTestCases {
		enabled := handler.Enabled(context.Background(), tc.level)
		if enabled != tc.expected {
			t.Errorf("After setting DEBUG level %s: level %v, expected %v, got %v", tc.desc, tc.level, tc.expected, enabled)
		}
	}
}

func TestConcurrentLevelAccess(t *testing.T) {
	// Test concurrent level access
	cfg := Config{
		Name:    "concurrent-test",
		Level:   LevelInfo,
		Console: ConsoleModeNormal,
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("failed to create logger. %v", err)
	}

	// Start multiple goroutines reading and writing the level simultaneously
	done := make(chan bool)
	iterations := 100

	// Writer goroutine
	go func() {
		for i := 0; i < iterations; i++ {
			level := Level(i % 7)
			switch level {
			case LevelTrace, LevelDebug, LevelInfo, LevelWarn, LevelError, LevelFatal, LevelOff:
				logger.SetLevel(level)
			}
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < iterations; i++ {
			_ = logger.GetLevel()
		}
		done <- true
	}()

	// Wait for both goroutines to finish
	<-done
	<-done

	// Ensure no panic occurred
	t.Log("Concurrent level access test completed, no panic")
}
