package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

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

func TestDebugLogOmitsRequestCredentials(t *testing.T) {
	const (
		authorization = "Bearer s3cr3t-authorization"
		cookie        = "session=s3cr3t-cookie"
		queryToken    = "s3cr3t-query-token"
		backendCookie = "session=s3cr3t-backend-cookie"
		remoteAddress = "192.0.2.1"
	)

	tests := []struct {
		desc    string
		method  string
		expLine string
	}{
		{
			desc:    "a request the plugin rewrites",
			method:  http.MethodGet,
			expLine: "Starting supported request",
		},
		{
			desc:    "a request the plugin leaves alone",
			method:  http.MethodPost,
			expLine: "Ignoring unsupported request",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			output := captureOutput(t, func() {
				next := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
					response.Header().Set("Content-Type", "text/html")
					response.Header().Add("Set-Cookie", backendCookie)

					_, _ = response.Write([]byte("foo"))
				})

				handler, err := New(context.Background(), next, &Config{LogLevel: -1}, "rewriteBody")
				if err != nil {
					t.Fatal(err)
				}

				req := httptest.NewRequest(test.method, "/account?token="+queryToken, nil)
				req.Header.Set("Accept", "text/html")
				req.Header.Set("Authorization", authorization)
				req.Header.Set("Cookie", cookie)

				handler.ServeHTTP(httptest.NewRecorder(), req)
			})

			for _, expected := range []string{test.expLine, `method="` + test.method + `"`, `path="/account"`} {
				if !strings.Contains(output, expected) {
					t.Errorf("log does not contain %q:\n%s", expected, output)
				}
			}

			for _, secret := range []string{authorization, cookie, queryToken, backendCookie, remoteAddress} {
				if strings.Contains(output, secret) {
					t.Errorf("log contains %q:\n%s", secret, output)
				}
			}
		})
	}
}
