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
}

type profileSaveOutput struct {
	Profile         profilesvc.Metadata `json:"profile"`
	Switched        bool                `json:"switched"`
	DependencyIndex int                 `json:"dependencyIndex"`
}

type profileListOutput struct {
	Profiles []profilesvc.Metadata `json:"profiles"`
}

type profileUseOutput struct {
	ProfileID   string `json:"profileId"`
	Action      string `json:"action"`
	RulesetFile string `json:"rulesetFile"`
}

type profileRefreshOutput struct {
	OldProfileID  string   `json:"oldProfileId"`
	NewProfileID  string   `json:"newProfileId"`
	RefreshedRule []string `json:"refreshedRules,omitempty"`
	Source        string   `json:"source"`
	InPlace       bool     `json:"inPlace"`
	DryRun        bool     `json:"dryRun,omitempty"`
}

type depsListRow struct {
	Index   int    `json:"index"`
	Source  string `json:"source"`
	Ref     string `json:"ref"`
	Export  string `json:"export,omitempty"`
	Locked  string `json:"locked,omitempty"`
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
	ProfileID      string   `json:"profileId"`
	SourceType     string   `json:"sourceType"`
	SourceRef      string   `json:"sourceRef"`
	CurrentHash    string   `json:"currentHash"`
	FreshHash      string   `json:"freshHash"`
	ChangedModules []string `json:"changedModules,omitempty"`
	AddedModules   []string `json:"addedModules,omitempty"`
	RemovedModules []string `json:"removedModules,omitempty"`
	RuleSelectors  []string `json:"ruleSelectors,omitempty"`
	UpdatedAt      string   `json:"updatedAt"`
}

func newOutdatedOutput(entries []outdatedEntry, outdatedCount int) outdatedOutput {
	return outdatedOutput{
		CheckedAt:     time.Now().UTC().Format(time.RFC3339),
		Dependencies:  entries,
		OutdatedCount: outdatedCount,
	}
}

func newProfileDiffOutput(profileID, sourceType, sourceRef, currentHash, freshHash string, changed, added, removed, selectors []string) profileDiffOutput {
	return profileDiffOutput{
		ProfileID:      profileID,
		SourceType:     sourceType,
		SourceRef:      sourceRef,
		CurrentHash:    currentHash,
		FreshHash:      freshHash,
		ChangedModules: changed,
		AddedModules:   added,
		RemovedModules: removed,
		RuleSelectors:  selectors,
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
}
