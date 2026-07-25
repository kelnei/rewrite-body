package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/kelnei/rewrite-body/compressutil"
	"github.com/kelnei/rewrite-body/httputil"
)

func TestServeHTTP(t *testing.T) {
	tests := []struct {
		desc            string
		contentEncoding string
		contentType     string `default:"text/html"`
		rewrites        []Rewrite
		lastModified    bool
		resBody         string
		expResBody      string
		expLastModified bool
	}{
		{
			desc: "should replace foo by bar",
			rewrites: []Rewrite{
				{
					Regex:       "foo",
					Replacement: "bar",
				},
			},
			contentType: "text/html",
			resBody:     "foo is the new bar",
			expResBody:  "bar is the new bar",
		},
		{
			desc: "should replace foo by bar, then by foo",
			rewrites: []Rewrite{
				{
					Regex:       "foo",
					Replacement: "bar",
				},
				{
					Regex:       "bar",
					Replacement: "foo",
				},
			},
			contentType: "text/html",
			resBody:     "foo is the new bar",
			expResBody:  "foo is the new foo",
		},
		{
			desc: "should not replace anything if content encoding is not identity or empty",
			rewrites: []Rewrite{
				{
					Regex:       "foo",
					Replacement: "bar",
				},
			},
			contentEncoding: "other",
			contentType:     "text/html",
			resBody:         "foo is the new bar",
			expResBody:      "foo is the new bar",
		},
		{
			desc: "should not replace anything if content type does not contain text or is not empty",
			rewrites: []Rewrite{
				{
					Regex:       "foo",
					Replacement: "bar",
				},
			},
			contentType: "image",
			resBody:     "foo is the new bar",
			expResBody:  "foo is the new bar",
		},
		{
			desc: "should replace foo by bar if content encoding is identity",
			rewrites: []Rewrite{
				{
					Regex:       "foo",
					Replacement: "bar",
				},
			},
			contentEncoding: "identity",
			contentType:     "text/html",
			resBody:         "foo is the new bar",
			expResBody:      "bar is the new bar",
		},
		{
			desc: "should not remove the last modified header",
			rewrites: []Rewrite{
				{
					Regex:       "foo",
					Replacement: "bar",
				},
			},
			contentEncoding: "identity",
			contentType:     "text/html",
			lastModified:    true,
			resBody:         "foo is the new bar",
			expResBody:      "bar is the new bar",
			expLastModified: true,
		},
		{
			desc: "should support gzip encoding",
			rewrites: []Rewrite{
				{
					Regex:       "foo",
					Replacement: "bar",
				},
			},
			contentEncoding: "gzip",
			contentType:     "text/html",
			lastModified:    true,
			resBody:         compressString("foo is the new bar", "gzip"),
			expResBody:      compressString("bar is the new bar", "gzip"),
			expLastModified: true,
		},
		{
			desc: "should support deflate encoding",
			rewrites: []Rewrite{
				{
					Regex:       "foo",
					Replacement: "bar",
				},
			},
			contentEncoding: "deflate",
			contentType:     "text/html",
			lastModified:    true,
			resBody:         compressString("foo is the new bar", "deflate"),
			expResBody:      compressString("bar is the new bar", "deflate"),
			expLastModified: true,
		},
		{
			desc: "should ignore unsupported encoding",
			rewrites: []Rewrite{
				{
					Regex:       "foo",
					Replacement: "bar",
				},
			},
			contentEncoding: "br",
			contentType:     "text/html",
			lastModified:    true,
			resBody:         "foo is the new bar",
			expResBody:      "foo is the new bar",
			expLastModified: true,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			config := &Config{
				LastModified: test.lastModified,
				Rewrites:     test.rewrites,
				LogLevel:     -1,
			}

			next := func(responseWriter http.ResponseWriter, req *http.Request) {
				responseWriter.Header().Set("Content-Encoding", test.contentEncoding)
				responseWriter.Header().Set("Content-Type", test.contentType)
				responseWriter.Header().Set("Last-Modified", "Thu, 02 Jun 2016 06:01:08 GMT")
				responseWriter.Header().Set("Content-Length", strconv.Itoa(len(test.resBody)))
				responseWriter.WriteHeader(http.StatusOK)

				_, _ = fmt.Fprintf(responseWriter, test.resBody)
			}

			rewriteBody, err := New(context.Background(), http.HandlerFunc(next), config, "rewriteBody")
			if err != nil {
				t.Fatal(err)
			}

			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Accept", "text/html")

			rewriteBody.ServeHTTP(recorder, req)

			if _, exists := recorder.Result().Header["Last-Modified"]; exists != test.expLastModified {
				t.Errorf("got last-modified header %v, want %v", exists, test.expLastModified)
			}

			if _, exists := recorder.Result().Header["Content-Length"]; exists {
				t.Error("The Content-Length Header must be deleted")
			}

			if !bytes.Equal([]byte(test.expResBody), recorder.Body.Bytes()) {
				t.Errorf("got body: %v\n wanted: %v", recorder.Body.Bytes(), []byte(test.expResBody))
			}
		})
	}
}

func compressString(value string, encoding string) string {
	compressed, _ := compressutil.Encode([]byte(value), encoding)

	return string(compressed)
}

