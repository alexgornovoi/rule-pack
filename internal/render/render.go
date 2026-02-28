package render

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"rulepack/internal/config"
	"rulepack/internal/pack"
)

var sanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

const mergedManagedHeader = "<!-- rulepack:managed -->"

func WriteCursor(target config.TargetEntry, modules []pack.Module) error {
	ext := target.Ext
	if ext == "" {
		ext = ".mdc"
	}
	if target.OutDir == "" {
		target.OutDir = ".cursor/rules"
	}
	if err := os.MkdirAll(target.OutDir, 0o755); err != nil {
		return err
	}
	cursorModules := make([]pack.Module, 0, len(modules))
	for _, m := range modules {
		rule, err := resolveCursorApplyRule(m)
		if err != nil {
			return err
		}
		if rule.Mode == "never" {
			continue
		}
		cursorModules = append(cursorModules, m)
	}
	if target.PerModule {
		planned := make([]struct {
			module pack.Module
			rule   cursorApplyRule
			path   string
		}, 0, len(cursorModules))
		pathToModule := make(map[string]string, len(cursorModules))
		for _, m := range cursorModules {
			rule, err := resolveCursorApplyRule(m)
			if err != nil {
				return err
			}
			fullPath := targetModuleFullPath(target.OutDir, m, ext)
			if existingID, ok := pathToModule[fullPath]; ok {
				return fmt.Errorf("cursor output collision: modules %s and %s both map to %s", existingID, m.ID, fullPath)
			}
			pathToModule[fullPath] = m.ID
			planned = append(planned, struct {
				module pack.Module
				rule   cursorApplyRule
				path   string
			}{module: m, rule: rule, path: fullPath})
		}
		for _, item := range planned {
			if err := os.MkdirAll(filepath.Dir(item.path), 0o755); err != nil {
				return err
			}
			content, err := cursorPerModuleContent(ext, item.module, item.rule)
			if err != nil {
				return err
			}
			if err := os.WriteFile(item.path, []byte(normalize(content)), 0o644); err != nil {
				return err
			}
		}
		return nil
	}
	for _, m := range cursorModules {
		rule, err := resolveCursorApplyRule(m)
		if err != nil {
			return err
		}
		if rule.Mode == "glob" || rule.Mode == "agent" || rule.Mode == "manual" {
			return fmt.Errorf("cursor target with perModule=false does not support apply mode %q for module %s", rule.Mode, m.ID)
		}
	}
	if target.OutFile == "" {
		target.OutFile = filepath.Join(target.OutDir, "rules"+ext)
	}
	return os.WriteFile(target.OutFile, []byte(normalize(merge(cursorModules, true))), 0o644)
}

func CursorUnmanagedOverwrites(target config.TargetEntry, modules []pack.Module) ([]string, error) {
	writePaths, err := cursorWritePaths(target, modules)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(writePaths))
	for _, path := range writePaths {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !isRulepackManagedCursorContent(string(data)) {
			out = append(out, path)
		}
	}
	return out, nil
}

