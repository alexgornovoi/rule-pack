package main

import (
	"os"
	"path/filepath"
	"testing"

	"rulepack/internal/config"
	"rulepack/internal/pack"
)

func TestResolveLocalPath_RelativeToConfigDir(t *testing.T) {
	cfgDir := t.TempDir()
	localDir := filepath.Join(cfgDir, "packs", "local-pack")
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localDir, config.RulesetFileName), []byte(`{"specVersion":"0.1","name":"x","version":"1.0.0","modules":[]}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	abs, rel, err := resolveLocalPath(cfgDir, "./packs/local-pack")
	if err != nil {
		t.Fatalf("resolveLocalPath: %v", err)
	}
	if abs != filepath.Clean(localDir) {
		t.Fatalf("expected abs=%s, got %s", filepath.Clean(localDir), abs)
	}
	if rel != "packs/local-pack" {
		t.Fatalf("expected rel path packs/local-pack, got %s", rel)
	}
}

func TestSourceDefaults(t *testing.T) {
	if got := dependencySource(config.Dependency{}); got != "git" {
		t.Fatalf("expected git default dependency source, got %s", got)
	}
	if got := lockSource(config.LockedSource{}); got != "git" {
		t.Fatalf("expected git default lock source, got %s", got)
	}
}

func TestInitTemplateRulepack(t *testing.T) {
	cfg, files, err := initTemplate("demo", "rulepack")
	if err != nil {
		t.Fatalf("initTemplate: %v", err)
	}
	if cfg.Name != "demo" {
		t.Fatalf("expected name demo, got %s", cfg.Name)
	}
	if len(cfg.Dependencies) != 1 {
		t.Fatalf("expected one dependency, got %d", len(cfg.Dependencies))
	}
	dep := cfg.Dependencies[0]
	if dep.Source != "local" || dep.Path != ".rulepack/packs/rule-authoring" || dep.Export != "default" {
		t.Fatalf("unexpected dependency: %+v", dep)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 scaffold files, got %d", len(files))
	}
}

func TestInitTemplateUnknown(t *testing.T) {
	_, _, err := initTemplate("demo", "unknown")
	if err == nil {
		t.Fatalf("expected unknown template error")
	}
}

func TestFindDependencyIndexBySource(t *testing.T) {
	cfg := config.Ruleset{
		Dependencies: []config.Dependency{
			{Source: "git", URI: "https://example.com/a.git"},
			{Source: "local", Path: "../rules"},
		},
	}
	idx, err := findDependencyIndex(cfg, "https://example.com/a.git")
	if err != nil {
		t.Fatalf("findDependencyIndex: %v", err)
	}
	if idx != 0 {
		t.Fatalf("expected index 0 got %d", idx)
	}
}

func TestMergeRefreshedModulesSelective(t *testing.T) {
	current := []pack.Module{
		{ID: "python.base", Priority: 100, Content: "old\n"},
		{ID: "ml.safety", Priority: 110, Content: "stay\n"},
	}
	fresh := []pack.Module{
		{ID: "python.base", Priority: 100, Content: "new\n"},
		{ID: "python.new", Priority: 120, Content: "add\n"},
	}

	out, refreshed, err := mergeRefreshedModules(current, fresh, []string{"python.*"})
	if err != nil {
		t.Fatalf("mergeRefreshedModules: %v", err)
	}
	if len(refreshed) != 2 {
		t.Fatalf("expected 2 refreshed ids, got %d", len(refreshed))
	}
	foundNew := false
	for _, m := range out {
		if m.ID == "python.base" && m.Content != "new\n" {
			t.Fatalf("expected python.base to refresh")
		}
		if m.ID == "python.new" {
			foundNew = true
		}
	}
	if !foundNew {
		t.Fatalf("expected python.new to be added from selective refresh")
	}
}

func TestLockReference(t *testing.T) {
	gitLocked := config.LockedSource{Source: "git", Commit: "abcdef0123456789"}
	if got := lockReference(gitLocked); got != "abcdef012345" {
		t.Fatalf("unexpected git lock reference: %s", got)
	}

	localLocked := config.LockedSource{Source: "local", ContentHash: "1234567890abcdef"}
	if got := lockReference(localLocked); got != "1234567890ab" {
		t.Fatalf("unexpected local lock reference: %s", got)
	}
}

func TestDryRunMessage(t *testing.T) {
	if got := dryRunMessage(true); got == "" {
		t.Fatalf("expected dry-run message")
	}
	if got := dryRunMessage(false); got == "" {
		t.Fatalf("expected non-dry-run message")
	}
}

func TestDiffModules(t *testing.T) {
	current := []pack.Module{
		{ID: "a", Priority: 100, Content: "one\n"},
		{ID: "b", Priority: 110, Content: "same\n"},
		{ID: "c", Priority: 120, Content: "remove\n"},
	}
	fresh := []pack.Module{
		{ID: "a", Priority: 100, Content: "two\n"},
		{ID: "b", Priority: 110, Content: "same\n"},
		{ID: "d", Priority: 130, Content: "add\n"},
	}
	changed, added, removed := diffModules(current, fresh)
	if len(changed) != 1 || changed[0] != "a" {
		t.Fatalf("unexpected changed: %#v", changed)
	}
	if len(added) != 1 || added[0] != "d" {
		t.Fatalf("unexpected added: %#v", added)
	}
	if len(removed) != 1 || removed[0] != "c" {
		t.Fatalf("unexpected removed: %#v", removed)
	}
}

func TestFilterModulesByPatterns(t *testing.T) {
	modules := []pack.Module{
		{ID: "python.base"},
		{ID: "ml.safety"},
		{ID: "go.base"},
	}
	filtered := filterModulesByPatterns(modules, []string{"python.*", "ml.*"})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(filtered))
	}
}
