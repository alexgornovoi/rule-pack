package profile

import (
	"strings"
	"testing"

	"rulepack/internal/pack"
)

func TestSaveListResolveRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	meta, err := SaveSnapshot(SaveInput{
		Alias: "py",
		Sources: []SourceSnapshot{{
			SourceType:   "git",
			SourceRef:    "https://example.com/a.git",
			SourceExport: "python",
			ModuleIDs:    []string{"a"},
		}},
		ContentHash: ComputeContentHash(sampleModules(), "python"),
		Modules:     sampleModules(),
	})
	if err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	if meta.ID == "" {
		t.Fatalf("expected saved profile id")
	}

	all, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected one profile, got %d", len(all))
	}

	resolved, _, err := ResolveIDOrAlias("py")
	if err != nil {
		t.Fatalf("ResolveIDOrAlias by alias: %v", err)
	}
	if resolved.ID != meta.ID {
		t.Fatalf("expected %s got %s", meta.ID, resolved.ID)
	}
}

func TestAliasCollision(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	hashA := ComputeContentHash(sampleModules(), "python")
	hashB := ComputeContentHash([]pack.Module{{ID: "b", Priority: 1, Content: "b\n"}}, "python")
	_, err := SaveSnapshot(SaveInput{
		Alias: "python",
		Sources: []SourceSnapshot{{
			SourceType:   "git",
			SourceRef:    "https://example.com/a.git",
			SourceExport: "python",
			ModuleIDs:    []string{"a"},
		}},
		ContentHash: hashA,
		Modules:     sampleModules(),
	})
	if err != nil {
		t.Fatalf("first SaveSnapshot: %v", err)
	}
	_, err = SaveSnapshot(SaveInput{
		Alias: "python",
		Sources: []SourceSnapshot{{
			SourceType:   "git",
			SourceRef:    "https://example.com/b.git",
			SourceExport: "python",
			ModuleIDs:    []string{"b"},
		}},
		ContentHash: hashB,
		Modules:     []pack.Module{{ID: "b", Priority: 1, Content: "b\n"}},
	})
	if err == nil {
		t.Fatalf("expected duplicate alias error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate alias error, got %v", err)
	}
}

func sampleModules() []pack.Module {
	return []pack.Module{
		{PackName: "x", PackVersion: "1.0.0", Commit: "abc", ID: "a", Priority: 10, Content: "a\n"},
	}
}
