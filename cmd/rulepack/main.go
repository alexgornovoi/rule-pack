package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"rulepack/internal/build"
	"rulepack/internal/cliout"
	"rulepack/internal/config"
	"rulepack/internal/git"
	"rulepack/internal/pack"
	profilesvc "rulepack/internal/profile"
	"rulepack/internal/render"
)

type app struct {
	renderer cliout.Renderer
	jsonMode bool
	noColor  bool
}

func main() {
	a := &app{}
	root := &cobra.Command{
		Use:           "rulepack",
		Short:         "Import rule packs and compile target-native rule outputs",
		Long:          "rulepack composes rule packs into target outputs. Use --json for machine-readable output.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if a.jsonMode {
				a.renderer = cliout.NewJSONRenderer()
			} else {
				a.renderer = cliout.NewHumanRenderer(a.noColor)
			}
			return nil
		},
	}

	root.PersistentFlags().BoolVar(&a.jsonMode, "json", false, "emit JSON output")
	root.PersistentFlags().BoolVar(&a.noColor, "no-color", false, "disable color in human output")

	root.AddCommand(a.newInitCmd())
	root.AddCommand(a.newAddCmd())
	root.AddCommand(a.newDepsCmd())
	root.AddCommand(a.newInstallCmd())
	root.AddCommand(a.newOutdatedCmd())
	root.AddCommand(a.newBuildCmd())
	root.AddCommand(a.newDoctorCmd())
	root.AddCommand(a.newProfileCmd())

	if err := root.Execute(); err != nil {
		if a.renderer == nil {
			if a.jsonMode {
				_ = cliout.NewJSONRenderer().RenderJSON("error", map[string]any{"error": map[string]string{"message": err.Error()}})
			} else {
				fmt.Fprintln(os.Stderr, err)
			}
			os.Exit(1)
		}
		a.renderer.RenderError("error", err)
		os.Exit(1)
	}
}

func (a *app) newInitCmd() *cobra.Command {
	var name string
	var template string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a starter rulepack.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := os.Stat(config.RulesetFileName); err == nil {
				return fmt.Errorf("%s already exists", config.RulesetFileName)
			}
			if name == "" {
				cwd, _ := os.Getwd()
				name = filepath.Base(cwd)
			}
			cfg, files, err := initTemplate(name, template)
			if err != nil {
				return err
			}
			if err := writeTemplateFiles(files); err != nil {
				return err
			}
			if err := config.SaveRuleset(config.RulesetFileName, cfg); err != nil {
				return err
			}
			templatePaths := make([]string, 0, len(files))
			rows := make([][]string, 0, len(files))
			for _, f := range files {
				templatePaths = append(templatePaths, f.Path)
				rows = append(rows, []string{f.Path})
			}
			out := initOutput{RulesetFile: config.RulesetFileName, Name: name, TemplateFiles: templatePaths}
			if a.jsonMode {
				return a.renderer.RenderJSON("init", out)
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "init",
				Title:   "Initialize Rulepack",
				Events:  []cliout.Event{{Level: "info", Message: "Created " + config.RulesetFileName}},
				Tables:  []cliout.Table{{Title: "Scaffolded Files", Columns: []string{"Path"}, Rows: rows}},
				Done:    "Initialization complete",
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "rulepack name")
	cmd.Flags().StringVar(&template, "template", "", "init template: rulepack")
	return cmd
}

func (a *app) newAddCmd() *cobra.Command {
	var exportName string
	var version string
	var ref string

	cmd := &cobra.Command{
		Use:   "add <git-url>",
		Short: "Add a dependency to rulepack.json",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if version != "" && ref != "" {
				return errors.New("use only one of --version or --ref")
			}
			cfg, err := config.LoadRuleset(config.RulesetFileName)
			if err != nil {
				return err
			}
			dep := config.Dependency{Source: "git", URI: args[0], Export: exportName, Ref: ref, Version: version}

			action := "added"
			old := config.Dependency{}
			replaced := false
			for i := range cfg.Dependencies {
				if cfg.Dependencies[i].URI == dep.URI {
					old = cfg.Dependencies[i]
					cfg.Dependencies[i] = dep
					action = "replaced"
					replaced = true
					break
				}
			}
			if !replaced {
				cfg.Dependencies = append(cfg.Dependencies, dep)
			}
			if err := config.SaveRuleset(config.RulesetFileName, cfg); err != nil {
				return err
			}
			out := addOutput{RulesetFile: config.RulesetFileName, Action: action, Dependency: dep}
			if a.jsonMode {
				return a.renderer.RenderJSON("add", out)
			}
			diffRows := [][]string{
				{"source", old.Source, dep.Source},
				{"uri", old.URI, dep.URI},
				{"export", old.Export, dep.Export},
				{"version", old.Version, dep.Version},
				{"ref", old.Ref, dep.Ref},
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "add",
				Title:   "Dependency Updated",
				Events:  []cliout.Event{{Level: "info", Message: "Action: " + action}},
				Tables:  []cliout.Table{{Title: "Dependency Diff", Columns: []string{"Field", "Old", "New"}, Rows: diffRows}},
				Done:    "Updated " + config.RulesetFileName,
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&exportName, "export", "", "export name from rulepack")
	cmd.Flags().StringVar(&version, "version", "", "semver range")
	cmd.Flags().StringVar(&ref, "ref", "", "ref (commit/tag/branch)")
	return cmd
}

func (a *app) newDepsCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "deps",
		Short: "Inspect dependency configuration",
	}
	root.AddCommand(a.newDepsListCmd())
	return root
}

func (a *app) newDepsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List dependencies configured in rulepack.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadRuleset(config.RulesetFileName)
			if err != nil {
				return err
			}
			var lock config.Lockfile
			_, lockErr := os.Stat(config.LockFileName)
			if lockErr == nil {
				lock, _ = config.LoadLockfile(config.LockFileName)
			}

			rows := make([]depsListRow, 0, len(cfg.Dependencies))
			for i, dep := range cfg.Dependencies {
				ref := dependencyReference(dep)
				if ref == "" {
					ref = "-"
				}
				locked := ""
				if i < len(lock.Resolved) {
					locked = lockReference(lock.Resolved[i])
				}
				rows = append(rows, depsListRow{
					Index:  i + 1,
					Source: dependencySource(dep),
					Ref:    ref,
					Export: dep.Export,
					Locked: locked,
				})
			}
			out := depsListOutput{Dependencies: rows}
			if a.jsonMode {
				return a.renderer.RenderJSON("deps.list", out)
			}
			tableRows := make([][]string, 0, len(rows))
			for _, r := range rows {
				tableRows = append(tableRows, []string{strconv.Itoa(r.Index), r.Source, r.Ref, r.Export, r.Locked})
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "deps.list",
				Title:   "Dependencies",
				Tables:  []cliout.Table{{Title: "Configured Dependencies", Columns: []string{"#", "Source", "Ref/Path/Profile", "Export", "Locked"}, Rows: tableRows}},
				Done:    "Dependency listing complete",
			})
			return nil
		},
	}
	return cmd
}

