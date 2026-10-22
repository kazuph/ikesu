package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const minimal = `id = "example.local"
name = "Local"
version = "0.1.0"
min_ikesu_version = "0.1.0"
`

func writeManifest(t *testing.T, text string) string {
	t.Helper()
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "ikesu-plugin.toml"), []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
	return dir
}
func TestManifestValidation(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		valid      bool
	}{
		{"minimal", minimal, true},
		{"required", strings.ReplaceAll(minimal, "min_ikesu_version = \"0.1.0\"", ""), false},
		{"newer", strings.ReplaceAll(minimal, "min_ikesu_version = \"0.1.0\"", "min_ikesu_version = \"0.2.0\""), false},
		{"id", strings.ReplaceAll(minimal, "example.local", "../local"), false},
		{"platform", minimal + "platforms = [\"plan9\"]\n", false},
		{"empty-command", minimal + "[[actions]]\nid=\"x\"\ntitle=\"X\"\ncommand=[]\n", false},
		{"local-dot", minimal + "[[actions]]\nid=\"x.y\"\ntitle=\"X\"\ncommand=[\"echo\"]\n", false},
		{"duplicate", minimal + strings.Repeat("[[actions]]\nid=\"x\"\ntitle=\"X\"\ncommand=[\"echo\"]\n", 2), false},
		{"pane", minimal + "[[panes]]\nid=\"x\"\ntitle=\"X\"\ncommand=[\"echo\"]\nwidth=\"80%\"\nheight=20\n", true},
		{"link-missing-action", minimal + "[[link_handlers]]\nid=\"x\"\ntitle=\"X\"\npattern=\"https://\"\naction=\"missing\"\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, e := ReadManifest(writeManifest(t, tc.text), "0.1.0")
			if (e == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, e)
			}
		})
	}
}
func TestRegistrySharedAndOffline(t *testing.T) {
	root := t.TempDir()
	s := &Store{ConfigRoot: filepath.Join(root, "config"), StateRoot: filepath.Join(root, "state"), Version: "0.1.0"}
	path := writeManifest(t, minimal)
	p, e := s.Link(path, false, nil)
	if e != nil {
		t.Fatal(e)
	}
	if p.Enabled {
		t.Fatal("disabled ignored")
	}
	s2 := &Store{ConfigRoot: s.ConfigRoot, StateRoot: s.StateRoot, Version: s.Version}
	if _, e = s2.SetEnabled(p.ID, true); e != nil {
		t.Fatal(e)
	}
	list, e := s.List("")
	if e != nil || len(list) != 1 || !list[0].Enabled {
		t.Fatal(list, e)
	}
	if e = os.Remove(filepath.Join(path, "ikesu-plugin.toml")); e != nil {
		t.Fatal(e)
	}
	list, e = s.List("")
	if e != nil || len(list) != 1 || len(list[0].Warnings) == 0 {
		t.Fatal(list, e)
	}
	removed, e := s.Unlink(p.ID)
	if e != nil || !removed {
		t.Fatal(removed, e)
	}
	if _, e = os.Stat(path); e != nil {
		t.Fatal("unlink removed source", e)
	}
	for _, p := range []string{filepath.Join(s.ConfigRoot, "plugins", "config", "example.local"), filepath.Join(s.StateRoot, "plugins", "example.local")} {
		if _, e = os.Stat(p); e != nil {
			t.Fatal("durable directory removed", e)
		}
	}
}
func TestRegistryConcurrentWriters(t *testing.T) {
	root := t.TempDir()
	s := &Store{ConfigRoot: root, StateRoot: filepath.Join(root, "state"), Version: "0.1.0"}
	var wg sync.WaitGroup
	for _, id := range []string{"a", "b", "c", "d"} {
		path := writeManifest(t, strings.ReplaceAll(minimal, "example.local", id))
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.Link(path, true, nil); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	list, e := s.List("")
	if e != nil || len(list) != 4 {
		t.Fatal(list, e)
	}
}
func TestRegistryFailureDoesNotReplace(t *testing.T) {
	root := t.TempDir()
	s := &Store{ConfigRoot: root, StateRoot: filepath.Join(root, "state"), Version: "0.1.0"}
	if _, e := s.Link(writeManifest(t, minimal), true, nil); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Link(writeManifest(t, strings.ReplaceAll(minimal, "min_ikesu_version = \"0.1.0\"", "min_ikesu_version = \"9.0.0\"")), false, nil); e == nil {
		t.Fatal("accepted newer manifest")
	}
	list, e := s.List("")
	if e != nil || len(list) != 1 || !list[0].Enabled {
		t.Fatal(list, e)
	}
	if e = os.WriteFile(filepath.Join(root, "plugins.json"), []byte("broken"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Link(writeManifest(t, minimal), true, nil); e == nil {
		t.Fatal("overwrote corrupt registry")
	}
}
