package httputil

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/kelnei/rewrite-body/logger"
)

// defaultMonitoring is the configuration this package's tests use when the
// monitored types and methods are not what is under test.
var defaultMonitoring = MonitoringConfig{
	Types:   []string{"text/html"},
	Methods: []string{http.MethodGet},
}

func newTestRequest(t *testing.T, method string) *http.Request {
	t.Helper()

	request, err := http.NewRequestWithContext(
		context.Background(),
		method,
		"http://google.com",
		&bytes.Reader{})
	if err != nil {
		t.Fatalf("Error creating request: %v", err)
	}

	return request
}

func TestGetEncodingTarget(t *testing.T) {
	tests := []struct {
		desc           string
		acceptEncoding string
		expectedTarget string
	}{
		{
			desc:           "Supports gzip",
			acceptEncoding: "gzip",
			expectedTarget: "gzip",
		},
		{
			desc:           "Supports deflate",
			acceptEncoding: "deflate",
			expectedTarget: "deflate",
		},
		{
			desc:           "Supports identity",
			acceptEncoding: "identity",
			expectedTarget: "identity",
		},
		{
			desc:           "Ignores brotli",
			acceptEncoding: "br, gzip",
			expectedTarget: "gzip",
		},
		{
			desc:           "Wildcard to gzip",
			acceptEncoding: "*",
			expectedTarget: "gzip",
		},
		{
			desc:           "Respects quality in order",
			acceptEncoding: "gzip;q=0.8, deflate;q=0.6",
			expectedTarget: "gzip",
		},
		{
			desc:           "Respects quality out of order",
			acceptEncoding: "gzip;q=0.8, deflate;q=0.9",
			expectedTarget: "deflate",
		},
	}

	defaultLogWriter := logger.CreateLogger(logger.Error)

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			request := newTestRequest(t, http.MethodGet)
			request.Header.Set("Accept-Encoding", test.acceptEncoding)

			wrappedRequest := WrapRequest(request, defaultMonitoring, *defaultLogWriter)
			target := wrappedRequest.GetEncodingTarget()

			if target != test.expectedTarget {
				t.Errorf("Expected: '%s' | Got: '%s'", test.expectedTarget, target)
			}
		})
	}
}

func TestRemoveUnuspportedEncoding(t *testing.T) {
	tests := []struct {
		desc           string
		acceptEncoding string
		expectedTarget string
	}{
		{
			desc:           "Supports gzip",
			acceptEncoding: "gzip",
			expectedTarget: "gzip",
		},
		{
			desc:           "Supports deflate",
			acceptEncoding: "deflate",
			expectedTarget: "deflate",
		},
		{
			desc:           "Supports identity",
			acceptEncoding: "identity",
			expectedTarget: "identity",
		},
		{
			desc:           "Ignores brotli",
			acceptEncoding: "br, gzip",
			expectedTarget: " gzip",
		},
		{
			desc:           "Wildcard is dropped",
			acceptEncoding: "*",
			expectedTarget: "",
		},
		{
			desc:           "Respects quality in order",
			acceptEncoding: "gzip;q=0.8, deflate;q=0.6",
			expectedTarget: "gzip;q=0.8, deflate;q=0.6",
		},
	}

	defaultLogWriter := logger.CreateLogger(logger.Error)

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			request := newTestRequest(t, http.MethodGet)
			request.Header.Set("Accept-Encoding", test.acceptEncoding)

			wrappedRequest := WrapRequest(request, defaultMonitoring, *defaultLogWriter)
			target := wrappedRequest.CloneWithSupportedEncoding().Header.Get("Accept-Encoding")

			if target != test.expectedTarget {
				t.Errorf("Expected: '%s' | Got: '%s'", test.expectedTarget, target)
			}
		})
	}
}

func TestSupportsProcessingWebsocketUpgrade(t *testing.T) {
	// A websocket upgrade must be handed straight to the underlying writer.
	// Buffering one would break the connection, which is what upstream saw as a
	// panic on upgrade. (traefik/plugin-rewritebody#15)
	//
	// The request is an upgrade when it says so the way RFC 6455 section 4.1
	// does: a Connection header listing the upgrade token and an Upgrade header
	// listing websocket, both case-insensitive lists. Anything short of that is
	// an ordinary request, and its response is rewritten like any other.
	tests := []struct {
		desc            string
		connection      []string
		upgrade         []string
		expectedSupport bool
	}{
		{
			desc:       "Skips a websocket upgrade",
			connection: []string{"Upgrade"},
			upgrade:    []string{"websocket"},
		},
		{
			desc:       "Skips when the header values differ in case",
			connection: []string{"upgrade"},
			upgrade:    []string{"WebSocket"},
		},
		{
			desc:       "Skips when Connection lists other tokens, as Firefox sends it",
			connection: []string{"keep-alive, Upgrade"},
			upgrade:    []string{"websocket"},
		},
		{
			desc:       "Skips when the tokens arrive on separate header lines",
			connection: []string{"keep-alive", "Upgrade"},
			upgrade:    []string{"h2c", "websocket"},
		},
		{
			desc:            "Processes Upgrade websocket without Connection upgrade",
			upgrade:         []string{"websocket"},
			expectedSupport: true,
		},
		{
			desc:            "Processes Connection upgrade for another protocol",
			connection:      []string{"Upgrade"},
			upgrade:         []string{"h2c"},
			expectedSupport: true,
		},
		{
			desc:            "Processes Connection upgrade without an Upgrade header",
			connection:      []string{"Upgrade"},
			expectedSupport: true,
		},
		{
			desc:            "Processes Connection keep-alive",
			connection:      []string{"keep-alive"},
			upgrade:         []string{"websocket"},
			expectedSupport: true,
		},
	}

	defaultLogWriter := logger.CreateLogger(logger.Error)

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			request := newTestRequest(t, http.MethodGet)
			request.Header.Set("Accept", "text/html")

			for _, value := range test.connection {
				request.Header.Add("Connection", value)
			}

			for _, value := range test.upgrade {
				request.Header.Add("Upgrade", value)
			}

			wrappedRequest := WrapRequest(request, defaultMonitoring, *defaultLogWriter)

			if got := wrappedRequest.SupportsProcessing(); got != test.expectedSupport {
				t.Errorf("Expected: %t | Got: %t", test.expectedSupport, got)
			}
		})
	}
}

