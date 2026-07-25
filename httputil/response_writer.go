// Package httputil a package for handling http data tasks
package httputil

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/kelnei/rewrite-body/compressutil"
	"github.com/kelnei/rewrite-body/logger"
)

// ResponseWrapper a wrapper used to simplify ResponseWriter data access and manipulation.
type ResponseWrapper struct {
	buffer       bytes.Buffer
	lastModified bool `default:"true"`
	wroteHeader  bool

	// passThrough records that this response is being forwarded rather than
	// buffered; see WriteHeader for where the choice is made.
	passThrough bool

	code int `default:"200"`

	// requestContext backs the deprecated CloseNotify shim; see the method.
	requestContext context.Context //nolint:containedctx // CloseNotify takes no arguments to pass it through
	logWriter      logger.LogWriter
	monitoring     MonitoringConfig

	http.ResponseWriter
}

// WrapWriter create a ResponseWrapper for provided configuration.
func WrapWriter(
	requestContext context.Context,
	responseWriter http.ResponseWriter,
	monitoringConfig MonitoringConfig,
	logWriter logger.LogWriter,
	lastModified bool,
) *ResponseWrapper {
	return &ResponseWrapper{
		buffer:         bytes.Buffer{},
		lastModified:   lastModified,
		wroteHeader:    false,
		passThrough:    false,
		code:           http.StatusOK,
		requestContext: requestContext,
		logWriter:      logWriter,
		monitoring:     monitoringConfig,
		ResponseWriter: responseWriter,
	}
}

// WriteHeader into wrapped ResponseWriter.
//
// This is where the response headers are last ours to change, and the first
// point at which they say what the body is, so it is also where the wrapper
// decides whether to buffer that body or forward it. Nothing after this can
// revisit the choice: the status line has gone out and any header edit past it
// is ignored.
func (wrapper *ResponseWrapper) WriteHeader(statusCode int) {
	if wrapper.wroteHeader {
		return
	}

	wrapper.code = statusCode
	wrapper.wroteHeader = true
	wrapper.passThrough = !wrapper.canRewrite()

	// A forwarded response reaches the client as the backend sent it, headers
	// included: its Content-Length still measures the body, and its
	// Last-Modified still describes content this plugin does not touch.
	if !wrapper.passThrough {
		if !wrapper.lastModified {
			wrapper.ResponseWriter.Header().Del("Last-Modified")
		}

		// Delegates the Content-Length Header creation to the final body write.
		wrapper.ResponseWriter.Header().Del("Content-Length")
	}

	wrapper.ResponseWriter.WriteHeader(statusCode)
}

// Write buffers data for rewriting, or forwards it to the client when this
// response is not one the plugin can rewrite.
//
// Forwarding is what keeps the plugin off the path of responses it was never
// going to change. A request can only be gated on its Accept header, which
// rules out very little -- fetch sends */* and plenty of clients send nothing
// at all -- so downloads and open-ended streams reach this method routinely.
// Buffering those would hold an entire body in memory for the life of the
// request, and would hold a stream back until it ended.
func (wrapper *ResponseWrapper) Write(data []byte) (int, error) {
	if !wrapper.wroteHeader {
		wrapper.WriteHeader(http.StatusOK)
	}

	if wrapper.passThrough {
		return wrapper.ResponseWriter.Write(data)
	}

	return wrapper.buffer.Write(data)
}

// GetBuffer get a pointer to the ResponseWriter buffer.
func (wrapper *ResponseWrapper) GetBuffer() *bytes.Buffer {
	return &wrapper.buffer
}

// GetContent load the content currently in the internal buffer
// accounting for possible encoding.
func (wrapper *ResponseWrapper) GetContent() ([]byte, error) {
	encoding := wrapper.getContentEncoding()

	// Decode consumes the buffer it reads from, and a failed decode consumes
	// whatever it got through first -- an unreadable gzip stream still costs the
	// 10 header bytes. Callers fall back to writing the buffered response when
	// decoding fails, so hand Decode a view over the bytes instead of the buffer
	// itself and leave the original intact for that fallback.
	return compressutil.Decode(bytes.NewBuffer(wrapper.buffer.Bytes()), encoding)
}

// SetContent write data to the internal ResponseWriter buffer
// and match initial encoding.
func (wrapper *ResponseWrapper) SetContent(data []byte, encoding string) {
	bodyBytes, err := compressutil.Encode(data, encoding)
	if err != nil {
		// bodyBytes is nil here, so writing it would send an empty body under a
		// success status -- a silent truncation. The response headers, including
		// Content-Encoding, went out with WriteHeader and cannot be taken back,
		// so the body still has to match the encoding the client was promised:
		// give up on the rewrite and send the original response instead.
		wrapper.logWriter.LogErrorf("unable to encode rewritten body as %q, sending the original response: %v", encoding, err)

		bodyBytes = wrapper.buffer.Bytes()
	}

	if !wrapper.wroteHeader {
		wrapper.WriteHeader(http.StatusOK)
	}

	if _, err := wrapper.ResponseWriter.Write(bodyBytes); err != nil {
		wrapper.logWriter.LogErrorf("unable to write rewriten body: %v", err)
		wrapper.LogHeaders()
	}
}

