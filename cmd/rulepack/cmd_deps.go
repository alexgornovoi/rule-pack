package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
	"rulepack/internal/cliout"
	"rulepack/internal/config"
	"rulepack/internal/git"
	profilesvc "rulepack/internal/profile"
)

func (a *app) newDepsCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "deps",
		Short: "Manage dependency lifecycle",
	}
	root.AddCommand(a.newDepsAddCmd())
	root.AddCommand(a.newDepsListCmd())
	root.AddCommand(a.newDepsRemoveCmd())
	root.AddCommand(a.newDepsInstallCmd())
	root.AddCommand(a.newDepsOutdatedCmd())
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

func (a *app) newDepsInstallCmd() *cobra.Command {
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
					"git":       strconv.Itoa(counts["git"]),
					"local":     strconv.Itoa(counts["local"]),
					"profile":   strconv.Itoa(counts["profile"]),
					"lock file": config.LockFileName,
				},
				Done: "Install complete",
			})
			return nil
		},
	}
	return cmd
}

func (a *app) newDepsOutdatedCmd() *cobra.Command {
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
				return fmt.Errorf("lockfile mismatch: run rulepack deps install")
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
