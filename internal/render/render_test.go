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

func TestWriteCursorPreservesNestedModulePath(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "rules")
	target := config.TargetEntry{OutDir: outDir, PerModule: true, Ext: ".mdc"}
	modules := []pack.Module{
		{ID: "backend.api.auth", Path: "modules/backend/api/auth.md", Priority: 100, Content: "A\n"},
	}
	if err := WriteCursor(target, modules); err != nil {
		t.Fatalf("WriteCursor: %v", err)
	}
	outFile := filepath.Join(outDir, "backend", "api", "100-auth.mdc")
	if _, err := os.Stat(outFile); err != nil {
		t.Fatalf("expected nested cursor output, stat err=%v", err)
	}
}

func TestWriteCursorDetectsNestedPathCollision(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "rules")
	target := config.TargetEntry{OutDir: outDir, PerModule: true, Ext: ".mdc"}
	modules := []pack.Module{
		{ID: "a.one", Path: "modules/backend/api/auth.md", Priority: 100, Content: "A\n"},
		{ID: "b.two", Path: "modules/backend/api/auth.txt", Priority: 100, Content: "B\n"},
	}
	err := WriteCursor(target, modules)
	if err == nil || !strings.Contains(err.Error(), "cursor output collision") {
		t.Fatalf("expected cursor collision error, got %v", err)
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

func TestWriteMergedAddsManagedHeader(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "rules.md")
	modules := []pack.Module{{ID: "a", Priority: 100, Content: "A\n"}}
	if err := WriteMerged(outFile, modules); err != nil {
		t.Fatalf("WriteMerged: %v", err)
	}
	content := mustReadFile(t, outFile)
	if !strings.HasPrefix(content, "<!-- rulepack:managed -->\n") {
		t.Fatalf("expected managed header, got %q", content)
	}
}

func TestWriteClaudeWritesPerModuleMarkdown(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "rules")
	target := config.TargetEntry{
		OutDir:    outDir,
		PerModule: true,
		Ext:       ".md",
	}
	modules := []pack.Module{
		{ID: "python.base", Priority: 100, Content: "A\n"},
	}
	if err := WriteClaude(target, modules); err != nil {
		t.Fatalf("WriteClaude: %v", err)
	}
	outFile := filepath.Join(outDir, "100-python_base.md")
	content := mustReadFile(t, outFile)
	if !strings.HasPrefix(content, "<!-- pack=") {
		t.Fatalf("expected provenance header, got %q", content)
	}
}

func TestWriteClaudePreservesNestedModulePath(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "rules")
	target := config.TargetEntry{OutDir: outDir, PerModule: true, Ext: ".md"}
	modules := []pack.Module{
		{ID: "backend.api.auth", Path: "modules/backend/api/auth.md", Priority: 100, Content: "A\n"},
	}
	if err := WriteClaude(target, modules); err != nil {
		t.Fatalf("WriteClaude: %v", err)
	}
	outFile := filepath.Join(outDir, "backend", "api", "100-auth.md")
	if _, err := os.Stat(outFile); err != nil {
		t.Fatalf("expected nested claude output, stat err=%v", err)
	}
}

func TestWriteClaude_NeverSkipsFile(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "rules")
	target := config.TargetEntry{OutDir: outDir, PerModule: true, Ext: ".md"}
	modules := []pack.Module{
		{ID: "a.never", Priority: 100, Content: "A\n", Apply: pack.ApplyConfig{Targets: map[string]pack.ApplyRule{"claude": {Mode: "never"}}}},
		{ID: "b.keep", Priority: 110, Content: "B\n"},
	}
	if err := WriteClaude(target, modules); err != nil {
		t.Fatalf("WriteClaude: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "100-a_never.md")); !os.IsNotExist(err) {
		t.Fatalf("expected never file to be skipped, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "110-b_keep.md")); err != nil {
		t.Fatalf("expected keep file to exist, stat err=%v", err)
	}
}

func TestWriteClaude_GlobAddsPathsFrontmatter(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "rules")
	target := config.TargetEntry{OutDir: outDir, PerModule: true, Ext: ".md"}
	modules := []pack.Module{
		{
			ID:       "a.glob",
			Priority: 100,
			Content:  "A\n",
			Apply: pack.ApplyConfig{
				Targets: map[string]pack.ApplyRule{
					"claude": {Mode: "glob", Globs: []string{"src/**/*.ts", "lib/**/*.ts"}},
				},
			},
		},
	}
	if err := WriteClaude(target, modules); err != nil {
		t.Fatalf("WriteClaude: %v", err)
	}
	content := mustReadFile(t, filepath.Join(outDir, "100-a_glob.md"))
	if !strings.HasPrefix(content, "---\npaths:\n") {
		t.Fatalf("expected paths frontmatter, got %q", content)
	}
	if !strings.Contains(content, "\"lib/**/*.ts\"") || !strings.Contains(content, "\"src/**/*.ts\"") {
		t.Fatalf("expected glob entries in frontmatter, got %q", content)
	}
}

