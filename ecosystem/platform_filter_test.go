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
