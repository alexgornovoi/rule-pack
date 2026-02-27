package main

import (
	"time"

	"rulepack/internal/config"
	profilesvc "rulepack/internal/profile"
)

type initOutput struct {
	RulesetFile   string   `json:"rulesetFile"`
	Name          string   `json:"name"`
	TemplateFiles []string `json:"templateFiles,omitempty"`
}

type addOutput struct {
	RulesetFile string            `json:"rulesetFile"`
	Action      string            `json:"action"`
	Dependency  config.Dependency `json:"dependency"`
}

type removedDependencyRow struct {
	Index      int               `json:"index"`
	Source     string            `json:"source"`
	Ref        string            `json:"ref"`
	Export     string            `json:"export,omitempty"`
	Dependency config.Dependency `json:"dependency"`
}

type removeOutput struct {
	RulesetFile string                 `json:"rulesetFile"`
	Removed     []removedDependencyRow `json:"removed"`
	Remaining   int                    `json:"remaining"`
}

type installResolvedRow struct {
	Index    int    `json:"index"`
	Source   string `json:"source"`
	Ref      string `json:"ref"`
	Export   string `json:"export,omitempty"`
	Resolved string `json:"resolved"`
	Hash     string `json:"hash"`
}

type installOutput struct {
	LockFile string               `json:"lockFile"`
	Resolved []installResolvedRow `json:"resolved"`
	Counts   map[string]int       `json:"counts"`
}

type buildTargetRow struct {
	Target string `json:"target"`
	Output string `json:"output"`
	Status string `json:"status"`
}

type buildOutput struct {
	ModuleCount int              `json:"moduleCount"`
	Targets     []buildTargetRow `json:"targets"`
	Warnings    []string         `json:"warnings,omitempty"`
}

type profileSaveOutput struct {
	Profile         profilesvc.Metadata `json:"profile"`
	Switched        bool                `json:"switched"`
	DependencyIndex int                 `json:"dependencyIndex"`
	Scope           string              `json:"scope"`
	SourceCount     int                 `json:"sourceCount"`
	Combined        bool                `json:"combined"`
}

type sourceStatus struct {
	Source string `json:"source"`
}

type sourceSkip struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

type profileListOutput struct {
	Profiles []profilesvc.Metadata `json:"profiles"`
}

type profileUseOutput struct {
	ProfileID   string `json:"profileId"`
	Action      string `json:"action"`
	RulesetFile string `json:"rulesetFile"`
}

type profileRemoveRow struct {
	ProfileID string `json:"profileId"`
	Alias     string `json:"alias,omitempty"`
	Path      string `json:"path"`
}

type profileRemoveOutput struct {
	ProfileID       string             `json:"profileId,omitempty"`
	Alias           string             `json:"alias,omitempty"`
	Path            string             `json:"path,omitempty"`
	Removed         bool               `json:"removed,omitempty"`
	RemovedProfiles []profileRemoveRow `json:"removedProfiles,omitempty"`
	Count           int                `json:"count"`
}

type profileRefreshOutput struct {
	OldProfileID     string         `json:"oldProfileId"`
	NewProfileID     string         `json:"newProfileId"`
	RefreshedRule    []string       `json:"refreshedRules,omitempty"`
	Source           string         `json:"source"`
	InPlace          bool           `json:"inPlace"`
	DryRun           bool           `json:"dryRun,omitempty"`
	RefreshedSources []sourceStatus `json:"refreshedSources,omitempty"`
	SkippedSources   []sourceSkip   `json:"skippedSources,omitempty"`
	ChangedModules   []string       `json:"changedModules,omitempty"`
	AddedModules     []string       `json:"addedModules,omitempty"`
	RemovedModules   []string       `json:"removedModules,omitempty"`
}

type depsListRow struct {
	Index  int    `json:"index"`
	Source string `json:"source"`
	Ref    string `json:"ref"`
	Export string `json:"export,omitempty"`
	Locked string `json:"locked,omitempty"`
}

type depsListOutput struct {
	Dependencies []depsListRow `json:"dependencies"`
}

type profileShowOutput struct {
	Profile profilesvc.Metadata `json:"profile"`
	Path    string              `json:"path"`
}

type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Details string `json:"details,omitempty"`
}

type doctorOutput struct {
	Checks []doctorCheck `json:"checks"`
}

type outdatedEntry struct {
	Index        int    `json:"index"`
	Source       string `json:"source"`
	Reference    string `json:"reference"`
	Locked       string `json:"locked,omitempty"`
	Latest       string `json:"latest,omitempty"`
	UpdateStatus string `json:"updateStatus"`
}

type outdatedOutput struct {
	CheckedAt     string          `json:"checkedAt"`
	Dependencies  []outdatedEntry `json:"dependencies"`
	OutdatedCount int             `json:"outdatedCount"`
}

type profileDiffOutput struct {
	ProfileID        string         `json:"profileId"`
	SourceType       string         `json:"sourceType"`
	SourceRef        string         `json:"sourceRef"`
	CurrentHash      string         `json:"currentHash"`
	FreshHash        string         `json:"freshHash"`
	ChangedModules   []string       `json:"changedModules,omitempty"`
	AddedModules     []string       `json:"addedModules,omitempty"`
	RemovedModules   []string       `json:"removedModules,omitempty"`
	RefreshedSources []sourceStatus `json:"refreshedSources,omitempty"`
	SkippedSources   []sourceSkip   `json:"skippedSources,omitempty"`
	RuleSelectors    []string       `json:"ruleSelectors,omitempty"`
	UpdatedAt        string         `json:"updatedAt"`
}

func newOutdatedOutput(entries []outdatedEntry, outdatedCount int) outdatedOutput {
	return outdatedOutput{
		CheckedAt:     time.Now().UTC().Format(time.RFC3339),
		Dependencies:  entries,
		OutdatedCount: outdatedCount,
	}
}

func newProfileDiffOutput(profileID, sourceType, sourceRef, currentHash, freshHash string, changed, added, removed []string, refreshed []sourceStatus, skipped []sourceSkip, selectors []string) profileDiffOutput {
	return profileDiffOutput{
		ProfileID:        profileID,
		SourceType:       sourceType,
		SourceRef:        sourceRef,
		CurrentHash:      currentHash,
		FreshHash:        freshHash,
		ChangedModules:   changed,
		AddedModules:     added,
		RemovedModules:   removed,
		RefreshedSources: refreshed,
		SkippedSources:   skipped,
		RuleSelectors:    selectors,
		UpdatedAt:        time.Now().UTC().Format(time.RFC3339),
	}
}

func profileRemoveRows(profiles []profilesvc.Metadata, paths []string) []profileRemoveRow {
	rows := make([]profileRemoveRow, 0, len(profiles))
	for i, meta := range profiles {
		path := ""
		if i < len(paths) {
			path = paths[i]
		}
		rows = append(rows, profileRemoveRow{
			ProfileID: meta.ID,
			Alias:     meta.Alias,
			Path:      path,
		})
	}
	return rows
}
