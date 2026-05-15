package zylog

import "fmt"

// 再包装一层主题
type slogWrapWithSubject struct {
	logger  Logger
	subject string
}

// WrapWithSubject 加上主题包装的日志记录器
func WrapWithSubject(src Logger, subject string) Logger {
	if subject == "" {
		return src
	}

	return &slogWrapWithSubject{
		logger:  src,
		subject: subject,
	}
}

func (l *slogWrapWithSubject) wrapMsgWithSubject(msg string) string {
	if l.subject != "" {
		return fmt.Sprintf("|%s| %s", l.subject, msg)
	} else {
		return msg
	}
}

func (l *slogWrapWithSubject) Trace(msg string, args ...any) {
	l.logger.Trace(l.wrapMsgWithSubject(msg), args...)
}

func (l *slogWrapWithSubject) Debug(msg string, args ...any) {
	l.logger.Debug(l.wrapMsgWithSubject(msg), args...)
}

func (l *slogWrapWithSubject) Info(msg string, args ...any) {
	l.logger.Info(l.wrapMsgWithSubject(msg), args...)
}

func (l *slogWrapWithSubject) Warn(msg string, args ...any) {
	l.logger.Warn(l.wrapMsgWithSubject(msg), args...)
}

func (l *slogWrapWithSubject) Error(msg string, args ...any) {
	l.logger.Error(l.wrapMsgWithSubject(msg), args...)
}

func (l *slogWrapWithSubject) Fatal(msg string, args ...any) {
	l.logger.Fatal(l.wrapMsgWithSubject(msg), args...)
}

func (l *slogWrapWithSubject) With(args ...any) Logger {
	return &slogWrapWithSubject{logger: l.logger.With(args...)}
}

// SetLevel 设置日志级别
func (l *slogWrapWithSubject) SetLevel(level Level) {
	l.logger.SetLevel(level)
}

// GetLevel 获取当前日志级别
func (l *slogWrapWithSubject) GetLevel() Level {
	return l.logger.GetLevel()
}

// Tracef 使用格式化字符串记录Trace级别日志
func (l *slogWrapWithSubject) Tracef(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Trace(msg)
}

// Debugf 使用格式化字符串记录Debug级别日志
func (l *slogWrapWithSubject) Debugf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Debug(msg)
}

// Infof 使用格式化字符串记录Info级别日志
func (l *slogWrapWithSubject) Infof(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Info(msg)
}

// Warnf 使用格式化字符串记录Warn级别日志
func (l *slogWrapWithSubject) Warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Warn(msg)
}

// Errorf 使用格式化字符串记录Error级别日志
func (l *slogWrapWithSubject) Errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Error(msg)
}

// Fatalf 使用格式化字符串记录Fatal级别日志
func (l *slogWrapWithSubject) Fatalf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Fatal(msg)
}
