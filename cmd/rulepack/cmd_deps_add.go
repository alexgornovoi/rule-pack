package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"rulepack/internal/cliout"
	"rulepack/internal/config"
)

func (a *app) newDepsAddCmd() *cobra.Command {
	var exportName string
	var version string
	var ref string
	var localPath string
	var yes bool

	cmd := &cobra.Command{
		Use:   "add [git-url]",
		Short: "Add a dependency to rulepack.json",
		Args: func(cmd *cobra.Command, args []string) error {
			hasGitURL := len(args) == 1
			hasLocal := strings.TrimSpace(localPath) != ""
			switch {
			case hasGitURL && hasLocal:
				return errors.New("use either <git-url> or --local <path>, not both")
			case !hasGitURL && !hasLocal:
				return errors.New("missing source: provide <git-url> or --local <path>")
			case hasLocal && len(args) > 0:
				return errors.New("--local mode does not accept positional arguments")
			case !hasLocal && len(args) != 1:
				return errors.New("git mode requires exactly one <git-url>")
			default:
				return nil
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			hasLocal := strings.TrimSpace(localPath) != ""
			if hasLocal {
				if version != "" || ref != "" {
					return errors.New("--version and --ref are only supported for git dependencies")
				}
			} else if version != "" && ref != "" {
				return errors.New("use only one of --version or --ref")
			}

			cfg, err := config.LoadRuleset(config.RulesetFileName)
			cwd, wdErr := os.Getwd()
			if wdErr != nil {
				return wdErr
			}
			cfgDir := cwd
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					cfg = config.DefaultRuleset(filepath.Base(cwd))
				} else {
					return err
				}
			}

			dep := config.Dependency{Export: exportName}
			matchKey := ""
			if hasLocal {
				_, normalizedPath, pathErr := resolveLocalPath(cfgDir, localPath)
				if pathErr != nil {
					return pathErr
				}
				dep.Source = "local"
				dep.Path = normalizedPath
				matchKey = dep.Path
			} else {
				dep.Source = "git"
				dep.URI = args[0]
				dep.Ref = ref
				dep.Version = version
				matchKey = dep.URI
			}

			action := "added"
			old := config.Dependency{}
			replaced := false
			for i := range cfg.Dependencies {
				if dependencyMatchKey(cfg.Dependencies[i]) == matchKey && dependencySource(cfg.Dependencies[i]) == dependencySource(dep) {
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

			if err := confirmRiskAction(
				cmd,
				a.jsonMode,
				yes,
				replaced,
				fmt.Sprintf("add would replace existing dependency %q", matchKey),
				fmt.Sprintf("Replace existing dependency %q in %s?", matchKey, config.RulesetFileName),
				[]string{
					fmt.Sprintf("old source=%q uri=%q path=%q export=%q version=%q ref=%q", old.Source, old.URI, old.Path, old.Export, old.Version, old.Ref),
					fmt.Sprintf("new source=%q uri=%q path=%q export=%q version=%q ref=%q", dep.Source, dep.URI, dep.Path, dep.Export, dep.Version, dep.Ref),
				},
				"add",
			); err != nil {
				return err
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
				{"path", old.Path, dep.Path},
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
	cmd.Flags().StringVar(&localPath, "local", "", "local rulepack path")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm risky replacement without prompting")
	return cmd
}

func dependencyMatchKey(dep config.Dependency) string {
	switch dependencySource(dep) {
	case "git":
		return dep.URI
	case "local":
		return filepath.ToSlash(filepath.Clean(dep.Path))
	default:
		return dependencyReference(dep)
	}
}
