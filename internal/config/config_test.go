package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRulesetDependencyValidation(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr string
	}{
		{
			name: "valid git dependency",
			json: `{"specVersion":"0.1","name":"x","dependencies":[{"source":"git","uri":"https://example.com/a.git","version":"^1.0.0"}]}`,
		},
		{
			name:    "missing source rejected",
			json:    `{"specVersion":"0.1","name":"x","dependencies":[{"uri":"https://example.com/a.git"}]}`,
			wantErr: "source is required",
		},
		{
			name: "valid local dependency",
			json: `{"specVersion":"0.1","name":"x","dependencies":[{"source":"local","path":"../rules","export":"default"}]}`,
		},
		{
			name:    "local missing path",
			json:    `{"specVersion":"0.1","name":"x","dependencies":[{"source":"local"}]}`,
			wantErr: "local source requires path",
		},
		{
			name:    "local with uri rejected",
			json:    `{"specVersion":"0.1","name":"x","dependencies":[{"source":"local","path":"../rules","uri":"https://example.com/a.git"}]}`,
			wantErr: "local source does not support uri/profile",
		},
		{
			name:    "git missing uri",
			json:    `{"specVersion":"0.1","name":"x","dependencies":[{"source":"git"}]}`,
			wantErr: "git source requires uri",
		},
		{
			name:    "git with path rejected",
			json:    `{"specVersion":"0.1","name":"x","dependencies":[{"source":"git","uri":"https://example.com/a.git","path":"../x"}]}`,
			wantErr: "git source does not support path/profile",
		},
		{
			name: "valid profile dependency",
			json: `{"specVersion":"0.1","name":"x","dependencies":[{"source":"profile","profile":"abc123__python__01"}]}`,
		},
		{
			name:    "profile missing id",
			json:    `{"specVersion":"0.1","name":"x","dependencies":[{"source":"profile"}]}`,
			wantErr: "profile source requires profile id",
		},
		{
			name:    "profile with local path rejected",
			json:    `{"specVersion":"0.1","name":"x","dependencies":[{"source":"profile","profile":"abc","path":"../x"}]}`,
			wantErr: "profile source does not support uri/path",
		},
		{
			name:    "git version and ref conflict",
			json:    `{"specVersion":"0.1","name":"x","dependencies":[{"source":"git","uri":"https://example.com/a.git","version":"^1.0.0","ref":"main"}]}`,
			wantErr: "use only one of version or ref",
		},
		{
			name:    "unknown source",
			json:    `{"specVersion":"0.1","name":"x","dependencies":[{"source":"http","uri":"https://example.com/a.git"}]}`,
			wantErr: "unsupported source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempFile(t, "rulepack.json", tt.json)
			_, err := LoadRuleset(path)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}

func TestLoadLockfileRejectsMissingSource(t *testing.T) {
	path := writeTempFile(t, "rulepack.lock.json", `{"lockVersion":"0.1","resolved":[{"uri":"https://example.com/a.git","commit":"abc123"}]}`)
	_, err := LoadLockfile(path)
	if err == nil {
		t.Fatalf("expected error for missing source")
	}
	if !strings.Contains(err.Error(), "missing source") {
		t.Fatalf("expected missing source error, got %v", err)
	}
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}
