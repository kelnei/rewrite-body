// Package logger a package for handling writing content to logs.
package logger

import (
	"bytes"
	"fmt"
	"log"
	"os"
)

// LogLevel type definition of supported log levels.
type LogLevel int8

const (
	// Trace level of logs.
	Trace LogLevel = iota - 2
	// Debug level of logs.
	Debug
	// Info level of logs.
	Info
	// Warning level of logs.
	Warning
	// Error level of logs.
	Error
)

// LogWriter the struct used for writing logs.
type LogWriter struct {
	level   LogLevel
	loggers map[LogLevel]*log.Logger
}

// CreateLogger create the LogWriter struct with required content.
func CreateLogger(level LogLevel) *LogWriter {
	loggers := make(map[LogLevel]*log.Logger, Error-1)

	loggers[Trace] = log.New(os.Stdout, "Rewrite-Body | TRACE", log.Ldate|log.Ltime|log.Lshortfile)
	loggers[Debug] = log.New(os.Stdout, "Rewrite-Body | DEBUG", log.Ldate|log.Ltime|log.Lshortfile)
	loggers[Info] = log.New(os.Stdout, "Rewrite-Body | INFO", log.Ldate|log.Ltime|log.Lshortfile)
	loggers[Warning] = log.New(os.Stdout, "Rewrite-Body | WARNING", log.Ldate|log.Ltime|log.Lshortfile)
	loggers[Error] = log.New(os.Stderr, "Rewrite-Body | ERROR", log.Ldate|log.Ltime|log.Lshortfile)

	return &LogWriter{
		level:   level,
		loggers: loggers,
	}
}

func createLoggerWithBuffer(level LogLevel, buffer *bytes.Buffer) *LogWriter {
	loggers := make(map[LogLevel]*log.Logger, Error-1)

	loggers[Trace] = log.New(buffer, "Rewrite-Body | TRACE", log.Ldate|log.Ltime|log.Lshortfile)
	loggers[Debug] = log.New(buffer, "Rewrite-Body | DEBUG", log.Ldate|log.Ltime|log.Lshortfile)
	loggers[Info] = log.New(buffer, "Rewrite-Body | INFO", log.Ldate|log.Ltime|log.Lshortfile)
	loggers[Warning] = log.New(buffer, "Rewrite-Body | WARNING", log.Ldate|log.Ltime|log.Lshortfile)
	loggers[Error] = log.New(buffer, "Rewrite-Body | ERROR", log.Ldate|log.Ltime|log.Lshortfile)

	return &LogWriter{
		level:   level,
		loggers: loggers,
	}
}

func (logger *LogWriter) writeLog(level LogLevel, message string) {
	if level < logger.level {
		return
	}

	output := logger.loggers[level]
	output.Print(message)
}

// writeLogf formats and writes a log line, checking the level BEFORE formatting.
//
// The level check has to happen here rather than in writeLog: passing
// fmt.Sprintf(...) as an argument evaluates it unconditionally, so a discarded
// message still pays for building the string. That matters because this plugin
// logs whole response bodies (handler.ServeHTTP logs the body twice per
// request), so the eager form allocated roughly twice the page size on every
// request even with logging disabled — and Yaegi interprets rather than
// compiles this, which makes the wasted work more expensive still.
func (logger *LogWriter) writeLogf(level LogLevel, format string, a ...interface{}) {
	if level < logger.level {
		return
	}

	logger.writeLog(level, fmt.Sprintf(format, a...))
}

// LogTrace write Trace level logs.
func (logger *LogWriter) LogTrace(message string) {
	logger.writeLog(Trace, message)
}

// LogDebug write Debug level logs.
func (logger *LogWriter) LogDebug(message string) {
	logger.writeLog(Debug, message)
}

// LogInfo write Info level logs.
func (logger *LogWriter) LogInfo(message string) {
	logger.writeLog(Info, message)
}

// LogWarning write Warning level logs.
func (logger *LogWriter) LogWarning(message string) {
	logger.writeLog(Warning, message)
}

// LogError write Error level logs.
func (logger *LogWriter) LogError(message string) {
	logger.writeLog(Error, message)
}

// LogTracef write Trace level logs with formatting similar to fmt.Sprintf.
func (logger *LogWriter) LogTracef(format string, a ...interface{}) {
	logger.writeLogf(Trace, format, a...)
}

// LogDebugf write Debug level logs with formatting similar to fmt.Sprintf.
func (logger *LogWriter) LogDebugf(format string, a ...interface{}) {
	logger.writeLogf(Debug, format, a...)
}

// LogInfof write Info level logs with formatting similar to fmt.Sprintf.
func (logger *LogWriter) LogInfof(format string, a ...interface{}) {
	logger.writeLogf(Info, format, a...)
}

// LogWarningf write Warning level logs with formatting similar to fmt.Sprintf.
func (logger *LogWriter) LogWarningf(format string, a ...interface{}) {
	logger.writeLogf(Warning, format, a...)
}

// LogErrorf write Error level logs with formatting similar to fmt.Sprintf.
func (logger *LogWriter) LogErrorf(format string, a ...interface{}) {
	logger.writeLogf(Error, format, a...)
}
