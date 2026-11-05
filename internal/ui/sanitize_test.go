package ui

import "testing"

func TestCleanLine(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain", "hello world", "hello world"},
		{"tab kept", "a\tb", "a\tb"},
		{"utf8 kept", "héllo ● ✓", "héllo ● ✓"},
		{"colour kept", "\x1b[31mred\x1b[0m", "\x1b[31mred\x1b[0m"},
		{"colour with params", "\x1b[1;38;5;208mx\x1b[m", "\x1b[1;38;5;208mx\x1b[m"},
		{"clipboard write", "a\x1b]52;c;ZXZpbA==\x07b", "ab"},
		{"title with ST", "a\x1b]0;pwned\x1b\\b", "ab"},
		{"cursor move", "a\x1b[2J\x1b[Hb", "ab"},
		{"dcs", "a\x1bPq#0;2;0;0;0\x1b\\b", "ab"},
		{"bare escape", "a\x1b7b", "ab"},
		{"c0 controls", "a\rb\x07c\x08d", "abcd"},
		{"c1 csi", "a\u009b2Jb", "a2Jb"},
		{"unterminated osc", "a\x1b]52;c;abc", "a"},
		{"unterminated csi", "a\x1b[31", "a"},
		{"trailing escape", "a\x1b", "a"},
	}
	for _, tt := range tests {
		if got := cleanLine(tt.in); got != tt.want {
			t.Errorf("%s: cleanLine(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}
