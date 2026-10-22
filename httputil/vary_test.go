package httputil

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/kelnei/rewrite-body/logger"
)

func TestAddVary(t *testing.T) {
	tests := []struct {
		desc     string
		existing []string
		expected []string
	}{
		{
			desc:     "Adds the field to a response that has no Vary",
			expected: []string{"Accept"},
		},
		{
			desc:     "Keeps what the backend listed and appends the field",
			existing: []string{"Cookie"},
			expected: []string{"Cookie", "Accept"},
		},
		{
			desc:     "Keeps every existing header line",
			existing: []string{"Cookie", "Origin"},
			expected: []string{"Cookie", "Origin", "Accept"},
		},
		{
			desc:     "Leaves a Vary that already lists the field",
			existing: []string{"Cookie, Accept"},
			expected: []string{"Cookie, Accept"},
		},
		{
			desc:     "Matches the field ignoring case and whitespace",
			existing: []string{"cookie ,  ACCEPT "},
			expected: []string{"cookie ,  ACCEPT "},
		},
		{
			desc:     "Finds the field on a later header line",
			existing: []string{"Cookie", "Accept"},
			expected: []string{"Cookie", "Accept"},
		},
		{
			desc:     "Leaves a Vary of star, which already covers every header",
			existing: []string{"*"},
			expected: []string{"*"},
		},
		{
			desc:     "Does not take a longer field name for the field",
			existing: []string{"Accept-Encoding"},
			expected: []string{"Accept-Encoding", "Accept"},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			header := http.Header{}

			for _, value := range test.existing {
				header.Add("Vary", value)
			}

			AddVary(header, "Accept")

			if got := header.Values("Vary"); !reflect.DeepEqual(got, test.expected) {
				t.Errorf("Expected: %q | Got: %q", test.expected, got)
			}
		})
	}
}

func TestConsultsAccept(t *testing.T) {
	tests := []struct {
		desc       string
		method     string
		accept     string
		connection string
		upgrade    string
		expected   bool
	}{
		{
			desc:     "Consults Accept for a monitored method",
			method:   http.MethodGet,
			accept:   "text/html",
			expected: true,
		},
		{
			desc:     "Still consults it when Accept rules the monitored type out",
			method:   http.MethodGet,
			accept:   "application/json",
			expected: true,
		},
		{
			desc:     "Still consults it when the request sends no Accept",
			method:   http.MethodGet,
			expected: true,
		},
		{
			desc:   "Does not consult it for a method that is not monitored",
			method: http.MethodPost,
			accept: "application/json",
		},
		{
			desc:   "Does not consult it for HEAD",
			method: http.MethodHead,
			accept: "application/json",
		},
		{
			desc:       "Does not consult it for a WebSocket upgrade",
			method:     http.MethodGet,
			accept:     "application/json",
			connection: "Upgrade",
			upgrade:    "websocket",
		},
	}

	defaultLogWriter := logger.CreateLogger(logger.Error)

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			request := newTestRequest(t, test.method)

			for name, value := range map[string]string{
				"Accept":     test.accept,
				"Connection": test.connection,
				"Upgrade":    test.upgrade,
			} {
				if value != "" {
					request.Header.Set(name, value)
				}
			}

			wrappedRequest := WrapRequest(request, defaultMonitoring, *defaultLogWriter)

			if got := wrappedRequest.ConsultsAccept(); got != test.expected {
				t.Errorf("Expected: %t | Got: %t", test.expected, got)
			}
		})
	}
}
