package main

import (
	"fmt"
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
	var yes bool
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
				return fmt.Errorf("lockfile mismatch: run rulepack deps install")
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
						return fmt.Errorf("local dependency changed; run rulepack deps install")
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
						return fmt.Errorf("profile snapshot drift detected; run rulepack deps install")
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
			warnings := make([]string, 0)
			unmanagedCollisions := make([]string, 0)
			for _, t := range targets {
				entry, ok := cfg.Targets[t]
				if !ok {
					return fmt.Errorf("target %q not configured", t)
				}
				switch t {
				case "cursor":
					collisions, err := render.CursorUnmanagedOverwrites(entry, modules)
					if err != nil {
						return err
					}
					for _, path := range collisions {
						unmanagedCollisions = append(unmanagedCollisions, path)
						warnings = append(warnings, fmt.Sprintf("cursor output will overwrite existing non-rulepack file: %s", path))
					}
				default:
					continue
				}
			}
			if err := confirmRiskAction(
				cmd,
				a.jsonMode,
				yes,
				len(unmanagedCollisions) > 0,
				fmt.Sprintf("build detected %d unmanaged cursor overwrite collision(s)", len(unmanagedCollisions)),
				fmt.Sprintf("Build will overwrite %d existing non-rulepack cursor file(s). Continue?", len(unmanagedCollisions)),
				unmanagedCollisions,
				"build",
			); err != nil {
				return err
			}
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

			out := buildOutput{ModuleCount: len(modules), Targets: targetRows, Warnings: warnings}
			if a.jsonMode {
				return a.renderer.RenderJSON("build", out)
			}
			rows := make([][]string, 0, len(targetRows))
			for _, r := range targetRows {
				rows = append(rows, []string{r.Target, r.Output, r.Status})
			}
			events := make([]cliout.Event, 0, len(warnings))
			for _, warning := range warnings {
				events = append(events, cliout.Event{Level: "warn", Message: warning})
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "build",
				Title:   "Build Outputs",
				Tables:  []cliout.Table{{Title: "Build Targets", Columns: []string{"Target", "Output", "Status"}, Rows: rows}},
				Events:  events,
				Summary: map[string]string{"moduleCount": strconv.Itoa(len(modules)), "duplicates": "none", "overrides": strconv.Itoa(len(cfg.Overrides))},
				Done:    "Build complete",
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "all", "target: cursor|copilot|codex|all")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm risky overwrites without prompting")
	return cmd
}
