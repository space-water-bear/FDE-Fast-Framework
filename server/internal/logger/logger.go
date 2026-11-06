package logger

import (
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Init 初始化 zap: 控制台 + 按天轮转的日志文件
func Init(level, director string, logInConsole bool) *zap.Logger {
	lvl := zapcore.InfoLevel
	switch strings.ToLower(level) {
	case "debug":
		lvl = zapcore.DebugLevel
	case "warn":
		lvl = zapcore.WarnLevel
	case "error":
		lvl = zapcore.ErrorLevel
	}

	enc := zapcore.NewConsoleEncoder(zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	})

	var cores []zapcore.Core
	if logInConsole {
		cores = append(cores, zapcore.NewCore(enc, zapcore.AddSync(os.Stdout), lvl))
	}
	if director != "" {
		if err := os.MkdirAll(director, 0o755); err == nil {
			fileWriter := &lumberjack.Logger{
				Filename:   filepath.Join(director, "server.log"),
				MaxSize:    100, // MB
				MaxBackups: 7,
				MaxAge:     30, // 天
				Compress:   true,
			}
			cores = append(cores, zapcore.NewCore(enc, zapcore.AddSync(fileWriter), lvl))
		}
	}
	return zap.New(zapcore.NewTee(cores...), zap.AddCaller(), zap.AddCallerSkip(1))
}
