package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"rulepack/internal/cliout"
	"rulepack/internal/config"
	"rulepack/internal/git"
	"rulepack/internal/pack"
	profilesvc "rulepack/internal/profile"
)

func (a *app) newProfileCmd() *cobra.Command {
	root := &cobra.Command{Use: "profile", Short: "Manage reusable globally saved profiles"}
	root.AddCommand(a.newProfileSaveCmd())
	root.AddCommand(a.newProfileListCmd())
	root.AddCommand(a.newProfileShowCmd())
	root.AddCommand(a.newProfileRemoveCmd())
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
		Short: "Save dependencies as a globally reusable local profile snapshot",
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
				return errors.New("cannot save profile: dependency not installed; run rulepack deps install")
			}
			resolvedAlias, err := resolveProfileAlias(cmd, alias)
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
			scope := "all"
			combined := true
			sourceCount := len(cfg.Dependencies)
			dependencyIndex := -1
			updatedRows := [][]string{}

			var meta profilesvc.Metadata
			if depSelector != "" {
				scope = "dep"
				combined = false
				idx, err := findDependencyIndex(cfg, depSelector)
				if err != nil {
					return err
				}
				dependencyIndex = idx
				sourceCount = 1
				dep := cfg.Dependencies[idx]
				locked := lock.Resolved[idx]
				modules, contentHash, sourceRef, provenance, err := expandDependencyForSnapshot(cfgDir, gc, dep, locked)
				if err != nil {
					return err
				}
				meta, err = profilesvc.SaveSnapshot(profilesvc.SaveInput{
					Alias: resolvedAlias,
					Sources: []profilesvc.SourceSnapshot{{
						SourceType:   dependencySource(dep),
						SourceRef:    sourceRef,
						SourceExport: dep.Export,
						Provenance:   provenance,
						ModuleIDs:    moduleIDs(modules),
					}},
					ContentHash: contentHash,
					Modules:     modules,
				})
				if err != nil {
					return err
				}
				if switchDependency {
					cfg.Dependencies[idx] = config.Dependency{Source: profilesvc.ProfileSource, Profile: meta.ID, Export: "default"}
					updatedRows = append(updatedRows, []string{strconv.Itoa(idx + 1), dependencyReference(dep), meta.ID})
				}
			} else {
				modules, sources, err := collectSnapshotForAllDependencies(cfg, lock, cfgDir, gc)
				if err != nil {
					return err
				}
				contentHash := profilesvc.ComputeContentHash(modules, "default")
				meta, err = profilesvc.SaveSnapshot(profilesvc.SaveInput{
					Alias:       resolvedAlias,
					Sources:     sources,
					ContentHash: contentHash,
					Modules:     modules,
				})
				if err != nil {
					return err
				}
				if switchDependency {
					for i, dep := range cfg.Dependencies {
						updatedRows = append(updatedRows, []string{strconv.Itoa(i + 1), dependencyReference(dep), meta.ID})
					}
					cfg.Dependencies = []config.Dependency{{Source: profilesvc.ProfileSource, Profile: meta.ID, Export: "default"}}
				}
			}
			if switchDependency {
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
			out := profileSaveOutput{
				Profile:         meta,
				Switched:        switchDependency,
				DependencyIndex: dependencyIndex,
				Scope:           scope,
				SourceCount:     sourceCount,
				Combined:        combined,
			}
			if a.jsonMode {
				return a.renderer.RenderJSON("profile.save", out)
			}
			rows := [][]string{{meta.ID, meta.Alias, profileSourceSummary(meta), "default", strconv.Itoa(meta.ModuleCount), shortSHA(meta.ContentHash)}}
			events := []cliout.Event{{Level: "info", Message: "Scope: " + scope}}
			if switchDependency {
				events = append(events, cliout.Event{Level: "info", Message: "Switched dependencies to profile source and refreshed lockfile"})
			}
			tables := []cliout.Table{{Title: "Snapshot", Columns: []string{"Profile ID", "Alias", "Source", "Export", "Modules", "Content Hash"}, Rows: rows}}
			if len(updatedRows) > 0 {
				tables = append(tables, cliout.Table{Title: "Dependency Updates", Columns: []string{"#", "Old Ref", "Profile ID"}, Rows: updatedRows})
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "profile.save",
				Title:   "Profile Saved",
				Events:  events,
				Tables:  tables,
				Done:    "Profile save complete",
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&depSelector, "dep", "", "dependency selector (index or source ref)")
	cmd.Flags().StringVar(&alias, "alias", "", "profile alias (required; prompts in interactive terminals)")
	cmd.Flags().BoolVar(&switchDependency, "switch", false, "switch dependency config to saved profile source")
	return cmd
}

