package semver

import (
	"testing"
)

func TestParse_ValidVersions(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantStr string
	}{
		{name: "simple", input: "1.0.0", wantStr: "1.0.0"},
		{name: "with patch", input: "1.2.3", wantStr: "1.2.3"},
		{name: "prerelease", input: "1.2.3-beta.1", wantStr: "1.2.3-beta.1"},
		{name: "v-prefix", input: "v1.0.0", wantStr: "1.0.0"},
		{name: "v-prefix with prerelease", input: "v2.0.0-rc.1", wantStr: "2.0.0-rc.1"},
		{name: "large numbers", input: "10.200.3000", wantStr: "10.200.3000"},
		{name: "zero version", input: "0.0.0", wantStr: "0.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) returned unexpected error: %v", tt.input, err)
			}
			if got := v.String(); got != tt.wantStr {
				t.Errorf("Parse(%q).String() = %q, want %q", tt.input, got, tt.wantStr)
			}
		})
	}
}

func TestParse_InvalidVersions(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty string", input: ""},
		{name: "garbage", input: "not-a-version"},
		{name: "negative segment", input: "1.-2.3"},
		{name: "trailing dot", input: "1.2.3."},
		{name: "four segments", input: "1.2.3.4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.input)
			if err == nil {
				t.Fatalf("Parse(%q) expected error, got nil", tt.input)
			}
		})
	}
}

func TestParseConstraint_Valid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "caret", input: "^1.2.3"},
		{name: "tilde", input: "~1.0.0"},
		{name: "gte", input: ">=1.0.0"},
		{name: "x-range", input: "1.x"},
		{name: "star", input: "*"},
		{name: "empty", input: ""},
		{name: "latest", input: "latest"},
		{name: "range", input: ">=1.0.0 <2.0.0"},
		{name: "or", input: "^1.0.0 || ^2.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := ParseConstraint(tt.input)
			if err != nil {
				t.Fatalf("ParseConstraint(%q) returned unexpected error: %v", tt.input, err)
			}
			if c == nil {
				t.Fatalf("ParseConstraint(%q) returned nil constraint", tt.input)
			}
		})
	}
}

func TestConstraint_Check(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		version    string
		want       bool
	}{
		// Caret: ^1.2.3 allows >=1.2.3 <2.0.0
		{name: "caret exact match", constraint: "^1.2.3", version: "1.2.3", want: true},
		{name: "caret higher minor", constraint: "^1.2.3", version: "1.9.0", want: true},
		{name: "caret higher patch", constraint: "^1.2.3", version: "1.2.9", want: true},
		{name: "caret next major", constraint: "^1.2.3", version: "2.0.0", want: false},
		{name: "caret lower patch", constraint: "^1.2.3", version: "1.2.2", want: false},
		{name: "caret lower minor", constraint: "^1.2.3", version: "1.1.0", want: false},

		// Tilde: ~1.2.3 allows >=1.2.3 <1.3.0
		{name: "tilde exact match", constraint: "~1.2.3", version: "1.2.3", want: true},
		{name: "tilde higher patch", constraint: "~1.2.3", version: "1.2.9", want: true},
		{name: "tilde next minor", constraint: "~1.2.3", version: "1.3.0", want: false},
		{name: "tilde lower patch", constraint: "~1.2.3", version: "1.2.2", want: false},

		// Caret 0.x: ^0.1.0 allows >=0.1.0 <0.2.0
		{name: "caret 0.x exact", constraint: "^0.1.0", version: "0.1.0", want: true},
		{name: "caret 0.x higher patch", constraint: "^0.1.0", version: "0.1.5", want: true},
		{name: "caret 0.x next minor", constraint: "^0.1.0", version: "0.2.0", want: false},

		// Star matches anything
		{name: "star matches low", constraint: "*", version: "0.0.1", want: true},
		{name: "star matches high", constraint: "*", version: "99.99.99", want: true},

		// Empty matches anything
		{name: "empty matches low", constraint: "", version: "0.0.1", want: true},
		{name: "empty matches high", constraint: "", version: "99.99.99", want: true},

		// Range: >=1.0.0 <2.0.0
		{name: "range lower bound", constraint: ">=1.0.0 <2.0.0", version: "1.0.0", want: true},
		{name: "range mid", constraint: ">=1.0.0 <2.0.0", version: "1.9.9", want: true},
		{name: "range upper bound excluded", constraint: ">=1.0.0 <2.0.0", version: "2.0.0", want: false},
		{name: "range below", constraint: ">=1.0.0 <2.0.0", version: "0.9.9", want: false},

		// Or: ^1.0.0 || ^2.0.0
		{name: "or first branch", constraint: "^1.0.0 || ^2.0.0", version: "1.5.0", want: true},
		{name: "or second branch", constraint: "^1.0.0 || ^2.0.0", version: "2.3.0", want: true},
		{name: "or neither branch", constraint: "^1.0.0 || ^2.0.0", version: "3.0.0", want: false},
		{name: "or below both", constraint: "^1.0.0 || ^2.0.0", version: "0.9.0", want: false},

		// Latest
		{name: "latest matches anything", constraint: "latest", version: "5.0.0", want: true},

		// x-range
		{name: "x-range 1.x matches 1.0.0", constraint: "1.x", version: "1.0.0", want: true},
		{name: "x-range 1.x matches 1.99.0", constraint: "1.x", version: "1.99.0", want: true},
		{name: "x-range 1.x rejects 2.0.0", constraint: "1.x", version: "2.0.0", want: false},

		// GTE
		{name: "gte match", constraint: ">=1.0.0", version: "1.0.0", want: true},
		{name: "gte higher", constraint: ">=1.0.0", version: "5.0.0", want: true},
		{name: "gte below", constraint: ">=1.0.0", version: "0.9.9", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := ParseConstraint(tt.constraint)
			if err != nil {
				t.Fatalf("ParseConstraint(%q) error: %v", tt.constraint, err)
			}
			v, err := Parse(tt.version)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.version, err)
			}
			if got := c.Check(v); got != tt.want {
				t.Errorf("Constraint(%q).Check(%q) = %v, want %v", tt.constraint, tt.version, got, tt.want)
			}
		})
	}
}

