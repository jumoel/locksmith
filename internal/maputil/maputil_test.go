package maputil

import (
	"testing"
)

func TestSortedKeys_Empty(t *testing.T) {
	got := SortedKeys(map[string]string{})
	if len(got) != 0 {
		t.Errorf("SortedKeys(empty) = %v, want empty", got)
	}
}

func TestSortedKeys_Single(t *testing.T) {
	got := SortedKeys(map[string]string{"alpha": "1"})
	if len(got) != 1 || got[0] != "alpha" {
		t.Errorf("SortedKeys(single) = %v, want [alpha]", got)
	}
}

func TestSortedKeys_Multiple(t *testing.T) {
	got := SortedKeys(map[string]string{"charlie": "3", "alpha": "1", "bravo": "2"})
	want := []string{"alpha", "bravo", "charlie"}
	if len(got) != len(want) {
		t.Fatalf("SortedKeys len = %d, want %d", len(got), len(want))
	}
	for i, k := range got {
		if k != want[i] {
			t.Errorf("SortedKeys[%d] = %q, want %q", i, k, want[i])
		}
	}
}

func TestSortedMapKeys_Empty(t *testing.T) {
	got := SortedMapKeys(map[string]int{})
	if len(got) != 0 {
		t.Errorf("SortedMapKeys(empty) = %v, want empty", got)
	}
}

func TestSortedMapKeys_IntValues(t *testing.T) {
	got := SortedMapKeys(map[string]int{"z": 26, "a": 1, "m": 13})
	want := []string{"a", "m", "z"}
	if len(got) != len(want) {
		t.Fatalf("SortedMapKeys len = %d, want %d", len(got), len(want))
	}
	for i, k := range got {
		if k != want[i] {
			t.Errorf("SortedMapKeys[%d] = %q, want %q", i, k, want[i])
		}
	}
}

func TestSortedMapKeys_StructValues(t *testing.T) {
	got := SortedMapKeys(map[string]struct{}{"beta": {}, "alpha": {}})
	want := []string{"alpha", "beta"}
	if len(got) != len(want) {
		t.Fatalf("SortedMapKeys len = %d, want %d", len(got), len(want))
	}
	for i, k := range got {
		if k != want[i] {
			t.Errorf("SortedMapKeys[%d] = %q, want %q", i, k, want[i])
		}
	}
}

func TestSortedKeys_Nil(t *testing.T) {
	got := SortedKeys(nil)
	if len(got) != 0 {
		t.Errorf("SortedKeys(nil) = %v, want empty", got)
	}
}

func TestSortedMapKeys_Nil(t *testing.T) {
	got := SortedMapKeys[int](nil)
	if len(got) != 0 {
		t.Errorf("SortedMapKeys(nil) = %v, want empty", got)
	}
}
