package logger

import (
	"BrainForever/infra/zylog"
	"log/slog"
)

var theLogger zylog.Logger

func TheLogger() zylog.Logger {
	if theLogger == nil {
		panic("TheLogger is nil")
	}

	return theLogger
}

// CreateTheLogger create the golbal logger
func CreateTheLogger(level zylog.Level, logFile string, lang zylog.Language, customLevelNames ...[]string) error {
	cfg := zylog.Config{
		Name:     "BrainForever",
		File:     logFile,
		MaxSize:  8,
		Console:  zylog.ConsoleModeColor,
		Language: lang,
		LevelColors: map[zylog.Level]zylog.ConsoleColorCode{
			zylog.LevelTrace: zylog.ColorGray,
			zylog.LevelDebug: zylog.ColorBlue,
			zylog.LevelInfo:  zylog.ColorGreen,
			zylog.LevelWarn:  zylog.ColorYellow,
			zylog.LevelError: zylog.ColorBoldMagenta,
			zylog.LevelFatal: zylog.ColorBoldRed,
		},
	}

	// If custom level names are provided, build the LevelNames map
	if len(customLevelNames) > 0 && len(customLevelNames[0]) > 0 {
		names := customLevelNames[0]
		cfg.LevelNames = make(map[slog.Level][2]string, len(names))
		// Map to standard levels: TRACE(-8), DEBUG(-4), INFO(0), WARN(4), ERROR(8), FATAL(12), OFF(999)
		standardLevels := []zylog.Level{zylog.LevelTrace, zylog.LevelDebug, zylog.LevelInfo, zylog.LevelWarn, zylog.LevelError, zylog.LevelFatal, zylog.LevelOff}
		levelStrs := []string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL", "OFF"}
		for i, lvl := range standardLevels {
			if i < len(names) {
				// index 0 = default name, index 1 = custom name
				cfg.LevelNames[slog.Level(lvl)] = [2]string{levelStrs[i], names[i]}
			}
		}
	}

	logger, err := zylog.NewLogger(cfg)
	if err != nil {
		return err
	}

	theLogger = logger
	return nil
}
