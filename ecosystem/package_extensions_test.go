package ecosystem

import (
	"testing"
)

func TestPackageExtensionSet_Apply_Match(t *testing.T) {
	pes := &PackageExtensionSet{
		Extensions: []PackageExtension{
			{
				Name:         "my-pkg",
				Dependencies: map[string]string{"extra-dep": "^1.0.0"},
			},
		},
	}
	meta := &VersionMetadata{
		Name:         "my-pkg",
		Version:      "2.0.0",
		Dependencies: map[string]string{"existing": "^3.0.0"},
	}

	pes.Apply(meta)

	if len(meta.Dependencies) != 2 {
		t.Fatalf("expected 2 deps, got %d", len(meta.Dependencies))
	}
	if meta.Dependencies["extra-dep"] != "^1.0.0" {
		t.Errorf("Dependencies[extra-dep] = %q, want %q", meta.Dependencies["extra-dep"], "^1.0.0")
	}
	if meta.Dependencies["existing"] != "^3.0.0" {
		t.Errorf("Dependencies[existing] = %q, want %q", meta.Dependencies["existing"], "^3.0.0")
	}
}

func TestPackageExtensionSet_Apply_VersionMatch(t *testing.T) {
	pes := &PackageExtensionSet{
		Extensions: []PackageExtension{
			{
				Name:         "my-pkg",
				VersionRange: "^2.0.0",
				Dependencies: map[string]string{"extra-dep": "^1.0.0"},
			},
		},
	}
	meta := &VersionMetadata{
		Name:    "my-pkg",
		Version: "2.3.0",
	}

	pes.Apply(meta)

	if len(meta.Dependencies) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(meta.Dependencies))
	}
	if meta.Dependencies["extra-dep"] != "^1.0.0" {
		t.Errorf("Dependencies[extra-dep] = %q, want %q", meta.Dependencies["extra-dep"], "^1.0.0")
	}
}

func TestPackageExtensionSet_Apply_VersionNoMatch(t *testing.T) {
	pes := &PackageExtensionSet{
		Extensions: []PackageExtension{
			{
				Name:         "my-pkg",
				VersionRange: "^2.0.0",
				Dependencies: map[string]string{"extra-dep": "^1.0.0"},
			},
		},
	}
	meta := &VersionMetadata{
		Name:    "my-pkg",
		Version: "1.5.0",
	}

	pes.Apply(meta)

	if len(meta.Dependencies) != 0 {
		t.Errorf("expected 0 deps (version should not match), got %d", len(meta.Dependencies))
	}
}

func TestPackageExtensionSet_Apply_NoOverwrite(t *testing.T) {
	pes := &PackageExtensionSet{
		Extensions: []PackageExtension{
			{
				Name:         "my-pkg",
				Dependencies: map[string]string{"foo": "^2.0.0"},
				PeerDeps:     map[string]string{"bar": "^3.0.0"},
			},
		},
	}
	meta := &VersionMetadata{
		Name:         "my-pkg",
		Version:      "1.0.0",
		Dependencies: map[string]string{"foo": "^1.0.0"},
		PeerDeps:     map[string]string{"bar": "^1.0.0"},
	}

	pes.Apply(meta)

	// Existing deps must NOT be overwritten.
	if meta.Dependencies["foo"] != "^1.0.0" {
		t.Errorf("Dependencies[foo] = %q, want %q (should not overwrite)", meta.Dependencies["foo"], "^1.0.0")
	}
	if meta.PeerDeps["bar"] != "^1.0.0" {
		t.Errorf("PeerDeps[bar] = %q, want %q (should not overwrite)", meta.PeerDeps["bar"], "^1.0.0")
	}
}

func TestPackageExtensionSet_Apply_Nil(t *testing.T) {
	// nil set - should not panic.
	var pes *PackageExtensionSet
	meta := &VersionMetadata{Name: "foo", Version: "1.0.0"}
	pes.Apply(meta) // must not panic

	// nil meta - should not panic.
	pes2 := &PackageExtensionSet{
		Extensions: []PackageExtension{
			{Name: "foo", Dependencies: map[string]string{"bar": "^1.0.0"}},
		},
	}
	pes2.Apply(nil) // must not panic
}

func TestPackageExtensionSet_Apply_PeerDeps(t *testing.T) {
	pes := &PackageExtensionSet{
		Extensions: []PackageExtension{
			{
				Name:     "my-pkg",
				PeerDeps: map[string]string{"react": "*", "react-dom": ">=16"},
			},
		},
	}
	meta := &VersionMetadata{
		Name:    "my-pkg",
		Version: "1.0.0",
	}

	pes.Apply(meta)

	if len(meta.PeerDeps) != 2 {
		t.Fatalf("expected 2 peer deps, got %d", len(meta.PeerDeps))
	}
	if meta.PeerDeps["react"] != "*" {
		t.Errorf("PeerDeps[react] = %q, want %q", meta.PeerDeps["react"], "*")
	}
	if meta.PeerDeps["react-dom"] != ">=16" {
		t.Errorf("PeerDeps[react-dom] = %q, want %q", meta.PeerDeps["react-dom"], ">=16")
	}
}

func TestPackageExtensionSet_Apply_NilDepsInitialized(t *testing.T) {
	// When meta has nil Dependencies map, Apply should initialize it.
	pes := &PackageExtensionSet{
		Extensions: []PackageExtension{
			{
				Name:         "my-pkg",
				Dependencies: map[string]string{"foo": "^1.0.0"},
				PeerDeps:     map[string]string{"bar": "*"},
			},
		},
	}
	meta := &VersionMetadata{
		Name:    "my-pkg",
		Version: "1.0.0",
		// Dependencies and PeerDeps are nil
	}

	pes.Apply(meta)

	if meta.Dependencies == nil {
		t.Fatal("Dependencies should be initialized, got nil")
	}
	if meta.Dependencies["foo"] != "^1.0.0" {
		t.Errorf("Dependencies[foo] = %q, want %q", meta.Dependencies["foo"], "^1.0.0")
	}
	if meta.PeerDeps == nil {
		t.Fatal("PeerDeps should be initialized, got nil")
	}
	if meta.PeerDeps["bar"] != "*" {
		t.Errorf("PeerDeps[bar] = %q, want %q", meta.PeerDeps["bar"], "*")
	}
}

func TestPackageExtensionSet_Apply_NameMismatch(t *testing.T) {
	pes := &PackageExtensionSet{
		Extensions: []PackageExtension{
			{
				Name:         "other-pkg",
				Dependencies: map[string]string{"foo": "^1.0.0"},
			},
		},
	}
	meta := &VersionMetadata{
		Name:    "my-pkg",
		Version: "1.0.0",
	}

	pes.Apply(meta)

	if len(meta.Dependencies) != 0 {
		t.Errorf("expected 0 deps (name mismatch), got %d", len(meta.Dependencies))
	}
}
