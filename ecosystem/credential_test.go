package ecosystem

import "testing"

func TestBearerCredential_AuthHeader(t *testing.T) {
	c := BearerCredential{Token: "npm_xxx"}
	got := c.AuthHeader()
	want := "Bearer npm_xxx"
	if got != want {
		t.Errorf("AuthHeader = %q, want %q", got, want)
	}
}

func TestBasicCredential_AuthHeader_FromCleartext(t *testing.T) {
	// base64("foo:bar") == "Zm9vOmJhcg==".
	c := BasicCredential{Username: "foo", Password: "bar"}
	got := c.AuthHeader()
	want := "Basic Zm9vOmJhcg=="
	if got != want {
		t.Errorf("AuthHeader = %q, want %q", got, want)
	}
}

func TestBasicCredential_AuthHeader_ColonInPassword(t *testing.T) {
	// Passwords are allowed to contain colons (RFC 7617 base64 of "user:pa:ss").
	c := BasicCredential{Username: "user", Password: "pa:ss"}
	got := c.AuthHeader()
	// base64("user:pa:ss") == "dXNlcjpwYTpzcw==".
	want := "Basic dXNlcjpwYTpzcw=="
	if got != want {
		t.Errorf("AuthHeader = %q, want %q", got, want)
	}
}

// TestNewBasicCredentialFromEncoded verifies the helper that takes an .npmrc
// `_auth=base64(user:pass)` value and produces a BasicCredential matching
// what would have been built from the cleartext form.
func TestNewBasicCredentialFromEncoded(t *testing.T) {
	c, err := NewBasicCredentialFromEncoded("Zm9vOmJhcg==") // base64("foo:bar")
	if err != nil {
		t.Fatalf("NewBasicCredentialFromEncoded: %v", err)
	}
	if c.Username != "foo" {
		t.Errorf("Username = %q, want \"foo\"", c.Username)
	}
	if c.Password != "bar" {
		t.Errorf("Password = %q, want \"bar\"", c.Password)
	}
	if c.AuthHeader() != "Basic Zm9vOmJhcg==" {
		t.Errorf("AuthHeader = %q, want \"Basic Zm9vOmJhcg==\"", c.AuthHeader())
	}
}

func TestNewBasicCredentialFromEncoded_NotBase64(t *testing.T) {
	_, err := NewBasicCredentialFromEncoded("not-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64, got nil")
	}
}

func TestNewBasicCredentialFromEncoded_NoColon(t *testing.T) {
	// "foobar" base64-encoded has no colon when decoded: not user:pass shape.
	encoded := "Zm9vYmFy" // base64("foobar")
	_, err := NewBasicCredentialFromEncoded(encoded)
	if err == nil {
		t.Fatal("expected error when decoded value has no colon separator")
	}
}

// TestNewBasicCredentialFromSplit decodes a `_password=base64(p)` value and
// combines with username to make a BasicCredential.
func TestNewBasicCredentialFromSplit(t *testing.T) {
	c, err := NewBasicCredentialFromSplit("foo", "YmFy") // base64("bar")
	if err != nil {
		t.Fatalf("NewBasicCredentialFromSplit: %v", err)
	}
	if c.Username != "foo" || c.Password != "bar" {
		t.Errorf("got %+v, want {foo bar}", c)
	}
	if c.AuthHeader() != "Basic Zm9vOmJhcg==" {
		t.Errorf("AuthHeader = %q, want \"Basic Zm9vOmJhcg==\"", c.AuthHeader())
	}
}

// TestCredentialIsSealed verifies the interface can be type-switched. Both
// concrete types should satisfy Credential.
func TestCredentialIsSealed(t *testing.T) {
	creds := []Credential{
		BearerCredential{Token: "t"},
		BasicCredential{Username: "u", Password: "p"},
	}
	for _, c := range creds {
		if h := c.AuthHeader(); h == "" {
			t.Errorf("empty AuthHeader from %T", c)
		}
	}
}
