package config

import (
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestG2SettingsAtomicSavePreservesUnknownConfig(t *testing.T) {
	teststate.Lock(t)
	p := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, EnvVar, p)
	os.WriteFile(p, []byte("[ui.toast]\ndelivery='off'\n[ui.sound]\ncustom_path='keep.mp3'\n[theme]\nauto_switch=true\n[other]\nkeep='unchanged'\n"), 0600)
	for _, err := range []error{SaveTheme("dracula"), SaveSound(false), SaveToast(ToastTerminal)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	c, err := Load()
	if err != nil || c.ThemeName() != "dracula" || c.SoundEnabled() || c.ToastDelivery() != ToastTerminal {
		t.Fatal(c, err)
	}
	b, _ := os.ReadFile(p)
	for _, v := range []string{"keep.mp3", "auto_switch = false", "unchanged"} {
		if !strings.Contains(string(b), v) {
			t.Fatal("lost config", v, string(b))
		}
	}
	if SaveToast("unsupported") == nil {
		t.Fatal("unsupported toast accepted")
	}
}
