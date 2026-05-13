// Package registryurl canonicalizes registry URLs so that scheme-less keys
// in .npmrc (e.g. "//host/path/:_authToken=X") and full URLs configured on
// the registry client (e.g. "https://host/path/") agree on a single lookup
// key.
//
// Canonical form: explicit scheme, lowercase host, no trailing slash on the
// path. Path case is preserved (some registries route on case-sensitive
// path segments). Scheme is preserved when present; defaulted to https
// otherwise.
package registryurl

import (
	"net/url"
	"strings"
)

// Normalize returns the canonical lookup key for a registry URL.
//
// Idempotent: Normalize(Normalize(x)) == Normalize(x).
//
// Empty input returns empty - callers can use this to detect "no URL set"
// without a separate nil check.
func Normalize(s string) string {
	if s == "" {
		return ""
	}
	// .npmrc scheme-less form: "//host/path".
	if strings.HasPrefix(s, "//") {
		s = "https:" + s
	} else if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}
