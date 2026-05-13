package ecosystem

import "testing"

// TestApplyOverride_NewSlice1Fields verifies that ApplyOverride propagates
// the slice-1 additions (LegacyPeerDeps, StrictPeerDeps). Behavior coverage
// for these fields lives in resolver-level tests; this test guards the API
// boundary so the CLI's policy-merge path (see ticket #14) doesn't silently
// drop a config-file setting.
func TestApplyOverride_NewSlice1Fields(t *testing.T) {
	cases := []struct {
		name     string
		override ResolverPolicy
		check    func(p ResolverPolicy) bool
	}{
		{
			name:     "LegacyPeerDeps",
			override: ResolverPolicy{LegacyPeerDeps: true},
			check:    func(p ResolverPolicy) bool { return p.LegacyPeerDeps },
		},
		{
			name:     "StrictPeerDeps",
			override: ResolverPolicy{StrictPeerDeps: true},
			check:    func(p ResolverPolicy) bool { return p.StrictPeerDeps },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := ResolverPolicy{}
			p.ApplyOverride(&tc.override)
			if !tc.check(p) {
				t.Errorf("ApplyOverride did not propagate %s", tc.name)
			}
		})
	}
}

// TestApplyOverride_NilNoOp keeps the existing nil-safety contract.
func TestApplyOverride_NilNoOp(t *testing.T) {
	p := ResolverPolicy{
		LegacyPeerDeps: true,
		StrictPeerDeps: true,
	}
	p.ApplyOverride(nil)
	if !p.LegacyPeerDeps || !p.StrictPeerDeps {
		t.Error("ApplyOverride(nil) clobbered existing fields")
	}
}
