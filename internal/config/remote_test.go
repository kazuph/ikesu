package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoteAutoDisconnectAfter(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value string
		want  time.Duration
		warn  bool
	}{
		{value: ""},
		{value: "0"},
		{value: `"0"`},
		{value: `"10m"`, want: 10 * time.Minute},
		{value: `"-10m"`, warn: true},
		{value: `"soon"`, warn: true},
		{value: "true", warn: true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			source := "[remote]\n"
			if tc.value != "" {
				source += "auto_disconnect_after = " + tc.value + "\n"
			}
			cfg, err := Decode([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Remote.AutoDisconnectAfter != tc.want || strings.Contains(cfg.Warning(), "remote.auto_disconnect_after") != tc.warn {
				t.Fatalf("duration = %v, warnings = %v", cfg.Remote.AutoDisconnectAfter, cfg.Warnings)
			}
		})
	}
}

func TestReloadRemoteAutoDisconnectAfter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv(EnvVar, path)
	write := func(value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("[remote]\nauto_disconnect_after = "+value+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(`"10m"`)
	current, err := Load()
	if err != nil || current.Remote.AutoDisconnectAfter != 10*time.Minute {
		t.Fatalf("load = %v, %v", current.Remote.AutoDisconnectAfter, err)
	}
	write("0")
	next, err := (Edit{Kind: "reload"}).ApplyTo(current)
	if err != nil || next.Remote.AutoDisconnectAfter != 0 || len(next.Warnings) != 0 {
		t.Fatalf("reload = %v, %v, warnings: %v", next.Remote.AutoDisconnectAfter, err, next.Warnings)
	}
}
