package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPaneHistoryAtomicPrivateFileAndWriteFailure(t *testing.T) {
	dir := t.TempDir()
	h := History{Version: CurrentVersion, Panes: map[int]PaneHistory{1: {ANSI: "secret", Lines: 1}}}
	if err := SaveHistory(dir, h); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(HistoryPath(dir))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	got, err := LoadHistory(dir)
	if err != nil || got.Panes[1].ANSI != "secret" {
		t.Fatal(got, err)
	}
	if err := SaveHistory(filepath.Join(dir, "missing"), h); err == nil {
		t.Fatal("write failure concealed")
	}
}
