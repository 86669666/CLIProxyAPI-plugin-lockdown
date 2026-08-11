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
		lowerKey := strings.ToLower(strings.TrimSpace(key))
		for i, value := range values {
			switch {
			case lowerKey == "cookie", lowerKey == "set-cookie":
				if value != "" {
					values[i] = redactedHeaderValue
				}
			case strings.Contains(lowerKey, "authorization"):
				values[i] = util.MaskAuthorizationHeader(value)
			case strings.Contains(lowerKey, "token"), strings.Contains(lowerKey, "key"), strings.Contains(lowerKey, "secret"):
				values[i] = util.HideAPIKey(value)
			}
		}
	}
	return redacted
}
