package httputil

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/kelnei/rewrite-body/compressutil"
	"github.com/kelnei/rewrite-body/logger"
)

// RequestWrapper a struct that centralizes request modifications.
type RequestWrapper struct {
	logWriter  logger.LogWriter
	monitoring MonitoringConfig

	http.Request
}

// WrapRequest to get a new instance of RequestWrapper.
func WrapRequest(request *http.Request, monitoringConfig MonitoringConfig, logWriter logger.LogWriter) *RequestWrapper {
	return &RequestWrapper{
		logWriter:  logWriter,
		monitoring: monitoringConfig,
		Request:    *request,
	}
}

// CloneNoEncode create an http.Request that request no encoding.
func (req *RequestWrapper) CloneNoEncode() *http.Request {
	clonedRequest := req.Clone(req.Context())

	clonedRequest.Header.Set("Accept-Encoding", compressutil.Identity)

	return clonedRequest
}

// CloneWithSupportedEncoding create an http.Request that request only supported encoding.
func (req *RequestWrapper) CloneWithSupportedEncoding() *http.Request {
	clonedRequest := req.Clone(req.Context())

	clonedRequest.Header.Set("Accept-Encoding", removeUnsupportedAcceptEncoding(clonedRequest.Header))

	return clonedRequest
}

// GetEncodingTarget get the supported encoding algorithm preferred by request.
func (req *RequestWrapper) GetEncodingTarget() string {
	// Limit Accept-Encoding header to encodings we can handle.
	// acceptEncoding := header.ParseAccept(req.Header, "Accept-Encoding")
	acceptEncoding := parseAcceptEncoding(req.Header)
	filteredEncodings := make([]encodingSpec, 0, len(acceptEncoding))

	for _, a := range acceptEncoding {
		switch a.Value {
		case compressutil.Gzip, compressutil.Deflate:
			filteredEncodings = append(filteredEncodings, a)
		}
	}

	if len(filteredEncodings) == 0 {
		return compressutil.Identity
	}

	sort.Slice(filteredEncodings, func(i, j int) bool {
		return filteredEncodings[i].Quality > filteredEncodings[j].Quality
	})

	return filteredEncodings[0].Value
}

type encodingSpec struct {
	Value   string
	Quality float64
}

func parseAcceptEncoding(header http.Header) []encodingSpec {
	encodingHeader := header.Get("Accept-Encoding")
	if encodingHeader == "*" {
		return []encodingSpec{{Quality: 1.0, Value: compressutil.Gzip}, {Quality: 1.0, Value: compressutil.Deflate}}
	}

	encodingList := strings.Split(encodingHeader, ",")
	result := make([]encodingSpec, 0, len(encodingList))

	for _, encoding := range encodingList {
		result = append(result, parseEncodingItem(encoding))
	}

	return result
}

func parseEncodingItem(encoding string) encodingSpec {
	encoding = strings.TrimSpace(encoding)
	if encoding == "*" {
		return encodingSpec{Value: compressutil.Gzip, Quality: 1.0}
	}

	split := strings.Split(encoding, ";q=")
	quality := 1.0

	if qualitySplitSize := 2; len(split) == qualitySplitSize {
		targetFloat := 64

		parsedQuality, err := strconv.ParseFloat(split[1], targetFloat)
		if err == nil {
			quality = parsedQuality
		}
	}

	return encodingSpec{Value: split[0], Quality: quality}
}

func removeUnsupportedAcceptEncoding(header http.Header) string {
	encodingList := strings.Split(header.Get("Accept-Encoding"), ",")
	result := make([]string, 0, len(encodingList))

	for _, encoding := range encodingList {
		split := strings.Split(strings.TrimSpace(encoding), ";q=")
		switch split[0] {
		case compressutil.Gzip, compressutil.Deflate, compressutil.Identity:
			result = append(result, encoding)
		}
	}

	return strings.Join(result, ",")
}

// acceptsMonitoredType reports whether the request's Accept header leaves room
// for a monitored content type.
//
// Accept states what the client would like, not what the response will be, so
// it can only be used to skip requests that clearly want something else.
// Anything open-ended has to count as a maybe: fetch and XMLHttpRequest send
// "Accept: */*" and some clients send no Accept at all, and treating those as
// unsupported meant their responses were never rewritten -- for a WordPress
// backend that is admin-ajax.php and the REST routes. The authoritative check
// is the response Content-Type, which ResponseWrapper.SupportsProcessing
// applies once the type is actually known.
func (req *RequestWrapper) acceptsMonitoredType() bool {
	acceptHeader := req.Header.Get("Accept")

	// No preference stated, or every type accepted.
	if acceptHeader == "" || strings.Contains(acceptHeader, "*/*") {
		return true
	}

	return req.monitoring.MatchesType(acceptHeader)
}

// SupportsProcessing determine if http.Request is supported by this plugin.
//
// The checks run cheapest first: rejecting on the method costs a comparison,
// while the Accept header check scans a header value once per monitored type.
func (req *RequestWrapper) SupportsProcessing() bool {
	// A response to HEAD carries no body, so there is nothing here to rewrite.
	// Processing one is actively harmful: the wrapper drops the upstream
	// Content-Length on the way through and no body is ever written to replace
	// it, which leaves the response with no framing at all -- the client waits
	// for bytes that never arrive. Skip HEAD even when it is configured as a
	// monitored method.
	if req.Method == http.MethodHead {
		return false
	}

	if !req.hasMonitoredMethod() {
		return false
	}

	if strings.Contains(req.Header.Get("Upgrade"), "websocket") {
		return false
	}

	return req.acceptsMonitoredType()
}

// hasMonitoredMethod reports whether the request uses a monitored method.
//
// Methods are exact tokens, so this compares whole values: a substring match
// would let a monitored GET pick up any method containing it, and would make an
// empty configured method match everything.
func (req *RequestWrapper) hasMonitoredMethod() bool {
	for _, monitoredMethod := range req.monitoring.Methods {
		if req.Method == monitoredMethod {
			return true
		}
	}

	return false
}
