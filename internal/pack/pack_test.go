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

func TestExpandLocalDependency_MissingNamedExportFails(t *testing.T) {
	root := writeLocalPack(t, `{
  "specVersion": "0.1",
  "name": "local-pack",
  "version": "1.0.0",
  "modules": [
    {"id":"standards.style","path":"modules/standards/style.md","priority":100},
    {"id":"tasks.setup","path":"modules/tasks/setup.md","priority":200}
  ],
  "exports": {
    "default": {"include":["**"]}
  }
}`)
	writeFile(t, filepath.Join(root, "modules", "standards", "style.md"), "S\n")
	writeFile(t, filepath.Join(root, "modules", "tasks", "setup.md"), "T\n")

	dep := config.Dependency{Source: "local", Path: ".", Export: "standards"}
	_, _, err := ExpandLocalDependency(root, dep, "local")
	if err == nil {
		t.Fatalf("expected missing export error")
	}
	if err != nil && err.Error() != `missing export "standards" in local-pack` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExpandLocalDependency_ExportWithFoldersSelector(t *testing.T) {
	root := writeLocalPack(t, `{
  "specVersion": "0.1",
  "name": "local-pack",
  "version": "1.0.0",
  "modules": [
    {"id":"standards.style","path":"modules/standards/style.md","priority":100},
    {"id":"languages.python.patterns","path":"modules/languages/python/patterns.md","priority":200},
    {"id":"tasks.setup","path":"modules/tasks/setup.md","priority":300}
  ],
  "exports": {
    "python-core": {"folders":["standards","languages/python"]}
  }
}`)
	writeFile(t, filepath.Join(root, "modules", "standards", "style.md"), "S\n")
	writeFile(t, filepath.Join(root, "modules", "languages", "python", "patterns.md"), "P\n")
	writeFile(t, filepath.Join(root, "modules", "tasks", "setup.md"), "T\n")

	dep := config.Dependency{Source: "local", Path: ".", Export: "python-core"}
	mods, _, err := ExpandLocalDependency(root, dep, "local")
	if err != nil {
		t.Fatalf("ExpandLocalDependency: %v", err)
	}
	if len(mods) != 2 {
		t.Fatalf("expected two selected modules, got %d", len(mods))
	}
	if mods[0].ID != "standards.style" || mods[1].ID != "languages.python.patterns" {
		t.Fatalf("unexpected module selection order: %s, %s", mods[0].ID, mods[1].ID)
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