func (a *app) newInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Resolve dependencies and write rulepack.lock.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadRuleset(config.RulesetFileName)
			if err != nil {
				return err
			}
			cfgPath, err := filepath.Abs(config.RulesetFileName)
			if err != nil {
				return err
			}
			cfgDir := filepath.Dir(cfgPath)
			gc, err := git.NewClient()
			if err != nil {
				return err
			}
			lock, resolvedRows, counts, err := buildLock(cfg, cfgDir, gc)
			if err != nil {
				return err
			}
			if err := config.SaveLockfile(config.LockFileName, lock); err != nil {
				return err
			}
			out := installOutput{LockFile: config.LockFileName, Resolved: resolvedRows, Counts: counts}
			if a.jsonMode {
				return a.renderer.RenderJSON("install", out)
			}
			rows := make([][]string, 0, len(resolvedRows))
			for _, r := range resolvedRows {
				rows = append(rows, []string{strconv.Itoa(r.Index), r.Source, r.Ref, r.Export, r.Resolved, r.Hash})
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "install",
				Title:   "Install Dependencies",
				Tables: []cliout.Table{{
					Title:   "Resolved Dependencies",
					Columns: []string{"#", "Source", "Ref/Path/Profile", "Export", "Resolved", "Hash/Commit"},
					Rows:    rows,
				}},
				Summary: map[string]string{
					"git":      strconv.Itoa(counts["git"]),
					"local":    strconv.Itoa(counts["local"]),
					"profile":  strconv.Itoa(counts["profile"]),
					"lock file": config.LockFileName,
				},
				Done: "Install complete",
			})
			return nil
		},
	}
	return cmd
}

func (a *app) newOutdatedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "outdated",
		Short: "Check whether dependencies have newer resolvable revisions",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadRuleset(config.RulesetFileName)
			if err != nil {
				return err
			}
			lock, err := config.LoadLockfile(config.LockFileName)
			if err != nil {
				return err
			}
			if len(cfg.Dependencies) != len(lock.Resolved) {
				return fmt.Errorf("lockfile mismatch: run rulepack install")
			}
			gc, err := git.NewClient()
			if err != nil {
				return err
			}

			rows := make([]outdatedEntry, 0, len(cfg.Dependencies))
			outdatedCount := 0
			for i, dep := range cfg.Dependencies {
				locked := lock.Resolved[i]
				source := dependencySource(dep)
				entry := outdatedEntry{
					Index:     i + 1,
					Source:    source,
					Reference: dependencyReference(dep),
				}
				switch source {
				case "git":
					repoDir, err := gc.EnsureRepo(dep.URI)
					if err != nil {
						entry.UpdateStatus = "error"
						entry.Latest = err.Error()
						rows = append(rows, entry)
						continue
					}
					res, err := gc.Resolve(repoDir, dep.Ref, dep.Version)
					if err != nil {
						entry.UpdateStatus = "error"
						entry.Latest = err.Error()
						rows = append(rows, entry)
						continue
					}
					entry.Locked = shortSHA(locked.Commit)
					entry.Latest = shortSHA(res.Commit)
					if locked.Commit != "" && res.Commit != locked.Commit {
						entry.UpdateStatus = "outdated"
						outdatedCount++
					} else {
						entry.UpdateStatus = "up-to-date"
					}
				case "local", profilesvc.ProfileSource:
					entry.Locked = lockReference(locked)
					entry.Latest = "-"
					entry.UpdateStatus = "n/a"
				default:
					entry.UpdateStatus = "unsupported"
				}
				rows = append(rows, entry)
			}

			out := newOutdatedOutput(rows, outdatedCount)
			if a.jsonMode {
				return a.renderer.RenderJSON("outdated", out)
			}
			tableRows := make([][]string, 0, len(rows))
			for _, r := range rows {
				tableRows = append(tableRows, []string{
					strconv.Itoa(r.Index),
					r.Source,
					r.Reference,
					r.Locked,
					r.Latest,
					r.UpdateStatus,
				})
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "outdated",
				Title:   "Dependency Update Check",
				Tables: []cliout.Table{{
					Title:   "Dependency Status",
					Columns: []string{"#", "Source", "Ref/Path/Profile", "Locked", "Latest", "Status"},
					Rows:    tableRows,
				}},
				Summary: map[string]string{
					"outdated": strconv.Itoa(outdatedCount),
					"total":    strconv.Itoa(len(rows)),
				},
				Done: "Outdated check complete",
			})
			return nil
		},
	}
	return cmd
}

