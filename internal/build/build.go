package build

import (
	"fmt"
	"sort"

	"rulepack/internal/config"
	"rulepack/internal/pack"
)

func ApplyOverrides(modules []pack.Module, overrides []config.Override) []pack.Module {
	index := make(map[string]config.Override, len(overrides))
	for _, ov := range overrides {
		index[ov.ID] = ov
	}
	out := make([]pack.Module, len(modules))
	copy(out, modules)
	for i := range out {
		if ov, ok := index[out[i].ID]; ok {
			if ov.Priority != nil {
				out[i].Priority = *ov.Priority
			}
		}
	}
	return out
}

func Sort(modules []pack.Module) {
	sort.Slice(modules, func(i, j int) bool {
		if modules[i].Priority == modules[j].Priority {
			return modules[i].ID < modules[j].ID
		}
		return modules[i].Priority < modules[j].Priority
	})
}

func CheckDuplicateIDs(modules []pack.Module) error {
	seen := map[string]pack.Module{}
	for _, m := range modules {
		if prev, ok := seen[m.ID]; ok {
			return fmt.Errorf(
				"duplicate module id %q after composition: first(pack=%s version=%s commit=%s) second(pack=%s version=%s commit=%s)",
				m.ID,
				prev.PackName, prev.PackVersion, shortCommit(prev.Commit),
				m.PackName, m.PackVersion, shortCommit(m.Commit),
			)
		}
		seen[m.ID] = m
	}
	return nil
}

func shortCommit(v string) string {
	if len(v) > 12 {
		return v[:12]
	}
	return v
}
