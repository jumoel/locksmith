package yarnrc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadCompressionLevel(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		{
			name:    "missing-key-returns-empty",
			content: "nodeLinker: node-modules\n",
			want:    "",
		},
		{
			name:    "mixed-as-string",
			content: "compressionLevel: mixed\n",
			want:    "mixed",
		},
		{
			name:    "integer-zero",
			content: "compressionLevel: 0\n",
			want:    "0",
		},
		{
			name:    "integer-positive",
			content: "compressionLevel: 9\n",
			want:    "9",
		},
		{
			name:    "quoted-string",
			content: "compressionLevel: \"mixed\"\n",
			want:    "mixed",
		},
		{
			name:    "empty-file",
			content: "",
			want:    "",
		},
		{
			name:    "malformed-yaml",
			content: ": : not yaml\n  : - :\n",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, ".yarnrc.yml")
			if err := os.WriteFile(p, []byte(tc.content), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			got, err := ReadCompressionLevel(p)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestReadCompressionLevel_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadCompressionLevel(filepath.Join(dir, "does-not-exist.yml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
