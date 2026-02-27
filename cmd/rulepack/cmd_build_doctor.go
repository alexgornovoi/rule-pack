package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
	"rulepack/internal/build"
	"rulepack/internal/cliout"
	"rulepack/internal/config"
	"rulepack/internal/git"
	"rulepack/internal/pack"
	profilesvc "rulepack/internal/profile"
	"rulepack/internal/render"
)

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
