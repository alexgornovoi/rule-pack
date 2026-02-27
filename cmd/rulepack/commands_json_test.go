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
	if err := runCmdJSON(t, projectDir, a.newOutdatedCmd(), &env); err != nil {
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
	t.Helper()
	root := t.TempDir()
	modulePath := filepath.Join(root, "modules", "python.md")
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
    { "id": "python.base", "path": "modules/python.md", "priority": 100 }
  ],
  "exports": {
    "default": {
      "include": ["python.*"]
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
		Alias:        "python-a",
		SourceType:   "local",
		SourceRef:    sourceDir,
		SourceExport: "default",
		ContentHash:  hash,
		Modules:      modules,
		Provenance:   map[string]string{"path": sourceDir},
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