func TestWriteClaudeDetectsNestedPathCollision(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "rules")
	target := config.TargetEntry{OutDir: outDir, PerModule: true, Ext: ".md"}
	modules := []pack.Module{
		{ID: "a.one", Path: "modules/backend/api/auth.md", Priority: 100, Content: "A\n"},
		{ID: "b.two", Path: "modules/backend/api/auth.txt", Priority: 100, Content: "B\n"},
	}
	err := WriteClaude(target, modules)
	if err == nil || !strings.Contains(err.Error(), "claude output collision") {
		t.Fatalf("expected claude collision error, got %v", err)
	}
}

func TestWriteClaude_GlobRequiresGlobs(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "rules")
	target := config.TargetEntry{OutDir: outDir, PerModule: true, Ext: ".md"}
	modules := []pack.Module{
		{
			ID:       "a.glob",
			Priority: 100,
			Content:  "A\n",
			Apply: pack.ApplyConfig{
				Targets: map[string]pack.ApplyRule{
					"claude": {Mode: "glob"},
				},
			},
		},
	}
	if err := WriteClaude(target, modules); err == nil {
		t.Fatalf("expected error for glob mode without globs")
	}
}

func TestWriteClaude_RejectsOutFile(t *testing.T) {
	target := config.TargetEntry{
		OutDir:    filepath.Join(t.TempDir(), "rules"),
		OutFile:   "CLAUDE.md",
		PerModule: true,
		Ext:       ".md",
	}
	err := WriteClaude(target, []pack.Module{{ID: "a", Priority: 100, Content: "A\n"}})
	if err == nil || !strings.Contains(err.Error(), "does not support outFile") {
		t.Fatalf("expected outFile validation error, got %v", err)
	}
}

func TestWriteClaude_RejectsPerModuleFalse(t *testing.T) {
	target := config.TargetEntry{
		OutDir:    filepath.Join(t.TempDir(), "rules"),
		PerModule: false,
		Ext:       ".md",
	}
	err := WriteClaude(target, []pack.Module{{ID: "a", Priority: 100, Content: "A\n"}})
	if err == nil || !strings.Contains(err.Error(), "requires perModule=true") {
		t.Fatalf("expected perModule validation error, got %v", err)
	}
}

