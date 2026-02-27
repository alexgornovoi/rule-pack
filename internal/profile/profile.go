package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"rulepack/internal/pack"
)

const (
	ProfileSource = "profile"
	ProfileCommit = "profile"
)

type Metadata struct {
	ID          string           `json:"id"`
	Alias       string           `json:"alias,omitempty"`
	Sources     []SourceSnapshot `json:"sources"`
	CreatedAt   string           `json:"createdAt"`
	ContentHash string           `json:"contentHash"`
	ModuleCount int              `json:"moduleCount"`
}

type SourceSnapshot struct {
	SourceType   string            `json:"sourceType"`
	SourceRef    string            `json:"sourceRef"`
	SourceExport string            `json:"sourceExport,omitempty"`
	Provenance   map[string]string `json:"provenance,omitempty"`
	ModuleIDs    []string          `json:"moduleIds,omitempty"`
}

type SaveInput struct {
	ID          string
	Alias       string
	Sources     []SourceSnapshot
	ContentHash string
	Modules     []pack.Module
}

func GlobalRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".rulepack", "profiles"), nil
}

func SaveSnapshot(input SaveInput) (Metadata, error) {
	root, err := GlobalRoot()
	if err != nil {
		return Metadata{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Metadata{}, err
	}
	if input.ContentHash == "" {
		return Metadata{}, errors.New("missing profile content hash")
	}
	if len(input.Sources) == 0 {
		return Metadata{}, errors.New("missing profile sources")
	}
	id := input.ID
	if id == "" {
		id = buildID(input.Sources, input.ContentHash)
	}
	profileDir := filepath.Join(root, id)
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return Metadata{}, err
	}

	modules := make([]snapshotModule, 0, len(input.Modules))
	for _, m := range input.Modules {
		name := fmt.Sprintf("%03d-%s.md", m.Priority, sanitizeID(m.ID))
		relPath := filepath.ToSlash(filepath.Join("modules", name))
		fullPath := filepath.Join(profileDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return Metadata{}, err
		}
		if err := os.WriteFile(fullPath, []byte(m.Content), 0o644); err != nil {
			return Metadata{}, err
		}
		modules = append(modules, snapshotModule{
			ID:       m.ID,
			Path:     relPath,
			Priority: m.Priority,
			Apply:    m.Apply,
		})
	}
	sort.Slice(modules, func(i, j int) bool {
		if modules[i].Priority == modules[j].Priority {
			return modules[i].ID < modules[j].ID
		}
		return modules[i].Priority < modules[j].Priority
	})
	rp := snapshotRulepack{
		SpecVersion: "0.1",
		Name:        "saved-profile-" + id,
		Version:     "1.0.0",
		Modules:     modules,
		Exports: map[string]snapshotExport{
			"default": {Include: []string{"**"}},
		},
	}
	if err := writeJSON(filepath.Join(profileDir, "rulepack.json"), rp); err != nil {
		return Metadata{}, err
	}

	meta := Metadata{
		ID:          id,
		Alias:       input.Alias,
		Sources:     input.Sources,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		ContentHash: input.ContentHash,
		ModuleCount: len(input.Modules),
	}
	metaPath := filepath.Join(profileDir, "profile.json")
	if _, err := os.Stat(metaPath); err == nil {
		existing, readErr := readProfile(profileDir)
		if readErr == nil {
			// Preserve original creation time/metadata for deterministic IDs.
			meta.CreatedAt = existing.CreatedAt
			if input.Alias == "" {
				meta.Alias = existing.Alias
			}
		}
	}
	if err := ensureAliasUnique(root, meta.Alias, meta.ID); err != nil {
		return Metadata{}, err
	}
	if err := writeJSON(metaPath, meta); err != nil {
		return Metadata{}, err
	}
	return meta, nil
}

func List() ([]Metadata, error) {
	root, err := GlobalRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Metadata, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := readProfile(filepath.Join(root, entry.Name()))
		if err != nil {
			continue
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func ResolveIDOrAlias(ref string) (Metadata, string, error) {
	root, err := GlobalRoot()
	if err != nil {
		return Metadata{}, "", err
	}
	directPath := filepath.Join(root, ref)
	if meta, err := readProfile(directPath); err == nil {
		return meta, directPath, nil
	} else if _, statErr := os.Stat(directPath); statErr == nil {
		return Metadata{}, "", err
	}

	all, err := List()
	if err != nil {
		return Metadata{}, "", err
	}
	matches := make([]Metadata, 0, 1)
	for _, entry := range all {
		if entry.Alias == ref {
			matches = append(matches, entry)
		}
	}
	if len(matches) == 0 {
		entries, err := os.ReadDir(root)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				profileDir := filepath.Join(root, entry.Name())
				_, readErr := readProfile(profileDir)
				if readErr == nil {
					continue
				}
				if !strings.Contains(readErr.Error(), "unsupported profile format") {
					continue
				}
				alias, aliasErr := readProfileAlias(profileDir)
				if aliasErr == nil && alias == ref {
					return Metadata{}, "", readErr
				}
			}
		}
	}
	if len(matches) == 0 {
		return Metadata{}, "", fmt.Errorf("profile %q not found locally", ref)
	}
	if len(matches) > 1 {
		return Metadata{}, "", fmt.Errorf("alias %q resolves to multiple profiles", ref)
	}
	return matches[0], filepath.Join(root, matches[0].ID), nil
}

