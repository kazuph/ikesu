//go:build !darwin && !linux

package filewatch

func watchPaths(*Watcher, []string, func()) bool { return false }
func unwatchPaths(*Watcher)                      {}