func TestCleanupManagedOutputs_MergedManagedAndUnmanaged(t *testing.T) {
	root := t.TempDir()
	managed := filepath.Join(root, "managed.md")
	unmanaged := filepath.Join(root, "manual.md")
	if err := os.WriteFile(managed, []byte("<!-- rulepack:managed -->\nA\n"), 0o644); err != nil {
		t.Fatalf("write managed: %v", err)
	}
	if err := os.WriteFile(unmanaged, []byte("manual\n"), 0o644); err != nil {
		t.Fatalf("write unmanaged: %v", err)
	}
	targets := map[string]config.TargetEntry{
		"codex": {OutFile: managed},
	}
	deletable, skipped, err := PreviewManagedCleanup(targets)
	if err != nil {
		t.Fatalf("PreviewManagedCleanup: %v", err)
	}
	if len(deletable) != 1 || deletable[0] != managed {
		t.Fatalf("unexpected deletable: %#v", deletable)
	}
	if len(skipped) != 0 {
		t.Fatalf("unexpected skipped: %#v", skipped)
	}
	deleted, skipped, err := CleanupManagedOutputs(targets)
	if err != nil {
		t.Fatalf("CleanupManagedOutputs: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != managed || len(skipped) != 0 {
		t.Fatalf("unexpected cleanup result deleted=%#v skipped=%#v", deleted, skipped)
	}
	if _, err := os.Stat(managed); !os.IsNotExist(err) {
		t.Fatalf("expected managed file deleted, stat err=%v", err)
	}
	if _, err := os.Stat(unmanaged); err != nil {
		t.Fatalf("expected unmanaged file untouched, stat err=%v", err)
	}
}

func TestCleanupManagedOutputs_ClaudeManagedAndUnmanaged(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "claude-rules")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	managed := filepath.Join(outDir, "100-python_base.md")
	unmanaged := filepath.Join(outDir, "110-manual.md")
	if err := os.WriteFile(managed, []byte("<!-- pack=x version=1 commit=a module=b priority=100 -->\nA\n"), 0o644); err != nil {
		t.Fatalf("write managed: %v", err)
	}
	if err := os.WriteFile(unmanaged, []byte("manual\n"), 0o644); err != nil {
		t.Fatalf("write unmanaged: %v", err)
	}
	targets := map[string]config.TargetEntry{
		"claude": {OutDir: outDir, PerModule: true, Ext: ".md"},
	}
	deleted, skipped, err := CleanupManagedOutputs(targets)
	if err != nil {
		t.Fatalf("CleanupManagedOutputs: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != managed {
		t.Fatalf("unexpected deleted: %#v", deleted)
	}
	if len(skipped) != 1 || skipped[0] != unmanaged {
		t.Fatalf("unexpected skipped: %#v", skipped)
	}
	if _, err := os.Stat(managed); !os.IsNotExist(err) {
		t.Fatalf("expected managed file deleted, stat err=%v", err)
	}
	if _, err := os.Stat(unmanaged); err != nil {
		t.Fatalf("expected unmanaged file untouched, stat err=%v", err)
	}
}

func TestCleanupManagedOutputs_ClaudeManagedAndUnmanagedNested(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "claude-rules")
	nestedDir := filepath.Join(outDir, "backend", "api")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	managed := filepath.Join(nestedDir, "100-auth.md")
	unmanaged := filepath.Join(nestedDir, "110-manual.md")
	if err := os.WriteFile(managed, []byte("<!-- pack=x version=1 commit=a module=b priority=100 -->\nA\n"), 0o644); err != nil {
		t.Fatalf("write managed: %v", err)
	}
	if err := os.WriteFile(unmanaged, []byte("manual\n"), 0o644); err != nil {
		t.Fatalf("write unmanaged: %v", err)
	}
	targets := map[string]config.TargetEntry{
		"claude": {OutDir: outDir, PerModule: true, Ext: ".md"},
	}
	deleted, skipped, err := CleanupManagedOutputs(targets)
	if err != nil {
		t.Fatalf("CleanupManagedOutputs: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != managed {
		t.Fatalf("unexpected deleted: %#v", deleted)
	}
	if len(skipped) != 1 || skipped[0] != unmanaged {
		t.Fatalf("unexpected skipped: %#v", skipped)
	}
}

func TestCleanupManagedOutputs_CursorManagedAndUnmanaged(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "rules")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	managed := filepath.Join(outDir, "100-python_base.mdc")
	unmanaged := filepath.Join(outDir, "110-manual.mdc")
	if err := os.WriteFile(managed, []byte("<!-- pack=x version=1 commit=a module=b priority=100 -->\nA\n"), 0o644); err != nil {
		t.Fatalf("write managed: %v", err)
	}
	if err := os.WriteFile(unmanaged, []byte("manual\n"), 0o644); err != nil {
		t.Fatalf("write unmanaged: %v", err)
	}
	targets := map[string]config.TargetEntry{
		"cursor": {OutDir: outDir, PerModule: true, Ext: ".mdc"},
	}
	deleted, skipped, err := CleanupManagedOutputs(targets)
	if err != nil {
		t.Fatalf("CleanupManagedOutputs: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != managed {
		t.Fatalf("unexpected deleted: %#v", deleted)
	}
	if len(skipped) != 1 || skipped[0] != unmanaged {
		t.Fatalf("unexpected skipped: %#v", skipped)
	}
	if _, err := os.Stat(managed); !os.IsNotExist(err) {
		t.Fatalf("expected managed file deleted, stat err=%v", err)
	}
	if _, err := os.Stat(unmanaged); err != nil {
		t.Fatalf("expected unmanaged file untouched, stat err=%v", err)
	}
}

func TestCleanupManagedOutputs_CursorManagedAndUnmanagedNested(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "rules")
	nestedDir := filepath.Join(outDir, "backend", "api")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	managed := filepath.Join(nestedDir, "100-auth.mdc")
	unmanaged := filepath.Join(nestedDir, "110-manual.mdc")
	if err := os.WriteFile(managed, []byte("<!-- pack=x version=1 commit=a module=b priority=100 -->\nA\n"), 0o644); err != nil {
		t.Fatalf("write managed: %v", err)
	}
	if err := os.WriteFile(unmanaged, []byte("manual\n"), 0o644); err != nil {
		t.Fatalf("write unmanaged: %v", err)
	}
	targets := map[string]config.TargetEntry{
		"cursor": {OutDir: outDir, PerModule: true, Ext: ".mdc"},
	}
	deleted, skipped, err := CleanupManagedOutputs(targets)
	if err != nil {
		t.Fatalf("CleanupManagedOutputs: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != managed {
		t.Fatalf("unexpected deleted: %#v", deleted)
	}
	if len(skipped) != 1 || skipped[0] != unmanaged {
		t.Fatalf("unexpected skipped: %#v", skipped)
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
