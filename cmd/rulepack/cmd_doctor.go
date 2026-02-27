package main

import (
	"os"

	"github.com/spf13/cobra"
	"rulepack/internal/cliout"
	"rulepack/internal/config"
	"rulepack/internal/git"
	profilesvc "rulepack/internal/profile"
)

func (a *app) newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate environment, config, lockfile, and profile store",
		RunE: func(cmd *cobra.Command, args []string) error {
			checks := []doctorCheck{}
			if _, err := os.Stat(config.RulesetFileName); err != nil {
				checks = append(checks, doctorCheck{Name: "ruleset file", Status: "fail", Details: err.Error()})
			} else {
				checks = append(checks, doctorCheck{Name: "ruleset file", Status: "ok"})
			}
			cfg, cfgErr := config.LoadRuleset(config.RulesetFileName)
			if cfgErr != nil {
				checks = append(checks, doctorCheck{Name: "ruleset parse", Status: "fail", Details: cfgErr.Error()})
			} else {
				checks = append(checks, doctorCheck{Name: "ruleset parse", Status: "ok"})
			}
			lock, lockErr := config.LoadLockfile(config.LockFileName)
			if lockErr != nil {
				checks = append(checks, doctorCheck{Name: "lockfile", Status: "warn", Details: lockErr.Error()})
			} else {
				checks = append(checks, doctorCheck{Name: "lockfile", Status: "ok"})
				if cfgErr == nil && len(cfg.Dependencies) != len(lock.Resolved) {
					checks = append(checks, doctorCheck{Name: "lock alignment", Status: "fail", Details: "dependency count differs from lockfile"})
				} else if cfgErr == nil {
					checks = append(checks, doctorCheck{Name: "lock alignment", Status: "ok"})
				}
			}
			profileRoot, pErr := profilesvc.GlobalRoot()
			if pErr != nil {
				checks = append(checks, doctorCheck{Name: "profile store", Status: "fail", Details: pErr.Error()})
			} else {
				if _, err := os.Stat(profileRoot); err == nil {
					checks = append(checks, doctorCheck{Name: "profile store", Status: "ok", Details: profileRoot})
				} else {
					checks = append(checks, doctorCheck{Name: "profile store", Status: "warn", Details: profileRoot + " (not created yet)"})
				}
			}
			_, gErr := git.NewClient()
			if gErr != nil {
				checks = append(checks, doctorCheck{Name: "git client", Status: "fail", Details: gErr.Error()})
			} else {
				checks = append(checks, doctorCheck{Name: "git client", Status: "ok"})
			}

			out := doctorOutput{Checks: checks}
			if a.jsonMode {
				return a.renderer.RenderJSON("doctor", out)
			}
			rows := make([][]string, 0, len(checks))
			for _, c := range checks {
				rows = append(rows, []string{c.Name, c.Status, c.Details})
			}
			a.renderer.RenderHuman(cliout.HumanPayload{
				Command: "doctor",
				Title:   "Diagnostics",
				Tables:  []cliout.Table{{Title: "Checks", Columns: []string{"Check", "Status", "Details"}, Rows: rows}},
				Done:    "Doctor run complete",
			})
			return nil
		},
	}
	return cmd
}