func TestAcceptHeaderGating(t *testing.T) {
	const resBody = "foo is the new bar"

	tests := []struct {
		desc        string
		accept      string
		contentType string
		expResBody  string
	}{
		{
			desc:        "should rewrite a fetch or XHR response",
			accept:      "*/*",
			contentType: "text/html; charset=utf-8",
			expResBody:  "bar is the new bar",
		},
		{
			desc:        "should rewrite when the request sends no Accept header",
			accept:      "",
			contentType: "text/html; charset=utf-8",
			expResBody:  "bar is the new bar",
		},
		{
			desc:        "should leave an unmonitored response type alone even when anything is accepted",
			accept:      "*/*",
			contentType: "application/json",
			expResBody:  resBody,
		},
		{
			desc:        "should skip a request that only wants an unmonitored type",
			accept:      "image/png",
			contentType: "text/html; charset=utf-8",
			expResBody:  resBody,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			config := &Config{
				Rewrites: []Rewrite{
					{
						Regex:       "foo",
						Replacement: "bar",
					},
				},
				LogLevel: 2,
			}

			next := func(responseWriter http.ResponseWriter, _ *http.Request) {
				responseWriter.Header().Set("Content-Type", test.contentType)
				responseWriter.WriteHeader(http.StatusOK)

				_, _ = responseWriter.Write([]byte(resBody))
			}

			rewriteBody, err := New(context.Background(), http.HandlerFunc(next), config, "rewriteBody")
			if err != nil {
				t.Fatal(err)
			}

			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)

			if test.accept != "" {
				req.Header.Set("Accept", test.accept)
			}

			rewriteBody.ServeHTTP(recorder, req)

			if recorder.Body.String() != test.expResBody {
				t.Errorf("got body %q, want %q", recorder.Body.String(), test.expResBody)
			}
		})
	}
}

func TestUndecodableBodyIsPassedThroughIntact(t *testing.T) {
	// A body that claims an encoding it does not actually use. The rewrite has to
	// be abandoned, but the client must still receive every byte the backend
	// sent -- decoding must not consume part of the buffered response on its way
	// to failing.
	resBody := "this is not gzip data at all, it is plain text"

	config := &Config{
		Rewrites: []Rewrite{
			{
				Regex:       "foo",
				Replacement: "bar",
			},
		},
		LogLevel: 2,
	}

	next := func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.Header().Set("Content-Encoding", compressutil.Gzip)
		responseWriter.Header().Set("Content-Type", "text/html")
		responseWriter.WriteHeader(http.StatusOK)

		_, _ = responseWriter.Write([]byte(resBody))
	}

	rewriteBody, err := New(context.Background(), http.HandlerFunc(next), config, "rewriteBody")
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")

	rewriteBody.ServeHTTP(recorder, req)

	if recorder.Body.String() != resBody {
		t.Errorf("got body %q, want the original %q", recorder.Body.String(), resBody)
	}
}

func TestPanicHandling(t *testing.T) {
	tests := []struct {
		desc       string
		panicValue interface{}
		expPanic   bool
	}{
		{
			desc:       "should re-panic http.ErrAbortHandler so net/http can abort the response",
			panicValue: http.ErrAbortHandler,
			expPanic:   true,
		},
		{
			desc:       "should re-panic an error wrapping http.ErrAbortHandler",
			panicValue: fmt.Errorf("upstream gave up: %w", http.ErrAbortHandler),
			expPanic:   true,
		},
		{
			desc:       "should recover an unrelated error",
			panicValue: errors.New("something unexpected"),
			expPanic:   false,
		},
		{
			desc:       "should recover a panic value that is not an error",
			panicValue: "something unexpected",
			expPanic:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			config := &Config{
				Rewrites: []Rewrite{
					{
						Regex:       "foo",
						Replacement: "bar",
					},
				},
				LogLevel: 2,
			}

			next := func(_ http.ResponseWriter, _ *http.Request) {
				panic(test.panicValue)
			}

			rewriteBody, err := New(context.Background(), http.HandlerFunc(next), config, "rewriteBody")
			if err != nil {
				t.Fatal(err)
			}

			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Accept", "text/html")

			recovery := catchPanic(func() {
				rewriteBody.ServeHTTP(recorder, req)
			})

			if !test.expPanic {
				if recovery != nil {
					t.Errorf("got propagated panic %v, want it recovered", recovery)
				}

				return
			}

			recoveredError, isError := recovery.(error)
			if !isError || !errors.Is(recoveredError, http.ErrAbortHandler) {
				t.Errorf("got panic %v, want an error matching http.ErrAbortHandler", recovery)
			}
		})
	}
}

// catchPanic runs call and returns the value it panicked with, or nil if it
// returned normally.
func catchPanic(call func()) (recovery interface{}) {
	defer func() {
		recovery = recover()
	}()

	call()

	return nil
}

func TestNew(t *testing.T) {
	tests := []struct {
		desc     string
		rewrites []Rewrite
		expErr   bool
	}{
		{
			desc: "should return no error",
			rewrites: []Rewrite{
				{
					Regex:       "foo",
					Replacement: "bar",
				},
				{
					Regex:       "bar",
					Replacement: "foo",
				},
			},
			expErr: false,
		},
		{
			desc: "should return an error",
			rewrites: []Rewrite{
				{
					Regex:       "*",
					Replacement: "bar",
				},
			},
			expErr: true,
		},
	}

	defaultMonitoring := httputil.MonitoringConfig{
		Types:   []string{"text/html"},
		Methods: []string{http.MethodGet},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			config := &Config{
				Rewrites:   test.rewrites,
				Monitoring: defaultMonitoring,
			}

			_, err := New(context.Background(), nil, config, "rewriteBody")
			if test.expErr && err == nil {
				t.Fatal("expected error on bad regexp format")
			}
		})
	}
}
