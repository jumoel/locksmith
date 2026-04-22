package npm

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jumoel/locksmith/ecosystem"
)

// testdataPath returns the absolute path to a file in the testdata directory.
func testdataPath(name string) string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "testdata", name)
}

func TestSpecParser_Parse_Minimal(t *testing.T) {
	data, err := os.ReadFile(testdataPath("minimal.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	p := NewSpecParser()
	spec, err := p.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if spec.Name != "my-minimal-app" {
		t.Errorf("Name = %q, want %q", spec.Name, "my-minimal-app")
	}
	if spec.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", spec.Version, "1.0.0")
	}
	if len(spec.Dependencies) != 0 {
		t.Errorf("Dependencies count = %d, want 0", len(spec.Dependencies))
	}
}

func TestSpecParser_Parse_Full(t *testing.T) {
	data, err := os.ReadFile(testdataPath("full.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	p := NewSpecParser()
	spec, err := p.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if spec.Name != "my-full-app" {
		t.Errorf("Name = %q, want %q", spec.Name, "my-full-app")
	}
	if spec.Version != "2.5.0" {
		t.Errorf("Version = %q, want %q", spec.Version, "2.5.0")
	}

	// 3 regular + 2 dev + 2 peer + 1 optional = 8
	if got := len(spec.Dependencies); got != 8 {
		t.Fatalf("Dependencies count = %d, want 8", got)
	}

	// Verify sorting: deps should be sorted by name.
	for i := 1; i < len(spec.Dependencies); i++ {
		prev := spec.Dependencies[i-1]
		curr := spec.Dependencies[i]
		if prev.Name > curr.Name {
			t.Errorf("dependencies not sorted: %q > %q at index %d", prev.Name, curr.Name, i)
		}
	}

	// Build a lookup for spot checks.
	type depKey struct {
		name string
		typ  ecosystem.DepType
	}
	lookup := make(map[depKey]string)
	for _, d := range spec.Dependencies {
		lookup[depKey{d.Name, d.Type}] = d.Constraint
	}

	checks := []struct {
		name       string
		typ        ecosystem.DepType
		constraint string
	}{
		{"express", ecosystem.DepRegular, "^4.18.0"},
		{"lodash", ecosystem.DepRegular, "~4.17.21"},
		{"axios", ecosystem.DepRegular, ">=1.0.0"},
		{"jest", ecosystem.DepDev, "^29.0.0"},
		{"typescript", ecosystem.DepDev, "~5.3.0"},
		{"react", ecosystem.DepPeer, "^18.0.0"},
		{"react-dom", ecosystem.DepPeer, "^18.0.0"},
		{"fsevents", ecosystem.DepOptional, "^2.3.0"},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			got, ok := lookup[depKey{c.name, c.typ}]
			if !ok {
				t.Fatalf("dependency %q (type %d) not found", c.name, c.typ)
			}
			if got != c.constraint {
				t.Errorf("constraint = %q, want %q", got, c.constraint)
			}
		})
	}

	// Verify peerDependenciesMeta: react-dom is optional, react is not.
	if spec.PeerDepsMeta == nil {
		t.Fatal("PeerDepsMeta is nil, expected non-nil")
	}
	if pm, ok := spec.PeerDepsMeta["react-dom"]; !ok {
		t.Error("PeerDepsMeta missing react-dom entry")
	} else if !pm.Optional {
		t.Error("PeerDepsMeta[react-dom].Optional = false, want true")
	}
	if _, ok := spec.PeerDepsMeta["react"]; ok {
		t.Error("PeerDepsMeta should not contain react (not in peerDependenciesMeta)")
	}
}

func TestSpecParser_Parse_PeerDepsMeta(t *testing.T) {
	t.Run("mixed optional and required peers", func(t *testing.T) {
		data := []byte(`{
			"name": "test",
			"peerDependencies": {
				"react": "^18.0.0",
				"react-dom": "^18.0.0",
				"@types/react": "^18.0.0"
			},
			"peerDependenciesMeta": {
				"react-dom": { "optional": true },
				"@types/react": { "optional": true }
			}
		}`)

		p := NewSpecParser()
		spec, err := p.Parse(data)
		if err != nil {
			t.Fatalf("Parse() error: %v", err)
		}

		if spec.PeerDepsMeta == nil {
			t.Fatal("PeerDepsMeta is nil")
		}
		if len(spec.PeerDepsMeta) != 2 {
			t.Fatalf("PeerDepsMeta count = %d, want 2", len(spec.PeerDepsMeta))
		}
		if !spec.PeerDepsMeta["react-dom"].Optional {
			t.Error("react-dom should be optional")
		}
		if !spec.PeerDepsMeta["@types/react"].Optional {
			t.Error("@types/react should be optional")
		}
		if _, ok := spec.PeerDepsMeta["react"]; ok {
			t.Error("react should not be in PeerDepsMeta")
		}
	})

	t.Run("no peerDependenciesMeta", func(t *testing.T) {
		data := []byte(`{
			"name": "test",
			"peerDependencies": {
				"react": "^18.0.0"
			}
		}`)

		p := NewSpecParser()
		spec, err := p.Parse(data)
		if err != nil {
			t.Fatalf("Parse() error: %v", err)
		}

		if spec.PeerDepsMeta != nil {
			t.Errorf("PeerDepsMeta should be nil, got %v", spec.PeerDepsMeta)
		}
	})

	t.Run("all peers optional", func(t *testing.T) {
		data := []byte(`{
			"name": "test",
			"peerDependencies": {
				"react": "^18.0.0",
				"react-dom": "^18.0.0"
			},
			"peerDependenciesMeta": {
				"react": { "optional": true },
				"react-dom": { "optional": true }
			}
		}`)

		p := NewSpecParser()
		spec, err := p.Parse(data)
		if err != nil {
			t.Fatalf("Parse() error: %v", err)
		}

		if spec.PeerDepsMeta == nil {
			t.Fatal("PeerDepsMeta is nil")
		}
		if len(spec.PeerDepsMeta) != 2 {
			t.Fatalf("PeerDepsMeta count = %d, want 2", len(spec.PeerDepsMeta))
		}
		if !spec.PeerDepsMeta["react"].Optional {
			t.Error("react should be optional")
		}
		if !spec.PeerDepsMeta["react-dom"].Optional {
			t.Error("react-dom should be optional")
		}
	})
}

func TestSpecParser_Parse_InvalidJSON(t *testing.T) {
	p := NewSpecParser()
	_, err := p.Parse([]byte(`{invalid json`))
	if err == nil {
		t.Fatal("Parse() expected error for invalid JSON, got nil")
	}
}

func TestSpecParser_Parse_MissingNameVersion(t *testing.T) {
	// Some package.json files (e.g., monorepo roots) omit name/version.
	data := []byte(`{"dependencies": {"foo": "^1.0.0"}}`)

	p := NewSpecParser()
	spec, err := p.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if spec.Name != "" {
		t.Errorf("Name = %q, want empty", spec.Name)
	}
	if spec.Version != "" {
		t.Errorf("Version = %q, want empty", spec.Version)
	}
	if len(spec.Dependencies) != 1 {
		t.Fatalf("Dependencies count = %d, want 1", len(spec.Dependencies))
	}
	if spec.Dependencies[0].Name != "foo" {
		t.Errorf("dep name = %q, want %q", spec.Dependencies[0].Name, "foo")
	}
	if spec.Dependencies[0].Type != ecosystem.DepRegular {
		t.Errorf("dep type = %d, want %d", spec.Dependencies[0].Type, ecosystem.DepRegular)
	}
}

func TestSpecParser_Parse_EmptyObject(t *testing.T) {
	p := NewSpecParser()
	spec, err := p.Parse([]byte(`{}`))
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if len(spec.Dependencies) != 0 {
		t.Errorf("Dependencies count = %d, want 0", len(spec.Dependencies))
	}
}

func TestSpecParser_Parse_DependenciesSorted(t *testing.T) {
	// Deliberately unsorted input to verify sort behavior.
	data := []byte(`{
		"dependencies": {
			"zlib": "^1.0.0",
			"axios": "^1.0.0",
			"morgan": "^1.0.0"
		}
	}`)

	p := NewSpecParser()
	spec, err := p.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	expected := []string{"axios", "morgan", "zlib"}
	if len(spec.Dependencies) != len(expected) {
		t.Fatalf("Dependencies count = %d, want %d", len(spec.Dependencies), len(expected))
	}
	for i, name := range expected {
		if spec.Dependencies[i].Name != name {
			t.Errorf("Dependencies[%d].Name = %q, want %q", i, spec.Dependencies[i].Name, name)
		}
	}
}
