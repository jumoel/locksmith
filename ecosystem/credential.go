package ecosystem

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// Credential carries enough information to produce an `Authorization:` header
// value for a registry request. Sealed: only BearerCredential and
// BasicCredential implement it. Client cert auth is deferred (see locksmith
// ticket #7).
type Credential interface {
	// AuthHeader returns the value that goes after `Authorization: ` on the
	// request - for example "Bearer xxx" or "Basic base64(user:pass)".
	AuthHeader() string

	// isCredential is unexported to keep the set closed.
	isCredential()
}

// BearerCredential is the most common form: a single opaque token sent as
// `Authorization: Bearer <token>`. Sourced from .npmrc `_authToken`,
// .yarnrc.yml `npmAuthToken`, and bunfig.toml `token`.
type BearerCredential struct {
	Token string
}

func (BearerCredential) isCredential() {}

// AuthHeader returns "Bearer <token>".
func (c BearerCredential) AuthHeader() string {
	return "Bearer " + c.Token
}

// BasicCredential is HTTP Basic auth (RFC 7617). Sourced from .npmrc `_auth`,
// .npmrc `username` + `_password`, .yarnrc.yml `npmAuthIdent`, and bunfig.toml
// `{ username, password }`.
//
// Both fields are stored in cleartext so the credential can be re-emitted in
// `--print-config` after redaction without needing a separate decoded form.
type BasicCredential struct {
	Username string
	Password string
}

func (BasicCredential) isCredential() {}

// AuthHeader returns "Basic <base64(user:pass)>".
func (c BasicCredential) AuthHeader() string {
	raw := c.Username + ":" + c.Password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))
}

// NewBasicCredentialFromEncoded decodes an .npmrc `_auth=...` value (which
// holds base64(user:pass) directly) into a BasicCredential.
func NewBasicCredentialFromEncoded(encoded string) (BasicCredential, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return BasicCredential{}, fmt.Errorf("decoding _auth value: %w", err)
	}
	colon := strings.IndexByte(string(decoded), ':')
	if colon < 0 {
		return BasicCredential{}, fmt.Errorf("_auth value has no `:` separator after base64-decoding; expected user:password")
	}
	return BasicCredential{
		Username: string(decoded[:colon]),
		Password: string(decoded[colon+1:]),
	}, nil
}

// NewBasicCredentialFromSplit combines a cleartext username with a
// base64-encoded password (the `.npmrc` `username` + `_password` form) into
// a BasicCredential.
func NewBasicCredentialFromSplit(username, encodedPassword string) (BasicCredential, error) {
	decoded, err := base64.StdEncoding.DecodeString(encodedPassword)
	if err != nil {
		return BasicCredential{}, fmt.Errorf("decoding _password value: %w", err)
	}
	return BasicCredential{
		Username: username,
		Password: string(decoded),
	}, nil
}
