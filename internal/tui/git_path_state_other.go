//go:build !darwin

package tui

import (
	"os"
	"reflect"
)

func sameGitFileState(a, b os.FileInfo) bool {
	return a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && reflect.DeepEqual(a.Sys(), b.Sys())
}