func TestSupportsProcessing(t *testing.T) {
	tests := []struct {
		desc             string
		inputType        string
		inputMethod      string
		monitoringConfig MonitoringConfig
		expectedSupport  bool
	}{
		{
			desc:            "Supports default config",
			inputType:       "text/html",
			inputMethod:     http.MethodGet,
			expectedSupport: true,
			monitoringConfig: MonitoringConfig{
				Types:   []string{"text/html"},
				Methods: []string{"GET"},
			},
		},
		{
			desc:            "Supports default browser load",
			inputType:       "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.9",
			inputMethod:     http.MethodGet,
			expectedSupport: true,
			monitoringConfig: MonitoringConfig{
				Types:   []string{"text/html"},
				Methods: []string{"GET"},
			},
		},
		{
			desc:            "Supports when types includes unsupported type first",
			inputType:       "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.9",
			inputMethod:     http.MethodGet,
			expectedSupport: true,
			monitoringConfig: MonitoringConfig{
				Types:   []string{"application/javascript", "text/html"},
				Methods: []string{"GET"},
			},
		},
		{
			desc:            "Supports multiple methods",
			inputType:       "text/html",
			inputMethod:     http.MethodPost,
			expectedSupport: true,
			monitoringConfig: MonitoringConfig{
				Types:   []string{"text/html"},
				Methods: []string{"GET", "POST"},
			},
		},
		{
			desc:            "Supports a fetch or XHR request that accepts anything",
			inputType:       "*/*",
			inputMethod:     http.MethodGet,
			expectedSupport: true,
			monitoringConfig: MonitoringConfig{
				Types:   []string{"text/html"},
				Methods: []string{"GET"},
			},
		},
		{
			desc:            "Supports a request that sends no Accept header",
			inputType:       "",
			inputMethod:     http.MethodGet,
			expectedSupport: true,
			monitoringConfig: MonitoringConfig{
				Types:   []string{"text/html"},
				Methods: []string{"GET"},
			},
		},
		{
			desc:            "Supports a wildcard over a monitored range",
			inputType:       "text/*",
			inputMethod:     http.MethodGet,
			expectedSupport: true,
			monitoringConfig: MonitoringConfig{
				Types:   []string{"text/html"},
				Methods: []string{"GET"},
			},
		},
		{
			desc:            "Does not support type not included",
			inputType:       "application/javascript",
			inputMethod:     http.MethodGet,
			expectedSupport: false,
			monitoringConfig: MonitoringConfig{
				Types:   []string{"text/html"},
				Methods: []string{"GET"},
			},
		},
		{
			desc:            "Does not support a request that only wants another range",
			inputType:       "image/*",
			inputMethod:     http.MethodGet,
			expectedSupport: false,
			monitoringConfig: MonitoringConfig{
				Types:   []string{"text/html"},
				Methods: []string{"GET"},
			},
		},
		{
			desc:            "Does not support method not included",
			inputType:       "text/html",
			inputMethod:     http.MethodPost,
			expectedSupport: false,
			monitoringConfig: MonitoringConfig{
				Types:   []string{"text/html"},
				Methods: []string{"GET"},
			},
		},
		{
			desc:            "Does not support HEAD even when it is configured",
			inputType:       "text/html",
			inputMethod:     http.MethodHead,
			expectedSupport: false,
			monitoringConfig: MonitoringConfig{
				Types:   []string{"text/html"},
				Methods: []string{"GET", "HEAD"},
			},
		},
		{
			desc:            "Does not support a method that merely contains a monitored method",
			inputType:       "text/html",
			inputMethod:     "GETX",
			expectedSupport: false,
			monitoringConfig: MonitoringConfig{
				Types:   []string{"text/html"},
				Methods: []string{"GET"},
			},
		},
		{
			desc:            "Does not treat an empty configured method as a wildcard",
			inputType:       "text/html",
			inputMethod:     http.MethodGet,
			expectedSupport: false,
			monitoringConfig: MonitoringConfig{
				Types:   []string{"text/html"},
				Methods: []string{""},
			},
		},
	}

	defaultLogWriter := logger.CreateLogger(logger.Error)

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			request := newTestRequest(t, test.inputMethod)
			request.Header.Set("Accept", test.inputType)

			wrappedRequest := WrapRequest(request, test.monitoringConfig, *defaultLogWriter)

			if test.expectedSupport != wrappedRequest.SupportsProcessing() {
				t.Errorf("Test input: '%v'", test)
			}
		})
	}
}
