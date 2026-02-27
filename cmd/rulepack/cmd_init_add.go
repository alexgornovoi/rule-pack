package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"rulepack/internal/cliout"
	"rulepack/internal/config"
)

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
				if errors.Is(err, os.ErrNotExist) {
					cwd, wdErr := os.Getwd()
					if wdErr != nil {
						return wdErr
					}
					cfg = config.DefaultRuleset(filepath.Base(cwd))
				} else {
					return err
				}
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
