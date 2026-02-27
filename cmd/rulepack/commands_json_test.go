package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"rulepack/internal/cliout"
	"rulepack/internal/config"
	"rulepack/internal/pack"
	profilesvc "rulepack/internal/profile"
)

type jsonEnvelope struct {
	Command string          `json:"command"`
	Result  json.RawMessage `json:"result"`
}

func TestOutdatedCommandJSON(t *testing.T) {
	repoDir, oldCommit, _, err := createGitRepoWithTwoCommits(t)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	projectDir := t.TempDir()
	cfg := config.Ruleset{
		SpecVersion: "0.1",
		Name:        "proj",
		Dependencies: []config.Dependency{
			{Source: "git", URI: repoDir},
		},
	}
	lock := config.Lockfile{
		LockVersion: "0.1",
		Resolved: []config.LockedSource{
			{Source: "git", URI: repoDir, Commit: oldCommit},
		},
	}
	if err := config.SaveRuleset(filepath.Join(projectDir, config.RulesetFileName), cfg); err != nil {
		t.Fatalf("save ruleset: %v", err)
	}
	if err := config.SaveLockfile(filepath.Join(projectDir, config.LockFileName), lock); err != nil {
		t.Fatalf("save lock: %v", err)
	}

	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var env jsonEnvelope
	if err := runCmdJSON(t, projectDir, a.newDepsOutdatedCmd(), &env); err != nil {
		t.Fatalf("outdated command failed: %v", err)
	}
	if env.Command != "outdated" {
		t.Fatalf("unexpected command: %s", env.Command)
	}
	var out outdatedOutput
	if err := json.Unmarshal(env.Result, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out.OutdatedCount != 1 {
		t.Fatalf("expected outdated count 1, got %d", out.OutdatedCount)
	}
	if len(out.Dependencies) != 1 || out.Dependencies[0].UpdateStatus != "outdated" {
		t.Fatalf("unexpected dependency status: %#v", out.Dependencies)
	}
}

func TestDepsListCommandJSON(t *testing.T) {
	projectDir := t.TempDir()
	cfg := config.Ruleset{
		SpecVersion: "0.1",
		Name:        "proj",
		Dependencies: []config.Dependency{
			{Source: "git", URI: "https://example.com/rules.git", Export: "python"},
		},
	}
	lock := config.Lockfile{
		LockVersion: "0.1",
		Resolved: []config.LockedSource{
			{Source: "git", URI: "https://example.com/rules.git", Commit: "abcdef0123456789"},
		},
	}
	if err := config.SaveRuleset(filepath.Join(projectDir, config.RulesetFileName), cfg); err != nil {
		t.Fatalf("save ruleset: %v", err)
	}
	if err := config.SaveLockfile(filepath.Join(projectDir, config.LockFileName), lock); err != nil {
		t.Fatalf("save lock: %v", err)
	}

	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var env jsonEnvelope
	if err := runCmdJSON(t, projectDir, a.newDepsListCmd(), &env); err != nil {
		t.Fatalf("deps list failed: %v", err)
	}

	var out depsListOutput
	if err := json.Unmarshal(env.Result, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(out.Dependencies) != 1 {
		t.Fatalf("expected one dependency row, got %d", len(out.Dependencies))
	}
	if out.Dependencies[0].Locked != "abcdef012345" {
		t.Fatalf("unexpected locked reference: %s", out.Dependencies[0].Locked)
	}
}

func TestBuildCommandJSON_RequiresYesOnCursorOverwriteCollision(t *testing.T) {
	projectDir := t.TempDir()
	sourceDir := createLocalSourcePackWithID(t, "python.base", "base rule\n")
	relSource, _ := filepath.Rel(projectDir, sourceDir)
	cfg := config.DefaultRuleset("proj")
	cfg.Dependencies = []config.Dependency{
		{Source: "local", Path: filepath.ToSlash(relSource), Export: "default"},
	}
	if err := config.SaveRuleset(filepath.Join(projectDir, config.RulesetFileName), cfg); err != nil {
		t.Fatalf("save ruleset: %v", err)
	}

	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var installEnv jsonEnvelope
	if err := runCmdJSON(t, projectDir, a.newDepsInstallCmd(), &installEnv); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	collision := filepath.Join(projectDir, ".cursor", "rules", "100-python_base.mdc")
	if err := os.MkdirAll(filepath.Dir(collision), 0o755); err != nil {
		t.Fatalf("mkdir collision dir: %v", err)
	}
	if err := os.WriteFile(collision, []byte("manual rule\n"), 0o644); err != nil {
		t.Fatalf("write collision file: %v", err)
	}

	var env jsonEnvelope
	err := runCmdJSON(t, projectDir, a.newBuildCmd(), &env)
	if err == nil {
		t.Fatalf("expected build to fail without --yes on unmanaged overwrite collision")
	}
	if !strings.Contains(err.Error(), "rerun with --yes") {
		t.Fatalf("unexpected build error: %v", err)
	}
	if err := runCmdJSON(t, projectDir, a.newBuildCmd(), &env, "--yes"); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	var out buildOutput
	if err := json.Unmarshal(env.Result, &out); err != nil {
		t.Fatalf("unmarshal build output: %v", err)
	}
	if len(out.Warnings) != 1 {
		t.Fatalf("expected one warning, got %#v", out.Warnings)
	}
	if !strings.Contains(out.Warnings[0], ".cursor/rules/100-python_base.mdc") {
		t.Fatalf("unexpected warning: %s", out.Warnings[0])
	}
}

func TestAddCommandJSON_RequiresYesWhenReplacingDependency(t *testing.T) {
	projectDir := t.TempDir()
	cfg := config.Ruleset{
		SpecVersion: "0.1",
		Name:        "proj",
		Dependencies: []config.Dependency{
			{Source: "git", URI: "https://example.com/rules.git", Export: "default"},
		},
	}
	if err := config.SaveRuleset(filepath.Join(projectDir, config.RulesetFileName), cfg); err != nil {
		t.Fatalf("save ruleset: %v", err)
	}
	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var env jsonEnvelope
	err := runCmdJSON(t, projectDir, a.newDepsAddCmd(), &env, "https://example.com/rules.git", "--export", "python")
	if err == nil {
		t.Fatalf("expected add to fail without --yes when replacing dependency")
	}
	if !strings.Contains(err.Error(), "rerun with --yes") {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := runCmdJSON(t, projectDir, a.newDepsAddCmd(), &env, "https://example.com/rules.git", "--export", "python", "--yes"); err != nil {
		t.Fatalf("add with --yes failed: %v", err)
	}
	var out addOutput
	if err := json.Unmarshal(env.Result, &out); err != nil {
		t.Fatalf("unmarshal add result: %v", err)
	}
	if out.Action != "replaced" {
		t.Fatalf("expected replaced action, got %s", out.Action)
	}
}

func TestAddCommandJSON_AutoInitWhenMissingRuleset(t *testing.T) {
	projectDir := t.TempDir()

	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var env jsonEnvelope
	if err := runCmdJSON(t, projectDir, a.newDepsAddCmd(), &env, "https://example.com/rules.git", "--export", "python"); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if env.Command != "add" {
		t.Fatalf("unexpected command: %s", env.Command)
	}

	var out addOutput
	if err := json.Unmarshal(env.Result, &out); err != nil {
		t.Fatalf("unmarshal add result: %v", err)
	}
	if out.Action != "added" {
		t.Fatalf("expected added action, got %s", out.Action)
	}

	cfg, err := config.LoadRuleset(filepath.Join(projectDir, config.RulesetFileName))
	if err != nil {
		t.Fatalf("load ruleset: %v", err)
	}
	if cfg.SpecVersion != "0.1" {
		t.Fatalf("unexpected specVersion: %s", cfg.SpecVersion)
	}
	if cfg.Name != filepath.Base(projectDir) {
		t.Fatalf("expected auto-init name %s, got %s", filepath.Base(projectDir), cfg.Name)
	}
	if len(cfg.Dependencies) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(cfg.Dependencies))
	}
	if cfg.Dependencies[0].URI != "https://example.com/rules.git" || cfg.Dependencies[0].Export != "python" {
		t.Fatalf("unexpected dependency: %+v", cfg.Dependencies[0])
	}
	if _, ok := cfg.Targets["cursor"]; !ok {
		t.Fatalf("expected default cursor target to be present")
	}
}

func TestAddCommandJSON_LocalDependency(t *testing.T) {
	projectDir := t.TempDir()
	localPack := createLocalSourcePackWithID(t, "python.base", "python rule\n")
	relLocal, err := filepath.Rel(projectDir, localPack)
	if err != nil {
		t.Fatalf("rel local: %v", err)
	}

	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var env jsonEnvelope
	if err := runCmdJSON(t, projectDir, a.newDepsAddCmd(), &env, "--local", relLocal, "--export", "python"); err != nil {
		t.Fatalf("add local failed: %v", err)
	}

	var out addOutput
	if err := json.Unmarshal(env.Result, &out); err != nil {
		t.Fatalf("unmarshal add output: %v", err)
	}
	if out.Dependency.Source != "local" {
		t.Fatalf("expected local source, got %#v", out.Dependency)
	}
	if out.Dependency.Path != filepath.ToSlash(relLocal) {
		t.Fatalf("expected normalized path %q, got %q", filepath.ToSlash(relLocal), out.Dependency.Path)
	}
	if out.Dependency.Export != "python" {
		t.Fatalf("expected export=python, got %#v", out.Dependency)
	}
}

func TestAddCommandJSON_LocalReplacementRequiresYes(t *testing.T) {
	projectDir := t.TempDir()
	localPack := createLocalSourcePackWithID(t, "python.base", "python rule\n")
	relLocal, err := filepath.Rel(projectDir, localPack)
	if err != nil {
		t.Fatalf("rel local: %v", err)
	}
	cfg := config.Ruleset{
		SpecVersion: "0.1",
		Name:        "proj",
		Dependencies: []config.Dependency{
			{Source: "local", Path: filepath.ToSlash(relLocal), Export: "default"},
		},
	}
	if err := config.SaveRuleset(filepath.Join(projectDir, config.RulesetFileName), cfg); err != nil {
		t.Fatalf("save ruleset: %v", err)
	}

	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var env jsonEnvelope
	err = runCmdJSON(t, projectDir, a.newDepsAddCmd(), &env, "--local", relLocal, "--export", "python")
	if err == nil {
		t.Fatalf("expected local replacement to require --yes")
	}
	if !strings.Contains(err.Error(), "rerun with --yes") {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := runCmdJSON(t, projectDir, a.newDepsAddCmd(), &env, "--local", relLocal, "--export", "python", "--yes"); err != nil {
		t.Fatalf("add local with --yes failed: %v", err)
	}
}

func TestAddCommandJSON_LocalValidation(t *testing.T) {
	projectDir := t.TempDir()
	localPack := createLocalSourcePackWithID(t, "python.base", "python rule\n")
	relLocal, err := filepath.Rel(projectDir, localPack)
	if err != nil {
		t.Fatalf("rel local: %v", err)
	}
	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var env jsonEnvelope

	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "missing source", args: []string{}, wantErr: "missing source: provide <git-url> or --local <path>"},
		{name: "both source modes", args: []string{"https://example.com/rules.git", "--local", relLocal}, wantErr: "use either <git-url> or --local <path>, not both"},
		{name: "local plus version", args: []string{"--local", relLocal, "--version", "^1.0.0"}, wantErr: "--version and --ref are only supported for git dependencies"},
		{name: "local plus ref", args: []string{"--local", relLocal, "--ref", "main"}, wantErr: "--version and --ref are only supported for git dependencies"},
		{name: "missing local path", args: []string{"--local", filepath.Join(projectDir, "missing-pack")}, wantErr: "local dependency path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runCmdJSON(t, projectDir, a.newDepsAddCmd(), &env, tc.args...)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestAddCommandJSON_LocalMissingRulepack(t *testing.T) {
	projectDir := t.TempDir()
	invalidLocal := filepath.Join(t.TempDir(), "rules")
	if err := os.MkdirAll(invalidLocal, 0o755); err != nil {
		t.Fatalf("mkdir invalid local: %v", err)
	}
	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var env jsonEnvelope
	err := runCmdJSON(t, projectDir, a.newDepsAddCmd(), &env, "--local", invalidLocal)
	if err == nil {
		t.Fatalf("expected missing rulepack.json error")
	}
	if !strings.Contains(err.Error(), "local dependency missing rulepack.json") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveCommandJSON(t *testing.T) {
	projectDir := t.TempDir()
	cfg := config.Ruleset{
		SpecVersion: "0.1",
		Name:        "proj",
		Dependencies: []config.Dependency{
			{Source: "git", URI: "https://example.com/rules.git", Export: "default"},
			{Source: "local", Path: "../local-rules", Export: "local"},
			{Source: "profile", Profile: "abc123__python__01", Export: "default"},
		},
	}
	if err := config.SaveRuleset(filepath.Join(projectDir, config.RulesetFileName), cfg); err != nil {
		t.Fatalf("save ruleset: %v", err)
	}

	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var env jsonEnvelope
	if err := runCmdJSON(t, projectDir, a.newDepsRemoveCmd(), &env, "1", "abc123__python__01", "--yes"); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if env.Command != "remove" {
		t.Fatalf("unexpected command: %s", env.Command)
	}

	var out removeOutput
	if err := json.Unmarshal(env.Result, &out); err != nil {
		t.Fatalf("unmarshal remove result: %v", err)
	}
	if len(out.Removed) != 2 {
		t.Fatalf("expected 2 removed dependencies, got %d", len(out.Removed))
	}
	if out.Remaining != 1 {
		t.Fatalf("expected 1 remaining dependency, got %d", out.Remaining)
	}
	newCfg, err := config.LoadRuleset(filepath.Join(projectDir, config.RulesetFileName))
	if err != nil {
		t.Fatalf("load ruleset: %v", err)
	}
	if len(newCfg.Dependencies) != 1 || newCfg.Dependencies[0].Source != "local" {
		t.Fatalf("unexpected remaining dependencies: %#v", newCfg.Dependencies)
	}
}

func TestRemoveCommandJSON_RequiresYes(t *testing.T) {
	projectDir := t.TempDir()
	cfg := config.Ruleset{
		SpecVersion: "0.1",
		Name:        "proj",
		Dependencies: []config.Dependency{
			{Source: "git", URI: "https://example.com/rules.git", Export: "default"},
		},
	}
	if err := config.SaveRuleset(filepath.Join(projectDir, config.RulesetFileName), cfg); err != nil {
		t.Fatalf("save ruleset: %v", err)
	}
	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var env jsonEnvelope
	err := runCmdJSON(t, projectDir, a.newDepsRemoveCmd(), &env, "1")
	if err == nil {
		t.Fatalf("expected remove to fail without --yes")
	}
	if !strings.Contains(err.Error(), "rerun with --yes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveCommand_AmbiguousSelector(t *testing.T) {
	projectDir := t.TempDir()
	cfg := config.Ruleset{
		SpecVersion: "0.1",
		Name:        "proj",
		Dependencies: []config.Dependency{
			{Source: "git", URI: "https://example.com/rules.git"},
			{Source: "git", URI: "https://example.com/rules.git"},
		},
	}
	if err := config.SaveRuleset(filepath.Join(projectDir, config.RulesetFileName), cfg); err != nil {
		t.Fatalf("save ruleset: %v", err)
	}

	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var env jsonEnvelope
	err := runCmdJSON(t, projectDir, a.newDepsRemoveCmd(), &env, "https://example.com/rules.git")
	if err == nil {
		t.Fatalf("expected remove to fail for ambiguous selector")
	}
	if !strings.Contains(err.Error(), "matched multiple dependencies") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRootHasNoTopLevelDependencyLifecycleCommands(t *testing.T) {
	a := &app{}
	root := &cobra.Command{
		Use: "rulepack",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			a.renderer = cliout.NewJSONRenderer()
			return nil
		},
	}
	root.AddCommand(a.newInitCmd())
	root.AddCommand(a.newDepsCmd())
	root.AddCommand(a.newBuildCmd())
	root.AddCommand(a.newDoctorCmd())
	root.AddCommand(a.newProfileCmd())
	for _, name := range []string{"add", "install", "outdated", "remove"} {
		if cmd, _, err := root.Find([]string{name}); err == nil && cmd != nil {
			t.Fatalf("expected no top-level %s command", name)
		}
	}
}

func TestProfileSave_AllDependenciesDefaultJSON(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	projectDir := t.TempDir()

	depA := createLocalSourcePackWithID(t, "alpha.base", "alpha v1\n")
	depB := createLocalSourcePackWithID(t, "beta.base", "beta v1\n")
	relA, _ := filepath.Rel(projectDir, depA)
	relB, _ := filepath.Rel(projectDir, depB)
	cfg := config.Ruleset{
		SpecVersion: "0.1",
		Name:        "proj",
		Dependencies: []config.Dependency{
			{Source: "local", Path: filepath.ToSlash(relA), Export: "default"},
			{Source: "local", Path: filepath.ToSlash(relB), Export: "default"},
		},
	}
	if err := config.SaveRuleset(filepath.Join(projectDir, config.RulesetFileName), cfg); err != nil {
		t.Fatalf("save ruleset: %v", err)
	}

	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var installEnv jsonEnvelope
	if err := runCmdJSON(t, projectDir, a.newDepsInstallCmd(), &installEnv); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	var env jsonEnvelope
	if err := runCmdJSON(t, projectDir, a.newProfileSaveCmd(), &env, "--alias", "combo"); err != nil {
		t.Fatalf("profile save failed: %v", err)
	}
	var out profileSaveOutput
	if err := json.Unmarshal(env.Result, &out); err != nil {
		t.Fatalf("unmarshal profile save: %v", err)
	}
	if out.Scope != "all" || !out.Combined || out.SourceCount != 2 {
		t.Fatalf("unexpected save scope output: %#v", out)
	}
	if out.Switched {
		t.Fatalf("expected switched=false by default")
	}
	newCfg, err := config.LoadRuleset(filepath.Join(projectDir, config.RulesetFileName))
	if err != nil {
		t.Fatalf("load ruleset: %v", err)
	}
	if len(newCfg.Dependencies) != 2 || newCfg.Dependencies[0].Source != "local" {
		t.Fatalf("expected dependencies unchanged, got %#v", newCfg.Dependencies)
	}
	meta, _, err := profilesvc.ResolveIDOrAlias(out.Profile.ID)
	if err != nil {
		t.Fatalf("resolve profile: %v", err)
	}
	if len(meta.Sources) != 2 {
		t.Fatalf("expected combined profile sources, got %#v", meta.Sources)
	}
}

func TestProfileSave_RequiresAliasInNonInteractiveMode(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	projectDir := t.TempDir()
	depA := createLocalSourcePackWithID(t, "alpha.base", "alpha v1\n")
	relA, _ := filepath.Rel(projectDir, depA)
	cfg := config.Ruleset{
		SpecVersion: "0.1",
		Name:        "proj",
		Dependencies: []config.Dependency{
			{Source: "local", Path: filepath.ToSlash(relA), Export: "default"},
		},
	}
	if err := config.SaveRuleset(filepath.Join(projectDir, config.RulesetFileName), cfg); err != nil {
		t.Fatalf("save ruleset: %v", err)
	}
	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var installEnv jsonEnvelope
	if err := runCmdJSON(t, projectDir, a.newDepsInstallCmd(), &installEnv); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	var env jsonEnvelope
	err := runCmdJSON(t, projectDir, a.newProfileSaveCmd(), &env)
	if err == nil {
		t.Fatalf("expected alias requirement error")
	}
	if !strings.Contains(err.Error(), "requires --alias in non-interactive mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProfileRefresh_BestEffortCombinedJSON(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	sourceDir := createLocalSourcePackWithID(t, "alpha.base", "alpha new\n")
	missingDir := filepath.Join(t.TempDir(), "missing")

	modules := []pack.Module{
		{PackName: "snapshot", PackVersion: "1.0.0", Commit: "profile", ID: "alpha.base", Priority: 100, Content: "alpha old\n"},
		{PackName: "snapshot", PackVersion: "1.0.0", Commit: "profile", ID: "beta.base", Priority: 110, Content: "beta old\n"},
	}
	meta, err := profilesvc.SaveSnapshot(profilesvc.SaveInput{
		Alias: "combo",
		Sources: []profilesvc.SourceSnapshot{
			{SourceType: "local", SourceRef: sourceDir, SourceExport: "default", ModuleIDs: []string{"alpha.base"}, Provenance: map[string]string{"path": sourceDir}},
			{SourceType: "local", SourceRef: missingDir, SourceExport: "default", ModuleIDs: []string{"beta.base"}, Provenance: map[string]string{"path": missingDir}},
		},
		ContentHash: profilesvc.ComputeContentHash(modules, "default"),
		Modules:     modules,
	})
	if err != nil {
		t.Fatalf("save combined profile: %v", err)
	}

	projectDir := t.TempDir()
	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var env jsonEnvelope
	if err := runCmdJSON(t, projectDir, a.newProfileRefreshCmd(), &env, meta.ID, "--dry-run"); err != nil {
		t.Fatalf("profile refresh failed: %v", err)
	}
	var out profileRefreshOutput
	if err := json.Unmarshal(env.Result, &out); err != nil {
		t.Fatalf("unmarshal profile refresh: %v", err)
	}
	if len(out.RefreshedSources) != 1 || len(out.SkippedSources) != 1 {
		t.Fatalf("expected best-effort source statuses, got %#v", out)
	}
	if len(out.ChangedModules) == 0 || out.ChangedModules[0] != "alpha.base" {
		t.Fatalf("expected alpha.base to change, got %#v", out.ChangedModules)
	}
}

func TestProfileRefreshJSON_RequiresYesForInPlaceChanges(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	sourceDir := createLocalSourcePack(t, "new content\n")
	savedMeta := createSavedProfile(t, sourceDir, "old content\n")
	projectDir := t.TempDir()
	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var env jsonEnvelope
	err := runCmdJSON(t, projectDir, a.newProfileRefreshCmd(), &env, savedMeta.ID)
	if err == nil {
		t.Fatalf("expected in-place refresh to fail without --yes")
	}
	if !strings.Contains(err.Error(), "rerun with --yes") {
		t.Fatalf("unexpected refresh error: %v", err)
	}
	if err := runCmdJSON(t, projectDir, a.newProfileRefreshCmd(), &env, savedMeta.ID, "--yes"); err != nil {
		t.Fatalf("refresh with --yes failed: %v", err)
	}
}

func TestProfileRemoveCommandsJSON(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	sourceDir := createLocalSourcePack(t, "content\n")
	meta := createSavedProfile(t, sourceDir, "snapshot\n")
	projectDir := t.TempDir()
	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}

	// single remove by alias
	{
		var env jsonEnvelope
		if err := runCmdJSON(t, projectDir, a.newProfileRemoveCmd(), &env, "python-a", "--yes"); err != nil {
			t.Fatalf("profile remove by alias failed: %v", err)
		}
		var out profileRemoveOutput
		if err := json.Unmarshal(env.Result, &out); err != nil {
			t.Fatalf("unmarshal profile remove output: %v", err)
		}
		if !out.Removed || out.ProfileID != meta.ID {
			t.Fatalf("unexpected profile remove output: %#v", out)
		}
	}

	// re-create two profiles and remove all
	_ = createSavedProfile(t, sourceDir, "snapshot2\n")
	mods := []pack.Module{{ID: "python.extra", Priority: 100, Content: "x\n", Commit: "local", PackName: "source-pack", PackVersion: "1.0.0"}}
	_, err := profilesvc.SaveSnapshot(profilesvc.SaveInput{
		Alias: "python-b",
		Sources: []profilesvc.SourceSnapshot{{
			SourceType:   "local",
			SourceRef:    sourceDir,
			SourceExport: "default",
			ModuleIDs:    []string{"python.extra"},
			Provenance:   map[string]string{"path": sourceDir},
		}},
		ContentHash: profilesvc.ComputeContentHash(mods, "default"),
		Modules:     mods,
	})
	if err != nil {
		t.Fatalf("save second profile: %v", err)
	}
	{
		var env jsonEnvelope
		if err := runCmdJSON(t, projectDir, a.newProfileRemoveCmd(), &env, "--all", "--yes"); err != nil {
			t.Fatalf("profile remove --all failed: %v", err)
		}
		var out profileRemoveOutput
		if err := json.Unmarshal(env.Result, &out); err != nil {
			t.Fatalf("unmarshal profile remove all output: %v", err)
		}
		if out.Count < 2 {
			t.Fatalf("expected at least 2 profiles removed, got %#v", out)
		}
	}

	// --all without --yes in non-interactive mode should fail
	{
		var env jsonEnvelope
		err := runCmdJSON(t, projectDir, a.newProfileRemoveCmd(), &env, "--all")
		if err == nil {
			t.Fatalf("expected non-interactive confirmation error")
		}
		if !strings.Contains(err.Error(), "requires --yes in non-interactive mode") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestDoctorCommandJSON(t *testing.T) {
	projectDir := t.TempDir()
	cfg := config.Ruleset{SpecVersion: "0.1", Name: "proj"}
	lock := config.Lockfile{LockVersion: "0.1"}
	if err := config.SaveRuleset(filepath.Join(projectDir, config.RulesetFileName), cfg); err != nil {
		t.Fatalf("save ruleset: %v", err)
	}
	if err := config.SaveLockfile(filepath.Join(projectDir, config.LockFileName), lock); err != nil {
		t.Fatalf("save lock: %v", err)
	}

	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var env jsonEnvelope
	if err := runCmdJSON(t, projectDir, a.newDoctorCmd(), &env); err != nil {
		t.Fatalf("doctor failed: %v", err)
	}

	var out doctorOutput
	if err := json.Unmarshal(env.Result, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(out.Checks) == 0 {
		t.Fatalf("expected checks")
	}
}

func TestProfileCommandsJSON(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	sourceDir := createLocalSourcePack(t, "new content\n")
	savedMeta := createSavedProfile(t, sourceDir, "old content\n")
	oldHash := savedMeta.ContentHash

	projectDir := t.TempDir()
	cfg := config.Ruleset{SpecVersion: "0.1", Name: "proj"}
	if err := config.SaveRuleset(filepath.Join(projectDir, config.RulesetFileName), cfg); err != nil {
		t.Fatalf("save ruleset: %v", err)
	}

	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}

	// profile list
	{
		var env jsonEnvelope
		if err := runCmdJSON(t, projectDir, a.newProfileListCmd(), &env); err != nil {
			t.Fatalf("profile list failed: %v", err)
		}
		var out profileListOutput
		if err := json.Unmarshal(env.Result, &out); err != nil {
			t.Fatalf("unmarshal profile list: %v", err)
		}
		if len(out.Profiles) != 1 {
			t.Fatalf("expected 1 profile, got %d", len(out.Profiles))
		}
	}

	// profile show
	{
		var env jsonEnvelope
		if err := runCmdJSON(t, projectDir, a.newProfileShowCmd(), &env, savedMeta.ID); err != nil {
			t.Fatalf("profile show failed: %v", err)
		}
		var out profileShowOutput
		if err := json.Unmarshal(env.Result, &out); err != nil {
			t.Fatalf("unmarshal profile show: %v", err)
		}
		if out.Profile.ID != savedMeta.ID {
			t.Fatalf("unexpected profile ID: %s", out.Profile.ID)
		}
		if !strings.Contains(out.Path, ".rulepack") {
			t.Fatalf("unexpected profile path: %s", out.Path)
		}
	}

	// profile use
	{
		var env jsonEnvelope
		if err := runCmdJSON(t, projectDir, a.newProfileUseCmd(), &env, savedMeta.ID); err != nil {
			t.Fatalf("profile use failed: %v", err)
		}
		newCfg, err := config.LoadRuleset(filepath.Join(projectDir, config.RulesetFileName))
		if err != nil {
			t.Fatalf("load ruleset: %v", err)
		}
		if len(newCfg.Dependencies) != 1 || newCfg.Dependencies[0].Source != profilesvc.ProfileSource {
			t.Fatalf("profile dependency not written: %#v", newCfg.Dependencies)
		}
	}

	// deps add should still allow composition after profile use
	{
		var env jsonEnvelope
		localPack := createLocalSourcePackWithID(t, "ml.base", "ml rule\n")
		relLocal, err := filepath.Rel(projectDir, localPack)
		if err != nil {
			t.Fatalf("rel local: %v", err)
		}
		if err := runCmdJSON(t, projectDir, a.newDepsAddCmd(), &env, "--local", relLocal, "--export", "default"); err != nil {
			t.Fatalf("deps add local after profile use failed: %v", err)
		}
		newCfg, err := config.LoadRuleset(filepath.Join(projectDir, config.RulesetFileName))
		if err != nil {
			t.Fatalf("load ruleset after deps add: %v", err)
		}
		if len(newCfg.Dependencies) != 2 {
			t.Fatalf("expected profile+local composition, got %#v", newCfg.Dependencies)
		}
		if newCfg.Dependencies[0].Source != profilesvc.ProfileSource || newCfg.Dependencies[1].Source != "local" {
			t.Fatalf("unexpected composed dependencies: %#v", newCfg.Dependencies)
		}
	}

	// profile diff should detect changed module content from source
	{
		var env jsonEnvelope
		if err := runCmdJSON(t, projectDir, a.newProfileDiffCmd(), &env, savedMeta.ID); err != nil {
			t.Fatalf("profile diff failed: %v", err)
		}
		var out profileDiffOutput
		if err := json.Unmarshal(env.Result, &out); err != nil {
			t.Fatalf("unmarshal profile diff: %v", err)
		}
		if len(out.ChangedModules) == 0 || out.ChangedModules[0] != "python.base" {
			t.Fatalf("expected python.base to be changed: %#v", out)
		}
	}

	// profile refresh dry-run should not mutate stored metadata hash
	{
		var env jsonEnvelope
		if err := runCmdJSON(t, projectDir, a.newProfileRefreshCmd(), &env, savedMeta.ID, "--dry-run", "--rule", "python.*"); err != nil {
			t.Fatalf("profile refresh dry-run failed: %v", err)
		}
		var out profileRefreshOutput
		if err := json.Unmarshal(env.Result, &out); err != nil {
			t.Fatalf("unmarshal profile refresh: %v", err)
		}
		if !out.DryRun {
			t.Fatalf("expected dryRun=true")
		}
		metaAfter, _, err := profilesvc.ResolveIDOrAlias(savedMeta.ID)
		if err != nil {
			t.Fatalf("resolve profile: %v", err)
		}
		if metaAfter.ContentHash != oldHash {
			t.Fatalf("dry-run changed content hash: old=%s new=%s", oldHash, metaAfter.ContentHash)
		}
	}
}

func TestLegacyProfileFormatHardFails(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	profileRoot := filepath.Join(homeDir, ".rulepack", "profiles", "legacy123")
	if err := os.MkdirAll(profileRoot, 0o755); err != nil {
		t.Fatalf("mkdir legacy profile: %v", err)
	}
	legacyMeta := `{
  "id": "legacy123",
  "alias": "legacy",
  "sourceType": "local",
  "sourceRef": "/tmp/old",
  "sourceExport": "default",
  "createdAt": "2026-01-01T00:00:00Z",
  "contentHash": "deadbeef",
  "moduleCount": 1
}`
	if err := os.WriteFile(filepath.Join(profileRoot, "profile.json"), []byte(legacyMeta), 0o644); err != nil {
		t.Fatalf("write legacy profile.json: %v", err)
	}

	projectDir := t.TempDir()
	cfg := config.Ruleset{SpecVersion: "0.1", Name: "proj"}
	if err := config.SaveRuleset(filepath.Join(projectDir, config.RulesetFileName), cfg); err != nil {
		t.Fatalf("save ruleset: %v", err)
	}
	a := &app{renderer: cliout.NewJSONRenderer(), jsonMode: true}
	var env jsonEnvelope
	for _, run := range []func() error{
		func() error { return runCmdJSON(t, projectDir, a.newProfileShowCmd(), &env, "legacy123") },
		func() error { return runCmdJSON(t, projectDir, a.newProfileShowCmd(), &env, "legacy") },
		func() error { return runCmdJSON(t, projectDir, a.newProfileUseCmd(), &env, "legacy123") },
		func() error { return runCmdJSON(t, projectDir, a.newProfileDiffCmd(), &env, "legacy123") },
		func() error {
			return runCmdJSON(t, projectDir, a.newProfileRefreshCmd(), &env, "legacy123", "--dry-run")
		},
	} {
		err := run()
		if err == nil {
			t.Fatalf("expected legacy profile to fail")
		}
		if !strings.Contains(err.Error(), "unsupported profile format: missing sources; re-save profile with current CLI") {
			t.Fatalf("unexpected legacy error: %v", err)
		}
	}
}

func runCmdJSON(t *testing.T, dir string, cmdRunner *cobra.Command, out any, args ...string) error {
	t.Helper()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	cmdRunner.SetArgs(args)
	bytes, err := captureStdout(func() error {
		return cmdRunner.Execute()
	})
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, out)
}

func captureStdout(fn func() error) ([]byte, error) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = oldStdout
	out, readErr := io.ReadAll(r)
	_ = r.Close()
	if readErr != nil {
		return nil, readErr
	}
	return out, runErr
}

func createLocalSourcePack(t *testing.T, moduleContent string) string {
	return createLocalSourcePackWithID(t, "python.base", moduleContent)
}

func createLocalSourcePackWithID(t *testing.T, moduleID string, moduleContent string) string {
	t.Helper()
	root := t.TempDir()
	moduleName := strings.ReplaceAll(moduleID, ".", "_")
	modulePath := filepath.Join(root, "modules", moduleName+".md")
	if err := os.MkdirAll(filepath.Dir(modulePath), 0o755); err != nil {
		t.Fatalf("mkdir source modules: %v", err)
	}
	if err := os.WriteFile(modulePath, []byte(moduleContent), 0o644); err != nil {
		t.Fatalf("write source module: %v", err)
	}
	rulepackContent := `{
  "specVersion": "0.1",
  "name": "source-pack",
  "version": "1.0.0",
  "modules": [
    { "id": "` + moduleID + `", "path": "modules/` + moduleName + `.md", "priority": 100 }
  ],
  "exports": {
    "default": {
      "include": ["` + moduleID + `"]
    }
  }
}`
	if err := os.WriteFile(filepath.Join(root, "rulepack.json"), []byte(rulepackContent), 0o644); err != nil {
		t.Fatalf("write source rulepack: %v", err)
	}
	return root
}

func createSavedProfile(t *testing.T, sourceDir string, snapshotContent string) profilesvc.Metadata {
	t.Helper()
	modules := []pack.Module{
		{
			PackName:    "source-pack",
			PackVersion: "1.0.0",
			Commit:      "local",
			ID:          "python.base",
			Priority:    100,
			Content:     snapshotContent,
		},
	}
	hash := profilesvc.ComputeContentHash(modules, "default")
	meta, err := profilesvc.SaveSnapshot(profilesvc.SaveInput{
		Alias: "python-a",
		Sources: []profilesvc.SourceSnapshot{{
			SourceType:   "local",
			SourceRef:    sourceDir,
			SourceExport: "default",
			ModuleIDs:    []string{"python.base"},
			Provenance:   map[string]string{"path": sourceDir},
		}},
		ContentHash: hash,
		Modules:     modules,
	})
	if err != nil {
		t.Fatalf("save profile snapshot: %v", err)
	}
	return meta
}

func createGitRepoWithTwoCommits(t *testing.T) (string, string, string, error) {
	t.Helper()
	repo := t.TempDir()
	if _, err := runGit(repo, "init"); err != nil {
		return "", "", "", err
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("v1\n"), 0o644); err != nil {
		return "", "", "", err
	}
	if _, err := runGit(repo, "add", "."); err != nil {
		return "", "", "", err
	}
	if _, err := runGit(repo, "-c", "user.email=test@example.com", "-c", "user.name=rulepack-test", "commit", "-m", "init"); err != nil {
		return "", "", "", err
	}
	oldCommit, err := runGit(repo, "rev-parse", "HEAD")
	if err != nil {
		return "", "", "", err
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("v2\n"), 0o644); err != nil {
		return "", "", "", err
	}
	if _, err := runGit(repo, "add", "."); err != nil {
		return "", "", "", err
	}
	if _, err := runGit(repo, "-c", "user.email=test@example.com", "-c", "user.name=rulepack-test", "commit", "-m", "update"); err != nil {
		return "", "", "", err
	}
	newCommit, err := runGit(repo, "rev-parse", "HEAD")
	if err != nil {
		return "", "", "", err
	}
	return repo, strings.TrimSpace(oldCommit), strings.TrimSpace(newCommit), nil
}

func runGit(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
