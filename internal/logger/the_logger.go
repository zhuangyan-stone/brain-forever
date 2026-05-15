package logger

import "BrainForever/infra/zylog"

var theLogger zylog.Logger

func TheLogger() zylog.Logger {
	if theLogger == nil {
		panic("TheLogger is nil")
	}

	return theLogger
}

// CreateTheLogger create the golbal logger
func CreateTheLogger(level zylog.Level, logFile string, lang zylog.Language) error {
	cfg := zylog.Config{
		Name:     "BF",
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

	logger, err := zylog.NewLogger(cfg)
	if err != nil {
		return err
	}

	theLogger = logger
	return nil
}
