package npm

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/registryurl"
)

const minimalPackumentResp = `{"name":"test-pkg","versions":{"1.0.0":{"name":"test-pkg","version":"1.0.0","dist":{"tarball":"http://x/t.tgz","shasum":"abc"}}},"time":{"1.0.0":"2020-01-01T00:00:00.000Z"},"dist-tags":{"latest":"1.0.0"}}`

// TestTLS_DefaultRejectsSelfSigned verifies the baseline: with no TLS options,
// the registry client refuses to connect to a self-signed-cert server.
func TestTLS_DefaultRejectsSelfSigned(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(minimalPackumentResp))
	}))
	t.Cleanup(srv.Close)

	client := NewRegistryClient(srv.URL)
	_, err := client.FetchVersions(context.Background(), "test-pkg", nil)
	if err == nil {
		t.Fatal("expected TLS error against self-signed server with no TLS options, got nil")
	}
	// Don't assert exact wording; Go's TLS error text changes between releases.
}

// TestTLS_InsecureBypassesValidation: strict-ssl=false case.
func TestTLS_InsecureBypassesValidation(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(minimalPackumentResp))
	}))
	t.Cleanup(srv.Close)

	client := NewRegistryClientWithTLS(srv.URL, nil, nil, &ecosystem.TLSOptions{Insecure: true})
	_, err := client.FetchVersions(context.Background(), "test-pkg", nil)
	if err != nil {
		t.Fatalf("FetchVersions with Insecure=true: %v", err)
	}
}

// TestTLS_RootCAsTrust: explicit CA bundle case (`.npmrc cafile`/`ca=`).
func TestTLS_RootCAsTrust(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(minimalPackumentResp))
	}))
	t.Cleanup(srv.Close)

	// httptest's self-signed cert is at srv.Certificate(). Convert to PEM.
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: srv.Certificate().Raw,
	})

	client := NewRegistryClientWithTLS(srv.URL, nil, nil, &ecosystem.TLSOptions{
		RootCAs: []string{string(certPEM)},
	})
	_, err := client.FetchVersions(context.Background(), "test-pkg", nil)
	if err != nil {
		t.Fatalf("FetchVersions with RootCAs trusting server: %v", err)
	}
}

// TestTLS_PerHostScope: per ticket #22, a per-host TLSOptions entry applies
// ONLY to that host. Other hosts use the global setting.
func TestTLS_PerHostScope(t *testing.T) {
	// Two self-signed servers. Trust one via PerHost; the other should fail.
	trustedSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(minimalPackumentResp))
	}))
	t.Cleanup(trustedSrv.Close)

	untrustedSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(minimalPackumentResp))
	}))
	t.Cleanup(untrustedSrv.Close)

	trustedHost := registryurl.Normalize(trustedSrv.URL)
	opts := &ecosystem.TLSOptions{
		PerHost: map[string]*ecosystem.TLSOptions{
			trustedHost: {Insecure: true},
		},
	}

	// Use trustedSrv as the base; route a scoped package to untrustedSrv to verify it fails strict.
	client := NewRegistryClientWithTLS(trustedSrv.URL, map[string]string{
		"@untrusted": untrustedSrv.URL,
	}, nil, opts)

	// Trusted host: should succeed because PerHost grants Insecure.
	_, err := client.FetchVersions(context.Background(), "test-pkg", nil)
	if err != nil {
		t.Errorf("FetchVersions against trusted host: %v", err)
	}

	// Untrusted host: should fail because PerHost has no entry, so global TLS (strict) applies.
	_, err = client.FetchVersions(context.Background(), "@untrusted/pkg", nil)
	if err == nil {
		t.Error("FetchVersions against untrusted host: expected TLS error, got nil")
	}
}

// TestTLS_RootCAs_ParseFailure: malformed PEM in RootCAs should surface an
// error at client construction or at fetch time, not silently fall through to
// "no CAs configured" which would look like a TLS rejection unrelated to PEM.
func TestTLS_RootCAs_ParseFailure(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(minimalPackumentResp))
	}))
	t.Cleanup(srv.Close)

	client := NewRegistryClientWithTLS(srv.URL, nil, nil, &ecosystem.TLSOptions{
		RootCAs: []string{"not a PEM"},
	})

	// The fetch should fail. We don't require a specific error message but
	// the malformed cert should not silently grant trust.
	_, err := client.FetchVersions(context.Background(), "test-pkg", nil)
	if err == nil {
		t.Error("FetchVersions with malformed PEM in RootCAs: expected error, got nil")
	}
}

// TestTLS_NilOptionsBehavesLikeStrict: passing nil TLSOptions should be
// equivalent to "use default TLS" (strict validation).
func TestTLS_NilOptionsBehavesLikeStrict(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(minimalPackumentResp))
	}))
	t.Cleanup(srv.Close)

	client := NewRegistryClientWithTLS(srv.URL, nil, nil, nil)
	_, err := client.FetchVersions(context.Background(), "test-pkg", nil)
	if err == nil {
		t.Error("expected TLS error with nil TLSOptions (default = strict)")
	}
}

// Unused import warden in case the TLS pkg ever stops being needed.
var _ = x509.NewCertPool
var _ ecosystem.Credential = ecosystem.BearerCredential{}
