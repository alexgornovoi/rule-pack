package main

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
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
