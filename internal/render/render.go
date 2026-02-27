package render

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"rulepack/internal/config"
	"rulepack/internal/pack"
)

var sanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

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
		for _, m := range cursorModules {
			rule, err := resolveCursorApplyRule(m)
			if err != nil {
				return err
			}
			name := fmt.Sprintf("%03d-%s%s", m.Priority, sanitizeID(m.ID), ext)
			fullPath := filepath.Join(target.OutDir, name)
			content, err := cursorPerModuleContent(ext, m, rule)
			if err != nil {
				return err
			}
			if err := os.WriteFile(fullPath, []byte(normalize(content)), 0o644); err != nil {
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

func WriteMerged(outFile string, modules []pack.Module) error {
	if outFile == "" {
		return fmt.Errorf("missing output file")
	}
	if err := os.MkdirAll(filepath.Dir(outFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outFile, []byte(normalize(merge(modules, false))), 0o644)
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
