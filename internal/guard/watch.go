package guard

import (
	"os"
	"path/filepath"
	"reflect"
)

// assetSnapshot covers the concrete paths checked by intact. Ancestor trust
// and executable trust are checked separately before reusing this snapshot.
func (a *Assets) assetSnapshot() (map[string]os.FileInfo, bool) {
	shell := filepath.Dir(a.RuntimePath())
	generation, err := os.Readlink(filepath.Join(shell, "current"))
	if err != nil || !generationName(generation) {
		return nil, false
	}
	paths := []string{shell, filepath.Join(shell, "current"), a.RuntimePath(), filepath.Join(shell, "current", "manifest.json"), filepath.Join(shell, "current", "ikesu"), a.MarkerPath, a.Bin, a.issuePath()}
	paths = append(paths, filepath.Join(shell, generation))
	for _, name := range []string{"bash-env", ".zshenv", ".zprofile", ".zshrc", ".zlogin"} {
		paths = append(paths, filepath.Join(shell, name), filepath.Join(shell, name+".current"), filepath.Join(shell, "current", name+".current"))
	}
	for _, cmd := range a.Config.Commands() {
		paths = append(paths, filepath.Join(a.Bin, cmd))
	}
	result := make(map[string]os.FileInfo, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, false
		}
		result[path] = info
	}
	return result, true
}

func sameAssetSnapshot(a, b map[string]os.FileInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for path, previous := range a {
		current, ok := b[path]
		if !ok || (previous == nil) != (current == nil) {
			return false
		}
		if previous != nil && (!os.SameFile(previous, current) || !reflect.DeepEqual(previous.Sys(), current.Sys())) {
			return false
		}
	}
	return true
}

// ObserveChanges replaces only the periodic integrity check when the native
// watch covers every asset. Failed registration retains the existing checks.
func (a *Assets) ObserveChanges(changed func()) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return false
	}
	if a.watchChanges == nil {
		a.watchChanges = func() {
			a.watchGeneration.Add(1)
			changed()
		}
		a.refreshChanges()
		// A change between the preceding Sync and registration must be observed.
		a.watchChanges()
	}
	return a.watchInstalled
}

func (a *Assets) refreshChanges() {
	if a.watchChanges != nil && !a.closed {
		if a.active == "" && a.issue == "" {
			unwatchAssets(a)
			a.watchInstalled = true
			return
		}
		a.watchInstalled = watchAssets(a, a.assetPaths(), a.watchChanges)
	}
}

func (a *Assets) assetPaths() []string {
	paths := []string{a.RuntimePath(), filepath.Join(filepath.Dir(a.RuntimePath()), "current"), a.MarkerPath, a.issuePath(), filepath.Join(a.Root, "guard", "shell", a.Pane, "ready")}
	for _, cmd := range a.Config.Commands() {
		paths = append(paths, filepath.Join(a.Bin, cmd))
	}
	// Ancestors cover deletion/rename of the whole generated directory chain.
	parents := map[string]bool{}
	for _, path := range paths {
		for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
			parents[dir] = true
			if dir == filepath.Dir(a.Root) || dir == filepath.Dir(dir) {
				break
			}
		}
	}
	for dir := range parents {
		paths = append(paths, dir)
	}
	return paths
}

func (a *Assets) stopChanges() {
	unwatchAssets(a)
	a.watchChanges = nil
	a.watchInstalled = false
}

// StopObserving releases watches without removing assets during handoff.
func (a *Assets) StopObserving() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopChanges()
}
