package ecosystem

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// countingRegistry wraps a Registry and counts FetchVersions calls per name.
type countingRegistry struct {
	inner    Registry
	versions atomic.Int64
	metadata atomic.Int64
	distTags atomic.Int64
}

func (c *countingRegistry) FetchVersions(ctx context.Context, name string, cutoff *time.Time) ([]VersionInfo, error) {
	c.versions.Add(1)
	return c.inner.FetchVersions(ctx, name, cutoff)
}

func (c *countingRegistry) FetchMetadata(ctx context.Context, name, version string) (*VersionMetadata, error) {
	c.metadata.Add(1)
	return c.inner.FetchMetadata(ctx, name, version)
}

func (c *countingRegistry) FetchDistTags(ctx context.Context, name string) (map[string]string, error) {
	c.distTags.Add(1)
	return c.inner.FetchDistTags(ctx, name)
}

// buildTree creates a synthetic dependency tree of given depth and branching
// factor. With depth=4, branch=3 we get 1 + 3 + 9 + 27 + 81 = 121 packages.
func buildTree(reg *mockRegistry, depth, branch int) {
	type node struct {
		name string
		d    int
	}
	queue := []node{{name: "root", d: 0}}
	reg.addVersion("root", "1.0.0", baseTime, nil)
	idx := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.d >= depth {
			continue
		}
		deps := make(map[string]string, branch)
		for i := 0; i < branch; i++ {
			idx++
			child := fmt.Sprintf("pkg-%d", idx)
			deps[child] = "^1.0.0"
			reg.addVersion(child, "1.0.0", baseTime, nil)
			queue = append(queue, node{name: child, d: cur.d + 1})
		}
		// Re-register parent with deps. Last write wins in the mock.
		if cur.name == "root" {
			reg.packages["root"].versions["1.0.0"].Dependencies = deps
		} else {
			meta := reg.packages[cur.name].versions["1.0.0"]
			meta.Dependencies = deps
		}
	}
}

// TestResolve_PrefetchedAndSerialProduceSameGraph is the critical
// determinism contract: enabling the prefetcher must not change the
// resolved graph in any way. Same nodes, same edges, same versions.
func TestResolve_PrefetchedAndSerialProduceSameGraph(t *testing.T) {
	reg := newMockRegistry()
	buildTree(reg, 3, 3) // 1 + 3 + 9 + 27 = 40 packages

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "root", Constraint: "^1.0.0", Type: DepRegular}},
	}
	policy := ResolverPolicy{CrossTreeDedup: true, AutoInstallPeers: true}

	// Serial baseline (workers=0 disables prefetcher).
	orig := PrefetchWorkers
	defer func() { PrefetchWorkers = orig }()

	PrefetchWorkers = 0
	serialGraph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("serial resolve: %v", err)
	}

	PrefetchWorkers = 8
	prefetchedGraph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("prefetched resolve: %v", err)
	}

	if len(serialGraph.Nodes) != len(prefetchedGraph.Nodes) {
		t.Fatalf("node count differs: serial=%d, prefetched=%d",
			len(serialGraph.Nodes), len(prefetchedGraph.Nodes))
	}
	for key, serialNode := range serialGraph.Nodes {
		preNode, ok := prefetchedGraph.Nodes[key]
		if !ok {
			t.Errorf("prefetched graph missing node %q", key)
			continue
		}
		if serialNode.Version != preNode.Version {
			t.Errorf("%s: version differs serial=%s prefetched=%s", key, serialNode.Version, preNode.Version)
		}
		if len(serialNode.Dependencies) != len(preNode.Dependencies) {
			t.Errorf("%s: edge count differs serial=%d prefetched=%d",
				key, len(serialNode.Dependencies), len(preNode.Dependencies))
		}
	}
}

// TestResolve_PrefetchedDiamond: a diamond pattern (A->C, B->C) must still
// dedupe to a single C node when prefetching is on.
func TestResolve_PrefetchedDiamond(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("C", "1.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: DepRegular},
			{Name: "B", Constraint: "^1.0.0", Type: DepRegular},
		},
	}

	orig := PrefetchWorkers
	defer func() { PrefetchWorkers = orig }()
	PrefetchWorkers = 4

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{CutoffDate: nil},
		ResolverPolicy{CrossTreeDedup: true})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Both A and B must point to the same C node.
	aEdge := graph.Root.Dependencies[0]
	bEdge := graph.Root.Dependencies[1]
	aCNode := aEdge.Target.Dependencies[0].Target
	bCNode := bEdge.Target.Dependencies[0].Target
	if aCNode != bCNode {
		t.Error("diamond dedup broken: A->C and B->C resolved to different nodes")
	}
}

// TestResolve_PrefetchCycle covers a simple A->B->A cycle while
// prefetching is on. The resolver's cycle detection must still terminate.
func TestResolve_PrefetchCycle(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"B": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, map[string]string{"A": "^1.0.0"})

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepRegular}},
	}

	orig := PrefetchWorkers
	defer func() { PrefetchWorkers = orig }()
	PrefetchWorkers = 4

	done := make(chan struct{})
	go func() {
		_, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
		if err != nil {
			t.Errorf("Resolve: %v", err)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Resolve hung on A<->B cycle with prefetcher enabled")
	}
}

// TestResolve_PrefetchReducesHttpCalls: with the prefetcher and a registry
// where every call is the same underlying packument, observe the call
// count. This won't drop on a mock that returns directly, but at least
// asserts we're not duplicating calls.
func TestResolve_PrefetchedCountIsBounded(t *testing.T) {
	reg := newMockRegistry()
	buildTree(reg, 3, 2) // 1 + 2 + 4 + 8 = 15 packages
	counted := &countingRegistry{inner: reg}

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "root", Constraint: "^1.0.0", Type: DepRegular}},
	}

	orig := PrefetchWorkers
	defer func() { PrefetchWorkers = orig }()
	PrefetchWorkers = 4

	_, err := Resolve(context.Background(), project, counted, ResolveOptions{},
		ResolverPolicy{CrossTreeDedup: true})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// We expect prefetch calls + serial calls. With a tight bound we just
	// assert we're not unbounded (a regression where every node is fetched
	// 10 times would be obvious).
	if v := counted.versions.Load(); v > 100 {
		t.Errorf("FetchVersions called %d times, expected far fewer", v)
	}
}
