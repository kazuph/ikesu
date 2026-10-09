package machine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RemoteDirectory expresses a path using the remote user's HOME, never the local HOME.
func RemoteDirectory(cwd, home, override string) string {
	if override != "" {
		if override == "~" {
			return `"$HOME"`
		}
		if strings.HasPrefix(override, "~/") {
			override = override[2:]
		}
		if filepath.IsAbs(override) {
			return Quote(filepath.Clean(override))
		}
		return `"$HOME"/` + Quote(override)
	}
	rel, err := filepath.Rel(home, cwd)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
		if rel == "." {
			return `"$HOME"`
		}
		return `"$HOME"/` + Quote(rel)
	}
	return Quote(cwd)
}

func RunArgs(p Profile, directory string, argv []string, pane bool) []string {
	mode := "-n"
	if pane {
		mode = "-t"
	}
	command := "cd -- " + directory + " && exec " + Command(argv)
	remote := `exec "${SHELL:-/bin/sh}" -lic ` + Quote(command)
	return []string{mode, "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "--", p.Target, remote}
}

// SyncPlan computes argv, without modifying the local checkout or any remote files.
// Every transfer omits --delete: remote ignored build products survive.
func SyncPlan(p Profile, localCwd, directory string) ([][]string, error) {
	root := localCwd
	gitRoot, err := exec.Command("git", "-C", localCwd, "rev-parse", "--show-toplevel").Output()
	gitManaged := err == nil
	if gitManaged {
		root = strings.TrimSpace(string(gitRoot))
	}
	remoteRoot := directory
	rel, err := filepath.Rel(root, localCwd)
	if err != nil {
		return nil, err
	}
	if rel != "." {
		for range strings.Split(filepath.ToSlash(rel), "/") {
			remoteRoot += "/.."
		}
	}
	mkdir := []string{"ssh", "-n", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "--", p.Target, "mkdir -p -- " + remoteRoot}
	rsync := []string{"rsync", "-a", "-e", "ssh -o BatchMode=yes -o ConnectTimeout=15", "--exclude=/.git"}
	if gitManaged {
		ignored, err := exec.Command("git", "-C", root, "ls-files", "--others", "--ignored", "--exclude-standard", "-z").Output()
		if err != nil {
			return nil, fmt.Errorf("read git ignored paths: %w", err)
		}
		for _, path := range strings.Split(string(ignored), "\x00") {
			if path != "" {
				rsync = append(rsync, "--exclude=/"+literalFilter(path))
			}
		}
		// Preserve tracked files and gitignore negations before applying root patterns.
		visible, err := exec.Command("git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "-z").Output()
		if err != nil {
			return nil, err
		}
		for _, path := range strings.Split(string(visible), "\x00") {
			if path == "" {
				continue
			}
			rsync = append(rsync, "--include=/"+literalFilter(path))
			for parent := filepath.Dir(path); parent != "."; parent = filepath.Dir(parent) {
				rsync = append(rsync, "--include=/"+literalFilter(filepath.ToSlash(parent))+"/")
			}
		}
		patterns, err := os.ReadFile(filepath.Join(root, ".gitignore"))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		// Existing ignored paths above are authoritative for negated patterns.
		// Root patterns additionally protect not-yet-existing build output.
		for _, line := range strings.Split(string(patterns), "\n") {
			line = strings.TrimSuffix(line, "\r")
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
				continue
			}
			rsync = append(rsync, "--exclude="+line)
		}
	}
	// rsync's remote operand is interpreted by its remote shell too.
	rsync = append(rsync, "--", root+"/", p.Target+":"+remoteRoot+"/")
	return [][]string{mkdir, rsync}, nil
}

func literalFilter(path string) string {
	return strings.NewReplacer("[", "[[]", "]", "[]]", "*", "[*]", "?", "[?]").Replace(path)
}
