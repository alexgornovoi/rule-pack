package main

import (
	"sort"
	"strconv"

	"github.com/spf13/cobra"
	"rulepack/internal/cliout"
	"rulepack/internal/config"
)

func (a *app) newRemoveCmd() *cobra.Command {
	return a.newDependencyRemoveCmd("remove <dep-selector> [dep-selector...]", "Remove one or more dependencies from rulepack.json")
}

func (a *app) newDepsRemoveCmd() *cobra.Command {
	return a.newDependencyRemoveCmd("remove <dep-selector> [dep-selector...]", "Remove one or more dependencies from rulepack.json")
}

func (a *app) newDependencyRemoveCmd(use string, short string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     use,
		Aliases: []string{"uninstall"},
		Short:   short,
		Args:    cobra.MinimumNArgs(1),
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

			cfg.Dependencies = kept
			if err := config.SaveRuleset(config.RulesetFileName, cfg); err != nil {
				return err
			}

			out := removeOutput{
				RulesetFile: config.RulesetFileName,
				Removed:     removed,
				Remaining:   len(cfg.Dependencies),
			}
			if a.jsonMode {
				return a.renderer.RenderJSON("remove", out)
			}
			rows := make([][]string, 0, len(removed))
			for _, r := range removed {
				rows = append(rows, []string{strconv.Itoa(r.Index), r.Source, r.Ref, r.Export})
			}
			events := []cliout.Event{}
			if len(removed) > 1 {
				events = append(events, cliout.Event{Level: "info", Message: "Removed " + strconv.Itoa(len(removed)) + " dependencies"})
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "remove",
				Title:   "Dependencies Removed",
				Events:  events,
				Tables:  []cliout.Table{{Title: "Removed Dependencies", Columns: []string{"#", "Source", "Ref/Path/Profile", "Export"}, Rows: rows}},
				Summary: map[string]string{"remaining": strconv.Itoa(len(cfg.Dependencies))},
				Done:    "Updated " + config.RulesetFileName,
			})
			return nil
		},
	}
	return cmd
}