func Remove(ref string) (Metadata, string, error) {
	meta, profileDir, err := ResolveIDOrAlias(ref)
	if err != nil {
		return Metadata{}, "", err
	}
	if err := os.RemoveAll(profileDir); err != nil {
		return Metadata{}, "", err
	}
	return meta, profileDir, nil
}

func RemoveAll() ([]Metadata, error) {
	root, err := GlobalRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	removed := make([]Metadata, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		profileDir := filepath.Join(root, entry.Name())
		meta, err := readProfile(profileDir)
		if err != nil {
			continue
		}
		if err := os.RemoveAll(profileDir); err != nil {
			return nil, err
		}
		removed = append(removed, meta)
	}
	sort.Slice(removed, func(i, j int) bool { return removed[i].ID < removed[j].ID })
	return removed, nil
}

func buildID(sources []SourceSnapshot, contentHash string) string {
	keys := make([]string, 0, len(sources))
	for _, s := range sources {
		keys = append(keys, s.SourceType+"|"+s.SourceRef+"|"+s.SourceExport)
	}
	sort.Strings(keys)
	sourceDigest := sha256.Sum256([]byte(strings.Join(keys, ";")))
	sourcePrefix := hex.EncodeToString(sourceDigest[:])[:12]
	hashPrefix := contentHash
	if len(hashPrefix) > 8 {
		hashPrefix = hashPrefix[:8]
	}
	return sourcePrefix + "__default__" + hashPrefix
}

func ComputeContentHash(modules []pack.Module, export string) string {
	type item struct {
		ID          string
		Priority    int
		Content     string
		PackName    string
		PackVersion string
		Commit      string
		Apply       string
	}
	items := make([]item, 0, len(modules))
	for _, m := range modules {
		applyJSON, _ := json.Marshal(m.Apply)
		items = append(items, item{
			ID:          m.ID,
			Priority:    m.Priority,
			Content:     m.Content,
			PackName:    m.PackName,
			PackVersion: m.PackVersion,
			Commit:      m.Commit,
			Apply:       string(applyJSON),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Priority == items[j].Priority {
			return items[i].ID < items[j].ID
		}
		return items[i].Priority < items[j].Priority
	})
	var b strings.Builder
	b.WriteString("export:")
	b.WriteString(export)
	for _, it := range items {
		b.WriteString("\nmodule:")
		b.WriteString(it.ID)
		b.WriteString("\npriority:")
		b.WriteString(fmt.Sprintf("%d", it.Priority))
		b.WriteString("\npack:")
		b.WriteString(it.PackName)
		b.WriteString("\nversion:")
		b.WriteString(it.PackVersion)
		b.WriteString("\ncommit:")
		b.WriteString(it.Commit)
		b.WriteString("\ncontent:\n")
		b.WriteString(it.Content)
		b.WriteString("\napply:\n")
		b.WriteString(it.Apply)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func sanitizeID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "default"
	}
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, " ", "_")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return b.String()
}

func readProfile(profileDir string) (Metadata, error) {
	bytes, err := os.ReadFile(filepath.Join(profileDir, "profile.json"))
	if err != nil {
		return Metadata{}, err
	}
	var meta Metadata
	if err := json.Unmarshal(bytes, &meta); err != nil {
		return Metadata{}, err
	}
	if meta.ID == "" {
		return Metadata{}, errors.New("invalid profile metadata")
	}
	if len(meta.Sources) == 0 {
		return Metadata{}, errors.New("unsupported profile format: missing sources; re-save profile with current CLI")
	}
	return meta, nil
}

func ensureAliasUnique(root, alias, currentID string) error {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := readProfile(filepath.Join(root, entry.Name()))
		if err != nil {
			continue
		}
		if meta.ID == currentID {
			continue
		}
		if meta.Alias == alias {
			return fmt.Errorf("alias %q already exists; choose a different alias", alias)
		}
	}
	return nil
}

func readProfileAlias(profileDir string) (string, error) {
	bytes, err := os.ReadFile(filepath.Join(profileDir, "profile.json"))
	if err != nil {
		return "", err
	}
	var payload struct {
		Alias string `json:"alias"`
	}
	if err := json.Unmarshal(bytes, &payload); err != nil {
		return "", err
	}
	return payload.Alias, nil
}

func writeJSON(path string, value any) error {
	bytes, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	bytes = append(bytes, '\n')
	return os.WriteFile(path, bytes, 0o644)
}

type snapshotRulepack struct {
	SpecVersion string                    `json:"specVersion"`
	Name        string                    `json:"name"`
	Version     string                    `json:"version"`
	Modules     []snapshotModule          `json:"modules"`
	Exports     map[string]snapshotExport `json:"exports,omitempty"`
}

type snapshotModule struct {
	ID       string           `json:"id"`
	Path     string           `json:"path"`
	Priority int              `json:"priority"`
	Apply    pack.ApplyConfig `json:"apply,omitempty"`
}

type snapshotExport struct {
	Include []string `json:"include,omitempty"`
}
