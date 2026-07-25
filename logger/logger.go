// Package logger a package for handling writing content to logs.
package logger

import (
	"bytes"
	"fmt"
	"io"
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

const logFlags = log.Ldate | log.Ltime | log.Lshortfile

// levelNames the label each level is written under.
var levelNames = map[LogLevel]string{
	Trace:   "TRACE",
	Debug:   "DEBUG",
	Info:    "INFO",
	Warning: "WARNING",
	Error:   "ERROR",
}

// newLoggers build one log.Logger per level, sending Error to errorOutput and
// everything else to output.
func newLoggers(output io.Writer, errorOutput io.Writer) map[LogLevel]*log.Logger {
	loggers := make(map[LogLevel]*log.Logger, len(levelNames))

	for level, name := range levelNames {
		levelOutput := output
		if level == Error {
			levelOutput = errorOutput
		}

		// The trailing separator matters. log.Ldate writes the date straight after
		// the prefix, so a prefix ending in the level name ran the two together as
		// "Rewrite-Body | DEBUG2026/07/24 20:31:41".
		loggers[level] = log.New(levelOutput, "Rewrite-Body | "+name+" | ", logFlags)
	}

	return loggers
}

// CreateLogger create the LogWriter struct with required content.
func CreateLogger(level LogLevel) *LogWriter {
	return &LogWriter{
		level:   level,
		loggers: newLoggers(os.Stdout, os.Stderr),
	}
}

func createLoggerWithBuffer(level LogLevel, buffer *bytes.Buffer) *LogWriter {
	return &LogWriter{
		level:   level,
		loggers: newLoggers(buffer, buffer),
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
