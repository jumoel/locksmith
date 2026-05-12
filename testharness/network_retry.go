package testharness

import "strings"

// isTransientNetworkError returns true if `output` contains a known transient
// network failure marker. Keep this conservative: a string match on a real
// bug message would mask it. Only patterns that are unambiguously "the
// network ate the request" belong here.
//
// Exported via the unexported name because nothing outside the package
// needs to call it; the function is reachable from the integration-tagged
// runDockerWithNetworkRetry helper in the same package.
func isTransientNetworkError(output string) bool {
	needles := []string{
		// Node.js / npm / pnpm / yarn / bun socket errors
		"ECONNRESET",
		"ETIMEDOUT",
		"ENOTFOUND",
		"EAI_AGAIN",
		"EHOSTUNREACH",
		"ECONNREFUSED",
		"EPIPE",
		"network aborted",
		"socket hang up",
		"Connection reset by peer",
		// Go networking errors (locksmith itself running inside docker images)
		"i/o timeout",
		"TLS handshake timeout",
		"no such host",
		// HTTP-layer transient failures from the registry
		"HTTP 429",
		"HTTP 502",
		"HTTP 503",
		"HTTP 504",
		"502 Bad Gateway",
		"503 Service Unavailable",
		"504 Gateway Timeout",
		"Too Many Requests",
	}
	for _, n := range needles {
		if strings.Contains(output, n) {
			return true
		}
	}
	return false
}
