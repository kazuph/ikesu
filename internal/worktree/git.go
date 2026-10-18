// Package worktree manages Git checkouts using Git's public command interface.
package worktree

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/childenv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kazuph/goterrace/internal/api"
)

type GitError struct {
	Args   []string
	Output string
}

func (e *GitError) Error() string {
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), e.Output)
}

func git(cwd string, args ...string) (string, error) {
	return gitWithEnv(cwd, nil, args...)
}

func gitWithEnv(cwd string, environment []string, args ...string) (string, error) {
	cmd := childenv.Command("git", append([]string{"-C", cwd}, args...)...)
	if len(environment) > 0 {
		cmd.Env = append(childenv.Scrub(os.Environ()), environment...)
	}
	b, err := cmd.CombinedOutput()
	if err != nil {
		return "", &GitError{Args: args, Output: strings.TrimSpace(string(b))}
	}
	return strings.TrimSpace(string(b)), nil
}

// List retains NUL-delimited porcelain on supported Git versions. Only an
// explicit unsupported -z usage error selects the older porcelain format.
func List(cwd string) ([]api.WorktreeInfo, error) {
	// This error is parsed for feature detection; keep only this probe in
	// the C locale, including gettext's LANGUAGE override.
	raw, err := gitWithEnv(cwd, []string{"LC_ALL=C", "LANGUAGE=C"}, "worktree", "list", "--porcelain", "-z")
	if err == nil {
		return parseWorktreeList(raw, "\x00", nil)
	}
	if !strings.Contains(err.Error(), "unknown switch `z'") || !strings.Contains(err.Error(), "usage: git worktree") {
		return nil, err
	}
	raw, err = git(cwd, "-c", "core.quotePath=true", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	// Git before 2.36 emits literal newlines in paths even with quotePath.
	// Match registered full paths before interpreting porcelain field lines.
	common, err := git(cwd, "rev-parse", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(cwd, common)
	}
	if canonical, err := filepath.EvalSymlinks(common); err == nil {
		common = canonical
	}
	paths := []string{common, filepath.Dir(common)}
	entries, err := os.ReadDir(filepath.Join(common, "worktrees"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(common, "worktrees", entry.Name(), "gitdir"))
		if err != nil {
			return nil, err
		}
		paths = append(paths, filepath.Dir(strings.TrimSuffix(string(data), "\n")))
	}
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	return parseWorktreeList(raw, "\n", paths)
}

func parseWorktreeList(raw, separator string, paths []string) ([]api.WorktreeInfo, error) {
	out := []api.WorktreeInfo{}
	var item *api.WorktreeInfo
	fields := strings.Split(raw, separator)
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		key, value, _ := strings.Cut(field, " ")
		switch key {
		case "worktree":
			tail := strings.Join(fields[i:], separator) + separator
			for _, path := range paths {
				if strings.HasPrefix(tail, "worktree "+path+"\n") {
					value = path
					i += strings.Count(path, "\n")
					break
				}
			}
			if separator == "\n" && strings.HasPrefix(value, `"`) {
				decoded, err := strconv.Unquote(value)
				if err != nil {
					return nil, fmt.Errorf("decode worktree path: %w", err)
				}
				value = decoded
			}
			out = append(out, api.WorktreeInfo{Path: value, Label: filepath.Base(value), IsLinkedWorktree: len(out) > 0})
			item = &out[len(out)-1]
		case "branch":
			if item != nil {
				v := strings.TrimPrefix(value, "refs/heads/")
				item.Branch = &v
				item.Label = v
			}
		case "bare":
			if item != nil {
				item.IsBare = true
			}
		case "detached":
			if item != nil {
				item.IsDetached = true
			}
		case "prunable":
			if item != nil {
				item.IsPrunable = true
			}
		}
	}
	return out, nil
}

func Create(cwd, path, branch, base string) error {
	if branch == "" {
		return fmt.Errorf("a new branch name is required")
	}
	if _, err := git(cwd, "check-ref-format", "--branch", branch); err != nil {
		return err
	}
	if _, err := git(cwd, "show-ref", "--verify", "refs/heads/"+branch); err == nil {
		if base != "" {
			return fmt.Errorf("base cannot be specified for an existing branch")
		}
		return Checkout(cwd, path, branch)
	}
	args := []string{"worktree", "add", "-b", branch, "--", path}
	if base != "" {
		args = append(args, base)
	}
	_, err := git(cwd, args...)
	return err
}

func Checkout(cwd, path, branch string) error {
	if branch == "" {
		return fmt.Errorf("an existing branch name is required")
	}
	if _, err := git(cwd, "show-ref", "--verify", "refs/heads/"+branch); err != nil {
		return err
	}
	_, err := git(cwd, "worktree", "add", "--", path, branch)
	return err
}

func Remove(cwd, path string, force bool) error {
	items, err := List(cwd)
	if err != nil {
		return err
	}
	found := false
	for _, w := range items {
		if SamePath(w.Path, path) {
			if !w.IsLinkedWorktree || w.IsBare {
				return fmt.Errorf("cannot remove the main repository checkout")
			}
			found = true
		}
	}
	if !found {
		return fmt.Errorf("path is not a registered linked worktree")
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, "--", path)
	_, err = git(cwd, args...)
	return err
}

func SamePath(a, b string) bool {
	canonical := func(p string) string {
		p, _ = filepath.Abs(p)
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return real
		}
		return filepath.Clean(p)
	}
	return canonical(a) == canonical(b)
}