func (wrapper *ResponseWrapper) getHeader(headerName string) string {
	return wrapper.ResponseWriter.Header().Get(headerName)
}

// LogHeaders writes current response headers.
func (wrapper *ResponseWrapper) LogHeaders() {
	wrapper.logWriter.LogDebugf("Error Headers: %v", wrapper.ResponseWriter.Header())
}

// getContentEncoding get the Content-Encoding header value.
func (wrapper *ResponseWrapper) getContentEncoding() string {
	return wrapper.getHeader("Content-Encoding")
}

// getContentType get the Content-Encoding header value.
func (wrapper *ResponseWrapper) getContentType() string {
	return wrapper.getHeader("Content-Type")
}

// SupportsProcessing reports whether the response was buffered for rewriting.
//
// This answers the question after the fact rather than deciding it: WriteHeader
// settled it, and by the time a caller asks, the body has either been buffered
// or already sent. A response that never wrote a header wrote no body either.
func (wrapper *ResponseWrapper) SupportsProcessing() bool {
	return wrapper.wroteHeader && !wrapper.passThrough
}

// canRewrite reports whether the response headers describe a body this plugin
// can decode, rewrite and re-encode.
func (wrapper *ResponseWrapper) canRewrite() bool {
	// This is the authoritative type check: the response Content-Type is what
	// decides whether a body gets rewritten.
	if !wrapper.monitoring.MatchesType(wrapper.getContentType()) {
		return false
	}

	// If content type is supported validate encoding as well
	switch wrapper.getContentEncoding() {
	case compressutil.Gzip, compressutil.Deflate, compressutil.Identity, "":
		return true
	default:
		return false
	}
}

// SetLastModified update the local lastModified variable from non-package-based users.
//
// Deprecated: pass lastModified to WrapWriter, which is the only setting this
// wrapper reads. Nothing in this module calls this, and it has no effect once
// the header has gone out -- WriteHeader is what acts on the value.
func (wrapper *ResponseWrapper) SetLastModified(value bool) {
	wrapper.lastModified = value
}

// Unwrap returns the wrapped http.ResponseWriter.
//
// http.ResponseController (Go 1.20+) follows Unwrap to find the optional
// interfaces implemented further down the chain -- Flusher, Hijacker, the
// deadline setters -- so implementing it makes those reachable through this
// wrapper. Flush and Hijack are also implemented directly here, and those keep
// precedence over anything Unwrap exposes.
func (wrapper *ResponseWrapper) Unwrap() http.ResponseWriter {
	return wrapper.ResponseWriter
}

// CloseNotify returns a channel that receives at most a
// single value (true) when the client connection has gone away.
//
// http.CloseNotifier is deprecated -- the request context is the modern signal
// -- but this stays so callers that still type-assert for a CloseNotifier keep
// working. This used to return a channel that no one could ever send on, which
// reads as "the client is still there" forever.
func (wrapper *ResponseWrapper) CloseNotify() <-chan bool {
	if closeNotifier, ok := wrapper.ResponseWriter.(http.CloseNotifier); ok {
		return closeNotifier.CloseNotify()
	}

	notify := make(chan bool, 1)

	if wrapper.requestContext == nil {
		return notify
	}

	// Read Done() out here so the goroutine captures the channel rather than the
	// wrapper: capturing the wrapper would keep the whole buffered response body
	// reachable until the request ends.
	//
	// A context that can never be canceled -- context.Background(), or anything
	// derived from it without a cancel -- returns a nil channel here. Receiving
	// from that blocks forever, so starting the goroutine would leak it for the
	// lifetime of the process rather than the request.
	done := wrapper.requestContext.Done()
	if done == nil {
		return notify
	}

	go func() {
		<-done

		notify <- true
	}()

	return notify
}

// Hijack hijacks the connection.
func (wrapper *ResponseWrapper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := wrapper.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}

	return nil, nil, fmt.Errorf("%T is not a http.Hijacker", wrapper.ResponseWriter)
}

// Flush sends any buffered data to the client.
//
// A forwarded response flushes through to the client the way the backend
// intended. A buffered one has nothing here to send: a regex can match across
// writes, so rewriting needs the whole body, and no part of it can leave before
// the handler has finished with it. Passing the flush along still commits the
// status and headers, which is what a caller flushing ahead of its first write
// is asking for.
//
// Under Traefik this is never called: Yaegi hands compiled code an
// http.ResponseWriter wrapper around the interpreted value, and that wrapper
// carries no other interface, so the proxy's check for an http.Flusher fails
// and it stops flushing for the route -- measured against Traefik 3.7.9, where
// a text/event-stream response that streams chunk by chunk without the plugin
// arrives in one piece with it. A forwarded body still leaves as net/http's
// response buffer fills rather than being held to the end, which is what keeps
// a long stream moving. The method stays for compiled callers, and for the day
// the interpreter grows the wrappers.
func (wrapper *ResponseWrapper) Flush() {
	// If WriteHeader was already called from the caller, this is a NOOP.
	// Otherwise, codeCatcher.code is actually a 200 here.
	wrapper.WriteHeader(wrapper.code)

	if flusher, ok := wrapper.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
