package main

import (
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
