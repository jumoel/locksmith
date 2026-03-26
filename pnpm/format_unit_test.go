package pnpm

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSpecifierNode(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantStyle yaml.Style
	}{
		{
			name:      "semver range is not quoted",
			value:     "^1.2.3",
			wantStyle: 0,
		},
		{
			name:      "tilde range is not quoted",
			value:     "~2.0.0",
			wantStyle: 0,
		},
		{
			name:      "star constraint is not quoted",
			value:     "*",
			wantStyle: 0,
		},
		{
			name:      "range with spaces is not quoted",
			value:     ">=1.0.0 <2.0.0",
			wantStyle: 0,
		},
		{
			name:      "bare integer gets quoted",
			value:     "1",
			wantStyle: yaml.SingleQuotedStyle,
		},
		{
			name:      "bare float gets quoted",
			value:     "2.0",
			wantStyle: yaml.SingleQuotedStyle,
		},
		{
			name:      "numeric-looking version gets quoted",
			value:     "3.14",
			wantStyle: yaml.SingleQuotedStyle,
		},
		{
			name:      "workspace protocol is not quoted",
			value:     "workspace:*",
			wantStyle: 0,
		},
		{
			name:      "npm alias is not quoted",
			value:     "npm:other-pkg@^1.0.0",
			wantStyle: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := specifierNode(tt.value)
			if node.Kind != yaml.ScalarNode {
				t.Fatalf("specifierNode(%q) kind = %v, want ScalarNode", tt.value, node.Kind)
			}
			if node.Value != tt.value {
				t.Errorf("specifierNode(%q) value = %q, want %q", tt.value, node.Value, tt.value)
			}
			if node.Style != tt.wantStyle {
				t.Errorf("specifierNode(%q) style = %v, want %v", tt.value, node.Style, tt.wantStyle)
			}
		})
	}
}
