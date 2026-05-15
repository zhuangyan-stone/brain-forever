package zylog

import (
	"context"
	"log/slog"
	"sync"
	"testing"
)

func TestLogLevelEnum(t *testing.T) {
	// 测试 LogLevel 枚举值
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
			t.Errorf("LogLevel %s 的值不正确: 期望 %d, 实际 %d", tc.name, tc.expected, tc.level)
		}
	}
}

func TestLevelConversion(t *testing.T) {
	// 测试 slog.Level 和 LogLevel 之间的转换
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
		// 测试 LogLevel -> slog.Level
		converted := logLevelToSlog(tc.logLevel)
		if converted != tc.slogLevel {
			t.Errorf("logLevelToSlog 转换错误: 输入 %v, 期望 %v, 实际 %v", tc.logLevel, tc.slogLevel, converted)
		}

		// 测试 slog.Level -> LogLevel
		back := slogToLogLevel(tc.slogLevel)
		if back != tc.logLevel {
			t.Errorf("slogToLogLevel 转换错误: 输入 %v, 期望 %v, 实际 %v", tc.slogLevel, tc.logLevel, back)
		}
	}
}

func TestSetGetLevel(t *testing.T) {
	// 创建一个测试日志器
	cfg := Config{
		Name:        "test",
		Level:       LevelInfo,
		File:        "", // 不输出到文件
		Console:     ConsoleModeNormal,
		Language:    LanguageEN,
		LevelColors: map[Level]string{},
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("创建日志器失败: %v", err)
	}

	// 测试获取初始级别
	initialLevel := logger.GetLevel()
	if initialLevel != LevelInfo {
		t.Errorf("初始级别不正确: 期望 INFO(%d), 实际 %v(%d)", LevelInfo, initialLevel, initialLevel)
	}

	// 测试设置和获取 DEBUG 级别
	logger.SetLevel(LevelDebug)
	debugLevel := logger.GetLevel()
	if debugLevel != LevelDebug {
		t.Errorf("DEBUG 级别设置失败: 期望 DEBUG(%d), 实际 %v(%d)", LevelDebug, debugLevel, debugLevel)
	}

	// 测试设置和获取 ERROR 级别
	logger.SetLevel(LevelError)
	errorLevel := logger.GetLevel()
	if errorLevel != LevelError {
		t.Errorf("ERROR 级别设置失败: 期望 ERROR(%d), 实际 %v(%d)", LevelError, errorLevel, errorLevel)
	}

	// 测试设置和获取 TRACE 级别
	logger.SetLevel(LevelTrace)
	traceLevel := logger.GetLevel()
	if traceLevel != LevelTrace {
		t.Errorf("TRACE 级别设置失败: 期望 TRACE(%d), 实际 %v(%d)", LevelTrace, traceLevel, traceLevel)
	}

	// 测试设置和获取 OFF 级别
	logger.SetLevel(LevelOff)
	offLevel := logger.GetLevel()
	if offLevel != LevelOff {
		t.Errorf("OFF 级别设置失败: 期望 OFF(%d), 实际 %v(%d)", LevelOff, offLevel, offLevel)
	}
}

func TestLevelAffectsLogging(t *testing.T) {
	// 由于我们无法直接修改 customHandler 的 writers，我们需要创建一个简单的测试
	// 这里我们主要测试级别设置是否影响 Enabled 方法

	// 创建一个 customHandler 进行测试
	handler := &customHandler{
		cfg: Config{
			Level: LevelInfo,
		},
		mu: sync.RWMutex{},
	}

	// 测试不同级别是否被启用
	testCases := []struct {
		level    slog.Level
		expected bool
		desc     string
	}{
		{slog.Level(-8), false, "TRACE 应该被 INFO 级别过滤"},
		{slog.LevelDebug, false, "DEBUG 应该被 INFO 级别过滤"},
		{slog.LevelInfo, true, "INFO 应该被启用"},
		{slog.LevelWarn, true, "WARN 应该被启用"},
		{slog.LevelError, true, "ERROR 应该被启用"},
	}

	for _, tc := range testCases {
		enabled := handler.Enabled(context.Background(), tc.level)
		if enabled != tc.expected {
			t.Errorf("%s: 级别 %v, 期望 %v, 实际 %v", tc.desc, tc.level, tc.expected, enabled)
		}
	}

	// 修改级别为 DEBUG
	handler.mu.Lock()
	handler.cfg.Level = LevelDebug
	handler.mu.Unlock()

	// 再次测试
	debugTestCases := []struct {
		level    slog.Level
		expected bool
		desc     string
	}{
		{slog.Level(-8), false, "TRACE 应该被 DEBUG 级别过滤"},
		{slog.LevelDebug, true, "DEBUG 应该被启用"},
		{slog.LevelInfo, true, "INFO 应该被启用"},
	}

	for _, tc := range debugTestCases {
		enabled := handler.Enabled(context.Background(), tc.level)
		if enabled != tc.expected {
			t.Errorf("DEBUG级别设置后 %s: 级别 %v, 期望 %v, 实际 %v", tc.desc, tc.level, tc.expected, enabled)
		}
	}
}

func TestConcurrentLevelAccess(t *testing.T) {
	// 测试并发访问级别设置
	cfg := Config{
		Name:    "concurrent-test",
		Level:   LevelInfo,
		Console: ConsoleModeNormal,
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("创建日志器失败: %v", err)
	}

	// 启动多个 goroutine 同时读写级别
	done := make(chan bool)
	iterations := 100

	// 写入 goroutine
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

	// 读取 goroutine
	go func() {
		for i := 0; i < iterations; i++ {
			_ = logger.GetLevel()
		}
		done <- true
	}()

	// 等待两个 goroutine 完成
	<-done
	<-done

	// 确保没有 panic 发生
	t.Log("并发级别访问测试完成，无 panic")
}
