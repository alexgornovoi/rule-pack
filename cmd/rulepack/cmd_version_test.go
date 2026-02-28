package main

import (
	"encoding/json"
	"testing"

	"rulepack/internal/cliout"
)

func TestVersionCommandJSON(t *testing.T) {
	projectDir := t.TempDir()
	orig := buildVersion
	buildVersion = "v1.2.3-test"
	t.Cleanup(func() { buildVersion = orig })

	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var env jsonEnvelope
	if err := runCmdJSON(t, projectDir, a.newVersionCmd(), &env); err != nil {
		t.Fatalf("version command failed: %v", err)
	}
	if env.Command != "version" {
		t.Fatalf("unexpected command: %s", env.Command)
	}
	var out versionOutput
	if err := json.Unmarshal(env.Result, &out); err != nil {
		t.Fatalf("unmarshal version output: %v", err)
	}
	if out.Version != "v1.2.3-test" {
		t.Fatalf("unexpected version: %s", out.Version)
	}
}
