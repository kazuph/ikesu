package endpoint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRightSidebarPreferenceFileBackwardReadAndNewModes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "local-default.json")
	for _, data := range []string{`{"sidebar_width":26,"sidebar_collapsed":true}`, `{"sidebar_width":26,"sidebar_hidden":true,"right_sidebar_mode":"compact"}`, `{"sidebar_width":26,"right_sidebar_mode":"hidden"}`, `{"sidebar_width":10,"sidebar_collapsed":true,"right_sidebar_mode":"compact","right_sidebar_width":5}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		p, err := LoadViewPreferences(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = SaveViewPreferences(path, *p); err != nil {
			t.Fatal(err)
		}
		got, err := LoadViewPreferences(path)
		if err != nil || got.SidebarWidth != p.SidebarWidth || got.SidebarHidden != p.SidebarHidden || got.RightSidebarMode != p.RightSidebarMode || got.RightSidebarWidth != p.RightSidebarWidth {
			t.Fatal("roundtrip", got, err)
		}
	}
	if err := os.WriteFile(path, []byte(`{"sidebar_width":26,"right_sidebar_mode":"invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadViewPreferences(path); err == nil {
		t.Fatal("unknown mode silently accepted")
	}
}

func TestRightSidebarPreferenceRejectsNegativeWidth(t *testing.T) {
	p := ViewPreferences{SidebarWidth: 26, RightSidebarWidth: -1}
	if p.Validate() == nil {
		t.Fatal("negative sidebar width accepted")
	}
}
