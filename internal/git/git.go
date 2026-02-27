package git

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"
)

type Client struct {
	CacheRoot string
}

type Resolution struct {
	Requested       string
	ResolvedVersion string
	Commit          string
}

func NewClient() (*Client, error) {
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("resolve cache dir: %w", err)
	}
	root := filepath.Join(cacheRoot, "rulepack")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Client{CacheRoot: root}, nil
}

func (c *Client) EnsureRepo(uri string) (string, error) {
	hash := sha256.Sum256([]byte(uri))
	repoDir := filepath.Join(c.CacheRoot, hex.EncodeToString(hash[:8]), "repo.git")
	if _, err := os.Stat(repoDir); err == nil {
		if _, err := run("git", "--git-dir", repoDir, "fetch", "--force", "--tags", "origin"); err != nil {
			return "", err
		}
		if _, err := run("git", "--git-dir", repoDir, "fetch", "--force", "origin", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
			return "", err
		}
		return repoDir, nil
	}
	if err := os.MkdirAll(filepath.Dir(repoDir), 0o755); err != nil {
		return "", err
	}
	if _, err := run("git", "clone", "--mirror", uri, repoDir); err != nil {
		return "", err
	}
	return repoDir, nil
}

func (c *Client) Resolve(repoDir string, ref string, version string) (Resolution, error) {
	if ref != "" {
		sha, err := revParse(repoDir, ref)
		if err != nil {
			return Resolution{}, err
		}
		return Resolution{Requested: ref, Commit: sha}, nil
	}
	if version != "" {
		v, tag, err := resolveTag(repoDir, version)
		if err != nil {
			return Resolution{}, err
		}
		sha, err := revParse(repoDir, tag)
		if err != nil {
			return Resolution{}, err
		}
		return Resolution{Requested: version, ResolvedVersion: v.String(), Commit: sha}, nil
	}
	sha, err := revParse(repoDir, "HEAD")
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{Requested: "HEAD", Commit: sha}, nil
}

func (c *Client) ShowFile(repoDir, commit, path string) ([]byte, error) {
	out, err := run("git", "--git-dir", repoDir, "show", fmt.Sprintf("%s:%s", commit, path))
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

func resolveTag(repoDir, constraint string) (*semver.Version, string, error) {
	cons, err := semver.NewConstraint(constraint)
	if err != nil {
		return nil, "", fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}
	output, err := run("git", "--git-dir", repoDir, "tag", "--list")
	if err != nil {
		return nil, "", err
	}
	tags := strings.Fields(output)
	type entry struct {
		version *semver.Version
		tag     string
	}
	var matches []entry
	for _, tag := range tags {
		normalized := strings.TrimPrefix(tag, "v")
		v, err := semver.NewVersion(normalized)
		if err != nil {
			continue
		}
		if cons.Check(v) {
			matches = append(matches, entry{version: v, tag: tag})
		}
	}
	if len(matches) == 0 {
		return nil, "", fmt.Errorf("no tags satisfy constraint %q", constraint)
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].version.GreaterThan(matches[j].version)
	})
	return matches[0].version, matches[0].tag, nil
}

func revParse(repoDir, ref string) (string, error) {
	sha, err := run("git", "--git-dir", repoDir, "rev-parse", fmt.Sprintf("%s^{commit}", ref))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sha), nil
}

func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s failed: %w\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
