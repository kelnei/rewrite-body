package httputil

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/kelnei/rewrite-body/logger"
)

// failingResponseWriter is a ResponseWriter whose client has gone away.
type failingResponseWriter struct {
	header http.Header
}

func (writer *failingResponseWriter) Header() http.Header {
	return writer.header
}

func (writer *failingResponseWriter) WriteHeader(int) {}

func (writer *failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("broken pipe")
}

// captureOutput runs call with stdout and stderr redirected and returns what it
// wrote to them. The logger binds to os.Stdout and os.Stderr when it is created,
// so anything under test has to be built inside call.
func captureOutput(t *testing.T, call func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = writer, writer

	collected := make(chan string)

	go func() {
		output, _ := io.ReadAll(reader)
		collected <- string(output)
	}()

	defer func() {
		os.Stdout, os.Stderr = stdout, stderr
	}()

	call()

	os.Stdout, os.Stderr = stdout, stderr

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return <-collected
}

func TestLogHeadersOmitsSetCookie(t *testing.T) {
	const cookie = "session=s3cr3t-backend-cookie"

	output := captureOutput(t, func() {
		responseWriter := &failingResponseWriter{header: http.Header{}}
		responseWriter.Header().Set("Content-Type", "text/html")
		responseWriter.Header().Set("Content-Encoding", "gzip")
		responseWriter.Header().Add("Set-Cookie", cookie)

		wrapper := WrapWriter(
			context.Background(),
			responseWriter,
			defaultMonitoring,
			*logger.CreateLogger(logger.Debug),
			true,
		)

		// The write to the failed client is what logs the headers.
		wrapper.SetContent([]byte("bar is the new bar"), "gzip")
	})

	for _, expected := range []string{"Error Headers", `content-type="text/html"`, `content-encoding="gzip"`} {
		if !strings.Contains(output, expected) {
			t.Errorf("log does not contain %q:\n%s", expected, output)
		}
	}

	if strings.Contains(output, cookie) {
		t.Errorf("log contains the Set-Cookie value:\n%s", output)
	}
}
