package client

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
)

func TestClientPreferencesPersistLocallyAndReportWriteFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GTX_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv(config.EnvVar, filepath.Join(root, "config.toml"))
	s, err := loadLocalSettings("endpoint-preferences")
	if err != nil {
		t.Fatal(err)
	}
	split := .7
	want := endpoint.ViewPreferences{SidebarWidth: 42, SidebarManual: true, SidebarCollapsed: true, SidebarSectionSplit: &split}
	if err := s.save(want); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadLocalSettings("endpoint-preferences")
	if err != nil || !reflect.DeepEqual(loaded.preferences, &want) {
		t.Fatalf("reload: %v, %v", loaded, err)
	}
	stat, err := os.Stat(s.path)
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatalf("preferences permissions: %v, %v", stat, err)
	}
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocked, "preferences.json")
	if err := s.save(endpoint.ViewPreferences{SidebarWidth: 26}); err == nil {
		t.Fatal("write failure was concealed")
	}
	if !reflect.DeepEqual(s.preferences, &want) {
		t.Fatal("failed write replaced committed preferences")
	}
	bytes, err := os.ReadFile(blocked)
	if err != nil || string(bytes) != "preserve" {
		t.Fatal("write failure damaged blocking file")
	}
}

func TestClientPreferencesRejectCorruptState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GTX_STATE_DIR", root)
	t.Setenv(config.EnvVar, filepath.Join(root, "config.toml"))
	s, err := loadLocalSettings("endpoint-preferences")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"{", `{"sidebar_width":0}`, `{"sidebar_width":26,"sidebar_section_split":0.95}`} {
		if err := os.WriteFile(s.path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadLocalSettings("endpoint-preferences"); err == nil {
			t.Fatalf("accepted invalid state: %s", data)
		}
	}
}
