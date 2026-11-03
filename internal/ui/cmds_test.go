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

func TestCountRecreated(t *testing.T) {
	out := ` Container demo-web-1 Recreate 
 Container demo-web-1 Recreated 
 Container demo-db-1 Running 
 Container demo-worker-1 Recreated 
 Container demo-web-1 Started 
`
	if n := countRecreated(out); n != 2 {
		t.Errorf("got %d, want 2", n)
	}
	if n := countRecreated(" Container demo-web-1 Running \n"); n != 0 {
		t.Errorf("got %d, want 0", n)
	}
}
