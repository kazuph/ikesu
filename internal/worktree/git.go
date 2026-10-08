// Package worktree manages Git checkouts using Git's public command interface.
package worktree

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kazuph/goterrace/internal/api"
)

func git(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(b)))
	}
	return strings.TrimSpace(string(b)), nil
}

// List uses NUL-delimited porcelain so paths with whitespace and newlines
// remain data rather than being interpreted as additional fields.
func List(cwd string) ([]api.WorktreeInfo, error) {
	raw, err := git(cwd, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	out := []api.WorktreeInfo{}
	var item *api.WorktreeInfo
	for _, field := range strings.Split(raw, "\x00") {
		key, value, _ := strings.Cut(field, " ")
		switch key {
		case "worktree":
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
