package guard

import "path/filepath"

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
