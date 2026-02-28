package main

import (
	"github.com/spf13/cobra"
	"rulepack/internal/cliout"
)

func (a *app) newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print rulepack version",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := versionOutput{Version: appVersion()}
			if a.jsonMode {
				return a.renderer.RenderJSON("version", out)
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "version",
				Title:   "Rulepack Version",
				Summary: map[string]string{"version": out.Version},
				Done:    "Version displayed",
			})
			return nil
		},
	}
	return cmd
}
