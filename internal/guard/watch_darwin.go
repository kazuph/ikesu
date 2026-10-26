//go:build darwin

package guard

import "github.com/kazuph/ikesu/internal/filewatch"

func watchAssets(a *Assets, paths []string, changed func()) bool {
	if a.pathWatch == nil {
		a.pathWatch = filewatch.New(changed)
	}
	return a.pathWatch.Update(paths)
}

func unwatchAssets(a *Assets) {
	if a.pathWatch != nil {
		a.pathWatch.Close()
		a.pathWatch = nil
	}
}
