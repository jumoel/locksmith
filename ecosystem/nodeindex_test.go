package ecosystem

import (
	"testing"

	"github.com/jumoel/locksmith/internal/semver"
)

func TestNodeIndex_Add_HasName(t *testing.T) {
	idx := NewNodeIndex()
	node := &Node{Name: "lodash", Version: "4.17.21"}

	idx.Add("lodash", node)

	if !idx.HasName("lodash") {
		t.Error("HasName(lodash) = false after Add, want true")
	}
	if idx.HasName("unknown-pkg") {
		t.Error("HasName(unknown-pkg) = true, want false")
	}
}

func TestNodeIndex_HasName_Empty(t *testing.T) {
	idx := NewNodeIndex()

	if idx.HasName("anything") {
		t.Error("HasName() on empty index = true, want false")
	}
}

func TestNodeIndex_FindSatisfying_SingleVersion(t *testing.T) {
	idx := NewNodeIndex()
	node := &Node{Name: "lodash", Version: "4.17.21"}
	idx.Add("lodash", node)

	c, err := semver.ParseConstraint("^4.0.0")
	if err != nil {
		t.Fatalf("ParseConstraint error: %v", err)
	}

	got := idx.FindSatisfying("lodash", c)
	if got == nil {
		t.Fatal("FindSatisfying() = nil, want node")
	}
	if got != node {
		t.Errorf("FindSatisfying() returned different node, got version %s", got.Version)
	}
}

func TestNodeIndex_FindSatisfying_NoMatch(t *testing.T) {
	idx := NewNodeIndex()
	node := &Node{Name: "lodash", Version: "3.10.1"}
	idx.Add("lodash", node)

	c, err := semver.ParseConstraint("^4.0.0")
	if err != nil {
		t.Fatalf("ParseConstraint error: %v", err)
	}

	got := idx.FindSatisfying("lodash", c)
	if got != nil {
		t.Errorf("FindSatisfying() = %v, want nil", got.Version)
	}
}

func TestNodeIndex_FindSatisfying_MultipleVersions(t *testing.T) {
	idx := NewNodeIndex()
	node1 := &Node{Name: "lodash", Version: "4.17.20"}
	node2 := &Node{Name: "lodash", Version: "4.17.21"}
	node3 := &Node{Name: "lodash", Version: "3.10.1"}

	idx.Add("lodash", node1)
	idx.Add("lodash", node2)
	idx.Add("lodash", node3)

	c, err := semver.ParseConstraint("^4.0.0")
	if err != nil {
		t.Fatalf("ParseConstraint error: %v", err)
	}

	// FindSatisfying returns the first node whose version satisfies the
	// constraint in insertion order. node1 (4.17.20) was added first.
	got := idx.FindSatisfying("lodash", c)
	if got == nil {
		t.Fatal("FindSatisfying() = nil, want node")
	}
	if got != node1 {
		t.Errorf("FindSatisfying() returned version %s, want %s (first inserted match)", got.Version, node1.Version)
	}
}

func TestNodeIndex_FindSatisfying_InvalidVersion(t *testing.T) {
	idx := NewNodeIndex()

	// This version string is unparseable as semver. The Add call will store
	// a nil Version, and FindSatisfying must skip it without panicking.
	badNode := &Node{Name: "weird", Version: "not-a-version"}
	goodNode := &Node{Name: "weird", Version: "1.0.0"}

	idx.Add("weird", badNode)
	idx.Add("weird", goodNode)

	c, err := semver.ParseConstraint(">=1.0.0")
	if err != nil {
		t.Fatalf("ParseConstraint error: %v", err)
	}

	got := idx.FindSatisfying("weird", c)
	if got == nil {
		t.Fatal("FindSatisfying() = nil, want the good node")
	}
	if got != goodNode {
		t.Errorf("FindSatisfying() returned version %s, want %s", got.Version, goodNode.Version)
	}
}

func TestNodeIndex_MultiplePackages(t *testing.T) {
	idx := NewNodeIndex()
	lodash := &Node{Name: "lodash", Version: "4.17.21"}
	express := &Node{Name: "express", Version: "4.18.2"}

	idx.Add("lodash", lodash)
	idx.Add("express", express)

	cLodash, err := semver.ParseConstraint("^4.17.0")
	if err != nil {
		t.Fatalf("ParseConstraint error: %v", err)
	}
	cExpress, err := semver.ParseConstraint("^4.18.0")
	if err != nil {
		t.Fatalf("ParseConstraint error: %v", err)
	}

	// lodash constraint should match lodash, not express.
	gotLodash := idx.FindSatisfying("lodash", cLodash)
	if gotLodash != lodash {
		t.Error("FindSatisfying(lodash) did not return the lodash node")
	}

	// express constraint should match express, not lodash.
	gotExpress := idx.FindSatisfying("express", cExpress)
	if gotExpress != express {
		t.Error("FindSatisfying(express) did not return the express node")
	}

	// Cross-check: looking up a name that has entries but with a constraint
	// that only the other package satisfies should return nil.
	cTooHigh, err := semver.ParseConstraint("^5.0.0")
	if err != nil {
		t.Fatalf("ParseConstraint error: %v", err)
	}
	if got := idx.FindSatisfying("lodash", cTooHigh); got != nil {
		t.Errorf("FindSatisfying(lodash, ^5.0.0) = %v, want nil", got.Version)
	}
}

func TestNodeIndex_FindSatisfying_EmptyName(t *testing.T) {
	idx := NewNodeIndex()
	node := &Node{Name: "lodash", Version: "4.17.21"}
	idx.Add("lodash", node)

	c, err := semver.ParseConstraint("^4.0.0")
	if err != nil {
		t.Fatalf("ParseConstraint error: %v", err)
	}

	got := idx.FindSatisfying("nonexistent", c)
	if got != nil {
		t.Errorf("FindSatisfying(nonexistent) = %v, want nil", got.Version)
	}
}