func (a *app) newBuildCmd() *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Compile resolved rule packs into target outputs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadRuleset(config.RulesetFileName)
			if err != nil {
				return err
			}
			cfgPath, err := filepath.Abs(config.RulesetFileName)
			if err != nil {
				return err
			}
			cfgDir := filepath.Dir(cfgPath)
			lock, err := config.LoadLockfile(config.LockFileName)
			if err != nil {
				return err
			}
			if len(cfg.Dependencies) != len(lock.Resolved) {
				return fmt.Errorf("lockfile mismatch: run rulepack install")
			}

			gc, err := git.NewClient()
			if err != nil {
				return err
			}

			var modules []pack.Module
			for i, dep := range cfg.Dependencies {
				locked := lock.Resolved[i]
				source := dependencySource(dep)
				lockedSource := lockSource(locked)
				if source != lockedSource {
					return fmt.Errorf("lockfile mismatch at index %d (source %s != %s)", i, source, lockedSource)
				}
				switch source {
				case "git":
					if dep.URI != locked.URI {
						return fmt.Errorf("lockfile mismatch at index %d (%s != %s)", i, dep.URI, locked.URI)
					}
					repoDir, err := gc.EnsureRepo(dep.URI)
					if err != nil {
						return err
					}
					expanded, err := pack.ExpandGitDependency(gc, repoDir, dep, locked)
					if err != nil {
						return err
					}
					modules = append(modules, expanded...)
				case "local":
					absLocalPath, relPath, err := resolveLocalPath(cfgDir, dep.Path)
					if err != nil {
						return err
					}
					if relPath != locked.Path {
						return fmt.Errorf("lockfile mismatch at index %d (%s != %s)", i, relPath, locked.Path)
					}
					expanded, contentHash, err := pack.ExpandLocalDependency(absLocalPath, dep, "local")
					if err != nil {
						return err
					}
					if contentHash != locked.ContentHash {
						return fmt.Errorf("local dependency changed; run rulepack install")
					}
					modules = append(modules, expanded...)
				case "profile":
					depProfile := dep.Profile
					if depProfile == "" {
						depProfile = locked.Profile
					}
					meta, profileDir, err := profilesvc.ResolveIDOrAlias(depProfile)
					if err != nil {
						return err
					}
					if locked.Profile != "" && meta.ID != locked.Profile {
						return fmt.Errorf("lockfile mismatch at index %d (%s != %s)", i, meta.ID, locked.Profile)
					}
					depRead := profileDependencyForRead(dep)
					expanded, contentHash, err := pack.ExpandProfileDependency(profileDir, depRead, profilesvc.ProfileCommit)
					if err != nil {
						return err
					}
					if contentHash != locked.ContentHash {
						return fmt.Errorf("profile snapshot drift detected; run rulepack install")
					}
					modules = append(modules, expanded...)
				default:
					return fmt.Errorf("unsupported source %q", dep.Source)
				}
			}

			modules = build.ApplyOverrides(modules, cfg.Overrides)
			if err := build.CheckDuplicateIDs(modules); err != nil {
				return err
			}
			build.Sort(modules)

			targets := resolveTargets(target)
			targetRows := make([]buildTargetRow, 0, len(targets))
			for _, t := range targets {
				entry, ok := cfg.Targets[t]
				if !ok {
					return fmt.Errorf("target %q not configured", t)
				}
				switch t {
				case "cursor":
					if err := render.WriteCursor(entry, modules); err != nil {
						return err
					}
					targetRows = append(targetRows, buildTargetRow{Target: t, Output: entry.OutDir, Status: "ok"})
				case "copilot":
					if err := render.WriteMerged(entry.OutFile, modules); err != nil {
						return err
					}
					targetRows = append(targetRows, buildTargetRow{Target: t, Output: entry.OutFile, Status: "ok"})
				case "codex":
					if err := render.WriteMerged(entry.OutFile, modules); err != nil {
						return err
					}
					targetRows = append(targetRows, buildTargetRow{Target: t, Output: entry.OutFile, Status: "ok"})
				default:
					return fmt.Errorf("unsupported target %q", t)
				}
			}

			out := buildOutput{ModuleCount: len(modules), Targets: targetRows}
			if a.jsonMode {
				return a.renderer.RenderJSON("build", out)
			}
			rows := make([][]string, 0, len(targetRows))
			for _, r := range targetRows {
				rows = append(rows, []string{r.Target, r.Output, r.Status})
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "build",
				Title:   "Build Outputs",
				Tables:  []cliout.Table{{Title: "Build Targets", Columns: []string{"Target", "Output", "Status"}, Rows: rows}},
				Summary: map[string]string{"moduleCount": strconv.Itoa(len(modules)), "duplicates": "none", "overrides": strconv.Itoa(len(cfg.Overrides))},
				Done:    "Build complete",
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "all", "target: cursor|copilot|codex|all")
	return cmd
}

func (a *app) newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate environment, config, lockfile, and profile store",
		RunE: func(cmd *cobra.Command, args []string) error {
			checks := []doctorCheck{}
			if _, err := os.Stat(config.RulesetFileName); err != nil {
				checks = append(checks, doctorCheck{Name: "ruleset file", Status: "fail", Details: err.Error()})
			} else {
				checks = append(checks, doctorCheck{Name: "ruleset file", Status: "ok"})
			}
			cfg, cfgErr := config.LoadRuleset(config.RulesetFileName)
			if cfgErr != nil {
				checks = append(checks, doctorCheck{Name: "ruleset parse", Status: "fail", Details: cfgErr.Error()})
			} else {
				checks = append(checks, doctorCheck{Name: "ruleset parse", Status: "ok"})
			}
			lock, lockErr := config.LoadLockfile(config.LockFileName)
			if lockErr != nil {
				checks = append(checks, doctorCheck{Name: "lockfile", Status: "warn", Details: lockErr.Error()})
			} else {
				checks = append(checks, doctorCheck{Name: "lockfile", Status: "ok"})
				if cfgErr == nil && len(cfg.Dependencies) != len(lock.Resolved) {
					checks = append(checks, doctorCheck{Name: "lock alignment", Status: "fail", Details: "dependency count differs from lockfile"})
				} else if cfgErr == nil {
					checks = append(checks, doctorCheck{Name: "lock alignment", Status: "ok"})
				}
			}
			profileRoot, pErr := profilesvc.GlobalRoot()
			if pErr != nil {
				checks = append(checks, doctorCheck{Name: "profile store", Status: "fail", Details: pErr.Error()})
			} else {
				if _, err := os.Stat(profileRoot); err == nil {
					checks = append(checks, doctorCheck{Name: "profile store", Status: "ok", Details: profileRoot})
				} else {
					checks = append(checks, doctorCheck{Name: "profile store", Status: "warn", Details: profileRoot + " (not created yet)"})
				}
			}
			_, gErr := git.NewClient()
			if gErr != nil {
				checks = append(checks, doctorCheck{Name: "git client", Status: "fail", Details: gErr.Error()})
			} else {
				checks = append(checks, doctorCheck{Name: "git client", Status: "ok"})
			}

			out := doctorOutput{Checks: checks}
			if a.jsonMode {
				return a.renderer.RenderJSON("doctor", out)
			}
			rows := make([][]string, 0, len(checks))
			for _, c := range checks {
				rows = append(rows, []string{c.Name, c.Status, c.Details})
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "doctor",
				Title:   "Diagnostics",
				Tables:  []cliout.Table{{Title: "Checks", Columns: []string{"Check", "Status", "Details"}, Rows: rows}},
				Done:    "Doctor run complete",
			})
			return nil
		},
	}
	return cmd
}