func resolveProfileAlias(cmd *cobra.Command, alias string) (string, error) {
	alias = strings.TrimSpace(alias)
	if alias != "" {
		return alias, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("profile save requires --alias in non-interactive mode")
	}
	reader := bufio.NewReader(cmd.InOrStdin())
	for {
		_, _ = fmt.Fprint(cmd.ErrOrStderr(), "Enter profile alias: ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line != "" {
			return line, nil
		}
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Alias cannot be empty")
	}
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
				rows = append(rows, []string{p.ID, alias, profileSourceSummary(p), "default", strconv.Itoa(p.ModuleCount), p.CreatedAt})
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
				{"sources", profileSourceSummary(meta)},
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

func (a *app) newProfileRemoveCmd() *cobra.Command {
	var yes bool
	var removeAll bool
	cmd := &cobra.Command{
		Use:     "remove <profile-id-or-alias>",
		Aliases: []string{"delete"},
		Short:   "Remove one or all saved profiles",
		Args: func(cmd *cobra.Command, args []string) error {
			if removeAll {
				if len(args) != 0 {
					return errors.New("profile remove --all does not accept an id or alias")
				}
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				if !term.IsTerminal(int(os.Stdin.Fd())) {
					return errors.New("profile remove requires --yes in non-interactive mode")
				}
				reader := bufio.NewReader(cmd.InOrStdin())
				if removeAll {
					_, _ = fmt.Fprint(cmd.ErrOrStderr(), "Remove all saved profiles? [y/N]: ")
				} else {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Remove saved profile %q? [y/N]: ", args[0])
				}
				line, err := reader.ReadString('\n')
				if err != nil {
					return err
				}
				answer := strings.ToLower(strings.TrimSpace(line))
				if answer != "y" && answer != "yes" {
					return errors.New("profile remove cancelled")
				}
			}

			if removeAll {
				removed, err := profilesvc.RemoveAll()
				if err != nil {
					return err
				}
				paths := make([]string, len(removed))
				root, _ := profilesvc.GlobalRoot()
				for i, meta := range removed {
					paths[i] = filepath.Join(root, meta.ID)
				}
				out := profileRemoveOutput{Count: len(removed), RemovedProfiles: profileRemoveRows(removed, paths)}
				if a.jsonMode {
					return a.renderer.RenderJSON("profile.remove", out)
				}
				rows := [][]string{}
				for _, r := range out.RemovedProfiles {
					rows = append(rows, []string{r.ProfileID, r.Alias, r.Path})
				}
				events := []cliout.Event{{Level: "warn", Message: "Removed all saved profiles"}}
				a.renderer.RenderHuman(cliout.HumanPayload{
					Command: "profile.remove",
					Title:   "Profiles Removed",
					Events:  events,
					Tables:  []cliout.Table{{Title: "Removed Profiles", Columns: []string{"Profile ID", "Alias", "Path"}, Rows: rows}},
					Summary: map[string]string{"count": strconv.Itoa(len(rows))},
					Done:    "Profile removal complete",
				})
				return nil
			}

			meta, path, err := profilesvc.Remove(args[0])
			if err != nil {
				return err
			}
			out := profileRemoveOutput{
				ProfileID:       meta.ID,
				Alias:           meta.Alias,
				Path:            path,
				Removed:         true,
				Count:           1,
				RemovedProfiles: profileRemoveRows([]profilesvc.Metadata{meta}, []string{path}),
			}
			if a.jsonMode {
				return a.renderer.RenderJSON("profile.remove", out)
			}
			rows := [][]string{{meta.ID, meta.Alias, path}}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "profile.remove",
				Title:   "Profile Removed",
				Tables:  []cliout.Table{{Title: "Removed Profiles", Columns: []string{"Profile ID", "Alias", "Path"}, Rows: rows}},
				Done:    "Profile removal complete",
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm deletion without prompting")
	cmd.Flags().BoolVar(&removeAll, "all", false, "remove all saved profiles")
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
			currentModules, _, err := pack.ExpandProfileDependency(profileDir, profileDependencyForRead(config.Dependency{Source: profilesvc.ProfileSource, Profile: meta.ID, Export: "default"}), profilesvc.ProfileCommit)
			if err != nil {
				return err
			}
			freshModules, refreshedSources, skippedSources, err := resolveFreshModulesForProfile(gc, meta, currentModules)
			if err != nil {
				return err
			}
			if len(rules) > 0 {
				currentModules = filterModulesByPatterns(currentModules, rules)
				freshModules = filterModulesByPatterns(freshModules, rules)
			}

			changed, added, removed := diffModules(currentModules, freshModules)
			currentHash := profilesvc.ComputeContentHash(currentModules, "default")
			freshHash := profilesvc.ComputeContentHash(freshModules, "default")
			out := newProfileDiffOutput(meta.ID, "combined", profileSourceSummary(meta), currentHash, freshHash, changed, added, removed, refreshedSources, skippedSources, rules)
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
			if len(skippedSources) > 0 {
				for _, s := range skippedSources {
					events = append(events, cliout.Event{Level: "warn", Message: "Skipped source " + s.Source + ": " + s.Reason})
				}
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
					"source":      profileSourceSummary(meta),
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
	var yes bool
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
			oldModules, _, err := pack.ExpandProfileDependency(profileDir, profileDependencyForRead(config.Dependency{Source: profilesvc.ProfileSource, Profile: meta.ID, Export: "default"}), profilesvc.ProfileCommit)
			if err != nil {
				return err
			}
			freshModules, refreshedSources, skippedSources, err := resolveFreshModulesForProfile(gc, meta, oldModules)
			if err != nil {
				return err
			}

			mergedModules, refreshedIDs, err := mergeRefreshedModules(oldModules, freshModules, rules)
			if err != nil {
				return err
			}
			changedModules, addedModules, removedModules := diffModules(oldModules, mergedModules)
			inPlaceWithDiff := !newID && !dryRun && (len(changedModules)+len(addedModules)+len(removedModules) > 0)
			preview := make([]string, 0, len(changedModules)+len(addedModules)+len(removedModules))
			for _, id := range changedModules {
				preview = append(preview, "changed: "+id)
			}
			for _, id := range addedModules {
				preview = append(preview, "added: "+id)
			}
			for _, id := range removedModules {
				preview = append(preview, "removed: "+id)
			}
			if err := confirmRiskAction(
				cmd,
				a.jsonMode,
				yes,
				inPlaceWithDiff,
				fmt.Sprintf("profile refresh would update profile %q in place with module diffs", meta.ID),
				fmt.Sprintf("Refresh profile %q in place with %d module change(s)?", meta.ID, len(preview)),
				preview,
				"profile refresh",
			); err != nil {
				return err
			}
			newHash := profilesvc.ComputeContentHash(mergedModules, "default")
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
					ID:          saveID,
					Alias:       alias,
					Sources:     meta.Sources,
					ContentHash: newHash,
					Modules:     mergedModules,
				})
				if err != nil {
					return err
				}
			}

			out := profileRefreshOutput{
				OldProfileID:     meta.ID,
				NewProfileID:     saved.ID,
				RefreshedRule:    refreshedIDs,
				Source:           profileSourceSummary(meta),
				InPlace:          !newID,
				DryRun:           dryRun,
				RefreshedSources: refreshedSources,
				SkippedSources:   skippedSources,
				ChangedModules:   changedModules,
				AddedModules:     addedModules,
				RemovedModules:   removedModules,
			}
			if a.jsonMode {
				return a.renderer.RenderJSON("profile.refresh", out)
			}
			rows := [][]string{{meta.ID, saved.ID, boolToYesNo(!newID), profileSourceSummary(meta)}}
			ruleRows := make([][]string, 0, len(refreshedIDs))
			for _, id := range refreshedIDs {
				ruleRows = append(ruleRows, []string{id})
			}
			tables := []cliout.Table{{Title: "Refresh Result", Columns: []string{"Old Profile", "New Profile", "In Place", "Source"}, Rows: rows}}
			if len(ruleRows) > 0 {
				tables = append(tables, cliout.Table{Title: "Refreshed Rules", Columns: []string{"Module ID"}, Rows: ruleRows})
			}
			if len(skippedSources) > 0 {
				skipRows := make([][]string, 0, len(skippedSources))
				for _, s := range skippedSources {
					skipRows = append(skipRows, []string{s.Source, s.Reason})
				}
				tables = append(tables, cliout.Table{Title: "Skipped Sources", Columns: []string{"Source", "Reason"}, Rows: skipRows})
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
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm risky in-place refresh without prompting")
	return cmd
}

func profileSourceSummary(meta profilesvc.Metadata) string {
	if len(meta.Sources) == 1 {
		s := meta.Sources[0]
		return s.SourceType + ":" + s.SourceRef
	}
	return strconv.Itoa(len(meta.Sources)) + " sources"
}

func moduleIDs(modules []pack.Module) []string {
	ids := make([]string, 0, len(modules))
	for _, m := range modules {
		ids = append(ids, m.ID)
	}
	buildSortStrings(ids)
	return ids
}
