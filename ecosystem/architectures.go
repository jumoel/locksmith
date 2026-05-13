package ecosystem

import "runtime"

// Architectures describes a multi-axis target platform filter, replacing the
// older single-pair Platform for callers that need to keep packages matching
// any of several OS/CPU/libc combinations in one lockfile (ticket #13).
//
// Zero value (every axis empty) means "no filtering" - keep every node
// regardless of os/cpu/libc restrictions. This matches the existing behavior
// when Platform was unset.
//
// Per-axis match: a node passes if its corresponding restriction list is
// empty (no restriction) OR shares at least one entry with the
// Architectures axis (intersection). Negation entries like "!win32" on the
// node side keep their existing meaning ("everything except this").
type Architectures struct {
	OS   []string
	CPU  []string
	Libc []string
}

// ArchitecturesFromPlatform converts a legacy single-pair Platform into the
// multi-axis Architectures shape. Used by the CLI's `--platform linux/x64`
// shorthand. Libc is left empty (Platform never carried it).
func ArchitecturesFromPlatform(p Platform) Architectures {
	return Architectures{
		OS:  []string{p.OS},
		CPU: []string{p.CPU},
	}
}

// NodeMatchesArchitectures returns true when node satisfies every axis of
// archs. An empty axis on archs is "no restriction" and always matches;
// node-side `!entry` negations keep their existing semantics.
func NodeMatchesArchitectures(node *Node, archs Architectures) bool {
	if !axisMatches(node.OS, archs.OS) {
		return false
	}
	if !axisMatches(node.CPU, archs.CPU) {
		return false
	}
	if !axisMatches(node.Libc, archs.Libc) {
		return false
	}
	return true
}

// axisMatches checks whether a node's restriction list is compatible with
// any of the configured targets. Empty configured-targets means "no
// filtering"; the node passes regardless. Negation entries on the node side
// (`!val`) exclude that target.
func axisMatches(nodeRestrictions, configuredTargets []string) bool {
	if len(configuredTargets) == 0 {
		return true
	}
	if len(nodeRestrictions) == 0 {
		return true
	}
	for _, target := range configuredTargets {
		if fieldMatchesPlatform(nodeRestrictions, target) {
			return true
		}
	}
	return false
}

// FilterGraphByArchitectures is the multi-axis counterpart of
// FilterGraphByPlatform. Same return contract: keys of removed nodes.
func FilterGraphByArchitectures(graph *Graph, archs Architectures) map[string]bool {
	removed := make(map[string]bool)

	rootOptional := make(map[string]bool)
	if graph.Root != nil {
		for _, edge := range graph.Root.Dependencies {
			if edge.Target != nil && edge.Type == DepOptional {
				rootOptional[edge.Target.Name+"@"+edge.Target.Version] = true
			}
		}
	}

	for key, node := range graph.Nodes {
		if rootOptional[key] {
			continue
		}
		if !NodeMatchesArchitectures(node, archs) {
			removed[key] = true
			delete(graph.Nodes, key)
		}
	}

	if len(removed) == 0 {
		return removed
	}

	pruneEdges(graph.Root, removed)
	for _, node := range graph.Nodes {
		pruneEdges(node, removed)
	}

	return removed
}

// ResolveCurrentSentinel rewrites any "current" entries in archs to the
// runtime's actual values. Yarn berry, pnpm, and bun all accept "current"
// in supportedArchitectures lists meaning "the runtime executing the install."
//
// OS and CPU resolve to runtime.GOOS and runtime.GOARCH. Libc detection on
// linux is left to runtimeLibc(); on non-linux platforms the libc axis is
// stripped entirely (libc is irrelevant outside of linux).
func ResolveCurrentSentinel(a Architectures) Architectures {
	resolveAxis := func(values []string, replacement string) []string {
		if len(values) == 0 {
			return values
		}
		out := make([]string, 0, len(values))
		for _, v := range values {
			if v == "current" {
				if replacement != "" {
					out = append(out, replacement)
				}
				continue
			}
			out = append(out, v)
		}
		return out
	}
	a.OS = resolveAxis(a.OS, runtime.GOOS)
	a.CPU = resolveAxis(a.CPU, runtime.GOARCH)
	if runtime.GOOS == "linux" {
		a.Libc = resolveAxis(a.Libc, runtimeLibc())
	} else if len(a.Libc) > 0 {
		// On non-linux, libc has no meaning; drop the axis.
		a.Libc = resolveAxis(a.Libc, "")
		if len(a.Libc) == 0 {
			a.Libc = nil
		}
	}
	return a
}

// runtimeLibc returns the detected C library variant on linux: "musl" if
// the musl dynamic linker is present, else "glibc". On non-linux this is
// not called (libc has no meaning).
func runtimeLibc() string {
	// Lazy detection: presence of /lib/ld-musl-*.so.1 indicates a musl
	// system (Alpine, etc.). Hardcode "glibc" as the fallback for any
	// non-musl linux. This is the same heuristic pnpm and yarn use.
	if fileExistsGlob("/lib/ld-musl-*.so.1") {
		return "musl"
	}
	return "glibc"
}
