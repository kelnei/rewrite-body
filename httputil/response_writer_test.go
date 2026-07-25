package httputil

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kelnei/rewrite-body/compressutil"
	"github.com/kelnei/rewrite-body/logger"
)

func newTestWrapper(responseWriter http.ResponseWriter) *ResponseWrapper {
	return newTestWrapperWithContext(context.Background(), responseWriter)
}

func newTestWrapperWithContext(
	requestContext context.Context,
	responseWriter http.ResponseWriter,
) *ResponseWrapper {
	return WrapWriter(requestContext, responseWriter, defaultMonitoring, *logger.CreateLogger(logger.Error), true)
}

func TestSetContent(t *testing.T) {
	const body = "bar is the new bar"

	tests := []struct {
		desc     string
		encoding string
	}{
		{
			desc:     "should write an unencoded body when no encoding is set",
			encoding: "",
		},
		{
			desc:     "should write an unencoded body for identity",
			encoding: compressutil.Identity,
		},
		{
			desc:     "should write a gzip encoded body",
			encoding: compressutil.Gzip,
		},
		{
			desc:     "should write a deflate encoded body",
			encoding: compressutil.Deflate,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			wrapper := newTestWrapper(recorder)
			wrapper.Header().Set("Content-Encoding", test.encoding)

			wrapper.SetContent([]byte(body), test.encoding)

			if recorder.Body.Len() == 0 {
				t.Fatal("wrote an empty body")
			}

			decoded, err := compressutil.Decode(bytes.NewBuffer(recorder.Body.Bytes()), test.encoding)
			if err != nil {
				t.Fatalf("could not decode the written body as %q: %v", test.encoding, err)
			}

			if string(decoded) != body {
				t.Errorf("got body %q, want %q", decoded, body)
			}
		})
	}
}

func TestUnwrap(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := newTestWrapper(recorder)

	if unwrapped := wrapper.Unwrap(); unwrapped != http.ResponseWriter(recorder) {
		t.Errorf("got %v, want the wrapped writer %v", unwrapped, recorder)
	}
}

func TestCloseNotifyFallsBackToRequestContext(t *testing.T) {
	// httptest.ResponseRecorder is not an http.CloseNotifier, so the wrapper has
	// to supply the signal itself.
	requestContext, cancel := context.WithCancel(context.Background())
	wrapper := newTestWrapperWithContext(requestContext, httptest.NewRecorder())

	notify := wrapper.CloseNotify()

	select {
	case <-notify:
		t.Fatal("client reported as gone before the request was canceled")
	default:
	}

	cancel()

	select {
	case gone := <-notify:
		if !gone {
			t.Error("got false on the close notification channel, want true")
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for the close notification")
	}
}

func TestWriteBuffersOnlyWhatItCanRewrite(t *testing.T) {
	const body = "foo is the new bar"

	tests := []struct {
		desc        string
		contentType string
		encoding    string
		expBuffered bool
	}{
		{
			desc:        "should buffer a monitored type",
			contentType: "text/html; charset=utf-8",
			expBuffered: true,
		},
		{
			desc:        "should buffer a monitored type in an encoding it can decode",
			contentType: "text/html",
			encoding:    compressutil.Gzip,
			expBuffered: true,
		},
		{
			desc:        "should forward an unmonitored type",
			contentType: "application/octet-stream",
			expBuffered: false,
		},
		{
			desc:        "should forward an encoding it cannot decode",
			contentType: "text/html",
			encoding:    "br",
			expBuffered: false,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			wrapper := newTestWrapper(recorder)
			wrapper.Header().Set("Content-Type", test.contentType)
			wrapper.Header().Set("Content-Encoding", test.encoding)

			if _, err := wrapper.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}

			// The body goes to exactly one of the two: held back for the rewrite, or
			// on its way to the client.
			wantBuffered, wantSent := body, ""
			if !test.expBuffered {
				wantBuffered, wantSent = "", body
			}

			if got := wrapper.GetBuffer().String(); got != wantBuffered {
				t.Errorf("got buffer %q, want %q", got, wantBuffered)
			}

			if got := recorder.Body.String(); got != wantSent {
				t.Errorf("got client body %q, want %q", got, wantSent)
			}

			if got := wrapper.SupportsProcessing(); got != test.expBuffered {
				t.Errorf("got SupportsProcessing %v, want %v", got, test.expBuffered)
			}
		})
	}
}

func TestFlushSendsAForwardedResponseWithoutWaiting(t *testing.T) {
	// A response the plugin cannot rewrite has to stream: the client gets each
	// flushed chunk when the backend sends it, not when the request ends.
	const chunk = "the first chunk of a stream"

	recorder := httptest.NewRecorder()
	wrapper := newTestWrapper(recorder)
	wrapper.Header().Set("Content-Type", "text/event-stream")

	if _, err := wrapper.Write([]byte(chunk)); err != nil {
		t.Fatal(err)
	}

	wrapper.Flush()

	if !recorder.Flushed {
		t.Error("the flush did not reach the wrapped writer")
	}

	if got := recorder.Body.String(); got != chunk {
		t.Errorf("got client body %q part way through the stream, want %q", got, chunk)
	}
}

func TestGetContentLeavesBufferIntact(t *testing.T) {
	const body = "this is not gzip data at all, it is plain text"

	recorder := httptest.NewRecorder()
	wrapper := newTestWrapper(recorder)
	wrapper.Header().Set("Content-Type", "text/html")
	wrapper.Header().Set("Content-Encoding", compressutil.Gzip)

	if _, err := wrapper.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}

	if _, err := wrapper.GetContent(); err == nil {
		t.Fatal("expected an error decoding a body that is not gzip")
	}

	if got := wrapper.GetBuffer().String(); got != body {
		t.Errorf("got buffer %q after a failed decode, want the original %q", got, body)
	}
}
