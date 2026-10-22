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
		// expPassThrough marks a response the plugin cannot rewrite. Those are
		// forwarded as the backend sent them, headers included, so the header
		// expectations follow from it.
		expPassThrough bool
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
			// lastModified is false, but that only asks for the header to be dropped
			// from a body this plugin rewrites.
			expLastModified: true,
			expPassThrough:  true,
		},
		{
			desc: "should not replace anything if content type does not contain text or is not empty",
			rewrites: []Rewrite{
				{
					Regex:       "foo",
					Replacement: "bar",
				},
			},
			contentType:     "image",
			resBody:         "foo is the new bar",
			expResBody:      "foo is the new bar",
			expLastModified: true,
			expPassThrough:  true,
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
			expPassThrough:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			config := &Config{
				LastModified: test.lastModified,
				Rewrites:     test.rewrites,
				LogLevel:     -1,
			}

			next := func(responseWriter http.ResponseWriter, _ *http.Request) {
				responseWriter.Header().Set("Content-Encoding", test.contentEncoding)
				responseWriter.Header().Set("Content-Type", test.contentType)
				responseWriter.Header().Set("Last-Modified", "Thu, 02 Jun 2016 06:01:08 GMT")
				responseWriter.Header().Set("Content-Length", strconv.Itoa(len(test.resBody)))
				responseWriter.WriteHeader(http.StatusOK)

				_, _ = responseWriter.Write([]byte(test.resBody))
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

			// A rewritten body has a length of its own, which the final write
			// establishes. A forwarded one keeps the Content-Length that already
			// measures it, so the response does not become chunked for nothing.
			if _, exists := recorder.Result().Header["Content-Length"]; exists != test.expPassThrough {
				t.Errorf("got content-length header %v, want %v", exists, test.expPassThrough)
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

// fooToBar is the rewrite most of the tests below exercise.
var fooToBar = []Rewrite{{Regex: "foo", Replacement: "bar"}}

// newTestHandler builds the plugin around next, logging at Error so a test run
// stays readable.
func newTestHandler(t *testing.T, rewrites []Rewrite, next http.HandlerFunc) http.Handler {
	t.Helper()

	config := &Config{
		Rewrites: rewrites,
		LogLevel: 2,
	}

	rewriteBody, err := New(context.Background(), next, config, "rewriteBody")
	if err != nil {
		t.Fatal(err)
	}

	return rewriteBody
}

// serveGet sends a GET carrying the given Accept header, omitting the header
// entirely when accept is empty, and returns what the client would receive.
func serveGet(handler http.Handler, accept string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	handler.ServeHTTP(recorder, req)

	return recorder
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
			next := func(responseWriter http.ResponseWriter, _ *http.Request) {
				responseWriter.Header().Set("Content-Type", test.contentType)
				responseWriter.WriteHeader(http.StatusOK)

				_, _ = responseWriter.Write([]byte(resBody))
			}

			recorder := serveGet(newTestHandler(t, fooToBar, next), test.accept)

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

	next := func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.Header().Set("Content-Encoding", compressutil.Gzip)
		responseWriter.Header().Set("Content-Type", "text/html")
		responseWriter.WriteHeader(http.StatusOK)

		_, _ = responseWriter.Write([]byte(resBody))
	}

	recorder := serveGet(newTestHandler(t, fooToBar, next), "text/html")

	if recorder.Body.String() != resBody {
		t.Errorf("got body %q, want the original %q", recorder.Body.String(), resBody)
	}
}

func TestMatchSpanningWrites(t *testing.T) {
	// The backend may deliver a body across any number of writes, and with
	// Transfer-Encoding: chunked those boundaries are not ours to choose. The
	// whole body is buffered before any regex runs, so a match that straddles a
	// boundary still has to be found. (traefik/plugin-rewritebody#28)
	chunks := []string{"<html><body>foo ", "is the", " new bar</body>", "</html>"}

	rewrites := []Rewrite{{Regex: "foo is the new", Replacement: "bar is the new"}}

	next := func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.Header().Set("Content-Type", "text/html")
		responseWriter.WriteHeader(http.StatusOK)

		for _, chunk := range chunks {
			_, _ = responseWriter.Write([]byte(chunk))
		}
	}

	recorder := serveGet(newTestHandler(t, rewrites, next), "text/html")

	want := "<html><body>bar is the new bar</body></html>"
	if recorder.Body.String() != want {
		t.Errorf("got body %q, want %q", recorder.Body.String(), want)
	}
}

func TestRewritesAfterEarlyHints(t *testing.T) {
	// A backend that preloads assets answers with 103 and its link headers before
	// the response proper. Traefik forwards that interim response through the
	// middleware chain, so the plugin has to recognize it as a preamble and go on
	// waiting for the status and Content-Type that decide anything.
	next := func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.Header().Set("Link", "</style.css>; rel=preload")
		responseWriter.WriteHeader(http.StatusEarlyHints)
		responseWriter.Header().Del("Link")

		responseWriter.Header().Set("Content-Type", "text/html; charset=utf-8")
		responseWriter.WriteHeader(http.StatusOK)

		_, _ = responseWriter.Write([]byte("foo is the new bar"))
	}

	recorder := serveGet(newTestHandler(t, fooToBar, next), "text/html")

	if got := recorder.Body.String(); got != "bar is the new bar" {
		t.Errorf("got body %q, want it rewritten to %q", got, "bar is the new bar")
	}
}