func WriteClaude(target config.TargetEntry, modules []pack.Module) error {
	if target.OutFile != "" {
		return fmt.Errorf("claude target does not support outFile; use outDir")
	}
	if !target.PerModule {
		return fmt.Errorf("claude target requires perModule=true")
	}
	ext := target.Ext
	if ext == "" {
		ext = ".md"
	}
	if target.OutDir == "" {
		target.OutDir = ".claude/rules"
	}
	if err := os.MkdirAll(target.OutDir, 0o755); err != nil {
		return err
	}
	pathToModule := make(map[string]string, len(modules))
	for _, m := range modules {
		rule, err := resolveClaudeApplyRule(m)
		if err != nil {
			return err
		}
		if rule.Mode == "never" {
			continue
		}
		fullPath := targetModuleFullPath(target.OutDir, m, ext)
		if existingID, ok := pathToModule[fullPath]; ok {
			return fmt.Errorf("claude output collision: modules %s and %s both map to %s", existingID, m.ID, fullPath)
		}
		pathToModule[fullPath] = m.ID
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return err
		}
		content := claudePerModuleContent(m, rule)
		if err := os.WriteFile(fullPath, []byte(normalize(content)), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func WriteMerged(outFile string, modules []pack.Module) error {
	if outFile == "" {
		return fmt.Errorf("missing output file")
	}
	if err := os.MkdirAll(filepath.Dir(outFile), 0o755); err != nil {
		return err
	}
	content := mergedManagedHeader + "\n" + normalize(merge(modules, false))
	return os.WriteFile(outFile, []byte(content), 0o644)
}

func PreviewManagedCleanup(targets map[string]config.TargetEntry) ([]string, []string, error) {
	if len(targets) == 0 {
		return nil, nil, nil
	}
	targetNames := make([]string, 0, len(targets))
	for name := range targets {
		targetNames = append(targetNames, name)
	}
	sort.Strings(targetNames)
	deletable := make([]string, 0)
	skipped := make([]string, 0)
	for _, name := range targetNames {
		entry := targets[name]
		var targetDelete []string
		var targetSkip []string
		var err error
		switch name {
		case "cursor":
			targetDelete, targetSkip, err = previewCursorCleanup(entry)
		case "copilot", "codex":
			targetDelete, targetSkip, err = previewMergedCleanup(entry)
		case "claude":
			targetDelete, targetSkip, err = previewClaudeCleanup(entry)
		default:
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		deletable = append(deletable, targetDelete...)
		skipped = append(skipped, targetSkip...)
	}
	sort.Strings(deletable)
	sort.Strings(skipped)
	return deletable, skipped, nil
}

func CleanupManagedOutputs(targets map[string]config.TargetEntry) ([]string, []string, error) {
	deletable, skipped, err := PreviewManagedCleanup(targets)
	if err != nil {
		return nil, nil, err
	}
	deleted := make([]string, 0, len(deletable))
	for _, p := range deletable {
		if err := os.Remove(p); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return deleted, skipped, err
		}
		deleted = append(deleted, p)
	}
	return deleted, skipped, nil
}

func merge(modules []pack.Module, includeProvenance bool) string {
	var b strings.Builder
	for i, m := range modules {
		if includeProvenance {
			b.WriteString(provenanceHeader(m))
			b.WriteString("\n")
		}
		b.WriteString(m.Content)
		if i != len(modules)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func provenanceHeader(m pack.Module) string {
	shortCommit := m.Commit
	if len(shortCommit) > 12 {
		shortCommit = shortCommit[:12]
	}
	return fmt.Sprintf("<!-- pack=%s version=%s commit=%s module=%s priority=%d -->", m.PackName, m.PackVersion, shortCommit, m.ID, m.Priority)
}

func sanitizeID(id string) string {
	id = strings.ReplaceAll(id, ".", "_")
	return sanitizeRe.ReplaceAllString(id, "_")
}

func normalizedModulePath(modulePath string) string {
	p := strings.ReplaceAll(strings.TrimSpace(modulePath), "\\", "/")
	p = strings.TrimPrefix(p, "./")
	p = path.Clean(p)
	if p == "." || p == "/" {
		return ""
	}
	if strings.HasPrefix(p, "modules/") {
		p = strings.TrimPrefix(p, "modules/")
	}
	if strings.HasPrefix(p, "../") || p == ".." {
		return ""
	}
	return strings.TrimPrefix(p, "/")
}

func nestedOutputDirFromModulePath(modulePath string) string {
	p := normalizedModulePath(modulePath)
	if p == "" {
		return ""
	}
	dir := path.Dir(p)
	if dir == "." || dir == "/" {
		return ""
	}
	return filepath.FromSlash(strings.TrimPrefix(dir, "/"))
}

func targetModuleFilename(module pack.Module, ext string) string {
	base := ""
	if module.Path != "" {
		moduleBase := path.Base(normalizedModulePath(module.Path))
		moduleBase = strings.TrimSuffix(moduleBase, path.Ext(moduleBase))
		base = moduleBase
	}
	if base == "" {
		base = sanitizeID(module.ID)
	}
	base = sanitizeID(base)
	return fmt.Sprintf("%03d-%s%s", module.Priority, base, ext)
}

func targetModuleFullPath(outDir string, module pack.Module, ext string) string {
	nested := nestedOutputDirFromModulePath(module.Path)
	name := targetModuleFilename(module, ext)
	if nested == "" {
		return filepath.Join(outDir, name)
	}
	return filepath.Join(outDir, nested, name)
}

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimRight(s, "\n") + "\n"
}

type cursorApplyRule struct {
	Mode        string
	Description string
	Globs       []string
}

type claudeApplyRule struct {
	Mode  string
	Globs []string
}

func resolveCursorApplyRule(m pack.Module) (cursorApplyRule, error) {
	var rule pack.ApplyRule
	if targetRule, ok := m.Apply.Targets["cursor"]; ok {
		rule = targetRule
	} else if m.Apply.Default != nil {
		rule = *m.Apply.Default
	}

	mode := strings.ToLower(strings.TrimSpace(rule.Mode))
	if mode == "" {
		mode = "always"
	}
	out := cursorApplyRule{
		Mode:        mode,
		Description: strings.TrimSpace(rule.Description),
		Globs:       append([]string(nil), rule.Globs...),
	}

	switch mode {
	case "always", "never", "agent", "glob", "manual":
	default:
		return cursorApplyRule{}, fmt.Errorf("unsupported cursor apply mode %q for module %s", rule.Mode, m.ID)
	}

	if mode == "glob" && len(out.Globs) == 0 {
		return cursorApplyRule{}, fmt.Errorf("cursor apply mode glob requires globs for module %s", m.ID)
	}
	if mode == "always" || mode == "never" {
		out.Globs = nil
	}
	return out, nil
}

func cursorPerModuleContent(ext string, m pack.Module, rule cursorApplyRule) (string, error) {
	var b strings.Builder
	if strings.EqualFold(ext, ".mdc") {
		b.WriteString(cursorFrontmatter(rule, m))
		b.WriteString("\n")
	}
	b.WriteString(provenanceHeader(m))
	b.WriteString("\n")
	b.WriteString(m.Content)
	return b.String(), nil
}

func resolveClaudeApplyRule(m pack.Module) (claudeApplyRule, error) {
	var rule pack.ApplyRule
	if targetRule, ok := m.Apply.Targets["claude"]; ok {
		rule = targetRule
	} else if m.Apply.Default != nil {
		rule = *m.Apply.Default
	}
	mode := strings.ToLower(strings.TrimSpace(rule.Mode))
	if mode == "" {
		mode = "always"
	}
	out := claudeApplyRule{
		Mode:  mode,
		Globs: append([]string(nil), rule.Globs...),
	}
	switch mode {
	case "always", "never", "agent", "glob", "manual":
	default:
		return claudeApplyRule{}, fmt.Errorf("unsupported claude apply mode %q for module %s", rule.Mode, m.ID)
	}
	if mode == "glob" && len(out.Globs) == 0 {
		return claudeApplyRule{}, fmt.Errorf("claude apply mode glob requires globs for module %s", m.ID)
	}
	if mode == "always" || mode == "never" || mode == "agent" || mode == "manual" {
		out.Globs = nil
	}
	return out, nil
}

func claudePerModuleContent(m pack.Module, rule claudeApplyRule) string {
	var b strings.Builder
	if rule.Mode == "glob" {
		b.WriteString("---\n")
		b.WriteString("paths:\n")
		globs := append([]string(nil), rule.Globs...)
		sort.Strings(globs)
		for _, g := range globs {
			b.WriteString("  - ")
			b.WriteString(quoteYAML(g))
			b.WriteString("\n")
		}
		b.WriteString("---\n")
		b.WriteString("\n")
	}
	b.WriteString(provenanceHeader(m))
	b.WriteString("\n")
	b.WriteString(m.Content)
	return b.String()
}

func cursorWritePaths(target config.TargetEntry, modules []pack.Module) ([]string, error) {
	ext := target.Ext
	if ext == "" {
		ext = ".mdc"
	}
	if target.OutDir == "" {
		target.OutDir = ".cursor/rules"
	}
	cursorModules := make([]pack.Module, 0, len(modules))
	for _, m := range modules {
		rule, err := resolveCursorApplyRule(m)
		if err != nil {
			return nil, err
		}
		if rule.Mode == "never" {
			continue
		}
		cursorModules = append(cursorModules, m)
	}
	if target.PerModule {
		out := make([]string, 0, len(cursorModules))
		pathToModule := make(map[string]string, len(cursorModules))
		for _, m := range cursorModules {
			fullPath := targetModuleFullPath(target.OutDir, m, ext)
			if existingID, ok := pathToModule[fullPath]; ok {
				return nil, fmt.Errorf("cursor output collision: modules %s and %s both map to %s", existingID, m.ID, fullPath)
			}
			pathToModule[fullPath] = m.ID
			out = append(out, fullPath)
		}
		return out, nil
	}
	for _, m := range cursorModules {
		rule, err := resolveCursorApplyRule(m)
		if err != nil {
			return nil, err
		}
		if rule.Mode == "glob" || rule.Mode == "agent" || rule.Mode == "manual" {
			return nil, fmt.Errorf("cursor target with perModule=false does not support apply mode %q for module %s", rule.Mode, m.ID)
		}
	}
	if target.OutFile == "" {
		target.OutFile = filepath.Join(target.OutDir, "rules"+ext)
	}
	return []string{target.OutFile}, nil
}

func isRulepackManagedCursorContent(content string) bool {
	return strings.Contains(content, "<!-- pack=") &&
		strings.Contains(content, " module=") &&
		strings.Contains(content, " priority=")
}

func isRulepackManagedMergedContent(content string) bool {
	return strings.HasPrefix(content, mergedManagedHeader)
}

func previewCursorCleanup(target config.TargetEntry) ([]string, []string, error) {
	ext := target.Ext
	if ext == "" {
		ext = ".mdc"
	}
	if target.OutDir == "" {
		target.OutDir = ".cursor/rules"
	}
	if target.PerModule {
		return previewPerModuleCleanup(target.OutDir, ext, isRulepackManagedCursorContent)
	}
	if target.OutFile == "" {
		target.OutFile = filepath.Join(target.OutDir, "rules"+ext)
	}
	data, err := os.ReadFile(target.OutFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	if isRulepackManagedCursorContent(string(data)) {
		return []string{target.OutFile}, nil, nil
	}
	return nil, []string{target.OutFile}, nil
}

func previewMergedCleanup(target config.TargetEntry) ([]string, []string, error) {
	if target.OutFile == "" {
		return nil, nil, nil
	}
	data, err := os.ReadFile(target.OutFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	if isRulepackManagedMergedContent(string(data)) {
		return []string{target.OutFile}, nil, nil
	}
	return nil, []string{target.OutFile}, nil
}

func previewClaudeCleanup(target config.TargetEntry) ([]string, []string, error) {
	ext := target.Ext
	if ext == "" {
		ext = ".md"
	}
	if target.OutDir == "" {
		target.OutDir = ".claude/rules"
	}
	return previewPerModuleCleanup(target.OutDir, ext, isRulepackManagedCursorContent)
}

func previewPerModuleCleanup(outDir, ext string, isManaged func(string) bool) ([]string, []string, error) {
	info, err := os.Stat(outDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, nil
	}
	deletable := make([]string, 0)
	skipped := make([]string, 0)
	if err := filepath.WalkDir(outDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || filepath.Ext(d.Name()) != ext {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if isManaged(string(data)) {
			deletable = append(deletable, p)
			return nil
		}
		skipped = append(skipped, p)
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return deletable, skipped, nil
}

func cursorFrontmatter(rule cursorApplyRule, m pack.Module) string {
	var b strings.Builder
	b.WriteString("---\n")
	switch rule.Mode {
	case "always":
		b.WriteString("alwaysApply: true\n")
	case "agent":
		b.WriteString("alwaysApply: false\n")
		desc := rule.Description
		if desc == "" {
			desc = "Apply when relevant: " + m.ID
		}
		b.WriteString("description: ")
		b.WriteString(quoteYAML(desc))
		b.WriteString("\n")
	case "glob":
		b.WriteString("alwaysApply: false\n")
		if rule.Description != "" {
			b.WriteString("description: ")
			b.WriteString(quoteYAML(rule.Description))
			b.WriteString("\n")
		}
		b.WriteString("globs:\n")
		globs := append([]string(nil), rule.Globs...)
		sort.Strings(globs)
		for _, g := range globs {
			b.WriteString("  - ")
			b.WriteString(quoteYAML(g))
			b.WriteString("\n")
		}
	case "manual":
		b.WriteString("alwaysApply: false\n")
		desc := rule.Description
		if desc == "" {
			desc = "Apply manually via @ mention: " + m.ID
		}
		b.WriteString("description: ")
		b.WriteString(quoteYAML(desc))
		b.WriteString("\n")
	default:
		b.WriteString("alwaysApply: true\n")
	}
	b.WriteString("---\n")
	return b.String()
}

func quoteYAML(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return "\"" + s + "\""
}
