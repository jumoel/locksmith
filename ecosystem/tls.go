package ecosystem

// TLSOptions controls the TLS configuration the registry client uses when
// talking to a registry. Sourced from `.npmrc` (`ca`, `cafile`, `strict-ssl`)
// and equivalents in bunfig.toml and .yarnrc.yml.
//
// Per-host scope (ticket #22): an entry in PerHost is a TOTAL REPLACEMENT
// for the outer TLSOptions when that host is contacted, not a delta. If a
// caller wants "global cafile + extra host cafile" they have to set the
// host entry to the union themselves. This matches npm's behavior and
// keeps the lookup rules predictable.
type TLSOptions struct {
	// RootCAs is a list of PEM-encoded CA certificates trusted in addition
	// to the system roots. Sourced from `ca=PEM`, `ca[]=PEM`, and `cafile=
	// /path` lines (cafile contents are read by the parser/CLI and inlined
	// here).
	RootCAs []string

	// Insecure disables certificate verification entirely. Sourced from
	// `strict-ssl=false`. Footgun, but it's what the user configured.
	Insecure bool

	// PerHost holds host-scoped TLSOptions. The map key is a normalized
	// registry URL (via internal/registryurl.Normalize); when the registry
	// client makes a request to that URL it uses the host-scoped options
	// in place of the outer TLSOptions.
	//
	// Nested PerHost on host entries has no special meaning; only the
	// top-level PerHost is consulted.
	PerHost map[string]*TLSOptions
}
