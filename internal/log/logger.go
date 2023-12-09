// Package log provides a global logger with configurable logging level. The intended use is for
// development builds.
package log

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Level for logging severity
type Level int

const (
	LevelNone    Level = iota // Disables logging.
	LevelError                // Logs anamolies that are not expected to occur during normal use.
	LevelWarning              // Logs anamolies that are expected to occur occasionally during normal use.
	LevelInfo                 // Logs major events.
	LevelDebug                // Logs detailed IO
)

var globalLogLevel Level
var logMutex sync.Mutex

var labels = map[Level]string{
	LevelDebug:   "[debug]",
	LevelInfo:    "[info ]",
	LevelWarning: "[warn ]",
	LevelError:   "[error]",
}

// SetLevel sets the global logging level.
func SetLevel(level Level) {
	logMutex.Lock()
	defer logMutex.Unlock()
	globalLogLevel = level
}

func logLevel() Level {
	logMutex.Lock()
	defer logMutex.Unlock()
	return globalLogLevel
}

func log(level Level, format string, a ...interface{}) {
	if level <= logLevel() {
		msg := fmt.Sprintf("%s %s ", time.Now().Format(time.RFC3339), labels[level])
		msg += fmt.Sprintf(format, a...)
		fmt.Fprintln(os.Stderr, msg)
	}
}

// Debug logs a message at the debug level.
func Debug(format string, a ...interface{}) {
	log(LevelDebug, format, a...)
}

// Info logs a message at the info level.
func Info(format string, a ...interface{}) {
	log(LevelInfo, format, a...)
}

// Warning logs a message at the warning level.
func Warning(format string, a ...interface{}) {
	log(LevelWarning, format, a...)
}

// Error logs a message at the error level.
func Error(format string, a ...interface{}) {
	log(LevelError, format, a...)
}
