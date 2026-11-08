package tui

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/childenv"
	"github.com/kazuph/ikesu/internal/filewatch"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/worktree"
)

type gitCwdActivity struct {
	watch         *filewatch.Watcher
	ready, loaded bool
	revision      uint64
	git           render.GitStatus
	name          string
	// source is the checkout this directory is in, read off the app loop.
	source        api.WorktreeSourceInfo
	sourceErr     error
	paths         map[string]gitPathState
	tracked       []string
	trackedNext   int
	statChecks    int
	trackedChecks int
	readChecks    int
	quietDirs     map[string]bool
	trackedDirty  bool
}

type gitPathState struct {
	info     os.FileInfo
	contents bool
}

// Ancestor directory writes include unrelated siblings. Keep their identity
// watches, but only Git metadata directories need content-change invalidation.
func unchangedGitActivity(entry gitCwdActivity) (gitCwdActivity, bool) {
	revision := entry.watch.Revision()
	entry.statChecks = 0
	entry.trackedChecks = 0
	probe := func(path string, previous gitPathState) bool {
		entry.statChecks++
		info, err := os.Stat(path)
		if err != nil && !os.IsNotExist(err) {
			return false
		}
		if previous.info == nil || info == nil {
			if previous.info != nil || info != nil {
				return false
			}
			return true
		}
		if !os.SameFile(previous.info, info) || previous.info.Mode() != info.Mode() ||
			(previous.contents && !sameGitFileState(previous.info, info)) {
			return false
		}
		return true
	}
	tracked := make(map[string]bool, len(entry.tracked))
	for _, path := range entry.tracked {
		tracked[path] = true
	}
	for path, previous := range entry.paths {
		// Recheck watched worktree directories at least once per tracked-file
		// cycle even if the native watcher stops delivering notifications.
		skipDir := entry.quietDirs[path] && revision == entry.revision && entry.trackedNext != 7
		if !tracked[path] && !skipDir && !probe(path, previous) {
			return entry, false
		}
	}
	// Visit evenly spaced files in each pass, alternating opposite halves so
	// neighbouring paths do not all wait for the end of the 12-second cycle.
	phase := (entry.trackedNext&1)<<2 | entry.trackedNext&2 | (entry.trackedNext&4)>>2
	changed := false
	for i := phase; i < len(entry.tracked); i += 8 {
		path := entry.tracked[i]
		entry.trackedChecks++
		if !probe(path, entry.paths[path]) {
			info, err := os.Stat(path)
			entry.statChecks++
			if err != nil && !os.IsNotExist(err) {
				return entry, false
			}
			state := entry.paths[path]
			state.info = info
			entry.paths[path] = state
			changed = true
		}
	}
	entry.trackedNext = (entry.trackedNext + 1) % 8
	entry.trackedDirty = changed
	// A notification arriving during the probes must remain pending.
	entry.revision = revision
	return entry, !changed
}

type gitActivityState struct{ entries map[string]gitCwdActivity }

func (a *App) closeGitActivity() {
	if a.gitActivity == nil {
		return
	}
	for _, entry := range a.gitActivity.entries {
		entry.watch.Close()
	}
	a.gitActivity = nil
}

func gitAncestors(path string, paths map[string]bool) {
	for path = filepath.Clean(path); ; path = filepath.Dir(path) {
		paths[path] = true
		if filepath.Dir(path) == path {
			return
		}
	}
}

func gitPathList(paths map[string]bool) []string {
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	return result
}

func gitActivityOutput(cwd string, args ...string) ([]byte, error) {
	return childenv.Command("git", append([]string{"-C", cwd}, args...)...).Output()
}

