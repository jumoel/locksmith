package orderedjson

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestMap_MarshalJSON_PreservesOrder(t *testing.T) {
	om := Map{
		{Key: "zebra", Value: "z"},
		{Key: "apple", Value: "a"},
		{Key: "mango", Value: "m"},
	}

	data, err := json.Marshal(om)
	if err != nil {
		t.Fatalf("MarshalJSON() error: %v", err)
	}

	got := string(data)
	want := `{"zebra":"z","apple":"a","mango":"m"}`
	if got != want {
		t.Errorf("MarshalJSON() = %s, want %s", got, want)
	}
}

func TestMap_MarshalJSON_NoHTMLEscaping(t *testing.T) {
	// MarshalJSON disables HTML escaping so engine constraints like ">=14"
	// are NOT written as "\u003e=14". Note: json.Marshal re-escapes HTML in
	// MarshalJSON output, so callers must use json.NewEncoder with
	// SetEscapeHTML(false) to preserve the unescaped output. This test calls
	// MarshalJSON directly to verify the implementation.
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "greater-than in engine constraint",
			value: ">=14",
			want:  `{"node":">=14"}`,
		},
		{
			name:  "less-than",
			value: "<20",
			want:  `{"node":"<20"}`,
		},
		{
			name:  "ampersand",
			value: "foo & bar",
			want:  `{"node":"foo & bar"}`,
		},
		{
			name:  "combined range",
			value: ">=14 <20",
			want:  `{"node":">=14 <20"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			om := Map{{Key: "node", Value: tt.value}}
			data, err := om.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON() error: %v", err)
			}
			if got := string(data); got != tt.want {
				t.Errorf("MarshalJSON() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestMap_MarshalJSON_EmptyMap(t *testing.T) {
	om := Map{}
	data, err := json.Marshal(om)
	if err != nil {
		t.Fatalf("MarshalJSON() error: %v", err)
	}

	if got := string(data); got != "{}" {
		t.Errorf("MarshalJSON() = %s, want {}", got)
	}
}

func TestMap_MarshalJSON_NestedMap(t *testing.T) {
	inner := Map{
		{Key: "c", Value: "cval"},
		{Key: "d", Value: "dval"},
	}
	outer := Map{
		{Key: "a", Value: "aval"},
		{Key: "nested", Value: inner},
	}

	data, err := json.Marshal(outer)
	if err != nil {
		t.Fatalf("MarshalJSON() error: %v", err)
	}

	got := string(data)
	want := `{"a":"aval","nested":{"c":"cval","d":"dval"}}`
	if got != want {
		t.Errorf("MarshalJSON() = %s, want %s", got, want)
	}
}

func TestMap_MarshalJSON_MixedValueTypes(t *testing.T) {
	om := Map{
		{Key: "str", Value: "hello"},
		{Key: "num", Value: 42},
		{Key: "bool", Value: true},
		{Key: "slice", Value: []string{"a", "b"}},
		{Key: "nested", Value: Map{{Key: "inner", Value: "val"}}},
	}

	data, err := json.Marshal(om)
	if err != nil {
		t.Fatalf("MarshalJSON() error: %v", err)
	}

	got := string(data)
	want := `{"str":"hello","num":42,"bool":true,"slice":["a","b"],"nested":{"inner":"val"}}`
	if got != want {
		t.Errorf("MarshalJSON() = %s, want %s", got, want)
	}
}

func TestMap_MarshalJSON_SpecialCharacters(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{
			name:  "quotes in value",
			key:   "k",
			value: `he said "hi"`,
			want:  `{"k":"he said \"hi\""}`,
		},
		{
			name:  "backslash in value",
			key:   "k",
			value: `path\to\file`,
			want:  `{"k":"path\\to\\file"}`,
		},
		{
			name:  "unicode in value",
			key:   "emoji",
			value: "cafe\u0301",
			want:  "{\"emoji\":\"cafe\u0301\"}",
		},
		{
			name:  "newline in value",
			key:   "k",
			value: "line1\nline2",
			want:  `{"k":"line1\nline2"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			om := Map{{Key: tt.key, Value: tt.value}}
			data, err := json.Marshal(om)
			if err != nil {
				t.Fatalf("MarshalJSON() error: %v", err)
			}
			if got := string(data); got != tt.want {
				t.Errorf("MarshalJSON() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestFromStringMap(t *testing.T) {
	t.Run("alphabetically sorted", func(t *testing.T) {
		m := map[string]string{
			"cherry": "3",
			"apple":  "1",
			"banana": "2",
		}
		result := FromStringMap(m)

		if len(result) != 3 {
			t.Fatalf("FromStringMap() returned %d entries, want 3", len(result))
		}

		wantKeys := []string{"apple", "banana", "cherry"}
		wantVals := []string{"1", "2", "3"}
		for i, entry := range result {
			if entry.Key != wantKeys[i] {
				t.Errorf("entry[%d].Key = %q, want %q", i, entry.Key, wantKeys[i])
			}
			if entry.Value != wantVals[i] {
				t.Errorf("entry[%d].Value = %q, want %q", i, entry.Value, wantVals[i])
			}
		}
	})

	t.Run("empty map", func(t *testing.T) {
		result := FromStringMap(map[string]string{})
		if len(result) != 0 {
			t.Errorf("FromStringMap({}) returned %d entries, want 0", len(result))
		}
	})

	t.Run("single entry", func(t *testing.T) {
		result := FromStringMap(map[string]string{"only": "one"})
		if len(result) != 1 {
			t.Fatalf("FromStringMap() returned %d entries, want 1", len(result))
		}
		if result[0].Key != "only" || result[0].Value != "one" {
			t.Errorf("entry = {%q, %v}, want {only, one}", result[0].Key, result[0].Value)
		}
	})
}

func TestFromStringMapSorted(t *testing.T) {
	packages := map[string]Map{
		"zlib":   {{Key: "version", Value: "1.0.0"}},
		"axios":  {{Key: "version", Value: "2.0.0"}},
		"lodash": {{Key: "version", Value: "4.0.0"}},
	}

	result := FromStringMapSorted(packages)

	if len(result) != 3 {
		t.Fatalf("FromStringMapSorted() returned %d entries, want 3", len(result))
	}

	wantKeys := []string{"axios", "lodash", "zlib"}
	for i, entry := range result {
		if entry.Key != wantKeys[i] {
			t.Errorf("entry[%d].Key = %q, want %q", i, entry.Key, wantKeys[i])
		}
		// Verify the value is the correct Map.
		innerMap, ok := entry.Value.(Map)
		if !ok {
			t.Fatalf("entry[%d].Value is not a Map", i)
		}
		if len(innerMap) != 1 {
			t.Fatalf("entry[%d] inner map length = %d, want 1", i, len(innerMap))
		}
	}
}

func TestMap_MarshalJSON_UsedByEncoder(t *testing.T) {
	om := Map{
		{Key: "engines", Value: Map{
			{Key: "node", Value: ">=14"},
		}},
		{Key: "name", Value: "test-pkg"},
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(om); err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	// json.NewEncoder appends a newline; trim it for comparison.
	got := bytes.TrimRight(buf.Bytes(), "\n")
	want := `{"engines":{"node":">=14"},"name":"test-pkg"}`
	if string(got) != want {
		t.Errorf("Encode() = %s, want %s", got, want)
	}
}
