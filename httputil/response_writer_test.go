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

func TestGetContentLeavesBufferIntact(t *testing.T) {
	const body = "this is not gzip data at all, it is plain text"

	recorder := httptest.NewRecorder()
	wrapper := newTestWrapper(recorder)
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
