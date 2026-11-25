package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/childenv"
)

// PruneRevision identifies the checked-out branch tip and the ref proving it merged.
type PruneRevision struct {
	Branch string `json:"branch"`
	Head   string `json:"head"`
	Base   string `json:"base"`
}

// PruneRepository shares the registered checkout list and base across one scan.
type PruneRepository struct {
	common, base, baseRef string
	checkouts             map[string]api.WorktreeInfo
}

func gitExit(err error, code int) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == code
}

func ReadPruneRepository(ctx context.Context, root string) (*PruneRepository, error) {
	items, err := ListContext(ctx, root)
	if err != nil {
		return nil, err
	}
	common, err := gitWithEnvContext(ctx, root, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	origin, err := gitWithEnvContext(ctx, root, nil, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
	if err != nil && !gitExit(err, 1) {
		return nil, err
	}
	refs, err := gitWithEnvContext(ctx, root, nil, "for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes/origin")
	if err != nil {
		return nil, err
	}
	exists := map[string]bool{}
	for _, ref := range strings.Split(refs, "\n") {
		exists[ref] = true
	}
	repo := &PruneRepository{common: canonicalPath(common), checkouts: make(map[string]api.WorktreeInfo, len(items))}
	if branch, ok := strings.CutPrefix(origin, "refs/remotes/origin/"); ok && branch != "HEAD" && (exists[origin] || exists["refs/heads/"+branch]) {
		repo.base = branch
	} else {
		for _, branch := range []string{"main", "master"} {
			if exists["refs/heads/"+branch] || exists["refs/remotes/origin/"+branch] {
				repo.base = branch
				break
			}
		}
	}
	if repo.base != "" {
		repo.baseRef = "refs/remotes/origin/" + repo.base
		if !exists[repo.baseRef] {
			repo.baseRef = "refs/heads/" + repo.base
		}
	}
	for _, item := range items {
		repo.checkouts[canonicalPath(item.Path)] = item
	}
	return repo, nil
}

// Check requires a registered, clean linked checkout and a merged attached branch.
func (repo *PruneRepository) Check(ctx context.Context, path string) (PruneRevision, string, error) {
	var rev PruneRevision
	path = canonicalPath(path)
	item, ok := repo.checkouts[path]
	if !ok || !item.IsLinkedWorktree || item.IsBare || item.IsPrunable {
		return rev, "not a linked worktree", nil
	}
	if item.IsDetached || item.Branch == nil {
		return rev, "detached HEAD", nil
	}
	if repo.baseRef == "" {
		return rev, "no default branch", nil
	}
	if *item.Branch == repo.base {
		return rev, "default branch", nil
	}
	// A parent checkout can contain other registered checkouts in ignored paths.
	for other := range repo.checkouts {
		if other != path && strings.HasPrefix(other, path+string(filepath.Separator)) {
			return rev, "contains another worktree", nil
		}
	}
	common, err := gitWithEnvContext(ctx, path, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return rev, "", err
	}
	if canonicalPath(common) != repo.common {
		return rev, "repository changed", nil
	}
	gitDir, err := gitWithEnvContext(ctx, path, nil, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return rev, "", err
	}
	if !Within(gitDir, filepath.Join(repo.common, "worktrees")) || SamePath(gitDir, repo.common) {
		return rev, "not a linked worktree", nil
	}
	if _, err := os.Lstat(filepath.Join(gitDir, "locked")); err == nil {
		return rev, "locked worktree", nil
	} else if !os.IsNotExist(err) {
		return rev, "", err
	}
	branch, err := gitWithEnvContext(ctx, path, nil, "symbolic-ref", "--quiet", "HEAD")
	if gitExit(err, 1) || (err == nil && branch != "refs/heads/"+*item.Branch) {
		return rev, "branch changed", nil
	}
	if err != nil {
		return rev, "", err
	}
	head, err := gitWithEnvContext(ctx, path, nil, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return rev, "", err
	}
	if _, err := gitWithEnvContext(ctx, path, nil, "--no-replace-objects", "merge-base", "--is-ancestor", head, repo.baseRef); err != nil {
		if gitExit(err, 1) {
			return rev, "unmerged", nil
		}
		return rev, "", err
	}
	reason, err := pruneIndexProtection(ctx, path)
	if err != nil || reason != "" {
		return rev, reason, err
	}
	status, err := gitWithEnvContext(ctx, path, []string{"GIT_OPTIONAL_LOCKS=0"}, "-c", "core.fsmonitor=false", "status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return rev, "", err
	}
	if status != "" {
		return rev, "dirty", nil
	}
	return PruneRevision{Branch: *item.Branch, Head: head, Base: strings.TrimPrefix(strings.TrimPrefix(repo.baseRef, "refs/remotes/"), "refs/heads/")}, "", nil
}

// Check the index without flipping flags or refreshing its stat data. The
// stage record identifies Gitlinks without relying on .gitmodules contents.
func pruneIndexProtection(ctx context.Context, path string) (string, error) {
	entries, err := gitWithEnvContext(ctx, path, nil, "ls-files", "-v", "--stage", "-z")
	if err != nil {
		return "", err
	}
	for entry := range strings.SplitSeq(entries, "\x00") {
		if entry == "" {
			continue
		}
		tag := entry[0]
		if tag == 'S' || (tag >= 'a' && tag <= 'z') {
			return "index hides tracked changes", nil
		}
		if strings.HasPrefix(entry, "H 160000 ") {
			_, name, ok := strings.Cut(entry, "\t")
			if !ok {
				return "", fmt.Errorf("invalid Gitlink index entry")
			}
			if _, err := os.Lstat(filepath.Join(path, name, ".git")); err == nil {
				return "initialized submodule", nil
			} else if !os.IsNotExist(err) {
				return "", err
			}
		}
	}
	return "", nil
}

// DiskUsage estimates allocated bytes, including ignored build output. du -P
// counts symlinks themselves without following their targets.
func DiskUsage(ctx context.Context, path string) (uint64, error) {
	cmd := childenv.CommandContextGroup(ctx, "du", "-P", "-sk", "--", path)
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if err != nil {
		return 0, fmt.Errorf("measure disk usage: %w", err)
	}
	blocks, _, _ := strings.Cut(string(out), "\t")
	kib, err := strconv.ParseUint(blocks, 10, 54)
	if err != nil {
		return 0, fmt.Errorf("parse disk usage: %w", err)
	}
	return kib * 1024, nil
}
