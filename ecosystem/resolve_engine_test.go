package ecosystem

import "testing"

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		wantOwner  string
		wantRepo   string
		wantOK     bool
	}{
		{
			name:       "github: shorthand",
			constraint: "github:owner/repo",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantOK:     true,
		},
		{
			name:       "github: with .git suffix",
			constraint: "github:owner/repo.git",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantOK:     true,
		},
		{
			name:       "git+ssh URL",
			constraint: "git+ssh://git@github.com/owner/repo.git",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantOK:     true,
		},
		{
			name:       "git+https URL",
			constraint: "git+https://github.com/owner/repo.git",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantOK:     true,
		},
		{
			name:       "git+ssh without .git suffix",
			constraint: "git+ssh://git@github.com/myorg/mylib",
			wantOwner:  "myorg",
			wantRepo:   "mylib",
			wantOK:     true,
		},
		{
			name:       "git@github.com colon syntax",
			constraint: "git@github.com:owner/repo.git",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantOK:     true,
		},
		{
			name:       "not a github URL",
			constraint: "^1.0.0",
			wantOwner:  "",
			wantRepo:   "",
			wantOK:     false,
		},
		{
			name:       "file: specifier",
			constraint: "file:./local-pkg",
			wantOwner:  "",
			wantRepo:   "",
			wantOK:     false,
		},
		{
			name:       "github: missing repo",
			constraint: "github:owneronly",
			wantOwner:  "",
			wantRepo:   "",
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, ok := parseGitHubURL(tt.constraint)
			if ok != tt.wantOK {
				t.Errorf("parseGitHubURL(%q) ok = %v, want %v", tt.constraint, ok, tt.wantOK)
				return
			}
			if owner != tt.wantOwner {
				t.Errorf("parseGitHubURL(%q) owner = %q, want %q", tt.constraint, owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("parseGitHubURL(%q) repo = %q, want %q", tt.constraint, repo, tt.wantRepo)
			}
		})
	}
}

func TestParseTarballURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		wantName    string
		wantVersion string
	}{
		{
			name:        "standard npm registry URL",
			url:         "https://registry.npmjs.org/is-odd/-/is-odd-3.0.1.tgz",
			wantName:    "is-odd",
			wantVersion: "3.0.1",
		},
		{
			name:        "hyphenated package name",
			url:         "https://registry.npmjs.org/my-cool-pkg/-/my-cool-pkg-1.2.3.tgz",
			wantName:    "my-cool-pkg",
			wantVersion: "1.2.3",
		},
		{
			name:        "prerelease version",
			url:         "https://registry.npmjs.org/foo/-/foo-2.0.0-beta.1.tgz",
			wantName:    "foo",
			wantVersion: "2.0.0-beta.1",
		},
		{
			name:        "yarnpkg registry URL",
			url:         "https://registry.yarnpkg.com/lodash/-/lodash-4.17.21.tgz",
			wantName:    "lodash",
			wantVersion: "4.17.21",
		},
		{
			name:        "no .tgz suffix",
			url:         "https://registry.npmjs.org/foo/-/foo-1.0.0",
			wantName:    "foo",
			wantVersion: "1.0.0",
		},
		{
			name:        "empty string",
			url:         "",
			wantName:    "",
			wantVersion: "",
		},
		{
			name:        "no slash",
			url:         "no-slash-at-all",
			wantName:    "",
			wantVersion: "",
		},
		{
			name:        "no dash separator in filename",
			url:         "https://registry.npmjs.org/pkg/-/pkg.tgz",
			wantName:    "",
			wantVersion: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, version := parseTarballURL(tt.url)
			if name != tt.wantName {
				t.Errorf("parseTarballURL(%q) name = %q, want %q", tt.url, name, tt.wantName)
			}
			if version != tt.wantVersion {
				t.Errorf("parseTarballURL(%q) version = %q, want %q", tt.url, version, tt.wantVersion)
			}
		})
	}
}

func TestIsNonRegistrySpecifier(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		want       bool
	}{
		// Non-registry specifiers (should return true).
		{"file: prefix", "file:./local-pkg", true},
		{"link: prefix", "link:../other-pkg", true},
		{"portal: prefix", "portal:../portal-pkg", true},
		{"git+ prefix", "git+https://github.com/foo/bar.git", true},
		{"git:// prefix", "git://github.com/foo/bar.git", true},
		{"git@ prefix", "git@github.com:foo/bar.git", true},
		{"github: prefix", "github:owner/repo", true},
		{"bitbucket: prefix", "bitbucket:owner/repo", true},
		{"gitlab: prefix", "gitlab:owner/repo", true},
		{"workspace: prefix", "workspace:^1.0.0", true},
		{"patch: prefix", "patch:pkg@1.0.0#./fix.patch", true},
		{"exec: prefix", "exec:./generate-pkg", true},
		{"http:// prefix", "http://example.com/pkg.tgz", true},
		{"https:// prefix", "https://registry.npmjs.org/foo/-/foo-1.0.0.tgz", true},
		// Registry specifiers (should return false).
		{"semver range", "^1.2.3", false},
		{"exact version", "1.0.0", false},
		{"tilde range", "~2.0.0", false},
		{"star", "*", false},
		{"npm: alias", "npm:other-pkg@^1.0.0", false},
		{"empty string", "", false},
		{"range with spaces", ">=1.0.0 <2.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNonRegistrySpecifier(tt.constraint)
			if got != tt.want {
				t.Errorf("isNonRegistrySpecifier(%q) = %v, want %v", tt.constraint, got, tt.want)
			}
		})
	}
}
