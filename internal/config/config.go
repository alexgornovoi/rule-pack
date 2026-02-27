package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

const (
	RulesetFileName = "rulepack.json"
	LockFileName    = "rulepack.lock.json"
)

type Ruleset struct {
	SpecVersion  string                 `json:"specVersion"`
	Name         string                 `json:"name"`
	Dependencies []Dependency           `json:"dependencies,omitempty"`
	Overrides    []Override             `json:"overrides,omitempty"`
	Targets      map[string]TargetEntry `json:"targets,omitempty"`
}

type Dependency struct {
	Source  string `json:"source"`
	URI     string `json:"uri"`
	Path    string `json:"path,omitempty"`
	Profile string `json:"profile,omitempty"`
	Version string `json:"version,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Export  string `json:"export,omitempty"`
}

type Override struct {
	ID       string `json:"id"`
	Priority *int   `json:"priority,omitempty"`
}

type TargetEntry struct {
	OutDir    string `json:"outDir,omitempty"`
	OutFile   string `json:"outFile,omitempty"`
	PerModule bool   `json:"perModule,omitempty"`
	Ext       string `json:"ext,omitempty"`
}

type Lockfile struct {
	LockVersion string         `json:"lockVersion"`
	Resolved    []LockedSource `json:"resolved"`
}

type LockedSource struct {
	Source          string `json:"source,omitempty"`
	URI             string `json:"uri"`
	Path            string `json:"path,omitempty"`
	Profile         string `json:"profile,omitempty"`
	Requested       string `json:"requested,omitempty"`
	ResolvedVersion string `json:"resolvedVersion,omitempty"`
	Commit          string `json:"commit"`
	ContentHash     string `json:"contentHash,omitempty"`
	Export          string `json:"export,omitempty"`
}

func DefaultRuleset(name string) Ruleset {
	return Ruleset{
		SpecVersion: "0.1",
		Name:        name,
		Targets: map[string]TargetEntry{
			"cursor": {
				OutDir:    ".cursor/rules",
				PerModule: true,
				Ext:       ".mdc",
			},
			"copilot": {
				OutFile: ".github/copilot-instructions.md",
			},
			"codex": {
				OutFile: ".codex/rules.md",
			},
		},
	}
}

func LoadRuleset(path string) (Ruleset, error) {
	var cfg Ruleset
	bytes, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(bytes, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.SpecVersion == "" {
		return cfg, errors.New("rulepack missing specVersion")
	}
	if err := validateDependencies(cfg.Dependencies); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func SaveRuleset(path string, cfg Ruleset) error {
	return saveJSON(path, cfg)
}

func LoadLockfile(path string) (Lockfile, error) {
	var lock Lockfile
	bytes, err := os.ReadFile(path)
	if err != nil {
		return lock, err
	}
	if err := json.Unmarshal(bytes, &lock); err != nil {
		return lock, fmt.Errorf("parse %s: %w", path, err)
	}
	for i := range lock.Resolved {
		if lock.Resolved[i].Source == "" {
			// Backward compatibility: old lock entries are implicitly git.
			lock.Resolved[i].Source = "git"
		}
	}
	return lock, nil
}

func SaveLockfile(path string, lock Lockfile) error {
	return saveJSON(path, lock)
}

func saveJSON(path string, value any) error {
	bytes, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	bytes = append(bytes, '\n')
	return os.WriteFile(path, bytes, 0o644)
}

func validateDependencies(deps []Dependency) error {
	for i, dep := range deps {
		source := dep.Source
		if source == "" {
			// Backward compatibility: previously all dependencies were git.
			source = "git"
		}
		switch source {
		case "git":
			if dep.URI == "" {
				return fmt.Errorf("dependency[%d]: git source requires uri", i)
			}
			if dep.Path != "" || dep.Profile != "" {
				return fmt.Errorf("dependency[%d]: git source does not support path/profile", i)
			}
			if dep.Ref != "" && dep.Version != "" {
				return fmt.Errorf("dependency[%d]: use only one of version or ref", i)
			}
		case "local":
			if dep.Path == "" {
				return fmt.Errorf("dependency[%d]: local source requires path", i)
			}
			if dep.URI != "" || dep.Profile != "" {
				return fmt.Errorf("dependency[%d]: local source does not support uri/profile", i)
			}
			if dep.Ref != "" || dep.Version != "" {
				return fmt.Errorf("dependency[%d]: local source does not support version or ref", i)
			}
		case "profile":
			if dep.Profile == "" {
				return fmt.Errorf("dependency[%d]: profile source requires profile id", i)
			}
			if dep.URI != "" || dep.Path != "" {
				return fmt.Errorf("dependency[%d]: profile source does not support uri/path", i)
			}
			if dep.Ref != "" || dep.Version != "" {
				return fmt.Errorf("dependency[%d]: profile source does not support version or ref", i)
			}
		default:
			return fmt.Errorf("dependency[%d]: unsupported source %q", i, dep.Source)
		}
	}
	return nil
}
