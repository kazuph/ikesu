package plugin

import (
	"bytes"
	"fmt"
	"github.com/kazuph/ikesu/internal/childenv"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var githubPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func ParseGitHubSource(value string) (Source, error) {
	parts := strings.Split(value, "/")
	if len(parts) < 2 {
		return Source{}, fmt.Errorf("expected owner/repo[/subdir...]")
	}
	for _, part := range parts {
		if !githubPart.MatchString(part) || part == "." || part == ".." {
			return Source{}, fmt.Errorf("invalid GitHub shorthand %q", value)
		}
	}
	return Source{Kind: "github", Owner: parts[0], Repo: parts[1], Subdir: strings.Join(parts[2:], "/")}, nil
}

type InstallPreview struct {
	Source   Source
	Manifest Manifest
	Root     string
}

func (s *Store) Install(source, ref string, confirm func(InstallPreview) bool) (Installed, error) {
	src, err := ParseGitHubSource(source)
	if err != nil {
		return Installed{}, err
	}
	if strings.HasPrefix(ref, "-") {
		return Installed{}, fmt.Errorf("invalid git ref %q", ref)
	}
	src.RequestedRef = ref
	managedRoot := filepath.Join(s.ConfigRoot, "plugins", "installed")
	if err = os.MkdirAll(managedRoot, 0700); err != nil {
		return Installed{}, err
	}
	temporary, err := os.MkdirTemp(managedRoot, ".install-*")
	if err != nil {
		return Installed{}, err
	}
	defer os.RemoveAll(temporary)
	checkout := filepath.Join(temporary, "checkout")
	runGit := func(args ...string) (string, error) {
		cmd := childenv.Command("git", args...)
		cmd.Env = childenv.Scrub(os.Environ())
		data, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, data)
		}
		return strings.TrimSpace(string(data)), nil
	}
	if _, err = runGit("clone", "--", "https://github.com/"+src.Owner+"/"+src.Repo+".git", checkout); err != nil {
		return Installed{}, err
	}
	if ref != "" {
		if _, err = runGit("-C", checkout, "checkout", "--detach", ref); err != nil {
			return Installed{}, err
		}
	}
	if src.ResolvedCommit, err = runGit("-C", checkout, "rev-parse", "HEAD"); err != nil {
		return Installed{}, err
	}
	root := filepath.Join(checkout, filepath.FromSlash(src.Subdir))
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return Installed{}, err
	}
	realCheckout, err := filepath.EvalSymlinks(checkout)
	if err != nil {
		return Installed{}, err
	}
	rel, err := filepath.Rel(realCheckout, realRoot)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Installed{}, fmt.Errorf("plugin subdir escapes checkout")
	}
	m, path, err := ReadManifest(root, s.Version)
	if err != nil {
		return Installed{}, err
	}
	items, err := s.List(m.ID)
	if err != nil {
		return Installed{}, err
	}
	if len(items) > 0 && items[0].Source.Kind != "github" {
		return Installed{}, fmt.Errorf("plugin %s is locally linked; unlink first", m.ID)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		return Installed{}, err
	}
	if confirm == nil || !confirm(InstallPreview{Source: src, Manifest: m, Root: root}) {
		return Installed{}, fmt.Errorf("plugin install not confirmed")
	}
	for i, build := range m.Build {
		if !supported(effectivePlatforms(build.Platforms, m.Platforms)) {
			continue
		}
		cmd := childenv.Command(build.Command[0], build.Command[1:]...)
		cmd.Env = []string{}
		cmd.Dir = root
		for _, kv := range childenv.Scrub(os.Environ()) {
			key, _, _ := strings.Cut(kv, "=")
			if strings.HasPrefix(key, "IKESU_") || strings.HasPrefix(key, "GTX_") || strings.HasPrefix(key, "HERDR_") {
				continue
			}
			cmd.Env = append(cmd.Env, kv)
		}
		if err := runBuild(cmd, m.ID, i, len(m.Build)); err != nil {
			return Installed{}, err
		}
		after, err := os.ReadFile(path)
		if err != nil {
			return Installed{}, err
		}
		if !bytes.Equal(before, after) {
			return Installed{}, fmt.Errorf("plugin %s manifest changed after preview", m.ID)
		}
	}
	destination := filepath.Join(managedRoot, m.ID)
	backup := filepath.Join(temporary, "previous")
	hadPrevious := false
	if _, err = os.Stat(destination); err == nil {
		if err = os.Rename(destination, backup); err != nil {
			return Installed{}, err
		}
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return Installed{}, err
	}
	if err = os.Rename(checkout, destination); err != nil {
		if hadPrevious {
			_ = os.Rename(backup, destination)
		}
		return Installed{}, err
	}
	src.ManagedPath = destination
	now := uint64(time.Now().UnixMilli())
	src.InstalledUnixMS = &now
	item, err := s.Link(filepath.Join(destination, filepath.FromSlash(src.Subdir)), true, &src)
	if err != nil {
		_ = os.RemoveAll(destination)
		if hadPrevious {
			_ = os.Rename(backup, destination)
		}
		return item, err
	}
	return item, nil
}
func (s *Store) Uninstall(idOrSource string) (string, bool, error) {
	items, err := s.List("")
	if err != nil {
		return "", false, err
	}
	for _, item := range items {
		source := item.Source.Owner + "/" + item.Source.Repo
		if item.Source.Subdir != "" {
			source += "/" + item.Source.Subdir
		}
		if item.ID != idOrSource && (item.Source.Kind != "github" || source != idOrSource) {
			continue
		}
		if item.Source.Kind == "github" {
			expected := filepath.Join(s.ConfigRoot, "plugins", "installed", item.ID)
			if item.Source.ManagedPath != expected {
				return "", false, fmt.Errorf("refusing to remove unmanaged path %q", item.Source.ManagedPath)
			}
			if err = os.RemoveAll(expected); err != nil {
				return "", false, err
			}
		}
		removed, err := s.Unlink(item.ID)
		return item.ID, removed, err
	}
	return idOrSource, false, nil
}
