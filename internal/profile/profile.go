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
	ID           string            `json:"id"`
	Alias        string            `json:"alias,omitempty"`
	SourceType   string            `json:"sourceType"`
	SourceRef    string            `json:"sourceRef"`
	SourceExport string            `json:"sourceExport,omitempty"`
	CreatedAt    string            `json:"createdAt"`
	ContentHash  string            `json:"contentHash"`
	ModuleCount  int               `json:"moduleCount"`
	Provenance   map[string]string `json:"provenance,omitempty"`
}

type SaveInput struct {
	ID           string
	Alias        string
	SourceType   string
	SourceRef    string
	SourceExport string
	ContentHash  string
	Modules      []pack.Module
	Provenance   map[string]string
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
	id := input.ID
	if id == "" {
		id = buildID(input.SourceType, input.SourceRef, input.SourceExport, input.ContentHash)
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
		ID:           id,
		Alias:        input.Alias,
		SourceType:   input.SourceType,
		SourceRef:    input.SourceRef,
		SourceExport: input.SourceExport,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		ContentHash:  input.ContentHash,
		ModuleCount:  len(input.Modules),
		Provenance:   input.Provenance,
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
	}

	all, err := List()
	if err != nil {
		return Metadata{}, "", err
	}
	matches := make([]Metadata, 0, 1)
	for _, meta := range all {
		if meta.Alias == ref {
			matches = append(matches, meta)
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

func buildID(sourceType, sourceRef, sourceExport, contentHash string) string {
	export := sourceExport
	if export == "" {
		export = "default"
	}
	sourceDigest := sha256.Sum256([]byte(sourceType + "|" + sourceRef))
	sourcePrefix := hex.EncodeToString(sourceDigest[:])[:12]
	hashPrefix := contentHash
	if len(hashPrefix) > 8 {
		hashPrefix = hashPrefix[:8]
	}
	return sourcePrefix + "__" + sanitizeID(export) + "__" + hashPrefix
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
	return meta, nil
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
	ID       string `json:"id"`
	Path     string `json:"path"`
	Priority int    `json:"priority"`
	Apply    pack.ApplyConfig `json:"apply,omitempty"`
}

type snapshotExport struct {
	Include []string `json:"include,omitempty"`
}
