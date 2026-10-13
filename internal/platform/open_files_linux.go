//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"strconv"
)

func OpenJSONLFiles(pid int) []string {
	dir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var paths []string
	for _, entry := range entries {
		path, err := os.Readlink(filepath.Join(dir, entry.Name()))
		if err == nil && filepath.Ext(path) == ".jsonl" {
			paths = append(paths, path)
		}
	}
	return paths
}
