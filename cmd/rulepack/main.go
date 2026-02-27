package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"rulepack/internal/build"
	"rulepack/internal/config"
	"rulepack/internal/git"
	"rulepack/internal/pack"
	profilesvc "rulepack/internal/profile"
)

func resolveTargets(target string) []string {
	target = strings.ToLower(target)
	if target == "" || target == "all" {
		return []string{"cursor", "copilot", "codex"}
	}
	return []string{target}
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func dryRunMessage(dryRun bool) string {
	if dryRun {
		return "Dry run only; no profile files were written"
	}
	return "Profile files updated"
}

func dependencySource(dep config.Dependency) string {
	if dep.Source == "" {
		return "git"
	}
	return dep.Source
}

func lockSource(locked config.LockedSource) string {
	if locked.Source == "" {
		return "git"
	}
	return locked.Source
}

func lockReference(locked config.LockedSource) string {
	switch lockSource(locked) {
	case "git":
		if locked.Commit != "" {
			return shortSHA(locked.Commit)
		}
		return "-"
	case "local", profilesvc.ProfileSource:
		if locked.ContentHash != "" {
			return shortSHA(locked.ContentHash)
		}
		if locked.Commit != "" {
			return shortSHA(locked.Commit)
		}
		return "-"
	default:
		if locked.Commit != "" {
			return shortSHA(locked.Commit)
		}
		return "-"
	}
}

func resolveLocalPath(cfgDir string, depPath string) (string, string, error) {
	if depPath == "" {
		return "", "", errors.New("local source requires path")
	}
	absPath := depPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(cfgDir, depPath)
	}
	absPath = filepath.Clean(absPath)
	info, err := os.Stat(absPath)
	if err != nil {
		return "", "", fmt.Errorf("local dependency path %q: %w", depPath, err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("local dependency path %q is not a directory", depPath)
	}
	if _, err := os.Stat(filepath.Join(absPath, config.RulesetFileName)); err != nil {
		return "", "", fmt.Errorf("local dependency missing %s at %s", config.RulesetFileName, absPath)
	}
	relPath, err := filepath.Rel(cfgDir, absPath)
	if err != nil {
		return "", "", err
	}
	relPath = filepath.ToSlash(relPath)
	if relPath == "" {
		relPath = "."
	}
	return absPath, relPath, nil
}

func profileDependencyForRead(dep config.Dependency) config.Dependency {
	out := dep
	if out.Export == "" {
		out.Export = "default"
	}
	return out
}

func buildLock(cfg config.Ruleset, cfgDir string, gc *git.Client) (config.Lockfile, []installResolvedRow, map[string]int, error) {
	lock := config.Lockfile{LockVersion: "0.1"}
	rows := make([]installResolvedRow, 0, len(cfg.Dependencies))
	counts := map[string]int{"git": 0, "local": 0, "profile": 0}
	for idx, dep := range cfg.Dependencies {
		source := dependencySource(dep)
		switch source {
		case "git":
			repoDir, err := gc.EnsureRepo(dep.URI)
			if err != nil {
				return lock, nil, nil, fmt.Errorf("prepare %s: %w", dep.URI, err)
			}
			res, err := gc.Resolve(repoDir, dep.Ref, dep.Version)
			if err != nil {
				return lock, nil, nil, fmt.Errorf("resolve %s: %w", dep.URI, err)
			}
			if _, err := pack.ExpandGitDependency(gc, repoDir, dep, config.LockedSource{Source: "git", URI: dep.URI, Commit: res.Commit, Export: dep.Export}); err != nil {
				return lock, nil, nil, err
			}
			lock.Resolved = append(lock.Resolved, config.LockedSource{Source: "git", URI: dep.URI, Requested: res.Requested, ResolvedVersion: res.ResolvedVersion, Commit: res.Commit, Export: dep.Export})
			rows = append(rows, installResolvedRow{Index: idx + 1, Source: "git", Ref: dep.URI, Export: dep.Export, Resolved: res.Requested, Hash: shortSHA(res.Commit)})
			counts["git"]++
		case "local":
			absLocalPath, relPath, err := resolveLocalPath(cfgDir, dep.Path)
			if err != nil {
				return lock, nil, nil, err
			}
			_, contentHash, err := pack.ExpandLocalDependency(absLocalPath, dep, "local")
			if err != nil {
				return lock, nil, nil, err
			}
			lock.Resolved = append(lock.Resolved, config.LockedSource{Source: "local", Path: relPath, Commit: "local", ContentHash: contentHash, Export: dep.Export})
			rows = append(rows, installResolvedRow{Index: idx + 1, Source: "local", Ref: relPath, Export: dep.Export, Resolved: "local", Hash: shortSHA(contentHash)})
			counts["local"]++
		case profilesvc.ProfileSource:
			if dep.Profile == "" {
				return lock, nil, nil, errors.New("profile source requires profile id")
			}
			meta, profileDir, err := profilesvc.ResolveIDOrAlias(dep.Profile)
			if err != nil {
				return lock, nil, nil, err
			}
			depRead := profileDependencyForRead(dep)
			_, contentHash, err := pack.ExpandProfileDependency(profileDir, depRead, profilesvc.ProfileCommit)
			if err != nil {
				return lock, nil, nil, err
			}
			lock.Resolved = append(lock.Resolved, config.LockedSource{Source: profilesvc.ProfileSource, Profile: meta.ID, Commit: profilesvc.ProfileCommit, ContentHash: contentHash, Export: depRead.Export})
			rows = append(rows, installResolvedRow{Index: idx + 1, Source: "profile", Ref: meta.ID, Export: depRead.Export, Resolved: "profile", Hash: shortSHA(contentHash)})
			counts["profile"]++
		default:
			return lock, nil, nil, fmt.Errorf("unsupported source %q", dep.Source)
		}
	}
	return lock, rows, counts, nil
}

func expandDependencyForSnapshot(cfgDir string, gc *git.Client, dep config.Dependency, locked config.LockedSource) ([]pack.Module, string, string, map[string]string, error) {
	source := dependencySource(dep)
	if source != lockSource(locked) {
		return nil, "", "", nil, errors.New("cannot save profile: dependency not installed; run rulepack install")
	}
	switch source {
	case "git":
		if dep.URI != locked.URI {
			return nil, "", "", nil, errors.New("cannot save profile: dependency not installed; run rulepack install")
		}
		repoDir, err := gc.EnsureRepo(dep.URI)
		if err != nil {
			return nil, "", "", nil, err
		}
		modules, err := pack.ExpandGitDependency(gc, repoDir, dep, locked)
		if err != nil {
			return nil, "", "", nil, err
		}
		hash := profilesvc.ComputeContentHash(modules, dep.Export)
		requestType := "head"
		if dep.Version != "" {
			requestType = "version"
		} else if dep.Ref != "" {
			requestType = "ref"
		}
		prov := map[string]string{
			"commit":          locked.Commit,
			"requested":       locked.Requested,
			"resolvedVersion": locked.ResolvedVersion,
			"requestType":     requestType,
		}
		return modules, hash, dep.URI, prov, nil
	case "local":
		absLocalPath, relPath, err := resolveLocalPath(cfgDir, dep.Path)
		if err != nil {
			return nil, "", "", nil, err
		}
		if locked.Path != "" && relPath != locked.Path {
			return nil, "", "", nil, errors.New("cannot save profile: dependency not installed; run rulepack install")
		}
		modules, hash, err := pack.ExpandLocalDependency(absLocalPath, dep, "local")
		if err != nil {
			return nil, "", "", nil, err
		}
		if locked.ContentHash != "" && hash != locked.ContentHash {
			return nil, "", "", nil, errors.New("cannot save profile: dependency not installed; run rulepack install")
		}
		prov := map[string]string{"path": relPath, "contentHash": hash}
		return modules, hash, absLocalPath, prov, nil
	case profilesvc.ProfileSource:
		profileRef := dep.Profile
		if profileRef == "" {
			profileRef = locked.Profile
		}
		meta, profileDir, err := profilesvc.ResolveIDOrAlias(profileRef)
		if err != nil {
			return nil, "", "", nil, err
		}
		if locked.Profile != "" && meta.ID != locked.Profile {
			return nil, "", "", nil, errors.New("cannot save profile: dependency not installed; run rulepack install")
		}
		depRead := profileDependencyForRead(dep)
		modules, hash, err := pack.ExpandProfileDependency(profileDir, depRead, profilesvc.ProfileCommit)
		if err != nil {
			return nil, "", "", nil, err
		}
		if locked.ContentHash != "" && hash != locked.ContentHash {
			return nil, "", "", nil, errors.New("cannot save profile: dependency not installed; run rulepack install")
		}
		prov := map[string]string{"profile": meta.ID, "contentHash": hash}
		return modules, hash, meta.ID, prov, nil
	default:
		return nil, "", "", nil, fmt.Errorf("unsupported source %q", dep.Source)
	}
}

func findDependencyIndex(cfg config.Ruleset, selector string) (int, error) {
	if selector == "" {
		return -1, errors.New("missing --dep selector")
	}
	if n, err := strconv.Atoi(selector); err == nil {
		if n >= 1 && n <= len(cfg.Dependencies) {
			return n - 1, nil
		}
		if n >= 0 && n < len(cfg.Dependencies) {
			return n, nil
		}
		return -1, fmt.Errorf("dependency index %d out of range", n)
	}
	index := -1
	for i, dep := range cfg.Dependencies {
		if dependencyReference(dep) == selector {
			if index != -1 {
				return -1, fmt.Errorf("selector %q matched multiple dependencies", selector)
			}
			index = i
		}
	}
	if index == -1 {
		return -1, fmt.Errorf("dependency selector %q not found", selector)
	}
	return index, nil
}

func dependencyReference(dep config.Dependency) string {
	switch dependencySource(dep) {
	case "git":
		return dep.URI
	case "local":
		return dep.Path
	case profilesvc.ProfileSource:
		return dep.Profile
	default:
		return ""
	}
}

func dependencyFromProfileMetadata(meta profilesvc.Metadata) (config.Dependency, error) {
	dep := config.Dependency{Source: meta.SourceType, Export: meta.SourceExport}
	switch meta.SourceType {
	case "git":
		dep.URI = meta.SourceRef
		requested := meta.Provenance["requested"]
		switch meta.Provenance["requestType"] {
		case "version":
			dep.Version = requested
		case "ref":
			dep.Ref = requested
		default:
			// Backward compatibility: old snapshots may not carry requestType.
			if requested != "" && requested != "HEAD" {
				dep.Ref = requested
			}
		}
	case "local":
		if !filepath.IsAbs(meta.SourceRef) {
			return config.Dependency{}, fmt.Errorf("profile %s local source is not absolute; cannot refresh safely", meta.ID)
		}
		dep.Path = meta.SourceRef
	case profilesvc.ProfileSource:
		dep.Profile = meta.SourceRef
	default:
		return config.Dependency{}, fmt.Errorf("unsupported profile source type %q", meta.SourceType)
	}
	return dep, nil
}

func resolveModulesForDependency(gc *git.Client, dep config.Dependency) ([]pack.Module, error) {
	switch dependencySource(dep) {
	case "git":
		repoDir, err := gc.EnsureRepo(dep.URI)
		if err != nil {
			return nil, err
		}
		res, err := gc.Resolve(repoDir, dep.Ref, dep.Version)
		if err != nil {
			return nil, err
		}
		return pack.ExpandGitDependency(gc, repoDir, dep, config.LockedSource{Source: "git", URI: dep.URI, Commit: res.Commit, Export: dep.Export})
	case "local":
		absPath := filepath.Clean(dep.Path)
		returnModules, _, err := pack.ExpandLocalDependency(absPath, dep, "local")
		return returnModules, err
	case profilesvc.ProfileSource:
		meta, profileDir, err := profilesvc.ResolveIDOrAlias(dep.Profile)
		if err != nil {
			return nil, err
		}
		depRead := profileDependencyForRead(config.Dependency{Source: profilesvc.ProfileSource, Profile: meta.ID, Export: "default"})
		mods, _, err := pack.ExpandProfileDependency(profileDir, depRead, profilesvc.ProfileCommit)
		return mods, err
	default:
		return nil, fmt.Errorf("unsupported source %q", dep.Source)
	}
}

func mergeRefreshedModules(current []pack.Module, fresh []pack.Module, rules []string) ([]pack.Module, []string, error) {
	if len(rules) == 0 {
		refreshed := make([]string, 0, len(fresh))
		for _, m := range fresh {
			refreshed = append(refreshed, m.ID)
		}
		build.Sort(fresh)
		return fresh, refreshed, nil
	}

	freshByID := make(map[string]pack.Module, len(fresh))
	for _, m := range fresh {
		freshByID[m.ID] = m
	}
	changed := map[string]struct{}{}
	out := make([]pack.Module, 0, len(current))
	for _, m := range current {
		if moduleMatchesAny(m.ID, rules) {
			newM, ok := freshByID[m.ID]
			if !ok {
				return nil, nil, fmt.Errorf("rule %s not found in refreshed source", m.ID)
			}
			out = append(out, newM)
			changed[m.ID] = struct{}{}
			continue
		}
		out = append(out, m)
	}
	for _, m := range fresh {
		if _, ok := changed[m.ID]; ok {
			continue
		}
		if moduleMatchesAny(m.ID, rules) {
			out = append(out, m)
			changed[m.ID] = struct{}{}
		}
	}
	if len(changed) == 0 {
		return nil, nil, errors.New("no rules matched --rule selectors")
	}
	build.Sort(out)
	refreshed := make([]string, 0, len(changed))
	for id := range changed {
		refreshed = append(refreshed, id)
	}
	buildSortStrings(refreshed)
	return out, refreshed, nil
}

func filterModulesByPatterns(modules []pack.Module, patterns []string) []pack.Module {
	if len(patterns) == 0 {
		return modules
	}
	out := make([]pack.Module, 0, len(modules))
	for _, m := range modules {
		if moduleMatchesAny(m.ID, patterns) {
			out = append(out, m)
		}
	}
	return out
}

func diffModules(current []pack.Module, fresh []pack.Module) ([]string, []string, []string) {
	currentByID := make(map[string]pack.Module, len(current))
	freshByID := make(map[string]pack.Module, len(fresh))
	for _, m := range current {
		currentByID[m.ID] = m
	}
	for _, m := range fresh {
		freshByID[m.ID] = m
	}
	changed := []string{}
	removed := []string{}
	for id, oldMod := range currentByID {
		newMod, ok := freshByID[id]
		if !ok {
			removed = append(removed, id)
			continue
		}
		if moduleDigest(oldMod) != moduleDigest(newMod) {
			changed = append(changed, id)
		}
	}
	added := []string{}
	for id := range freshByID {
		if _, ok := currentByID[id]; !ok {
			added = append(added, id)
		}
	}
	buildSortStrings(changed)
	buildSortStrings(added)
	buildSortStrings(removed)
	return changed, added, removed
}

func moduleDigest(m pack.Module) string {
	applyJSON, _ := json.Marshal(m.Apply)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%s", m.ID, m.Priority, m.Content, string(applyJSON))))
	return hex.EncodeToString(sum[:])
}

func moduleMatchesAny(id string, patterns []string) bool {
	for _, p := range patterns {
		if p == id || p == "*" || p == "**" {
			return true
		}
		matched, err := path.Match(p, id)
		if err == nil && matched {
			return true
		}
		if strings.HasSuffix(p, "*") && strings.HasPrefix(id, strings.TrimSuffix(p, "*")) {
			return true
		}
	}
	return false
}

func buildSortStrings(values []string) {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}

func boolToYesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

type templateFile struct {
	Path    string
	Content string
}

func initTemplate(name string, template string) (config.Ruleset, []templateFile, error) {
	cfg := config.DefaultRuleset(name)
	switch template {
	case "", "default":
		return cfg, nil, nil
	case "rulepack":
		cfg.Dependencies = []config.Dependency{{Source: "local", Path: ".rulepack/packs/rule-authoring", Export: "default"}}
		return cfg, []templateFile{
			{
				Path: ".rulepack/packs/rule-authoring/rulepack.json",
				Content: "{\n" +
					"  \"specVersion\": \"0.1\",\n" +
					"  \"name\": \"rule-authoring\",\n" +
					"  \"version\": \"0.1.0\",\n" +
					"  \"modules\": [\n" +
					"    {\n" +
					"      \"id\": \"authoring.basics\",\n" +
					"      \"path\": \"modules/authoring/basics.md\",\n" +
					"      \"priority\": 100\n" +
					"    },\n" +
					"    {\n" +
					"      \"id\": \"authoring.tests\",\n" +
					"      \"path\": \"modules/authoring/tests.md\",\n" +
					"      \"priority\": 110\n" +
					"    }\n" +
					"  ],\n" +
					"  \"exports\": {\n" +
					"    \"default\": {\n" +
					"      \"include\": [\"authoring.*\"]\n" +
					"    }\n" +
					"  }\n" +
					"}\n",
			},
			{Path: ".rulepack/packs/rule-authoring/modules/authoring/basics.md", Content: "# Rule Authoring Basics\n\n- Keep each rule scoped to one behavior.\n- Prefer examples that show correct and incorrect usage.\n- Write rules as actionable constraints, not abstract advice.\n"},
			{Path: ".rulepack/packs/rule-authoring/modules/authoring/tests.md", Content: "# Rule Authoring Testability\n\n- Add at least one acceptance criterion for each rule module.\n- Validate generated outputs in CI with deterministic checks.\n- Fail builds when local rule dependencies drift without reinstall.\n"},
		}, nil
	default:
		return config.Ruleset{}, nil, fmt.Errorf("unknown template %q (supported: rulepack)", template)
	}
}

func writeTemplateFiles(files []templateFile) error {
	for _, file := range files {
		if _, err := os.Stat(file.Path); err == nil {
			return fmt.Errorf("template file already exists: %s", file.Path)
		}
	}
	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file.Path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file.Path, []byte(file.Content), 0o644); err != nil {
			return err
		}
	}
	return nil
}
