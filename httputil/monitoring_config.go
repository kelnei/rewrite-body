package httputil

import (
	"net/http"
	"strings"
)

// MonitoringConfig structure of data for handling configuration for
// controlling what content is monitored.
type MonitoringConfig struct {
	Types   []string `json:"types,omitempty" yaml:"types,omitempty" toml:"types,omitempty" export:"true"`
	Methods []string `json:"methods,omitempty" yaml:"methods,omitempty" toml:"methods,omitempty" export:"true"`
}

// EnsureDefaults check Types and Methods for empty arrays and apply default values if found.
func (config *MonitoringConfig) EnsureDefaults() {
	if len(config.Methods) == 0 {
		config.Methods = []string{http.MethodGet}
	}

	if len(config.Types) == 0 {
		config.Types = []string{"text/html"}
	}
}

// MatchesType reports whether a media type header names a monitored type.
//
// The header may be a response Content-Type or a request Accept, so a monitored
// type is also matched through the range that covers it: an Accept of text/*
// leaves room for text/html.
func (config *MonitoringConfig) MatchesType(header string) bool {
	for _, monitoredType := range config.Types {
		if strings.Contains(header, monitoredType) {
			return true
		}

		separator := strings.Index(monitoredType, "/")
		if separator < 0 {
			continue
		}

		if strings.Contains(header, monitoredType[:separator+1]+"*") {
			return true
		}
	}

	return false
}
