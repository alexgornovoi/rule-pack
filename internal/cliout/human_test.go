package cliout

import "testing"

func TestRenderTable(t *testing.T) {
	out := renderTable(
		[]string{"ColA", "ColB"},
		[][]string{{"a", "bbb"}, {"aaaa", "b"}},
	)
	if out == "" {
		t.Fatalf("expected table output")
	}
	if out[0] != '|' {
		t.Fatalf("expected markdown-style table")
	}
}
