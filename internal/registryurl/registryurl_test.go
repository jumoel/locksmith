package registryurl

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// Forms drawn from real .npmrc files / registry docs.

		// npm public registry.
		{"npm-public-no-slash", "https://registry.npmjs.org", "https://registry.npmjs.org"},
		{"npm-public-trailing-slash", "https://registry.npmjs.org/", "https://registry.npmjs.org"},
		{"npm-public-host-keyed", "//registry.npmjs.org/", "https://registry.npmjs.org"},
		{"npm-public-host-keyed-no-slash", "//registry.npmjs.org", "https://registry.npmjs.org"},

		// GitHub Packages.
		{"github-packages-trailing", "https://npm.pkg.github.com/", "https://npm.pkg.github.com"},
		{"github-packages-host-keyed", "//npm.pkg.github.com/", "https://npm.pkg.github.com"},

		// Artifactory: subpath registries are common.
		{"artifactory-with-path", "https://example.jfrog.io/artifactory/api/npm/npm/", "https://example.jfrog.io/artifactory/api/npm/npm"},
		{"artifactory-host-keyed", "//example.jfrog.io/artifactory/api/npm/npm/", "https://example.jfrog.io/artifactory/api/npm/npm"},

		// Azure Artifacts.
		{"azure-with-path", "https://pkgs.dev.azure.com/org/_packaging/feed/npm/registry/", "https://pkgs.dev.azure.com/org/_packaging/feed/npm/registry"},

		// Case-insensitive hostnames, case-sensitive paths.
		{"mixed-case-host", "https://Registry.Npmjs.Org/", "https://registry.npmjs.org"},
		{"mixed-case-path", "https://example.com/PathSegment/", "https://example.com/PathSegment"},

		// HTTP is preserved when explicit (used for self-signed local dev / Verdaccio with strict-ssl=false).
		{"http-preserved", "http://localhost:4873/", "http://localhost:4873"},
		{"http-host-keyed-becomes-https", "//localhost:4873/", "https://localhost:4873"},

		// Empty input.
		{"empty", "", ""},

		// Bare hostname (no slash).
		{"bare-hostname", "example.com", "https://example.com"},

		// Port preserved.
		{"port-preserved", "https://registry.example.com:8443/", "https://registry.example.com:8443"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Normalize(tc.in)
			if got != tc.want {
				t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestNormalize_Idempotent verifies Normalize(Normalize(x)) == Normalize(x).
// This matters because both the parser and the registry client call it,
// sometimes on values that have already been normalized by an earlier pass.
func TestNormalize_Idempotent(t *testing.T) {
	inputs := []string{
		"https://registry.npmjs.org/",
		"//npm.pkg.github.com/",
		"//example.jfrog.io/artifactory/api/npm/npm/",
		"https://Registry.NpmJS.Org/",
		"http://localhost:4873/",
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			once := Normalize(in)
			twice := Normalize(once)
			if once != twice {
				t.Errorf("Normalize not idempotent for %q: first=%q second=%q", in, once, twice)
			}
		})
	}
}
