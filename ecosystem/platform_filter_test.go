package ecosystem

import "testing"

func TestParsePlatform(t *testing.T) {
	tests := []struct {
		input   string
		wantOS  string
		wantCPU string
		wantErr bool
	}{
		{"linux/x64", "linux", "x64", false},
		{"darwin/arm64", "darwin", "arm64", false},
		{"win32/ia32", "win32", "ia32", false},
		{"linux", "", "", true},
		{"", "", "", true},
		{"/x64", "", "", true},
		{"linux/", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			plat, err := ParsePlatform(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParsePlatform(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if err == nil {
				if plat.OS != tt.wantOS || plat.CPU != tt.wantCPU {
					t.Errorf("ParsePlatform(%q) = {%s, %s}, want {%s, %s}",
						tt.input, plat.OS, plat.CPU, tt.wantOS, tt.wantCPU)
				}
			}
		})
	}
}

func TestNodeMatchesPlatform(t *testing.T) {
	linuxX64 := Platform{OS: "linux", CPU: "x64"}

	tests := []struct {
		name string
		node *Node
		plat Platform
		want bool
	}{
		{
			name: "no restrictions",
			node: &Node{Name: "foo", Version: "1.0.0"},
			plat: linuxX64,
			want: true,
		},
		{
			name: "matching OS only",
			node: &Node{Name: "foo", Version: "1.0.0", OS: []string{"linux", "darwin"}},
			plat: linuxX64,
			want: true,
		},
		{
			name: "non-matching OS",
			node: &Node{Name: "foo", Version: "1.0.0", OS: []string{"darwin"}},
			plat: linuxX64,
			want: false,
		},
		{
			name: "matching CPU only",
			node: &Node{Name: "foo", Version: "1.0.0", CPU: []string{"x64", "arm64"}},
			plat: linuxX64,
			want: true,
		},
		{
			name: "non-matching CPU",
			node: &Node{Name: "foo", Version: "1.0.0", CPU: []string{"arm64"}},
			plat: linuxX64,
			want: false,
		},
		{
			name: "matching both OS and CPU",
			node: &Node{Name: "foo", Version: "1.0.0", OS: []string{"linux"}, CPU: []string{"x64"}},
			plat: linuxX64,
			want: true,
		},
		{
			name: "matching OS but not CPU",
			node: &Node{Name: "foo", Version: "1.0.0", OS: []string{"linux"}, CPU: []string{"arm64"}},
			plat: linuxX64,
			want: false,
		},
		{
			name: "negation excludes target OS",
			node: &Node{Name: "foo", Version: "1.0.0", OS: []string{"!linux"}},
			plat: linuxX64,
			want: false,
		},
		{
			name: "negation does not exclude target OS",
			node: &Node{Name: "foo", Version: "1.0.0", OS: []string{"!win32"}},
			plat: linuxX64,
			want: true,
		},
		{
			name: "negation excludes target CPU",
			node: &Node{Name: "foo", Version: "1.0.0", CPU: []string{"!x64"}},
			plat: linuxX64,
			want: false,
		},
		{
			name: "negation does not exclude target CPU",
			node: &Node{Name: "foo", Version: "1.0.0", CPU: []string{"!arm64"}},
			plat: linuxX64,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NodeMatchesPlatform(tt.node, tt.plat)
			if got != tt.want {
				t.Errorf("NodeMatchesPlatform() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterGraphByPlatform(t *testing.T) {
	darwinNode := &Node{
		Name: "darwin-pkg", Version: "1.0.0",
		OS: []string{"darwin"},
	}
	linuxNode := &Node{
		Name: "linux-pkg", Version: "2.0.0",
		OS: []string{"linux"},
	}
	universalNode := &Node{
		Name: "universal", Version: "3.0.0",
	}

	graph := &Graph{
		Root: &Node{
			Name:    "root",
			Version: "0.0.0",
			Dependencies: []*Edge{
				{Name: "darwin-pkg", Target: darwinNode},
				{Name: "linux-pkg", Target: linuxNode},
				{Name: "universal", Target: universalNode},
			},
		},
		Nodes: map[string]*Node{
			"darwin-pkg@1.0.0": darwinNode,
			"linux-pkg@2.0.0":  linuxNode,
			"universal@3.0.0":  universalNode,
		},
	}

	plat := Platform{OS: "linux", CPU: "x64"}
	removed := FilterGraphByPlatform(graph, plat)

	if !removed["darwin-pkg@1.0.0"] {
		t.Error("expected darwin-pkg to be removed")
	}
	if removed["linux-pkg@2.0.0"] {
		t.Error("expected linux-pkg to be kept")
	}
	if removed["universal@3.0.0"] {
		t.Error("expected universal to be kept")
	}

	if _, ok := graph.Nodes["darwin-pkg@1.0.0"]; ok {
		t.Error("darwin-pkg should be removed from graph.Nodes")
	}
	if _, ok := graph.Nodes["linux-pkg@2.0.0"]; !ok {
		t.Error("linux-pkg should remain in graph.Nodes")
	}

	// Root edges should have darwin-pkg removed.
	for _, edge := range graph.Root.Dependencies {
		if edge.Name == "darwin-pkg" {
			t.Error("darwin-pkg edge should be pruned from root")
		}
	}
	if len(graph.Root.Dependencies) != 2 {
		t.Errorf("root should have 2 edges, got %d", len(graph.Root.Dependencies))
	}
}

func TestFilterGraphByPlatform_RootOptionalDepsExempt(t *testing.T) {
	// Root optional deps that are platform-incompatible must NOT be filtered.
	// Package managers require them in the lockfile specifiers even when the
	// platform does not match; the PM handles platform checks at install time.
	darwinOnlyNode := &Node{
		Name: "darwin-optional", Version: "1.0.0",
		OS: []string{"darwin"},
	}
	win32OnlyNode := &Node{
		Name: "win32-optional", Version: "2.0.0",
		OS: []string{"win32"},
	}
	transitiveIncompatible := &Node{
		Name: "transitive-darwin", Version: "1.0.0",
		OS: []string{"darwin"},
	}

	graph := &Graph{
		Root: &Node{
			Name:    "root",
			Version: "0.0.0",
			Dependencies: []*Edge{
				// Root optional dep - incompatible but should be KEPT.
				{Name: "darwin-optional", Target: darwinOnlyNode, Type: DepOptional},
				// Root optional dep - also incompatible, should be KEPT.
				{Name: "win32-optional", Target: win32OnlyNode, Type: DepOptional},
				// Non-optional root dep that has an incompatible transitive dep.
				{Name: "transitive-darwin", Target: transitiveIncompatible, Type: DepRegular},
			},
		},
		Nodes: map[string]*Node{
			"darwin-optional@1.0.0":    darwinOnlyNode,
			"win32-optional@2.0.0":     win32OnlyNode,
			"transitive-darwin@1.0.0":  transitiveIncompatible,
		},
	}

	plat := Platform{OS: "linux", CPU: "x64"}
	removed := FilterGraphByPlatform(graph, plat)

	// Root optional deps must NOT be removed despite platform mismatch.
	if removed["darwin-optional@1.0.0"] {
		t.Error("root optional dep darwin-optional should NOT be filtered")
	}
	if removed["win32-optional@2.0.0"] {
		t.Error("root optional dep win32-optional should NOT be filtered")
	}

	// Non-optional incompatible dep SHOULD be removed.
	if !removed["transitive-darwin@1.0.0"] {
		t.Error("non-optional incompatible dep transitive-darwin should be filtered")
	}

	// Verify the nodes map reflects the exemption.
	if _, ok := graph.Nodes["darwin-optional@1.0.0"]; !ok {
		t.Error("darwin-optional should remain in graph.Nodes")
	}
	if _, ok := graph.Nodes["win32-optional@2.0.0"]; !ok {
		t.Error("win32-optional should remain in graph.Nodes")
	}
	if _, ok := graph.Nodes["transitive-darwin@1.0.0"]; ok {
		t.Error("transitive-darwin should be removed from graph.Nodes")
	}
}

func TestFilterGraphByPlatform_NonOptionalRootDepsStillFiltered(t *testing.T) {
	// Regular (non-optional) root deps that are platform-incompatible
	// should still be filtered normally.
	darwinRegular := &Node{
		Name: "darwin-regular", Version: "1.0.0",
		OS: []string{"darwin"},
	}

	graph := &Graph{
		Root: &Node{
			Name:    "root",
			Version: "0.0.0",
			Dependencies: []*Edge{
				{Name: "darwin-regular", Target: darwinRegular, Type: DepRegular},
			},
		},
		Nodes: map[string]*Node{
			"darwin-regular@1.0.0": darwinRegular,
		},
	}

	plat := Platform{OS: "linux", CPU: "x64"}
	removed := FilterGraphByPlatform(graph, plat)

	if !removed["darwin-regular@1.0.0"] {
		t.Error("non-optional incompatible root dep should be filtered")
	}
}
