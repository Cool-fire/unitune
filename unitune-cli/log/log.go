package log

import (
	"log/slog"
	"os"
)

var logger *slog.Logger

func init() {
	// Default to text handler for CLI-friendly output
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	})
	logger = slog.New(handler)
	slog.SetDefault(logger)
}

// SetLevel sets the logging level
func SetLevel(level slog.Level) {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	logger = slog.New(handler)
	slog.SetDefault(logger)
}

// SetJSON switches to JSON output format
func SetJSON() {
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger = slog.New(handler)
	slog.SetDefault(logger)
}

// Info logs an info message
func Info(msg string, args ...any) {
	logger.Info(msg, args...)
}

// Debug logs a debug message
func Debug(msg string, args ...any) {
	logger.Debug(msg, args...)
}

// Warn logs a warning message
func Warn(msg string, args ...any) {
	logger.Warn(msg, args...)
}

// Error logs an error message
func Error(msg string, args ...any) {
	logger.Error(msg, args...)
}

// Fatal logs an error message and exits
func Fatal(msg string, args ...any) {
	logger.Error(msg, args...)
	os.Exit(1)
}

// With returns a logger with additional attributes
func With(args ...any) *slog.Logger {
	return logger.With(args...)
}
