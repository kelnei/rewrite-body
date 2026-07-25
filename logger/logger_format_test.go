package logger

import (
	"bytes"
	"strings"
	"testing"
)

// spyStringer records how many times it is formatted. fmt.Sprintf invokes
// String() for %v/%s, so a non-zero count proves formatting actually ran.
type spyStringer struct {
	calls *int
}

func (spy spyStringer) String() string {
	*spy.calls++

	return "formatted"
}

// logf calls the LogWriter's formatting method for the given level.
func logf(logger *LogWriter, level LogLevel, format string, a ...interface{}) {
	switch level {
	case Trace:
		logger.LogTracef(format, a...)
	case Debug:
		logger.LogDebugf(format, a...)
	case Info:
		logger.LogInfof(format, a...)
	case Warning:
		logger.LogWarningf(format, a...)
	case Error:
		logger.LogErrorf(format, a...)
	}
}

// TestFormattingSkippedWhenSuppressed guards the reason this fork exists: the
// *f methods must check the log level BEFORE formatting. Building the message
// eagerly (by passing fmt.Sprintf as an argument) meant a discarded log line
// still stringified the whole response body on every request.
func TestFormattingSkippedWhenSuppressed(t *testing.T) {
	levels := []struct {
		desc  string
		level LogLevel
	}{
		{desc: "Tracef", level: Trace},
		{desc: "Debugf", level: Debug},
		{desc: "Infof", level: Info},
		{desc: "Warningf", level: Warning},
	}

	for _, target := range levels {
		t.Run(target.desc+" suppressed at Error", func(t *testing.T) {
			var buffer bytes.Buffer

			calls := 0
			// Error is above every level under test, so nothing should be written.
			logger := createLoggerWithBuffer(Error, &buffer)

			logf(logger, target.level, "body: %v", spyStringer{calls: &calls})

			if calls != 0 {
				t.Errorf("argument was formatted %d time(s) for a suppressed %v message; want 0",
					calls, target.desc)
			}

			if buffer.Len() != 0 {
				t.Errorf("suppressed %v message was written: %q", target.desc, buffer.String())
			}
		})
	}
}

// TestFormattingRunsWhenEnabled is the other half: the level check must not
// swallow messages that should be written.
func TestFormattingRunsWhenEnabled(t *testing.T) {
	levels := []struct {
		desc  string
		level LogLevel
	}{
		{desc: "Tracef", level: Trace},
		{desc: "Debugf", level: Debug},
		{desc: "Infof", level: Info},
		{desc: "Warningf", level: Warning},
		{desc: "Errorf", level: Error},
	}

	for _, target := range levels {
		t.Run(target.desc+" enabled at Trace", func(t *testing.T) {
			var buffer bytes.Buffer

			calls := 0
			logger := createLoggerWithBuffer(Trace, &buffer)

			logf(logger, target.level, "body: %v", spyStringer{calls: &calls})

			if calls != 1 {
				t.Errorf("argument was formatted %d time(s) for an enabled %v message; want 1",
					calls, target.desc)
			}

			if !strings.Contains(buffer.String(), "body: formatted") {
				t.Errorf("enabled %v message missing or unformatted: %q", target.desc, buffer.String())
			}
		})
	}
}

// TestLogLinePrefix guards the separator that ends the prefix. log.Ldate writes
// the date immediately after it, so a prefix ending in the level name ran the
// two together and every line read "Rewrite-Body | DEBUG2026/07/24 20:31:41".
func TestLogLinePrefix(t *testing.T) {
	for level, name := range levelNames {
		t.Run(name, func(t *testing.T) {
			var buffer bytes.Buffer

			logger := createLoggerWithBuffer(Trace, &buffer)

			logf(logger, level, "a message")

			prefix := "Rewrite-Body | " + name + " | "
			if !strings.HasPrefix(buffer.String(), prefix) {
				t.Errorf("got %q, want it to start with %q", buffer.String(), prefix)
			}
		})
	}
}

// BenchmarkSuppressedLogDebugf documents the cost this fix removes. The handler
// logs the full response body twice per request, so this runs on every request
// through the middleware regardless of the configured level.
func BenchmarkSuppressedLogDebugf(b *testing.B) {
	body := strings.Repeat("<div>https://www.example.com/some/path</div>", 5000) // ~220 KB
	logger := createLoggerWithBuffer(Error, &bytes.Buffer{})

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		logger.LogDebugf("Response body: %s", body)
	}
}
