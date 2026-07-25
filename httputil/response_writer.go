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
		code:           http.StatusOK,
		requestContext: requestContext,
		logWriter:      logWriter,
		monitoring:     monitoringConfig,
		ResponseWriter: responseWriter,
	}
}

// WriteHeader into wrapped ResponseWriter.
func (wrapper *ResponseWrapper) WriteHeader(statusCode int) {
	if wrapper.wroteHeader {
		return
	}

	if !wrapper.lastModified {
		wrapper.ResponseWriter.Header().Del("Last-Modified")
	}

	wrapper.code = statusCode
	wrapper.wroteHeader = true

	// Delegates the Content-Length Header creation to the final body write.
	wrapper.ResponseWriter.Header().Del("Content-Length")

	wrapper.ResponseWriter.WriteHeader(statusCode)
}

// Write data to internal buffer and mark the status code as http.StatusOK.
func (wrapper *ResponseWrapper) Write(data []byte) (int, error) {
	if !wrapper.wroteHeader {
		wrapper.WriteHeader(http.StatusOK)
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

// SupportsProcessing determine if HttpWrapper is supported by this plugin based on encoding.
func (wrapper *ResponseWrapper) SupportsProcessing() bool {
	// This is the authoritative type check: the response Content-Type is what
	// decides whether a body gets rewritten.
	if !wrapper.monitoring.MatchesType(wrapper.getContentType()) {
		return false
	}

	encoding := wrapper.getContentEncoding()

	// If content type is supported validate encoding as well
	switch encoding {
	case compressutil.Gzip, compressutil.Deflate, compressutil.Identity, "":
		return true
	default:
		return false
	}
}

// SetLastModified update the local lastModified variable from non-package-based users.
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
	done := wrapper.requestContext.Done()

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
func (wrapper *ResponseWrapper) Flush() {
	// If WriteHeader was already called from the caller, this is a NOOP.
	// Otherwise, codeCatcher.code is actually a 200 here.
	wrapper.WriteHeader(wrapper.code)

	if flusher, ok := wrapper.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
