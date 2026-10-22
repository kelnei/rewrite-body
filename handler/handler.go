// Package handler a plugin to rewrite response body.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/kelnei/rewrite-body/httputil"
	"github.com/kelnei/rewrite-body/logger"
)

type rewriteBody struct {
	name             string
	next             http.Handler
	rewrites         []rewrite
	lastModified     bool
	logger           logger.LogWriter
	monitoringConfig httputil.MonitoringConfig
}

// New creates and returns a new rewrite body plugin instance.
func New(_ context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	rewrites := make([]rewrite, len(config.Rewrites))

	for index, rewriteConfig := range config.Rewrites {
		regex, err := regexp.Compile(rewriteConfig.Regex)
		if err != nil {
			return nil, fmt.Errorf("error compiling regex %q: %w", rewriteConfig.Regex, err)
		}

		rewrites[index] = rewrite{
			regex:       regex,
			replacement: []byte(rewriteConfig.Replacement),
		}
	}

	logWriter := *logger.CreateLogger(logger.LogLevel(config.LogLevel))

	config.Monitoring.EnsureDefaults()

	result := &rewriteBody{
		name:             name,
		next:             next,
		rewrites:         rewrites,
		lastModified:     config.LastModified,
		logger:           logWriter,
		monitoringConfig: config.Monitoring,
	}

	data, _ := json.Marshal(config)

	logWriter.LogDebugf("Initial config: %v", string(data))

	return result, nil
}

// ServeHTTP does not recover from a panic in the handler it wraps. Traefik's
// recovery middleware answers 500 while nothing has been written and aborts the
// response after, and a route behaves the same with this plugin on it as
// without. A panic that passes through here untouched also keeps its identity,
// so http.ErrAbortHandler is still recognized as the abort it is.
func (bodyRewrite *rewriteBody) ServeHTTP(response http.ResponseWriter, req *http.Request) {
	wrappedRequest := httputil.WrapRequest(req, bodyRewrite.monitoringConfig, bodyRewrite.logger)
	// allow default http.ResponseWriter to handle calls targeting WebSocket upgrades and non GET methods
	if !wrappedRequest.SupportsProcessing() {
		bodyRewrite.logRequest("Ignoring unsupported request", req)

		// A request skipped on its Accept header is answered with the page as the
		// backend sent it, from the same URL that answers another request with a
		// rewritten one. The backend has not said yet what it is sending, so this
		// goes on before the call, and the writer stays the one Traefik gave us.
		if wrappedRequest.ConsultsAccept() {
			httputil.AddVary(response.Header(), "Accept")
		}

		bodyRewrite.next.ServeHTTP(response, req)

		return
	}

	bodyRewrite.logRequest("Starting supported request", req)

	wrappedWriter := httputil.WrapWriter(
		req.Context(),
		response,
		bodyRewrite.monitoringConfig,
		bodyRewrite.logger,
		bodyRewrite.lastModified,
	)

	// look into using https://pkg.go.dev/net/http#RoundTripper
	//
	//nolint:contextcheck // the clone carries req's own context; see CloneWithSupportedEncoding
	bodyRewrite.next.ServeHTTP(wrappedWriter, wrappedRequest.CloneWithSupportedEncoding())

	if !wrappedWriter.SupportsProcessing() {
		// Nothing to do: the wrapper forwarded this response to the client as the
		// backend wrote it, rather than buffering a body it could not rewrite.
		bodyRewrite.logger.LogDebugf(
			"Ignoring unsupported response: content-type=%q content-encoding=%q",
			wrappedWriter.Header().Get("Content-Type"),
			wrappedWriter.Header().Get("Content-Encoding"),
		)

		return
	}

	bodyBytes, err := wrappedWriter.GetContent()
	if err != nil {
		bodyRewrite.logger.LogErrorf("Error loading content: %v", err)

		if _, err := response.Write(wrappedWriter.GetBuffer().Bytes()); err != nil {
			bodyRewrite.logger.LogErrorf("unable to write error content: %v", err)
		}

		return
	}

	bodyRewrite.logger.LogTracef("Response body: %s", bodyBytes)

	if len(bodyBytes) == 0 {
		// If the body is empty there is no purpose in continuing this process.
		return
	}

	for _, rwt := range bodyRewrite.rewrites {
		bodyBytes = rwt.regex.ReplaceAll(bodyBytes, rwt.replacement)
	}

	bodyRewrite.logger.LogTracef("Transformed body: %s", bodyBytes)

	encoding := wrappedWriter.Header().Get("Content-Encoding")
	wrappedWriter.SetContent(bodyBytes, encoding)
}

// logRequest writes the routing decision for a request at Debug level.
//
// It names the request by the fields that decide the routing rather than
// printing the request. Formatting a *http.Request with %v dumps its whole
// header map -- Authorization and Cookie included -- along with the query
// string and the client address, and Debug is the level operators are told to
// run in production. The arguments are passed through rather than formatted
// here so that the level check still comes before any formatting.
func (bodyRewrite *rewriteBody) logRequest(message string, req *http.Request) {
	bodyRewrite.logger.LogDebugf(
		"%s: method=%q host=%q path=%q accept=%q accept-encoding=%q upgrade=%q",
		message,
		req.Method,
		req.Host,
		req.URL.Path,
		req.Header.Get("Accept"),
		req.Header.Get("Accept-Encoding"),
		req.Header.Get("Upgrade"),
	)
}
