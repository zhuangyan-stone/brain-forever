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

// ConsoleMode 控制台输出模式
type ConsoleMode int

const (
	ConsoleModeNone   ConsoleMode = iota // 不输出到控制台
	ConsoleModeNormal                    // 输出到控制台，不带颜色
	ConsoleModeColor                     // 输出到控制台，带颜色
)

// Level 自定义日志级别枚举
type Level int

const (
	LevelTrace Level = -8  // TRACE
	LevelDebug Level = -4  // DEBUG
	LevelInfo  Level = 0   // INFO
	LevelWarn  Level = 4   // WARN
	LevelError Level = 8   // ERROR
	LevelFatal Level = 12  // FATAL
	LevelOff   Level = 999 // OFF - 完全不输出日志
)

// Language 日志语言类型
type Language int

const (
	LanguageEN Language = 0 // 英文
	LanguageZH Language = 1 // 中文
)

// ConsoleColorCode ANSI 控制台颜色代码的类型别名
type ConsoleColorCode = string

// Config 日志配置
type Config struct {
	Name string // 日志器名称

	Level    Level       // 日志级别
	File     string      // 日志文件路径
	MaxSize  int         // MB，日志文件最大大小
	Console  ConsoleMode // 控制台输出模式
	Language Language    // 日志语言

	// 级别到颜色代码的映射
	LevelColors map[Level]ConsoleColorCode
}

// ConsoleModeFromStr 将字符串控制台模式转换为 ConsoleMode
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

// LanguageFromStr 将字符串语言转换为 Language
func LanguageFromStr(language string) Language {
	switch language {
	case "zh":
		return LanguageZH // 1
	case "en", "":
		return LanguageEN // 0
	default:
		return LanguageEN // 0
	}
}

// levelNames 日志级别名称映射表，索引0为英文名称，索引1为中文名称
var levelNames = map[slog.Level][2]string{
	slog.Level(-8):  {"TRACE", "跟踪"}, // TRACE
	slog.LevelDebug: {"DEBUG", "调试"}, // DEBUG
	slog.LevelInfo:  {"INFO", "信息"},  // INFO
	slog.LevelWarn:  {"WARN", "警告"},  // WARN
	slog.LevelError: {"ERROR", "错误"}, // ERROR
	slog.Level(12):  {"FATAL", "失败"}, // FATAL
	slog.Level(999): {"OFF", "关闭"},   // OFF - 完全不输出日志
}

// NameToLevel 日志级别名字（字符串，支持中英）到日志级别，未找到时会返回 LevelOff
func NameToLevel(name string) Level {
	for k, v := range levelNames {
		for _, n := range v {
			if name == n {
				return Level(k)
			}
		}
	}

	return LevelOff
}

// Logger 是应用程序的日志接口
type Logger interface {
	Trace(msg string, args ...any)
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	Fatal(msg string, args ...any)

	// 格式化日志方法
	Tracef(format string, args ...any)
	Debugf(format string, args ...any)
	Warnf(format string, args ...any)
	Infof(format string, args ...any)
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)

	// With 方法返回一个新的 Logger，包含额外的上下文信息
	With(args ...any) Logger

	// SetLevel 设置日志级别（线程安全）
	SetLevel(level Level)

	// GetLevel 获取当前日志级别（线程安全）
	GetLevel() Level
}

// slogWithName 是 slog.Logger 的包装器
type slogWithName struct {
	logger  *slog.Logger
	name    string         // 日志器名称
	handler *customHandler // 指向底层处理器，用于级别操作
}

// getCustomHandler 尝试从 slog.Logger 中提取 customHandler
func getCustomHandler(logger *slog.Logger) *customHandler {
	handler := logger.Handler()
	if ch, ok := handler.(*customHandler); ok {
		return ch
	}
	return nil
}

// 创建带有名称的 slogLogger
func newSlogLoggerWithName(logger *slog.Logger, name string) Logger {
	return &slogWithName{
		logger:  logger,
		name:    name,
		handler: getCustomHandler(logger),
	}
}

func (l *slogWithName) Trace(msg string, args ...any) {
	// LevelTrace 的值为 -8
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
	// Fatal 级别比 Error 更高，使用 Level(12)
	l.logger.Log(context.Background(), slog.Level(12), msg, args...)
}

func (l *slogWithName) With(args ...any) Logger {
	return &slogWithName{logger: l.logger.With(args...), handler: l.handler}
}

// SetLevel 设置日志级别
func (l *slogWithName) SetLevel(level Level) {
	if l.handler == nil {
		return
	}
	l.handler.mu.Lock()
	defer l.handler.mu.Unlock()
	l.handler.cfg.Level = level
}

// GetLevel 获取当前日志级别
func (l *slogWithName) GetLevel() Level {
	if l.handler == nil {
		return LevelInfo // 默认级别
	}
	l.handler.mu.RLock()
	defer l.handler.mu.RUnlock()
	return l.handler.cfg.Level
}

// Tracef 使用格式化字符串记录Trace级别日志
func (l *slogWithName) Tracef(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Trace(msg)
}

// Debugf 使用格式化字符串记录Debug级别日志
func (l *slogWithName) Debugf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Debug(msg)
}

