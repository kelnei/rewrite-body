package httputil_test

import (
	"net/http"
	"testing"

	"github.com/kelnei/rewrite-body/httputil"
)

func TestMatchesType(t *testing.T) {
	tests := []struct {
		desc     string
		types    []string
		header   string
		expected bool
	}{
		{
			desc:     "matches the type inside a full Content-Type",
			types:    []string{"text/html"},
			header:   "text/html; charset=utf-8",
			expected: true,
		},
		{
			desc:     "matches through the range that covers the type",
			types:    []string{"text/html"},
			header:   "text/*",
			expected: true,
		},
		{
			desc:     "does not match another type",
			types:    []string{"text/html"},
			header:   "application/json",
			expected: false,
		},
		{
			desc:     "does not match another range",
			types:    []string{"text/html"},
			header:   "image/*",
			expected: false,
		},
		{
			// An empty needle is a substring of everything, so this used to monitor
			// every response -- images and downloads included.
			desc:     "does not treat an empty configured type as a wildcard",
			types:    []string{""},
			header:   "image/png",
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			config := httputil.MonitoringConfig{Types: test.types}

			if got := config.MatchesType(test.header); got != test.expected {
				t.Errorf("got %v, want %v", got, test.expected)
			}
		})
	}
}

func TestMonitoringConfigParsing(t *testing.T) {
	tests := []struct {
		desc           string
		inputConfig    httputil.MonitoringConfig
		expectedConfig httputil.MonitoringConfig
	}{
		{
			desc: "defaults will be supplied for empty arrays",
			inputConfig: httputil.MonitoringConfig{
				Types:   []string{},
				Methods: []string{},
			},
			expectedConfig: httputil.MonitoringConfig{
				Types:   []string{"text/html"},
				Methods: []string{http.MethodGet},
			},
		},
		{
			desc: "defaults will be supplied for empty types with populated methods",
			inputConfig: httputil.MonitoringConfig{
				Types:   []string{},
				Methods: []string{"POST"},
			},
			expectedConfig: httputil.MonitoringConfig{
				Types:   []string{"text/html"},
				Methods: []string{http.MethodPost},
			},
		},
		{
			desc: "defaults will be supplied for populated types with empty methods",
			inputConfig: httputil.MonitoringConfig{
				Types:   []string{"application/json"},
				Methods: []string{},
			},
			expectedConfig: httputil.MonitoringConfig{
				Types:   []string{"application/json"},
				Methods: []string{http.MethodGet},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			config := test.inputConfig

			config.EnsureDefaults()

			if len(config.Types) != len(test.expectedConfig.Types) {
				t.Errorf("Expected Types: '%v' | Got Types: '%v'", test.expectedConfig.Types, config.Types)
			}

			for i, v := range test.expectedConfig.Types {
				if v != config.Types[i] {
					t.Errorf("Expected Types: '%v' | Got Types: '%v'", test.expectedConfig.Types, config.Types)
				}
			}

			if len(config.Methods) != len(test.expectedConfig.Methods) {
				t.Errorf("Expected Methods: '%v' | Got Methods: '%v'", test.expectedConfig.Methods, config.Methods)
			}

			for i, v := range test.expectedConfig.Methods {
				if v != config.Methods[i] {
					t.Errorf("Expected Methods: '%v' | Got Methods: '%v'", test.expectedConfig.Methods, config.Methods)
				}
			}
		})
	}
}
