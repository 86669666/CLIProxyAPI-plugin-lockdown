package logging

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
)

const redactedHeaderValue = "<redacted>"

// RedactHeaders returns a copy of headers with sensitive values masked.
func RedactHeaders(headers map[string][]string) map[string][]string {
	redacted := cloneHeaders(headers)
	for key, values := range redacted {
		for i, value := range values {
			values[i] = redactHeaderValue(key, value)
		}
	}
	return redacted
}

func redactHeaderValue(key, value string) string {
	lowerKey := strings.ToLower(strings.TrimSpace(key))
	switch {
	case lowerKey == "cookie", lowerKey == "set-cookie":
		if value != "" {
			return redactedHeaderValue
		}
	case strings.Contains(lowerKey, "authorization"):
		return util.MaskAuthorizationHeader(value)
	case strings.Contains(lowerKey, "token"), strings.Contains(lowerKey, "key"), strings.Contains(lowerKey, "secret"):
		return util.HideAPIKey(value)
	}
	return value
}