// Infof 使用格式化字符串记录Info级别日志
func (l *slogWithName) Infof(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.logger.Info(msg)
}

// Warnf 使用格式化字符串记录Warn级别日志
func (l *slogWithName) Warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Warn(msg)
}

// Errorf 使用格式化字符串记录Error级别日志
func (l *slogWithName) Errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.logger.Error(msg)
}

// Fatalf 使用格式化字符串记录Fatal级别日志
func (l *slogWithName) Fatalf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Fatal(msg)
}

// logLevelToSlog 将自定义 LogLevel 转换为 slog.Level
func logLevelToSlog(level Level) slog.Level {
	return slog.Level(level)
}

// slogToLogLevel 将 slog.Level 转换为自定义 LogLevel
func slogToLogLevel(level slog.Level) Level {
	return Level(level)
}

// ANSI 颜色代码
// 这些常量用于在控制台输出不同颜色的日志级别。
// 例如，ColorRed 表示红色，ColorGreen 表示绿色，ColorBoldRed 表示加粗的红色。
// 日志输出时会用这些转义序列包裹文本，实现彩色显示。
// ColorReset 用于重置颜色，防止影响后续输出。
const (
	ColorReset ConsoleColorCode = "\033[0m" // 重置所有属性

	ColorBlack   = "\033[30m" // 黑色
	ColorRed     = "\033[31m" // 红色
	ColorGreen   = "\033[32m" // 绿色
	ColorYellow  = "\033[33m" // 黄色
	ColorBlue    = "\033[34m" // 蓝色
	ColorMagenta = "\033[35m" // 洋红
	ColorCyan    = "\033[36m" // 青色
	ColorWhite   = "\033[37m" // 白色
	ColorGray    = "\033[90m" // 灰色（亮黑色）

	ColorBrightRed     = "\033[91m" // 亮红色
	ColorBrightGreen   = "\033[92m" // 亮绿色
	ColorBrightYellow  = "\033[93m" // 亮黄色
	ColorBrightBlue    = "\033[94m" // 亮蓝色
	ColorBrightMagenta = "\033[95m" // 亮洋红
	ColorBrightCyan    = "\033[96m" // 亮青色
	ColorBrightWhite   = "\033[97m" // 亮白色

	ColorBoldRed     = "\033[1;31m" // 加粗红色
	ColorBoldGreen   = "\033[1;32m" // 加粗绿色
	ColorBoldYellow  = "\033[1;33m" // 加粗黄色
	ColorBoldBlue    = "\033[1;34m" // 加粗蓝色
	ColorBoldMagenta = "\033[1;35m" // 加粗洋红
	ColorBoldCyan    = "\033[1;36m" // 加粗青色
	ColorBoldWhite   = "\033[1;37m" // 加粗白色
)

// isTerminal 检查 writer 是否指向终端
func isTerminal(w io.Writer) bool {
	// 尝试类型断言到 *os.File
	if f, ok := w.(*os.File); ok {
		// 检查是否是标准输出或标准错误
		return f == os.Stdout || f == os.Stderr
	}
	return false
}

// getLevelColor 根据日志级别返回对应的颜色代码
// 如果 cfg 为 nil 或 LevelColors 为空，或者对应级别没有配置颜色，则返回空字符串
func getLevelColor(level slog.Level, cfg *Config) (colorCode, resetCode ConsoleColorCode) {
	resetCode = ColorReset

	// 首先检查配置中的颜色映射
	if cfg != nil && len(cfg.LevelColors) > 0 {
		// 将 slog.Level 转换为 LogLevel
		logLevel := Level(level)
		if colorCode, ok := cfg.LevelColors[logLevel]; ok && colorCode != "" {
			// LevelColors 中存储的已经是 ANSI 颜色代码
			return colorCode, resetCode
		}
	}

	// 如果没有配置颜色，返回空字符串
	return "", ""
}

// customHandler 自定义日志处理器，支持名称显示
// 现在支持多个writers，可以为每个writer单独处理颜色
type customHandler struct {
	handler            slog.Handler
	cfg                Config       // 日志配置
	terminalWriters    []io.Writer  // 终端输出目标（可能支持颜色）
	nonTerminalWriters []io.Writer  // 非终端输出目标（不支持颜色）
	mu                 sync.RWMutex // 保护配置等运行期可配置字段的读写锁
}

func (h *customHandler) Enabled(ctx context.Context, level slog.Level) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// 如果日志级别设置为 OFF (999)，则始终返回 false，不输出任何日志
	slogLevel := logLevelToSlog(h.cfg.Level)
	if slogLevel == slog.Level(999) {
		return false
	}
	return level >= slogLevel
}