func TestForwardedResponseStreamsWhileTheBackendWrites(t *testing.T) {
	// An Accept header cannot tell us a response will be unrewritable, so a
	// stream reaches the plugin like anything else -- here with no Accept at all,
	// the most open-ended case. It must still arrive at the client chunk by
	// chunk: a stream delivered only once the backend has finished is not a
	// stream, and one held in a buffer costs its whole length in memory.
	chunks := []string{"data: foo\n\n", "data: still foo\n\n"}

	recorder := httptest.NewRecorder()

	next := func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.Header().Set("Content-Type", "text/event-stream")
		responseWriter.WriteHeader(http.StatusOK)

		flusher, isFlusher := responseWriter.(http.Flusher)
		if !isFlusher {
			t.Fatalf("got %T, want an http.Flusher", responseWriter)
		}

		sent := ""

		for _, chunk := range chunks {
			if _, err := responseWriter.Write([]byte(chunk)); err != nil {
				t.Fatal(err)
			}

			flusher.Flush()

			sent += chunk

			if got := recorder.Body.String(); got != sent {
				t.Errorf("the client had %q part way through the stream, want %q", got, sent)
			}
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)

	newTestHandler(t, fooToBar, next).ServeHTTP(recorder, request)

	want := chunks[0] + chunks[1]
	if got := recorder.Body.String(); got != want {
		t.Errorf("got body %q, want it forwarded unchanged as %q", got, want)
	}
}

// TestDownstreamPanicPropagates pins that the plugin does not recover from a
// panic in the handler it wraps. Traefik's recovery middleware owns that:
// it answers 500 while nothing has been written, and aborts the response
// after, which is what the same request gets on a route without this plugin.
// Recovering here instead delivered a 200 with an empty body, and an
// abandoned response looked like a success.
func TestDownstreamPanicPropagates(t *testing.T) {
	tests := []struct {
		desc       string
		panicValue interface{}
	}{
		{
			desc:       "http.ErrAbortHandler",
			panicValue: http.ErrAbortHandler,
		},
		{
			desc:       "an error wrapping http.ErrAbortHandler",
			panicValue: fmt.Errorf("upstream gave up: %w", http.ErrAbortHandler),
		},
		{
			desc:       "an unrelated error",
			panicValue: errors.New("something unexpected"),
		},
		{
			desc:       "a value that is not an error",
			panicValue: "something unexpected",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			next := func(_ http.ResponseWriter, _ *http.Request) {
				panic(test.panicValue)
			}

			rewriteBody := newTestHandler(t, fooToBar, next)

			recovery := catchPanic(func() {
				serveGet(rewriteBody, "text/html")
			})

			// Compared as interfaces: the value has to arrive as the same value,
			// not as a copy or a wrapper, or the identity checks downstream fail.
			if recovery != test.panicValue {
				t.Errorf("got panic %v, want the downstream panic %v unchanged", recovery, test.panicValue)
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
