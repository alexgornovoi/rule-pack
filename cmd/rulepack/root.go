package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"rulepack/internal/cliout"
)

type app struct {
	renderer cliout.Renderer
	jsonMode bool
	noColor  bool
}

func main() {
	a := &app{}
	root := &cobra.Command{
		Use:           "rulepack",
		Short:         "Import rule packs and compile target-native rule outputs",
		Long:          "rulepack composes rule packs into target outputs. Use --json for machine-readable output.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if a.jsonMode {
				a.renderer = cliout.NewJSONRenderer()
			} else {
				a.renderer = cliout.NewHumanRenderer(a.noColor)
			}
			return nil
		},
	}

	root.PersistentFlags().BoolVar(&a.jsonMode, "json", false, "emit JSON output")
	root.PersistentFlags().BoolVar(&a.noColor, "no-color", false, "disable color in human output")

	root.AddCommand(a.newInitCmd())
	root.AddCommand(a.newDepsCmd())
	root.AddCommand(a.newBuildCmd())
	root.AddCommand(a.newDoctorCmd())
	root.AddCommand(a.newProfileCmd())

	if err := root.Execute(); err != nil {
		if a.renderer == nil {
			if a.jsonMode {
				_ = cliout.NewJSONRenderer().RenderJSON("error", map[string]any{"error": map[string]string{"message": err.Error()}})
			} else {
				fmt.Fprintln(os.Stderr, err)
			}
			os.Exit(1)
		}
		a.renderer.RenderError("error", err)
		os.Exit(1)
	}
}
