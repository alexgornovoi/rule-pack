package pack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"rulepack/internal/config"
	"rulepack/internal/git"
)

type RulePack struct {
	SpecVersion string                    `json:"specVersion"`
	Name        string                    `json:"name"`
	Version     string                    `json:"version"`
	Modules     []ModuleEntry             `json:"modules"`
	Exports     map[string]ExportSelector `json:"exports,omitempty"`
}

type ModuleEntry struct {
	ID        string   `json:"id"`
	Path      string   `json:"path"`
	Priority  int      `json:"priority"`
	AppliesTo []string `json:"appliesTo,omitempty"`
	Apply     ApplyConfig `json:"apply,omitempty"`
}

type ExportSelector struct {
	Include   []string `json:"include,omitempty"`
	Folders   []string `json:"folders,omitempty"`
	AppliesTo []string `json:"appliesTo,omitempty"`
}

type ApplyConfig struct {
	Default *ApplyRule          `json:"default,omitempty"`
	Targets map[string]ApplyRule `json:"targets,omitempty"`
}

type ApplyRule struct {
	Mode        string   `json:"mode,omitempty"`
	Description string   `json:"description,omitempty"`
	Globs       []string `json:"globs,omitempty"`
}

type Module struct {
	PackName    string
	PackVersion string
	Commit      string
	ID          string
	Priority    int
	Content     string
	Apply       ApplyConfig
}

type fileReader interface {
	ReadFile(path string) ([]byte, error)
}

type gitFileReader struct {
	client  *git.Client
	repoDir string
	commit  string
}

func (r gitFileReader) ReadFile(filePath string) ([]byte, error) {
	return r.client.ShowFile(r.repoDir, r.commit, filePath)
}

type localFileReader struct {
	root string
}

func (r localFileReader) ReadFile(filePath string) ([]byte, error) {
	fullPath, err := safeJoinPath(r.root, filePath)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(fullPath)
}

func ExpandGitDependency(gc *git.Client, repoDir string, dep config.Dependency, lock config.LockedSource) ([]Module, error) {
	reader := gitFileReader{client: gc, repoDir: repoDir, commit: lock.Commit}
	return expandDependency(reader, dep, lock.Commit)
}

func ExpandLocalDependency(localRoot string, dep config.Dependency, commit string) ([]Module, string, error) {
	reader := localFileReader{root: localRoot}
	modules, hash, err := expandDependencyWithHash(reader, dep, commit)
	if err != nil {
		return nil, "", err
	}
	return modules, hash, nil
}

func ExpandProfileDependency(profileRoot string, dep config.Dependency, commit string) ([]Module, string, error) {
	reader := localFileReader{root: profileRoot}
	modules, hash, err := expandDependencyWithHash(reader, dep, commit)
	if err != nil {
		return nil, "", err
	}
	return modules, hash, nil
}

func expandDependency(reader fileReader, dep config.Dependency, commit string) ([]Module, error) {
	modules, _, err := expandDependencyWithHash(reader, dep, commit)
	return modules, err
}

func expandDependencyWithHash(reader fileReader, dep config.Dependency, commit string) ([]Module, string, error) {
	rp, err := loadRulePack(reader)
	if err != nil {
		return nil, "", err
	}
	selector, err := exportSelector(rp, dep.Export)
	if err != nil {
		return nil, "", err
	}

	selected := selectModules(rp.Modules, selector)
	mods := make([]Module, 0, len(selected))
	hashState := hashState{
		packName:    rp.Name,
		packVersion: rp.Version,
		export:      dep.Export,
	}

	for _, m := range selected {
		bytes, err := reader.ReadFile(m.Path)
		if err != nil {
			return nil, "", fmt.Errorf("read module %s (%s): %w", m.ID, m.Path, err)
		}
		content := normalizeNewlines(string(bytes))
		mods = append(mods, Module{
			PackName:    rp.Name,
			PackVersion: rp.Version,
			Commit:      commit,
			ID:          m.ID,
			Priority:    m.Priority,
			Content:     content,
			Apply:       m.Apply,
		})
		applyJSON, err := json.Marshal(m.Apply)
		if err != nil {
			return nil, "", fmt.Errorf("marshal apply metadata for module %s: %w", m.ID, err)
		}
		hashState.modules = append(hashState.modules, hashedModule{
			ID:       m.ID,
			Path:     m.Path,
			Priority: m.Priority,
			Content:  content,
			Apply:    string(applyJSON),
		})
	}

	return mods, hashState.sum(), nil
}

func loadRulePack(reader fileReader) (RulePack, error) {
	var rp RulePack
	content, err := reader.ReadFile("rulepack.json")
	if err != nil {
		return rp, fmt.Errorf("read rulepack.json: %w", err)
	}
	if err := json.Unmarshal(content, &rp); err != nil {
		return rp, fmt.Errorf("parse rulepack.json: %w", err)
	}
	if rp.SpecVersion == "" || rp.Name == "" || rp.Version == "" {
		return rp, fmt.Errorf("invalid rulepack metadata")
	}
	return rp, nil
}