func readGitActivity(cwd string, entry gitCwdActivity) gitCwdActivity {
	values := func(ready bool, revision uint64) gitCwdActivity {
		entry.git = readWorkspaceGit(cwd)
		entry.source, entry.sourceErr = worktree.Source(cwd)
		entry.name = filepath.Base(cwd)
		if entry.sourceErr == nil {
			entry.name = filepath.Base(entry.source.SourceCheckoutPath)
		}
		entry.revision, entry.ready, entry.loaded = revision, ready, true
		return entry
	}
	if entry.trackedDirty {
		entry.trackedDirty = false
		entry.readChecks = 0
		entry.git.Added, entry.git.Deleted = readWorkspaceGitDiff(cwd)
		return entry
	}
	if entry.loaded && !entry.ready && entry.watch.Revision() == entry.revision {
		return values(false, entry.revision)
	}
	paths := map[string]bool{}
	gitAncestors(cwd, paths)
	for path := filepath.Clean(cwd); ; path = filepath.Dir(path) {
		paths[filepath.Join(path, ".git")] = true
		if filepath.Dir(path) == path {
			break
		}
	}
	var metadataDirs []string
	trackedPaths := map[string]bool{}
	trackedDirs := map[string]bool{}
	watches := make(map[string]bool, len(paths))
	for path := range paths {
		watches[path] = true
	}
	// Watch only discovery and key metadata paths. File state is checked by
	// the metadata worker without opening a descriptor per tracked file.
	ready := entry.watch.Update(gitPathList(paths))
	revision := entry.watch.Revision()
	if !ready {
		return values(false, revision)
	}
	directory, err := gitActivityOutput(cwd, "rev-parse", "--absolute-git-dir")
	if err == nil {
		common, commonErr := gitActivityOutput(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
		ready = ready && commonErr == nil
		for _, dir := range []string{strings.TrimSpace(string(directory)), strings.TrimSpace(string(common))} {
			if dir == "" {
				continue
			}
			gitAncestors(dir, paths)
			metadataDirs = append(metadataDirs, dir)
			watches[dir] = true
			for _, name := range []string{"HEAD", "index", "packed-refs", "config"} {
				watches[filepath.Join(dir, name)] = true
			}
			walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
				if os.IsNotExist(err) {
					return nil
				}
				if err != nil {
					return err
				}
				paths[path] = true
				if d.IsDir() && path != dir && !gitStatusMetadataDir(dir, path) {
					// Object stores, reflogs, hooks and other worktrees'
					// metadata never change the status shown for this cwd
					// without also changing HEAD, the index or a ref.
					return fs.SkipDir
				}
				return nil
			})
			ready = ready && walkErr == nil
		}
		root, rootErr := gitActivityOutput(cwd, "rev-parse", "--show-toplevel")
		if rootErr == nil {
			tracked, trackedErr := gitActivityOutput(cwd, "ls-files", "--cached", "--full-name", "-z")
			ready = ready && trackedErr == nil
			for _, name := range strings.Split(string(tracked), "\x00") {
				if name != "" {
					path := filepath.Join(strings.TrimSpace(string(root)), name)
					if filepath.Base(path) != ".gitattributes" {
						trackedPaths[path] = true
					}
					gitAncestors(path, paths)
					for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
						trackedDirs[dir] = true
						watches[dir] = true
						paths[filepath.Join(dir, ".gitattributes")] = true
						if dir == strings.TrimSpace(string(root)) || filepath.Dir(dir) == dir {
							break
						}
					}
				}
			}
		}
	}
	if err == nil {
		configPaths := map[string]bool{}
		ready = gitConfigurationPaths(cwd, configPaths) && ready
		for path := range configPaths {
			paths[path] = true
			delete(trackedPaths, path)
		}
	}
	covered := entry.watch.Update(gitPathList(watches))
	entry.paths = make(map[string]gitPathState, len(paths))
	entry.quietDirs = make(map[string]bool)
	entry.tracked = entry.tracked[:0]
	entry.trackedNext = 0
	entry.readChecks = 0
	for path := range paths {
		entry.readChecks++
		info, statErr := os.Stat(path)
		if statErr != nil && !os.IsNotExist(statErr) {
			covered = false
		}
		contents := info != nil && (!info.IsDir() || trackedDirs[path])
		for _, dir := range metadataDirs {
			if path == dir || strings.HasPrefix(path, dir+string(filepath.Separator)) {
				contents = true
			}
		}
		if trackedDirs[path] && watches[path] && info != nil && info.IsDir() {
			metadata := false
			for _, dir := range metadataDirs {
				if path == dir || strings.HasPrefix(path, dir+string(filepath.Separator)) {
					metadata = true
				}
			}
			if !metadata {
				entry.quietDirs[path] = true
			}
		}
		entry.paths[path] = gitPathState{info: info, contents: contents}
		if trackedPaths[path] && !trackedDirs[path] {
			entry.tracked = append(entry.tracked, path)
		}
	}
	sort.Strings(entry.tracked)
	return values(ready && covered, revision)
}

func gitConfigurationPaths(cwd string, paths map[string]bool) bool {
	add := func(path string) {
		if path == "" {
			return
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		gitAncestors(path, paths)
		if real, err := filepath.EvalSymlinks(path); err == nil {
			gitAncestors(real, paths)
		}
	}
	// Ask Git which config files it actually read, including include/includeIf.
	// --name-only deliberately excludes credential/config values from the output.
	data, err := gitActivityOutput(cwd, "config", "--null", "--show-origin", "--name-only", "--list")
	if err != nil {
		return false
	}
	fields := strings.Split(string(data), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		if path, ok := strings.CutPrefix(fields[i], "file:"); ok {
			add(path)
		}
	}
	for _, variable := range []string{"GIT_CONFIG_SYSTEM", "GIT_CONFIG_GLOBAL"} {
		data, err = gitActivityOutput(cwd, "var", variable)
		if err != nil {
			return false
		}
		for _, path := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			add(path)
			if variable == "GIT_CONFIG_SYSTEM" && path != "" {
				add(filepath.Join(filepath.Dir(path), "gitattributes"))
			}
		}
	}
	attributes, err := gitActivityOutput(cwd, "config", "--path", "--get", "core.attributesfile")
	if err == nil {
		add(strings.TrimSpace(string(attributes)))
	} else {
		root := os.Getenv("XDG_CONFIG_HOME")
		if root == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return false
			}
			root = filepath.Join(home, ".config")
		}
		add(filepath.Join(root, "git", "attributes"))
	}
	return true
}

// gitStatusMetadataDir reports whether a directory under a Git metadata root
// holds state the sidebar status reads: refs and in-progress operations.
func gitStatusMetadataDir(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return true
	}
	switch strings.SplitN(filepath.ToSlash(rel), "/", 2)[0] {
	case "refs", "rebase-merge", "rebase-apply", "sequencer":
		return true
	}
	return false
}
