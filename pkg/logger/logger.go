package logger

import (
	"log"
	"os"
)

var (
	debugEnabled bool
	infoLogger   *log.Logger
	errorLogger  *log.Logger
	debugLogger  *log.Logger
)

func init() {
	infoLogger = log.New(os.Stderr, "", 0)
	errorLogger = log.New(os.Stderr, "", 0)
	debugLogger = log.New(os.Stderr, "[DEBUG] ", 0)
}

// SetDebug enables or disables debug logging
func SetDebug(enabled bool) {
	debugEnabled = enabled
}

// Info logs an informational message
func Info(format string, args ...interface{}) {
	infoLogger.Printf(format, args...)
}

// Error logs an error message
func Error(format string, args ...interface{}) {
	errorLogger.Printf(format, args...)
}

// Debug logs a debug message if debug logging is enabled
func Debug(format string, args ...interface{}) {
	if debugEnabled {
		debugLogger.Printf(format, args...)
	}
}

// Infof is an alias for Info for consistency
func Infof(format string, args ...interface{}) {
	Info(format, args...)
}

// Errorf is an alias for Error for consistency
func Errorf(format string, args ...interface{}) {
	Error(format, args...)
}

// Debugf is an alias for Debug for consistency
func Debugf(format string, args ...interface{}) {
	Debug(format, args...)
}

// Fatal logs an error message and exits with status 1
func Fatal(format string, args ...interface{}) {
	errorLogger.Printf(format, args...)
	os.Exit(1)
}

// Fatalf is an alias for Fatal for consistency
func Fatalf(format string, args ...interface{}) {
	Fatal(format, args...)
}