func exportSelector(rp RulePack, name string) (ExportSelector, error) {
	if name == "" {
		if exp, ok := rp.Exports["default"]; ok {
			return exp, nil
		}
		return ExportSelector{Include: []string{"**"}}, nil
	}
	exp, ok := rp.Exports[name]
	if !ok {
		// Convenience fallback: if no named export exists, allow export-by-folder.
		// Example: --export standards selects modules under modules/standards/.
		if hasModulesInFolder(rp.Modules, name) {
			return ExportSelector{Folders: []string{name}}, nil
		}
		return ExportSelector{}, fmt.Errorf("missing export %q in %s", name, rp.Name)
	}
	return exp, nil
}

func selectModules(modules []ModuleEntry, selector ExportSelector) []ModuleEntry {
	include := selector.Include
	folders := normalizeFolders(selector.Folders)
	if len(include) == 0 && len(folders) == 0 {
		include = []string{"**"}
	}
	applies := make(map[string]struct{}, len(selector.AppliesTo))
	for _, key := range selector.AppliesTo {
		applies[key] = struct{}{}
	}
	out := make([]ModuleEntry, 0, len(modules))
	for _, m := range modules {
		if !matchesAny(m.ID, include) && !matchesAnyFolder(m.Path, folders) {
			continue
		}
		if len(applies) > 0 && len(m.AppliesTo) > 0 && !intersects(m.AppliesTo, applies) {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority == out[j].Priority {
			return out[i].ID < out[j].ID
		}
		return out[i].Priority < out[j].Priority
	})
	return out
}

func matchesAny(id string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern == "**" || pattern == "*" {
			return true
		}
		matched, err := path.Match(pattern, id)
		if err == nil && matched {
			return true
		}
		if strings.HasSuffix(pattern, "*") && strings.HasPrefix(id, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}

func intersects(values []string, want map[string]struct{}) bool {
	for _, value := range values {
		if _, ok := want[value]; ok {
			return true
		}
	}
	return false
}

func hasModulesInFolder(modules []ModuleEntry, folder string) bool {
	folders := normalizeFolders([]string{folder})
	for _, m := range modules {
		if matchesAnyFolder(m.Path, folders) {
			return true
		}
	}
	return false
}

func normalizeFolders(folders []string) []string {
	out := make([]string, 0, len(folders))
	for _, raw := range folders {
		folder := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
		folder = strings.Trim(folder, "/")
		if folder == "" {
			continue
		}
		if strings.Contains(folder, ".") && !strings.Contains(folder, "/") {
			folder = strings.ReplaceAll(folder, ".", "/")
		}
		out = append(out, folder)
	}
	return out
}

func matchesAnyFolder(modulePath string, folders []string) bool {
	if len(folders) == 0 {
		return false
	}
	modulePath = strings.TrimSpace(strings.ReplaceAll(modulePath, "\\", "/"))
	modulePath = strings.Trim(modulePath, "/")
	for _, folder := range folders {
		modulePrefix := "modules/" + folder
		if modulePath == modulePrefix || strings.HasPrefix(modulePath, modulePrefix+"/") {
			return true
		}
		if modulePath == folder || strings.HasPrefix(modulePath, folder+"/") {
			return true
		}
	}
	return false
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimRight(s, "\n") + "\n"
}

func safeJoinPath(root, relativePath string) (string, error) {
	cleanPath := filepath.Clean(filepath.FromSlash(relativePath))
	fullPath := filepath.Join(root, cleanPath)
	rel, err := filepath.Rel(root, fullPath)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes rulepack root", relativePath)
	}
	return fullPath, nil
}

type hashState struct {
	packName    string
	packVersion string
	export      string
	modules     []hashedModule
}

type hashedModule struct {
	ID       string
	Path     string
	Priority int
	Content  string
	Apply    string
}

func (h hashState) sum() string {
	var b strings.Builder
	b.WriteString("pack:")
	b.WriteString(h.packName)
	b.WriteString("\nversion:")
	b.WriteString(h.packVersion)
	b.WriteString("\nexport:")
	b.WriteString(h.export)
	for _, m := range h.modules {
		b.WriteString("\nmodule:")
		b.WriteString(m.ID)
		b.WriteString("\npath:")
		b.WriteString(m.Path)
		b.WriteString("\npriority:")
		b.WriteString(fmt.Sprintf("%d", m.Priority))
		b.WriteString("\ncontent:\n")
		b.WriteString(m.Content)
		b.WriteString("\napply:\n")
		b.WriteString(m.Apply)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
