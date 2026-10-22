// Package handler a plugin to rewrite response body.
package handler

import (
	"context"
	"encoding/json"
	"errors"
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

func (bodyRewrite *rewriteBody) ServeHTTP(response http.ResponseWriter, req *http.Request) {
	defer bodyRewrite.handlePanic()

	wrappedRequest := httputil.WrapRequest(req, bodyRewrite.monitoringConfig, bodyRewrite.logger)
	// allow default http.ResponseWriter to handle calls targeting WebSocket upgrades and non GET methods
	if !wrappedRequest.SupportsProcessing() {
		bodyRewrite.logRequest("Ignoring unsupported request", req)
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

func (bodyRewrite *rewriteBody) handlePanic() {
	recovery := recover()
	if recovery == nil {
		return
	}

	err, isError := recovery.(error)
	if !isError {
		bodyRewrite.logger.LogWarningf("Unhandled error: %v", recovery)

		return
	}

	// http.ErrAbortHandler is a sentinel, not a failure: something downstream
	// panicked with it to tell net/http to abandon the response and close the
	// connection without logging a stack trace. Recovering it here and returning
	// normally would make the server believe the handler finished, so it would
	// try to complete a response that was deliberately abandoned. Measured
	// against Traefik 3.7.9, that reached the client as a 200 with
	// Content-Length: 0 -- an abandoned response delivered as a success.
	// Re-panic and let net/http perform the abort it was asked for.
	//
	// Expect one ERROR line per aborted request from Traefik's recovery
	// middleware, where a route without this plugin logs the same abort at
	// DEBUG. Yaegi wraps the value we re-panic with, which defeats Traefik's
	// errors.Is(err, http.ErrAbortHandler) check. Log noise only, and not
	// something a plugin can fix from in here.
	if errors.Is(err, http.ErrAbortHandler) {
		panic(recovery)
	}

	bodyRewrite.logger.LogWarningf("Recovered from: %v", err)
}