func (a *app) newProfileCmd() *cobra.Command {
	root := &cobra.Command{Use: "profile", Short: "Manage reusable globally saved profiles"}
	root.AddCommand(a.newProfileSaveCmd())
	root.AddCommand(a.newProfileListCmd())
	root.AddCommand(a.newProfileShowCmd())
	root.AddCommand(a.newProfileUseCmd())
	root.AddCommand(a.newProfileDiffCmd())
	root.AddCommand(a.newProfileRefreshCmd())
	return root
}

func (a *app) newProfileSaveCmd() *cobra.Command {
	var depSelector string
	var alias string
	var switchDependency bool
	cmd := &cobra.Command{
		Use:   "save",
		Short: "Save a resolved dependency as a globally reusable profile snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadRuleset(config.RulesetFileName)
			if err != nil {
				return err
			}
			lock, err := config.LoadLockfile(config.LockFileName)
			if err != nil {
				return err
			}
			if len(cfg.Dependencies) != len(lock.Resolved) {
				return errors.New("cannot save profile: dependency not installed; run rulepack install")
			}
			idx, err := findDependencyIndex(cfg, depSelector)
			if err != nil {
				return err
			}
			cfgPath, err := filepath.Abs(config.RulesetFileName)
			if err != nil {
				return err
			}
			cfgDir := filepath.Dir(cfgPath)
			gc, err := git.NewClient()
			if err != nil {
				return err
			}

			dep := cfg.Dependencies[idx]
			locked := lock.Resolved[idx]
			modules, contentHash, sourceRef, provenance, err := expandDependencyForSnapshot(cfgDir, gc, dep, locked)
			if err != nil {
				return err
			}

			meta, err := profilesvc.SaveSnapshot(profilesvc.SaveInput{
				Alias:        alias,
				SourceType:   dependencySource(dep),
				SourceRef:    sourceRef,
				SourceExport: dep.Export,
				ContentHash:  contentHash,
				Modules:      modules,
				Provenance:   provenance,
			})
			if err != nil {
				return err
			}

			if switchDependency {
				cfg.Dependencies[idx] = config.Dependency{Source: profilesvc.ProfileSource, Profile: meta.ID, Export: "default"}
				if err := config.SaveRuleset(config.RulesetFileName, cfg); err != nil {
					return err
				}
				newLock, _, _, err := buildLock(cfg, cfgDir, gc)
				if err != nil {
					return err
				}
				if err := config.SaveLockfile(config.LockFileName, newLock); err != nil {
					return err
				}
			}

			out := profileSaveOutput{Profile: meta, Switched: switchDependency, DependencyIndex: idx}
			if a.jsonMode {
				return a.renderer.RenderJSON("profile.save", out)
			}
			rows := [][]string{{meta.ID, meta.Alias, meta.SourceRef, meta.SourceExport, strconv.Itoa(meta.ModuleCount), shortSHA(meta.ContentHash)}}
			events := []cliout.Event{}
			if switchDependency {
				events = append(events, cliout.Event{Level: "info", Message: "Switched dependency to profile source and refreshed lockfile"})
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "profile.save",
				Title:   "Profile Saved",
				Events:  events,
				Tables:  []cliout.Table{{Title: "Snapshot", Columns: []string{"Profile ID", "Alias", "Source", "Export", "Modules", "Content Hash"}, Rows: rows}},
				Done:    "Profile save complete",
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&depSelector, "dep", "", "dependency selector (index or source ref)")
	cmd.Flags().StringVar(&alias, "alias", "", "optional human-readable alias")
	cmd.Flags().BoolVar(&switchDependency, "switch", true, "switch selected dependency to saved profile source")
	_ = cmd.MarkFlagRequired("dep")
	return cmd
}

func (a *app) newProfileListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List globally saved profiles in a table",
		RunE: func(cmd *cobra.Command, args []string) error {
			profiles, err := profilesvc.List()
			if err != nil {
				return err
			}
			out := profileListOutput{Profiles: profiles}
			if a.jsonMode {
				return a.renderer.RenderJSON("profile.list", out)
			}
			rows := make([][]string, 0, len(profiles))
			for _, p := range profiles {
				alias := p.Alias
				if alias == "" {
					alias = "-"
				}
				rows = append(rows, []string{p.ID, alias, p.SourceRef, p.SourceExport, strconv.Itoa(p.ModuleCount), p.CreatedAt})
			}
			events := []cliout.Event{}
			if len(profiles) == 0 {
				events = append(events, cliout.Event{Level: "info", Message: "No saved profiles"})
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "profile.list",
				Title:   "Saved Profiles",
				Events:  events,
				Tables:  []cliout.Table{{Title: "Profiles", Columns: []string{"Profile ID", "Alias", "Source", "Export", "Modules", "Created"}, Rows: rows}},
				Done:    "List complete",
			})
			return nil
		},
	}
	return cmd
}

func (a *app) newProfileShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <profile-id-or-alias>",
		Short: "Show details for a saved profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			meta, path, err := profilesvc.ResolveIDOrAlias(args[0])
			if err != nil {
				return err
			}
			out := profileShowOutput{Profile: meta, Path: path}
			if a.jsonMode {
				return a.renderer.RenderJSON("profile.show", out)
			}
			rows := [][]string{
				{"id", meta.ID},
				{"alias", meta.Alias},
				{"sourceType", meta.SourceType},
				{"sourceRef", meta.SourceRef},
				{"sourceExport", meta.SourceExport},
				{"createdAt", meta.CreatedAt},
				{"contentHash", shortSHA(meta.ContentHash)},
				{"moduleCount", strconv.Itoa(meta.ModuleCount)},
				{"path", path},
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "profile.show",
				Title:   "Profile Details",
				Tables:  []cliout.Table{{Title: "Profile", Columns: []string{"Field", "Value"}, Rows: rows}},
				Done:    "Profile details shown",
			})
			return nil
		},
	}
	return cmd
}

