package pack

import (
	"os"
	"path/filepath"
	"testing"

	"rulepack/internal/config"
)

func TestExpandLocalDependency_DefaultExportAndDeterministicHash(t *testing.T) {
	root := writeLocalPack(t, `{
  "specVersion": "0.1",
  "name": "local-pack",
  "version": "1.0.0",
  "modules": [
    {"id":"a.alpha","path":"mods/a.md","priority":100},
    {"id":"b.beta","path":"mods/b.md","priority":200}
  ],
  "exports": {
    "default": {"include":["a.*"]},
    "all": {"include":["**"]}
  }
}`)
	writeFile(t, filepath.Join(root, "mods", "a.md"), "A\n")
	writeFile(t, filepath.Join(root, "mods", "b.md"), "B\n")

	dep := config.Dependency{Source: "local", Path: ".", Export: "default"}
	mods1, hash1, err := ExpandLocalDependency(root, dep, "local")
	if err != nil {
		t.Fatalf("ExpandLocalDependency: %v", err)
	}
	mods2, hash2, err := ExpandLocalDependency(root, dep, "local")
	if err != nil {
		t.Fatalf("ExpandLocalDependency second: %v", err)
	}

	if len(mods1) != 1 {
		t.Fatalf("expected one selected module, got %d", len(mods1))
	}
	if mods1[0].ID != "a.alpha" {
		t.Fatalf("expected module a.alpha, got %s", mods1[0].ID)
	}
	if hash1 != hash2 {
		t.Fatalf("expected deterministic hash, got %s != %s", hash1, hash2)
	}
	if len(mods2) != len(mods1) {
		t.Fatalf("expected same module count, got %d vs %d", len(mods2), len(mods1))
	}
}

func TestExpandLocalDependency_NamedExportAndHashDrift(t *testing.T) {
	root := writeLocalPack(t, `{
  "specVersion": "0.1",
  "name": "local-pack",
  "version": "1.0.0",
  "modules": [
    {"id":"a.alpha","path":"mods/a.md","priority":100},
    {"id":"b.beta","path":"mods/b.md","priority":200}
  ],
  "exports": {
    "default": {"include":["a.*"]},
    "all": {"include":["**"]}
  }
}`)
	aPath := filepath.Join(root, "mods", "a.md")
	writeFile(t, aPath, "A\n")
	writeFile(t, filepath.Join(root, "mods", "b.md"), "B\n")

	dep := config.Dependency{Source: "local", Path: ".", Export: "all"}
	mods, hash1, err := ExpandLocalDependency(root, dep, "local")
	if err != nil {
		t.Fatalf("ExpandLocalDependency: %v", err)
	}
	if len(mods) != 2 {
		t.Fatalf("expected two selected modules, got %d", len(mods))
	}

	writeFile(t, aPath, "A changed\n")
	_, hash2, err := ExpandLocalDependency(root, dep, "local")
	if err != nil {
		t.Fatalf("ExpandLocalDependency changed: %v", err)
	}
	if hash1 == hash2 {
		t.Fatalf("expected hash change after content edit, both %s", hash1)
	}
}

func writeLocalPack(t *testing.T, rulepackJSON string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "rulepack.json"), rulepackJSON)
	return root
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
