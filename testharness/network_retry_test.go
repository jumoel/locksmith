package testharness

import "testing"

func TestIsTransientNetworkError(t *testing.T) {
	transient := []string{
		// Real failure that motivated this helper.
		"npm ERR! code ECONNRESET\nnpm ERR! network aborted\nnpm ERR! network This is a problem related to network connectivity.",
		// Variant DNS failures
		"npm ERR! code ENOTFOUND\nnpm ERR! syscall getaddrinfo\nnpm ERR! errno ENOTFOUND",
		// Yarn / pnpm
		"error An unexpected error occurred: \"https://registry.yarnpkg.com/foo: socket hang up\".",
		"ERR_PNPM_FETCH_404  GET https://registry.npmjs.org/foo: Connection reset by peer",
		// HTTP-layer 5xx
		"npm ERR! 503 Service Unavailable - GET https://registry.npmjs.org/express",
		"got HTTP 502 from registry",
		"Too Many Requests",
		// Go transport-level
		"Get \"https://registry.npmjs.org/foo\": dial tcp: i/o timeout",
		"net/http: TLS handshake timeout",
		"dial tcp: lookup registry.npmjs.org: no such host",
	}
	for _, tt := range transient {
		if !isTransientNetworkError(tt) {
			t.Errorf("expected transient classification for:\n%s", tt)
		}
	}

	nonTransient := []string{
		// Real assertion failures we must NOT retry on.
		"npm ERR! code ERESOLVE\nnpm ERR! ERESOLVE could not resolve\nnpm ERR! While resolving: foo@1.0.0",
		"sha512-AAAA does not match the integrity recorded in the lockfile",
		"error This module's tarball signature is invalid",
		"yarn install v1.22.22\n[1/4] Resolving packages...\nerror Couldn't find package \"foo@1.0.0\".",
		"ERR_PNPM_OUTDATED_LOCKFILE  Cannot install with frozen-lockfile",
		// Empty output should not be transient.
		"",
		// Lockfile drift, not a network problem.
		"npm ERR! `npm ci` can only install packages when your package.json and package-lock.json are in sync.",
	}
	for _, tt := range nonTransient {
		if isTransientNetworkError(tt) {
			t.Errorf("expected non-transient classification for:\n%s", tt)
		}
	}
}
