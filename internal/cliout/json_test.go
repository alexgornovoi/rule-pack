package cliout

import "testing"

func TestMustJSON(t *testing.T) {
	b := mustJSON(map[string]string{"a": "b"})
	if len(b) == 0 {
		t.Fatalf("expected bytes")
	}
	if b[len(b)-1] != '\n' {
		t.Fatalf("expected newline-terminated json")
	}
}
