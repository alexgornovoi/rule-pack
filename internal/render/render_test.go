package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rulepack/internal/config"
	"rulepack/internal/pack"
)

func TestWriteCursorApplyModes(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "rules")
	target := config.TargetEntry{
		OutDir:    outDir,
		PerModule: true,
		Ext:       ".mdc",
	}
	modules := []pack.Module{
		{
			ID:       "a.default",
			Priority: 100,
			Content:  "A\n",
		},
		{
			ID:       "b.glob",
			Priority: 110,
			Content:  "B\n",
			Apply: pack.ApplyConfig{
				Targets: map[string]pack.ApplyRule{
					"cursor": {Mode: "glob", Globs: []string{"**/*.py"}, Description: "Python files only"},
				},
			},
		},
		{
			ID:       "c.never",
			Priority: 120,
			Content:  "C\n",
			Apply: pack.ApplyConfig{
				Targets: map[string]pack.ApplyRule{
					"cursor": {Mode: "never"},
				},
			},
		},
	}

	if err := WriteCursor(target, modules); err != nil {
		t.Fatalf("WriteCursor: %v", err)
	}
	files, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files after skipping never, got %d", len(files))
	}

	first := mustReadFile(t, filepath.Join(outDir, files[0].Name()))
	second := mustReadFile(t, filepath.Join(outDir, files[1].Name()))
	full := first + "\n" + second
	if !strings.Contains(full, "alwaysApply: true") {
		t.Fatalf("expected alwaysApply: true in output")
	}
	if !strings.Contains(full, "globs:") || !strings.Contains(full, "\"**/*.py\"") {
		t.Fatalf("expected glob frontmatter in output")
	}
}

func TestWriteCursorGlobModeRequiresGlobs(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "rules")
	target := config.TargetEntry{
		OutDir:    outDir,
		PerModule: true,
		Ext:       ".mdc",
	}
	modules := []pack.Module{
		{
			ID:       "bad.glob",
			Priority: 100,
			Content:  "X\n",
			Apply: pack.ApplyConfig{
				Targets: map[string]pack.ApplyRule{
					"cursor": {Mode: "glob"},
				},
			},
		},
	}
	if err := WriteCursor(target, modules); err == nil {
		t.Fatalf("expected error for glob mode without globs")
	}
}

func TestCursorUnmanagedOverwrites_WarnsOnNonManagedCollision(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "rules")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	existing := filepath.Join(outDir, "100-python_base.mdc")
	if err := os.WriteFile(existing, []byte("manual content\n"), 0o644); err != nil {
		t.Fatalf("WriteFile existing: %v", err)
	}

	target := config.TargetEntry{
		OutDir:    outDir,
		PerModule: true,
		Ext:       ".mdc",
	}
	modules := []pack.Module{
		{
			ID:       "python.base",
			Priority: 100,
			Content:  "A\n",
		},
	}

	warnings, err := CursorUnmanagedOverwrites(target, modules)
	if err != nil {
		t.Fatalf("CursorUnmanagedOverwrites: %v", err)
	}
	if len(warnings) != 1 || warnings[0] != existing {
		t.Fatalf("unexpected warnings: %#v", warnings)
	}
}

func TestCursorUnmanagedOverwrites_IgnoresManagedCollision(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "rules")
	target := config.TargetEntry{
		OutDir:    outDir,
		PerModule: true,
		Ext:       ".mdc",
	}
	modules := []pack.Module{
		{
			PackName:    "pack-a",
			PackVersion: "1.0.0",
			Commit:      "abcdef123456",
			ID:          "python.base",
			Priority:    100,
			Content:     "A\n",
		},
	}
	if err := WriteCursor(target, modules); err != nil {
		t.Fatalf("WriteCursor: %v", err)
	}
	warnings, err := CursorUnmanagedOverwrites(target, modules)
	if err != nil {
		t.Fatalf("CursorUnmanagedOverwrites: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for managed file, got %#v", warnings)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(bytes)
}