func (h *customHandler) Handle(ctx context.Context, r slog.Record) error {
	// 防御性检查：如果日志级别是 OFF，直接返回，不处理任何日志
	// 注意：正常情况下 Enabled 方法已经过滤了 OFF 级别的日志，这里是为了额外的安全
	slogLevel := logLevelToSlog(h.cfg.Level)
	if slogLevel == slog.Level(999) {
		return nil
	}

	// 格式化时间
	timeStr := r.Time.Format(logTimeFormat)

	// 获取级别字符串
	var levelStr string
	if names, ok := levelNames[r.Level]; ok {
		// 根据语言选择名称
		if h.cfg.Language == LanguageZH {
			levelStr = names[1] // 中文名称
		} else {
			levelStr = names[0] // 英文名称
		}
	} else {
		// 未知级别，使用默认字符串表示
		levelStr = r.Level.String()
	}

	// 收集属性
	var attrs []string
	r.Attrs(func(attr slog.Attr) bool {
		// 跳过 name 属性，因为已经在前缀中显示了
		if attr.Key == "name" {
			return true
		}
		attrs = append(attrs, fmt.Sprintf("%s=%v", attr.Key, attr.Value.Any()))
		return true
	})

	// 构造消息 前缀（此时先不区分有没有颜色）
	prefix := fmt.Sprintf("%s [%s]", timeStr, levelStr)
	// 有应用名字，加上名字
	if h.cfg.Name != "" {
		prefix = fmt.Sprintf("%s %s>", prefix, h.cfg.Name)
	}

	// 构建消息内容（消息和属性部分）
	var body string
	if len(attrs) > 0 {
		body = fmt.Sprintf(" %s %s\n", r.Message, strings.Join(attrs, " "))
	} else {
		body = fmt.Sprintf(" %s\n", r.Message)
	}

	// 写入非终端输出
	var firstErr error
	if len(h.nonTerminalWriters) > 0 {
		plainLogLine := prefix + body
		for _, w := range h.nonTerminalWriters {
			if _, err := w.Write([]byte(plainLogLine)); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	// 写入终端输出
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
	// 创建一个新的基础处理器并添加属性
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

// 根据配置创建新的日志器
// 支持多个输出目标：
// 1. 如果配置了 File，则输出到文件
// 2. 如果配置了 Console=true 或没有配置文件输出，则输出到控制台
// 3. 可以同时输出到文件和控制台（使用 io.MultiWriter）
func NewLogger(cfg Config) (Logger, error) {
	var writers []io.Writer

	// 如果配置了文件输出，则添加文件写入器
	if cfg.File != "" {
		// 确保日志目录存在
		dir := filepath.Dir(cfg.File)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("创建日志目录失败: %w", err)
		}

		// 打开日志文件（追加模式）
		file, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("打开日志文件失败: %w", err)
		}

		// 如果配置了日志轮转，使用自定义的轮转写入器
		if cfg.MaxSize > 0 {
			writers = append(writers, newRotatingWriter(file, cfg))
		} else {
			writers = append(writers, file)
		}
	}

	// cfg.Console 已经是 ConsoleMode 类型，无需解析
	consoleMode := cfg.Console

	// 如果配置了控制台输出，则添加控制台写入器
	if consoleMode != ConsoleModeNone || len(writers) == 0 {
		writers = append(writers, os.Stdout)
	}

	// 将writers分为终端和非终端两类
	var terminalWriters, nonTerminalWriters []io.Writer
	for _, w := range writers {
		if isTerminal(w) {
			terminalWriters = append(terminalWriters, w)
		} else {
			nonTerminalWriters = append(nonTerminalWriters, w)
		}
	}

	// 创建自定义处理器，包含配置
	customHandler := &customHandler{
		handler:            nil, // 我们不再使用基础处理器
		cfg:                cfg,
		terminalWriters:    terminalWriters,
		nonTerminalWriters: nonTerminalWriters,
	}

	// 创建 slog.Logger 的包装器，传递名称
	logger := newSlogLoggerWithName(slog.New(customHandler), cfg.Name)

	levelName := levelNames[slog.Level(cfg.Level)][0] + "/" + levelNames[slog.Level(cfg.Level)][1]

	// 记录日志初始化信息
	logger.Tracef("日志器创建成功。级别控制 %s, 终端模式 %d，日志文件 %s，文件限制 %d M",
		levelName, cfg.Console, cfg.File, cfg.MaxSize)

	return logger, nil
}

// rotatingWriter 实现简单的日志轮转
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
	// 检查是否需要轮转
	if w.currentSize+int64(len(p)) > int64(w.cfg.MaxSize)*1024*1024 {
		if err := w.rotate(); err != nil {
			// 如果轮转失败，继续写入当前文件
			log.Printf("日志轮转失败: %v", err)
		}
	}

	n, err = w.file.Write(p)
	if err == nil {
		w.currentSize += int64(n)
	}
	return n, err
}

func (w *rotatingWriter) rotate() error {
	// 关闭当前文件
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("关闭日志文件失败: %w", err)
	}

	// 重命名当前文件
	timestamp := time.Now().Format("2006_01_02-15_04_05")
	backupPath := w.cfg.File + "." + timestamp
	if err := os.Rename(w.cfg.File, backupPath); err != nil {
		return fmt.Errorf("重命名日志文件失败: %w", err)
	}

	// 创建新文件
	file, err := os.OpenFile(w.cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("创建新日志文件失败: %w", err)
	}

	w.file = file
	w.currentSize = 0

	return nil
}

// Context 相关函数
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
