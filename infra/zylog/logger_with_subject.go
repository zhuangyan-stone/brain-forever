package zylog

import "fmt"

// WrapWithSubject wraps a Logger with a subject prefix.
type slogWrapWithSubject struct {
	logger  Logger
	subject string
}

// WrapWithSubject wraps a Logger with a subject prefix for context tagging.
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

// SetLevel sets the log level.
func (l *slogWrapWithSubject) SetLevel(level Level) {
	l.logger.SetLevel(level)
}

// GetLevel returns the current log level.
func (l *slogWrapWithSubject) GetLevel() Level {
	return l.logger.GetLevel()
}

// Tracef logs a formatted Trace-level log.
func (l *slogWrapWithSubject) Tracef(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Trace(msg)
}

// Debugf logs a formatted Debug-level log.
func (l *slogWrapWithSubject) Debugf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Debug(msg)
}

// Infof logs a formatted Info-level log.
func (l *slogWrapWithSubject) Infof(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Info(msg)
}

// Warnf logs a formatted Warn-level log.
func (l *slogWrapWithSubject) Warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Warn(msg)
}

// Errorf logs a formatted Error-level log.
func (l *slogWrapWithSubject) Errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Error(msg)
}

// Fatalf logs a formatted Fatal-level log.
func (l *slogWrapWithSubject) Fatalf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Fatal(msg)
}