func (a *app) newProfileUseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "use <profile-id-or-alias>",
		Short: "Add/update dependency to use a saved global profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			meta, _, err := profilesvc.ResolveIDOrAlias(args[0])
			if err != nil {
				return err
			}
			cfg, err := config.LoadRuleset(config.RulesetFileName)
			if err != nil {
				return err
			}
			dep := config.Dependency{Source: profilesvc.ProfileSource, Profile: meta.ID, Export: "default"}
			action := "added"
			updated := false
			for i := range cfg.Dependencies {
				if dependencySource(cfg.Dependencies[i]) == profilesvc.ProfileSource && cfg.Dependencies[i].Profile == meta.ID {
					cfg.Dependencies[i] = dep
					updated = true
					action = "updated"
					break
				}
			}
			if !updated {
				cfg.Dependencies = append(cfg.Dependencies, dep)
			}
			if err := config.SaveRuleset(config.RulesetFileName, cfg); err != nil {
				return err
			}
			out := profileUseOutput{ProfileID: meta.ID, Action: action, RulesetFile: config.RulesetFileName}
			if a.jsonMode {
				return a.renderer.RenderJSON("profile.use", out)
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "profile.use",
				Title:   "Profile Applied",
				Events:  []cliout.Event{{Level: "info", Message: "Action: " + action}, {Level: "info", Message: "Profile: " + meta.ID}},
				Done:    "Updated " + config.RulesetFileName,
			})
			return nil
		},
	}
	return cmd
}

func (a *app) newProfileDiffCmd() *cobra.Command {
	var rules []string
	cmd := &cobra.Command{
		Use:   "diff <profile-id-or-alias>",
		Short: "Compare a saved profile snapshot with its current source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			meta, profileDir, err := profilesvc.ResolveIDOrAlias(args[0])
			if err != nil {
				return err
			}
			gc, err := git.NewClient()
			if err != nil {
				return err
			}
			sourceDep, err := dependencyFromProfileMetadata(meta)
			if err != nil {
				return err
			}
			freshModules, err := resolveModulesForDependency(gc, sourceDep)
			if err != nil {
				return err
			}
			currentModules, _, err := pack.ExpandProfileDependency(profileDir, profileDependencyForRead(config.Dependency{Source: profilesvc.ProfileSource, Profile: meta.ID, Export: "default"}), profilesvc.ProfileCommit)
			if err != nil {
				return err
			}
			if len(rules) > 0 {
				currentModules = filterModulesByPatterns(currentModules, rules)
				freshModules = filterModulesByPatterns(freshModules, rules)
			}

			changed, added, removed := diffModules(currentModules, freshModules)
			currentHash := profilesvc.ComputeContentHash(currentModules, meta.SourceExport)
			freshHash := profilesvc.ComputeContentHash(freshModules, meta.SourceExport)
			out := newProfileDiffOutput(meta.ID, meta.SourceType, meta.SourceRef, currentHash, freshHash, changed, added, removed, rules)
			if a.jsonMode {
				return a.renderer.RenderJSON("profile.diff", out)
			}

			diffRows := make([][]string, 0, len(changed)+len(added)+len(removed))
			for _, id := range changed {
				diffRows = append(diffRows, []string{"changed", id})
			}
			for _, id := range added {
				diffRows = append(diffRows, []string{"added", id})
			}
			for _, id := range removed {
				diffRows = append(diffRows, []string{"removed", id})
			}
			events := []cliout.Event{}
			if len(rules) > 0 {
				events = append(events, cliout.Event{Level: "info", Message: "Filtered by selectors: " + strings.Join(rules, ", ")})
			}
			if len(diffRows) == 0 {
				events = append(events, cliout.Event{Level: "info", Message: "No differences found"})
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "profile.diff",
				Title:   "Profile Diff",
				Events:  events,
				Tables:  []cliout.Table{{Title: "Module Changes", Columns: []string{"Type", "Module ID"}, Rows: diffRows}},
				Summary: map[string]string{
					"profile":     meta.ID,
					"source":      meta.SourceRef,
					"currentHash": shortSHA(currentHash),
					"freshHash":   shortSHA(freshHash),
				},
				Done: "Profile diff complete",
			})
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&rules, "rule", nil, "diff only specific module IDs/patterns")
	return cmd
}

func (a *app) newProfileRefreshCmd() *cobra.Command {
	var newID bool
	var rules []string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "refresh <profile-id-or-alias>",
		Short: "Refresh a saved profile from its original source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			meta, profileDir, err := profilesvc.ResolveIDOrAlias(args[0])
			if err != nil {
				return err
			}
			gc, err := git.NewClient()
			if err != nil {
				return err
			}
			sourceDep, err := dependencyFromProfileMetadata(meta)
			if err != nil {
				return err
			}
			freshModules, err := resolveModulesForDependency(gc, sourceDep)
			if err != nil {
				return err
			}
			oldModules, _, err := pack.ExpandProfileDependency(profileDir, profileDependencyForRead(config.Dependency{Source: profilesvc.ProfileSource, Profile: meta.ID, Export: "default"}), profilesvc.ProfileCommit)
			if err != nil {
				return err
			}

			mergedModules, refreshedIDs, err := mergeRefreshedModules(oldModules, freshModules, rules)
			if err != nil {
				return err
			}
			newHash := profilesvc.ComputeContentHash(mergedModules, meta.SourceExport)
			saveID := ""
			if !newID {
				saveID = meta.ID
			}
			saved := meta
			saved.ContentHash = newHash
			saved.ModuleCount = len(mergedModules)
			if dryRun {
				if newID {
					saved.ID = "dry-run:new-id"
				}
			} else {
				alias := meta.Alias
				saved, err = profilesvc.SaveSnapshot(profilesvc.SaveInput{
					ID:           saveID,
					Alias:        alias,
					SourceType:   meta.SourceType,
					SourceRef:    meta.SourceRef,
					SourceExport: meta.SourceExport,
					ContentHash:  newHash,
					Modules:      mergedModules,
					Provenance:   meta.Provenance,
				})
				if err != nil {
					return err
				}
			}

			out := profileRefreshOutput{
				OldProfileID:  meta.ID,
				NewProfileID:  saved.ID,
				RefreshedRule: refreshedIDs,
				Source:        meta.SourceRef,
				InPlace:       !newID,
				DryRun:        dryRun,
			}
			if a.jsonMode {
				return a.renderer.RenderJSON("profile.refresh", out)
			}
			rows := [][]string{{meta.ID, saved.ID, boolToYesNo(!newID), meta.SourceRef}}
			ruleRows := make([][]string, 0, len(refreshedIDs))
			for _, id := range refreshedIDs {
				ruleRows = append(ruleRows, []string{id})
			}
			tables := []cliout.Table{{Title: "Refresh Result", Columns: []string{"Old Profile", "New Profile", "In Place", "Source"}, Rows: rows}}
			if len(ruleRows) > 0 {
				tables = append(tables, cliout.Table{Title: "Refreshed Rules", Columns: []string{"Module ID"}, Rows: ruleRows})
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "profile.refresh",
				Title:   "Profile Refreshed",
				Events:  []cliout.Event{{Level: "info", Message: dryRunMessage(dryRun)}},
				Tables:  tables,
				Done:    "Profile refresh complete",
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&newID, "new-id", false, "create a new profile ID instead of updating in place")
	cmd.Flags().StringArrayVar(&rules, "rule", nil, "refresh only specific module IDs/patterns")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview refresh result without writing profile files")
	return cmd
}

