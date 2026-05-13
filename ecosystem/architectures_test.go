package ecosystem

import "testing"

func TestArchitectures_FromPlatform(t *testing.T) {
	plat := Platform{OS: "linux", CPU: "x64"}
	a := ArchitecturesFromPlatform(plat)
	if len(a.OS) != 1 || a.OS[0] != "linux" {
		t.Errorf("OS = %v, want [linux]", a.OS)
	}
	if len(a.CPU) != 1 || a.CPU[0] != "x64" {
		t.Errorf("CPU = %v, want [x64]", a.CPU)
	}
	if a.Libc != nil {
		t.Errorf("Libc = %v, want nil (Platform has no libc)", a.Libc)
	}
}

func TestArchitectures_Empty_KeepsAll(t *testing.T) {
	// Zero-value Architectures means "no filtering"; every node is kept.
	a := Architectures{}
	cases := []*Node{
		{Name: "x", OS: []string{"linux"}, CPU: []string{"x64"}},
		{Name: "y", OS: []string{"win32"}, CPU: []string{"arm64"}, Libc: []string{"musl"}},
		{Name: "z"},
	}
	for _, n := range cases {
		if !NodeMatchesArchitectures(n, a) {
			t.Errorf("zero-value Architectures should match %+v but did not", n)
		}
	}
}

func TestArchitectures_OSFilter(t *testing.T) {
	a := Architectures{OS: []string{"linux", "darwin"}}
	if !NodeMatchesArchitectures(&Node{OS: []string{"linux"}}, a) {
		t.Error("linux node should match {linux, darwin}")
	}
	if !NodeMatchesArchitectures(&Node{OS: []string{"darwin"}}, a) {
		t.Error("darwin node should match {linux, darwin}")
	}
	if NodeMatchesArchitectures(&Node{OS: []string{"win32"}}, a) {
		t.Error("win32 node should NOT match {linux, darwin}")
	}
	if !NodeMatchesArchitectures(&Node{OS: nil}, a) {
		t.Error("empty-OS node should match (no restriction)")
	}
}

func TestArchitectures_CPUFilter(t *testing.T) {
	a := Architectures{CPU: []string{"x64", "arm64"}}
	if !NodeMatchesArchitectures(&Node{CPU: []string{"arm64"}}, a) {
		t.Error("arm64 should match {x64, arm64}")
	}
	if NodeMatchesArchitectures(&Node{CPU: []string{"ia32"}}, a) {
		t.Error("ia32 should NOT match {x64, arm64}")
	}
}

func TestArchitectures_LibcFilter(t *testing.T) {
	a := Architectures{Libc: []string{"glibc"}}
	if !NodeMatchesArchitectures(&Node{Libc: []string{"glibc"}}, a) {
		t.Error("glibc should match {glibc}")
	}
	if NodeMatchesArchitectures(&Node{Libc: []string{"musl"}}, a) {
		t.Error("musl should NOT match {glibc}")
	}
	if !NodeMatchesArchitectures(&Node{Libc: nil}, a) {
		t.Error("empty-libc node (any libc) should match")
	}
}

func TestArchitectures_AllAxes(t *testing.T) {
	a := Architectures{OS: []string{"linux"}, CPU: []string{"x64"}, Libc: []string{"musl"}}
	// Match
	if !NodeMatchesArchitectures(&Node{OS: []string{"linux"}, CPU: []string{"x64"}, Libc: []string{"musl"}}, a) {
		t.Error("exact match should pass")
	}
	// Mismatch any axis -> false
	if NodeMatchesArchitectures(&Node{OS: []string{"linux"}, CPU: []string{"x64"}, Libc: []string{"glibc"}}, a) {
		t.Error("libc mismatch should fail even when OS+CPU match")
	}
	if NodeMatchesArchitectures(&Node{OS: []string{"linux"}, CPU: []string{"arm64"}, Libc: []string{"musl"}}, a) {
		t.Error("cpu mismatch should fail")
	}
}

func TestArchitectures_NegationPreserved(t *testing.T) {
	// Negation entries in node restrictions: "!win32" means "everything
	// except win32" - existing fieldMatchesPlatform semantics.
	a := Architectures{OS: []string{"linux"}}
	if !NodeMatchesArchitectures(&Node{OS: []string{"!win32"}}, a) {
		t.Error("negation '!win32' should allow linux target")
	}
	a2 := Architectures{OS: []string{"win32"}}
	if NodeMatchesArchitectures(&Node{OS: []string{"!win32"}}, a2) {
		t.Error("negation '!win32' should refuse win32 target")
	}
}

func TestArchitectures_MultiArch_AnyAxisMember(t *testing.T) {
	// Multi-arch means: keep package if ANY of the configured os/cpu/libc
	// values is compatible with the node. Per ticket #13.
	a := Architectures{OS: []string{"linux", "darwin"}, CPU: []string{"x64", "arm64"}}
	node := &Node{OS: []string{"linux"}, CPU: []string{"arm64"}}
	if !NodeMatchesArchitectures(node, a) {
		t.Errorf("node {linux, arm64} should match Architectures {linux+darwin, x64+arm64}")
	}
}

func TestResolveCurrentSentinel(t *testing.T) {
	a := Architectures{OS: []string{"current"}, CPU: []string{"current"}}
	resolved := ResolveCurrentSentinel(a)
	if len(resolved.OS) != 1 || resolved.OS[0] == "current" {
		t.Errorf("OS still contains 'current': %v", resolved.OS)
	}
	if len(resolved.CPU) != 1 || resolved.CPU[0] == "current" {
		t.Errorf("CPU still contains 'current': %v", resolved.CPU)
	}
}