func TestMaxSatisfying(t *testing.T) {
	tests := []struct {
		name       string
		versions   []string
		constraint string
		want       string // empty string means nil expected
	}{
		{
			name:       "picks highest in caret range",
			versions:   []string{"1.0.0", "1.2.0", "1.5.0", "2.0.0"},
			constraint: "^1.0.0",
			want:       "1.5.0",
		},
		{
			name:       "picks highest in tilde range",
			versions:   []string{"1.2.0", "1.2.5", "1.2.9", "1.3.0"},
			constraint: "~1.2.0",
			want:       "1.2.9",
		},
		{
			name:       "no match returns nil",
			versions:   []string{"1.0.0", "1.1.0"},
			constraint: "^2.0.0",
			want:       "",
		},
		{
			name:       "star picks highest",
			versions:   []string{"0.1.0", "1.0.0", "3.0.0", "2.0.0"},
			constraint: "*",
			want:       "3.0.0",
		},
		{
			name:       "empty list returns nil",
			versions:   []string{},
			constraint: "^1.0.0",
			want:       "",
		},
		{
			name:       "single matching version",
			versions:   []string{"1.0.0"},
			constraint: "^1.0.0",
			want:       "1.0.0",
		},
		{
			name:       "or picks highest across branches",
			versions:   []string{"1.0.0", "1.5.0", "2.0.0", "2.3.0"},
			constraint: "^1.0.0 || ^2.0.0",
			want:       "2.3.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var versions []*Version
			for _, vs := range tt.versions {
				v, err := Parse(vs)
				if err != nil {
					t.Fatalf("Parse(%q) error: %v", vs, err)
				}
				versions = append(versions, v)
			}

			c, err := ParseConstraint(tt.constraint)
			if err != nil {
				t.Fatalf("ParseConstraint(%q) error: %v", tt.constraint, err)
			}

			got := MaxSatisfying(versions, c)
			if tt.want == "" {
				if got != nil {
					t.Errorf("MaxSatisfying() = %q, want nil", got.String())
				}
				return
			}
			if got == nil {
				t.Fatalf("MaxSatisfying() = nil, want %q", tt.want)
			}
			if got.String() != tt.want {
				t.Errorf("MaxSatisfying() = %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestConstraint_String(t *testing.T) {
	// String() should return the original raw input.
	inputs := []string{"^1.2.3", "~1.0.0", "", "*", "latest", ">=1.0.0 <2.0.0"}
	for _, input := range inputs {
		c, err := ParseConstraint(input)
		if err != nil {
			t.Fatalf("ParseConstraint(%q) error: %v", input, err)
		}
		if got := c.String(); got != input {
			t.Errorf("Constraint.String() = %q, want %q", got, input)
		}
	}
}

func TestVersion_Comparisons(t *testing.T) {
	v1, _ := Parse("1.0.0")
	v2, _ := Parse("2.0.0")
	v1dup, _ := Parse("1.0.0")

	if !v1.LessThan(v2) {
		t.Error("expected 1.0.0 < 2.0.0")
	}
	if v2.LessThan(v1) {
		t.Error("expected 2.0.0 not < 1.0.0")
	}
	if !v2.GreaterThan(v1) {
		t.Error("expected 2.0.0 > 1.0.0")
	}
	if !v1.Equal(v1dup) {
		t.Error("expected 1.0.0 == 1.0.0")
	}
	if v1.Equal(v2) {
		t.Error("expected 1.0.0 != 2.0.0")
	}
}

func TestVersion_Prerelease(t *testing.T) {
	v, err := Parse("1.2.3-beta.1")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if got := v.Prerelease(); got != "beta.1" {
		t.Errorf("Prerelease() = %q, want %q", got, "beta.1")
	}

	v2, err := Parse("1.2.3")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if got := v2.Prerelease(); got != "" {
		t.Errorf("Prerelease() = %q, want empty", got)
	}
}
