package main

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/spf13/cobra"
	"rulepack/internal/cliout"
	"rulepack/internal/config"
)

func (a *app) newDepsRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "remove <dep-selector> [dep-selector...]",
		Aliases: []string{"uninstall"},
		Short:   "Remove one or more dependencies from rulepack.json",
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
			preview := make([]string, 0, len(removed))
			for _, row := range removed {
				preview = append(preview, fmt.Sprintf("#%d %s %s export=%s", row.Index, row.Source, row.Ref, row.Export))
			}
			if err := confirmRiskAction(
				cmd,
				a.jsonMode,
				yes,
				len(removed) > 0,
				fmt.Sprintf("remove would delete %d dependency entries from %s", len(removed), config.RulesetFileName),
				fmt.Sprintf("Remove %d dependency entries from %s?", len(removed), config.RulesetFileName),
				preview,
				"remove",
			); err != nil {
				return err
			}

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
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm dependency removal without prompting")
	return cmd
}