func resolveTargets(target string) []string {
	target = strings.ToLower(target)
	if target == "" || target == "all" {
		return []string{"cursor", "copilot", "codex"}
	}
	return []string{target}
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func dryRunMessage(dryRun bool) string {
	if dryRun {
		return "Dry run only; no profile files were written"
	}
	return "Profile files updated"
}

func dependencySource(dep config.Dependency) string {
	if dep.Source == "" {
		return "git"
	}
	return dep.Source
}

func lockSource(locked config.LockedSource) string {
	if locked.Source == "" {
		return "git"
	}
	return locked.Source
}

func lockReference(locked config.LockedSource) string {
	switch lockSource(locked) {
	case "git":
		if locked.Commit != "" {
			return shortSHA(locked.Commit)
		}
		return "-"
	case "local", profilesvc.ProfileSource:
		if locked.ContentHash != "" {
			return shortSHA(locked.ContentHash)
		}
		if locked.Commit != "" {
			return shortSHA(locked.Commit)
		}
		return "-"
	default:
		if locked.Commit != "" {
			return shortSHA(locked.Commit)
		}
		return "-"
	}
}

func resolveLocalPath(cfgDir string, depPath string) (string, string, error) {
	if depPath == "" {
		return "", "", errors.New("local source requires path")
	}
	absPath := depPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(cfgDir, depPath)
	}
	absPath = filepath.Clean(absPath)
	info, err := os.Stat(absPath)
	if err != nil {
		return "", "", fmt.Errorf("local dependency path %q: %w", depPath, err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("local dependency path %q is not a directory", depPath)
	}
	if _, err := os.Stat(filepath.Join(absPath, config.RulesetFileName)); err != nil {
		return "", "", fmt.Errorf("local dependency missing %s at %s", config.RulesetFileName, absPath)
	}
	relPath, err := filepath.Rel(cfgDir, absPath)
	if err != nil {
		return "", "", err
	}
	relPath = filepath.ToSlash(relPath)
	if relPath == "" {
		relPath = "."
	}
	return absPath, relPath, nil
}

func profileDependencyForRead(dep config.Dependency) config.Dependency {
	out := dep
	if out.Export == "" {
		out.Export = "default"
	}
	return out
}

func buildLock(cfg config.Ruleset, cfgDir string, gc *git.Client) (config.Lockfile, []installResolvedRow, map[string]int, error) {
	lock := config.Lockfile{LockVersion: "0.1"}
	rows := make([]installResolvedRow, 0, len(cfg.Dependencies))
	counts := map[string]int{"git": 0, "local": 0, "profile": 0}
	for idx, dep := range cfg.Dependencies {
		source := dependencySource(dep)
		switch source {
		case "git":
			repoDir, err := gc.EnsureRepo(dep.URI)
			if err != nil {
				return lock, nil, nil, fmt.Errorf("prepare %s: %w", dep.URI, err)
			}
			res, err := gc.Resolve(repoDir, dep.Ref, dep.Version)
			if err != nil {
				return lock, nil, nil, fmt.Errorf("resolve %s: %w", dep.URI, err)
			}
			if _, err := pack.ExpandGitDependency(gc, repoDir, dep, config.LockedSource{Source: "git", URI: dep.URI, Commit: res.Commit, Export: dep.Export}); err != nil {
				return lock, nil, nil, err
			}
			lock.Resolved = append(lock.Resolved, config.LockedSource{Source: "git", URI: dep.URI, Requested: res.Requested, ResolvedVersion: res.ResolvedVersion, Commit: res.Commit, Export: dep.Export})
			rows = append(rows, installResolvedRow{Index: idx + 1, Source: "git", Ref: dep.URI, Export: dep.Export, Resolved: res.Requested, Hash: shortSHA(res.Commit)})
			counts["git"]++
		case "local":
			absLocalPath, relPath, err := resolveLocalPath(cfgDir, dep.Path)
			if err != nil {
				return lock, nil, nil, err
			}
			_, contentHash, err := pack.ExpandLocalDependency(absLocalPath, dep, "local")
			if err != nil {
				return lock, nil, nil, err
			}
			lock.Resolved = append(lock.Resolved, config.LockedSource{Source: "local", Path: relPath, Commit: "local", ContentHash: contentHash, Export: dep.Export})
			rows = append(rows, installResolvedRow{Index: idx + 1, Source: "local", Ref: relPath, Export: dep.Export, Resolved: "local", Hash: shortSHA(contentHash)})
			counts["local"]++
		case profilesvc.ProfileSource:
			if dep.Profile == "" {
				return lock, nil, nil, errors.New("profile source requires profile id")
			}
			meta, profileDir, err := profilesvc.ResolveIDOrAlias(dep.Profile)
			if err != nil {
				return lock, nil, nil, err
			}
			depRead := profileDependencyForRead(dep)
			_, contentHash, err := pack.ExpandProfileDependency(profileDir, depRead, profilesvc.ProfileCommit)
			if err != nil {
				return lock, nil, nil, err
			}
			lock.Resolved = append(lock.Resolved, config.LockedSource{Source: profilesvc.ProfileSource, Profile: meta.ID, Commit: profilesvc.ProfileCommit, ContentHash: contentHash, Export: depRead.Export})
			rows = append(rows, installResolvedRow{Index: idx + 1, Source: "profile", Ref: meta.ID, Export: depRead.Export, Resolved: "profile", Hash: shortSHA(contentHash)})
			counts["profile"]++
		default:
			return lock, nil, nil, fmt.Errorf("unsupported source %q", dep.Source)
		}
	}
	return lock, rows, counts, nil
}

func expandDependencyForSnapshot(cfgDir string, gc *git.Client, dep config.Dependency, locked config.LockedSource) ([]pack.Module, string, string, map[string]string, error) {
	source := dependencySource(dep)
	if source != lockSource(locked) {
		return nil, "", "", nil, errors.New("cannot save profile: dependency not installed; run rulepack install")
	}
	switch source {
	case "git":
		if dep.URI != locked.URI {
			return nil, "", "", nil, errors.New("cannot save profile: dependency not installed; run rulepack install")
		}
		repoDir, err := gc.EnsureRepo(dep.URI)
		if err != nil {
			return nil, "", "", nil, err
		}
		modules, err := pack.ExpandGitDependency(gc, repoDir, dep, locked)
		if err != nil {
			return nil, "", "", nil, err
		}
		hash := profilesvc.ComputeContentHash(modules, dep.Export)
		requestType := "head"
		if dep.Version != "" {
			requestType = "version"
		} else if dep.Ref != "" {
			requestType = "ref"
		}
		prov := map[string]string{
			"commit":          locked.Commit,
			"requested":       locked.Requested,
			"resolvedVersion": locked.ResolvedVersion,
			"requestType":     requestType,
		}
		return modules, hash, dep.URI, prov, nil
	case "local":
		absLocalPath, relPath, err := resolveLocalPath(cfgDir, dep.Path)
		if err != nil {
			return nil, "", "", nil, err
		}
		if locked.Path != "" && relPath != locked.Path {
			return nil, "", "", nil, errors.New("cannot save profile: dependency not installed; run rulepack install")
		}
		modules, hash, err := pack.ExpandLocalDependency(absLocalPath, dep, "local")
		if err != nil {
			return nil, "", "", nil, err
		}
		if locked.ContentHash != "" && hash != locked.ContentHash {
			return nil, "", "", nil, errors.New("cannot save profile: dependency not installed; run rulepack install")
		}
		prov := map[string]string{"path": relPath, "contentHash": hash}
		return modules, hash, absLocalPath, prov, nil
	case profilesvc.ProfileSource:
		profileRef := dep.Profile
		if profileRef == "" {
			profileRef = locked.Profile
		}
		meta, profileDir, err := profilesvc.ResolveIDOrAlias(profileRef)
		if err != nil {
			return nil, "", "", nil, err
		}
		if locked.Profile != "" && meta.ID != locked.Profile {
			return nil, "", "", nil, errors.New("cannot save profile: dependency not installed; run rulepack install")
		}
		depRead := profileDependencyForRead(dep)
		modules, hash, err := pack.ExpandProfileDependency(profileDir, depRead, profilesvc.ProfileCommit)
		if err != nil {
			return nil, "", "", nil, err
		}
		if locked.ContentHash != "" && hash != locked.ContentHash {
			return nil, "", "", nil, errors.New("cannot save profile: dependency not installed; run rulepack install")
		}
		prov := map[string]string{"profile": meta.ID, "contentHash": hash}
		return modules, hash, meta.ID, prov, nil
	default:
		return nil, "", "", nil, fmt.Errorf("unsupported source %q", dep.Source)
	}
}

func findDependencyIndex(cfg config.Ruleset, selector string) (int, error) {
	if selector == "" {
		return -1, errors.New("missing --dep selector")
	}
	if n, err := strconv.Atoi(selector); err == nil {
		if n >= 1 && n <= len(cfg.Dependencies) {
			return n - 1, nil
		}
		if n >= 0 && n < len(cfg.Dependencies) {
			return n, nil
		}
		return -1, fmt.Errorf("dependency index %d out of range", n)
	}
	index := -1
	for i, dep := range cfg.Dependencies {
		if dependencyReference(dep) == selector {
			if index != -1 {
				return -1, fmt.Errorf("selector %q matched multiple dependencies", selector)
			}
			index = i
		}
	}
	if index == -1 {
		return -1, fmt.Errorf("dependency selector %q not found", selector)
	}
	return index, nil
}

func dependencyReference(dep config.Dependency) string {
	switch dependencySource(dep) {
	case "git":
		return dep.URI
	case "local":
		return dep.Path
	case profilesvc.ProfileSource:
		return dep.Profile
	default:
		return ""
	}
}

func dependencyFromProfileMetadata(meta profilesvc.Metadata) (config.Dependency, error) {
	dep := config.Dependency{Source: meta.SourceType, Export: meta.SourceExport}
	switch meta.SourceType {
	case "git":
		dep.URI = meta.SourceRef
		requested := meta.Provenance["requested"]
		switch meta.Provenance["requestType"] {
		case "version":
			dep.Version = requested
		case "ref":
			dep.Ref = requested
		default:
			// Backward compatibility: old snapshots may not carry requestType.
			if requested != "" && requested != "HEAD" {
				dep.Ref = requested
			}
		}
	case "local":
		if !filepath.IsAbs(meta.SourceRef) {
			return config.Dependency{}, fmt.Errorf("profile %s local source is not absolute; cannot refresh safely", meta.ID)
		}
		dep.Path = meta.SourceRef
	case profilesvc.ProfileSource:
		dep.Profile = meta.SourceRef
	default:
		return config.Dependency{}, fmt.Errorf("unsupported profile source type %q", meta.SourceType)
	}
	return dep, nil
}

func resolveModulesForDependency(gc *git.Client, dep config.Dependency) ([]pack.Module, error) {
	switch dependencySource(dep) {
	case "git":
		repoDir, err := gc.EnsureRepo(dep.URI)
		if err != nil {
			return nil, err
		}
		res, err := gc.Resolve(repoDir, dep.Ref, dep.Version)
		if err != nil {
			return nil, err
		}
		return pack.ExpandGitDependency(gc, repoDir, dep, config.LockedSource{Source: "git", URI: dep.URI, Commit: res.Commit, Export: dep.Export})
	case "local":
		absPath := filepath.Clean(dep.Path)
		returnModules, _, err := pack.ExpandLocalDependency(absPath, dep, "local")
		return returnModules, err
	case profilesvc.ProfileSource:
		meta, profileDir, err := profilesvc.ResolveIDOrAlias(dep.Profile)
		if err != nil {
			return nil, err
		}
		depRead := profileDependencyForRead(config.Dependency{Source: profilesvc.ProfileSource, Profile: meta.ID, Export: "default"})
		mods, _, err := pack.ExpandProfileDependency(profileDir, depRead, profilesvc.ProfileCommit)
		return mods, err
	default:
		return nil, fmt.Errorf("unsupported source %q", dep.Source)
	}
}

func mergeRefreshedModules(current []pack.Module, fresh []pack.Module, rules []string) ([]pack.Module, []string, error) {
	if len(rules) == 0 {
		refreshed := make([]string, 0, len(fresh))
		for _, m := range fresh {
			refreshed = append(refreshed, m.ID)
		}
		build.Sort(fresh)
		return fresh, refreshed, nil
	}

	freshByID := make(map[string]pack.Module, len(fresh))
	for _, m := range fresh {
		freshByID[m.ID] = m
	}
	changed := map[string]struct{}{}
	out := make([]pack.Module, 0, len(current))
	for _, m := range current {
		if moduleMatchesAny(m.ID, rules) {
			newM, ok := freshByID[m.ID]
			if !ok {
				return nil, nil, fmt.Errorf("rule %s not found in refreshed source", m.ID)
			}
			out = append(out, newM)
			changed[m.ID] = struct{}{}
			continue
		}
		out = append(out, m)
	}
	for _, m := range fresh {
		if _, ok := changed[m.ID]; ok {
			continue
		}
		if moduleMatchesAny(m.ID, rules) {
			out = append(out, m)
			changed[m.ID] = struct{}{}
		}
	}
	if len(changed) == 0 {
		return nil, nil, errors.New("no rules matched --rule selectors")
	}
	build.Sort(out)
	refreshed := make([]string, 0, len(changed))
	for id := range changed {
		refreshed = append(refreshed, id)
	}
	buildSortStrings(refreshed)
	return out, refreshed, nil
}

func filterModulesByPatterns(modules []pack.Module, patterns []string) []pack.Module {
	if len(patterns) == 0 {
		return modules
	}
	out := make([]pack.Module, 0, len(modules))
	for _, m := range modules {
		if moduleMatchesAny(m.ID, patterns) {
			out = append(out, m)
		}
	}
	return out
}

func diffModules(current []pack.Module, fresh []pack.Module) ([]string, []string, []string) {
	currentByID := make(map[string]pack.Module, len(current))
	freshByID := make(map[string]pack.Module, len(fresh))
	for _, m := range current {
		currentByID[m.ID] = m
	}
	for _, m := range fresh {
		freshByID[m.ID] = m
	}
	changed := []string{}
	removed := []string{}
	for id, oldMod := range currentByID {
		newMod, ok := freshByID[id]
		if !ok {
			removed = append(removed, id)
			continue
		}
		if moduleDigest(oldMod) != moduleDigest(newMod) {
			changed = append(changed, id)
		}
	}
	added := []string{}
	for id := range freshByID {
		if _, ok := currentByID[id]; !ok {
			added = append(added, id)
		}
	}
	buildSortStrings(changed)
	buildSortStrings(added)
	buildSortStrings(removed)
	return changed, added, removed
}

func moduleDigest(m pack.Module) string {
	applyJSON, _ := json.Marshal(m.Apply)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%s", m.ID, m.Priority, m.Content, string(applyJSON))))
	return hex.EncodeToString(sum[:])
}

func moduleMatchesAny(id string, patterns []string) bool {
	for _, p := range patterns {
		if p == id || p == "*" || p == "**" {
			return true
		}
		matched, err := path.Match(p, id)
		if err == nil && matched {
			return true
		}
		if strings.HasSuffix(p, "*") && strings.HasPrefix(id, strings.TrimSuffix(p, "*")) {
			return true
		}
	}
	return false
}

func buildSortStrings(values []string) {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}

func boolToYesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

type templateFile struct {
	Path    string
	Content string
}

func initTemplate(name string, template string) (config.Ruleset, []templateFile, error) {
	cfg := config.DefaultRuleset(name)
	switch template {
	case "", "default":
		return cfg, nil, nil
	case "rulepack":
		cfg.Dependencies = []config.Dependency{{Source: "local", Path: ".rulepack/packs/rule-authoring", Export: "default"}}
		return cfg, []templateFile{
			{
				Path: ".rulepack/packs/rule-authoring/rulepack.json",
				Content: "{\n" +
					"  \"specVersion\": \"0.1\",\n" +
					"  \"name\": \"rule-authoring\",\n" +
					"  \"version\": \"0.1.0\",\n" +
					"  \"modules\": [\n" +
					"    {\n" +
					"      \"id\": \"authoring.basics\",\n" +
					"      \"path\": \"modules/authoring/basics.md\",\n" +
					"      \"priority\": 100\n" +
					"    },\n" +
					"    {\n" +
					"      \"id\": \"authoring.tests\",\n" +
					"      \"path\": \"modules/authoring/tests.md\",\n" +
					"      \"priority\": 110\n" +
					"    }\n" +
					"  ],\n" +
					"  \"exports\": {\n" +
					"    \"default\": {\n" +
					"      \"include\": [\"authoring.*\"]\n" +
					"    }\n" +
					"  }\n" +
					"}\n",
			},
			{Path: ".rulepack/packs/rule-authoring/modules/authoring/basics.md", Content: "# Rule Authoring Basics\n\n- Keep each rule scoped to one behavior.\n- Prefer examples that show correct and incorrect usage.\n- Write rules as actionable constraints, not abstract advice.\n"},
			{Path: ".rulepack/packs/rule-authoring/modules/authoring/tests.md", Content: "# Rule Authoring Testability\n\n- Add at least one acceptance criterion for each rule module.\n- Validate generated outputs in CI with deterministic checks.\n- Fail builds when local rule dependencies drift without reinstall.\n"},
		}, nil
	default:
		return config.Ruleset{}, nil, fmt.Errorf("unknown template %q (supported: rulepack)", template)
	}
}

func writeTemplateFiles(files []templateFile) error {
	for _, file := range files {
		if _, err := os.Stat(file.Path); err == nil {
			return fmt.Errorf("template file already exists: %s", file.Path)
		}
	}
	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file.Path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file.Path, []byte(file.Content), 0o644); err != nil {
			return err
		}
	}
	return nil
}
