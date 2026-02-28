package main

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/spf13/cobra"
	"rulepack/internal/cliout"
	"rulepack/internal/config"
	"rulepack/internal/render"
)

func (a *app) newDepsUninstallCmd() *cobra.Command {
	var yes bool
	var cleanup bool
	cmd := &cobra.Command{
		Use:   "uninstall <dep-selector> [dep-selector...]",
		Short: "Uninstall one or more dependencies from rulepack.json",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadRuleset(config.RulesetFileName)
			if err != nil {
				return err
			}

			toRemove := make(map[int]struct{}, len(args))
			for _, selector := range args {
				idx, err := findDependencyIndex(cfg, selector)
				if err != nil {
					return err
				}
				toRemove[idx] = struct{}{}
			}

			removed := make([]removedDependencyRow, 0, len(toRemove))
			kept := make([]config.Dependency, 0, len(cfg.Dependencies)-len(toRemove))
			for i, dep := range cfg.Dependencies {
				if _, ok := toRemove[i]; ok {
					removed = append(removed, removedDependencyRow{
						Index:      i + 1,
						Source:     dependencySource(dep),
						Ref:        dependencyReference(dep),
						Export:     dep.Export,
						Dependency: dep,
					})
					continue
				}
				kept = append(kept, dep)
			}
			sort.Slice(removed, func(i, j int) bool { return removed[i].Index < removed[j].Index })
			preview := make([]string, 0, len(removed))
			for _, row := range removed {
				preview = append(preview, fmt.Sprintf("#%d %s %s export=%s", row.Index, row.Source, row.Ref, row.Export))
			}
			if err := confirmRiskAction(
				cmd,
				a.jsonMode,
				yes,
				len(removed) > 0,
				fmt.Sprintf("uninstall would delete %d dependency entries from %s", len(removed), config.RulesetFileName),
				fmt.Sprintf("Uninstall %d dependency entries from %s?", len(removed), config.RulesetFileName),
				preview,
				"uninstall",
			); err != nil {
				return err
			}

			cfg.Dependencies = kept
			if err := config.SaveRuleset(config.RulesetFileName, cfg); err != nil {
				return err
			}

			cleanupRequested := cleanup
			if !cleanupRequested && !a.jsonMode && isInteractiveTerminal() {
				previewDelete, _, err := render.PreviewManagedCleanup(cfg.Targets)
				if err != nil {
					return err
				}
				if len(previewDelete) > 0 {
					cleanupRequested, err = promptOptionalAction(cmd, fmt.Sprintf("Cleanup %d managed generated file(s)?", len(previewDelete)), previewDelete)
					if err != nil {
						return err
					}
				}
			}

			cleanupPerformed := false
			cleanupDeleted := make([]string, 0)
			cleanupSkipped := make([]string, 0)
			if cleanupRequested {
				cleanupPerformed = true
				deleted, skipped, err := render.CleanupManagedOutputs(cfg.Targets)
				if err != nil {
					return err
				}
				cleanupDeleted = deleted
				cleanupSkipped = skipped
			}

			out := uninstallOutput{
				RulesetFile:      config.RulesetFileName,
				Removed:          removed,
				Remaining:        len(cfg.Dependencies),
				CleanupRequested: cleanupRequested,
				CleanupPerformed: cleanupPerformed,
				CleanupDeleted:   cleanupDeleted,
				CleanupSkipped:   cleanupSkipped,
			}
			if a.jsonMode {
				return a.renderer.RenderJSON("uninstall", out)
			}
			rows := make([][]string, 0, len(removed))
			for _, r := range removed {
				rows = append(rows, []string{strconv.Itoa(r.Index), r.Source, r.Ref, r.Export})
			}
			events := []cliout.Event{}
			if len(removed) > 1 {
				events = append(events, cliout.Event{Level: "info", Message: "Uninstalled " + strconv.Itoa(len(removed)) + " dependencies"})
			}
			if cleanupPerformed {
				events = append(events, cliout.Event{Level: "info", Message: "Cleanup deleted " + strconv.Itoa(len(cleanupDeleted)) + " managed file(s)"})
				if len(cleanupSkipped) > 0 {
					events = append(events, cliout.Event{Level: "warn", Message: "Cleanup skipped " + strconv.Itoa(len(cleanupSkipped)) + " unmanaged file(s)"})
				}
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "uninstall",
				Title:   "Dependencies Uninstalled",
				Events:  events,
				Tables:  []cliout.Table{{Title: "Uninstalled Dependencies", Columns: []string{"#", "Source", "Ref/Path/Profile", "Export"}, Rows: rows}},
				Summary: map[string]string{"remaining": strconv.Itoa(len(cfg.Dependencies)), "cleanupDeleted": strconv.Itoa(len(cleanupDeleted)), "cleanupSkipped": strconv.Itoa(len(cleanupSkipped))},
				Done:    "Updated " + config.RulesetFileName,
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm dependency uninstall without prompting")
	cmd.Flags().BoolVar(&cleanup, "cleanup", false, "cleanup managed generated outputs after uninstall")
	return cmd
}
