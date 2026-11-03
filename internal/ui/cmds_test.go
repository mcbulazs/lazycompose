package ui

import (
	"reflect"
	"testing"
)

func TestEditorCommand(t *testing.T) {
	tests := []struct {
		config, visual, editor string
		want                   []string
	}{
		{"code --wait", "vim", "nano", []string{"code", "--wait", "f.yaml"}},
		{"", "vim", "nano", []string{"vim", "f.yaml"}},
		{"", "", "nano -w", []string{"nano", "-w", "f.yaml"}},
		{"  ", "", "nano", []string{"nano", "f.yaml"}},
	}
	for _, tt := range tests {
		t.Setenv("VISUAL", tt.visual)
		t.Setenv("EDITOR", tt.editor)
		cmd, err := editorCommand(tt.config, "f.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cmd.Args, tt.want) {
			t.Errorf("config %q: got %v, want %v", tt.config, cmd.Args, tt.want)
		}
	}
}
