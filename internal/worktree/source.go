package worktree

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kazuph/ikesu/internal/api"
)

func Source(cwd string) (api.WorktreeSourceInfo, error) {
	common, err := git(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return api.WorktreeSourceInfo{}, err
	}
	common = filepath.Clean(common)
	if p, e := filepath.EvalSymlinks(common); e == nil {
		common = p
	}
	items, err := List(cwd)
	if err != nil {
		return api.WorktreeSourceInfo{}, err
	}
	root := items[0].Path
	checkout := root
	if bare, _ := git(cwd, "rev-parse", "--is-bare-repository"); bare != "true" {
		checkout, err = git(cwd, "rev-parse", "--show-toplevel")
		if err != nil {
			return api.WorktreeSourceInfo{}, err
		}
	}
	name := strings.TrimSuffix(filepath.Base(root), ".git")
	home, err := os.UserHomeDir()
	if err != nil {
		return api.WorktreeSourceInfo{}, err
	}
	prefix := filepath.Join(home, ".gtx", "worktrees", name) + string(filepath.Separator)
	return api.WorktreeSourceInfo{RepoKey: common, RepoName: name, RepoRoot: root, SourceCheckoutPath: checkout, CheckoutPathPrefix: &prefix}, nil
}

func Metadata(source api.WorktreeSourceInfo, path string) *api.WorkspaceWorktreeInfo {
	return &api.WorkspaceWorktreeInfo{RepoKey: source.RepoKey, RepoName: source.RepoName, RepoRoot: source.RepoRoot, CheckoutPath: path, IsLinkedWorktree: !SamePath(source.RepoRoot, path)}
}

// Slug keeps a branch in one server-owned path component.
func Slug(branch string) string {
	var b strings.Builder
	for _, r := range branch {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}
