package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
)

// Directory existence cannot choose the canonical stage-one namespace.
func TestStageOnePathReadWriteMatrix(t *testing.T) {
	t.Cleanup(func() { api.ConfigureExecutable("gtx") })
	for _, dirs := range []string{"neither", "old-only", "new-only", "both"} {
		for _, names := range []string{"none", "old", "new", "conflict"} {
			for _, explicit := range []bool{false, true} {
				for _, exe := range []string{"gtx", "ikesu"} {
					t.Run(fmt.Sprintf("%s/%s/session-%v/%s", dirs, names, explicit, exe), func(t *testing.T) {
						home := t.TempDir()
						t.Setenv("HOME", home)
						t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
						for _, key := range []string{"GTX_CONFIG", "IKESU_CONFIG", "GTX_STATE_DIR", "IKESU_STATE_DIR", "GTX_SOCKET_PATH", "IKESU_SOCKET_PATH", "GTX_SOCKET_PATH_EXPLICIT", "IKESU_SOCKET_PATH_EXPLICIT", "GTX_DB_PATH", "IKESU_DB_PATH"} {
							t.Setenv(key, "")
						}
						for _, ns := range []string{"gtx", "ikesu"} {
							exists := dirs == "both" || dirs == "old-only" && ns == "gtx" || dirs == "new-only" && ns == "ikesu"
							if exists {
								if err := os.MkdirAll(filepath.Join(home, ".local", "state", ns), 0700); err != nil {
									t.Fatal(err)
								}
								configDir := filepath.Join(home, "config", ns)
								if err := os.MkdirAll(configDir, 0700); err != nil {
									t.Fatal(err)
								}
								text := "onboarding=false\n"
								if ns == "ikesu" {
									text = "onboarding=true\n"
								}
								if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(text), 0600); err != nil {
									t.Fatal(err)
								}
							}
						}
						root := filepath.Join(home, ".local", "state", "gtx")
						cfg := filepath.Join(home, "config", "gtx", "config.toml")
						socket := ""
						for _, ns := range []string{"GTX", "IKESU"} {
							active := names == "conflict" || names == "old" && ns == "GTX" || names == "new" && ns == "IKESU"
							if active {
								dir := filepath.Join(home, ns)
								t.Setenv(ns+"_STATE_DIR", filepath.Join(dir, "state"))
								t.Setenv(ns+"_CONFIG", filepath.Join(dir, "config.toml"))
								t.Setenv(ns+"_SOCKET_PATH", filepath.Join(dir, "session", "gtx.sock"))
								root = filepath.Join(dir, "state")
								cfg = filepath.Join(dir, "config.toml")
								socket = filepath.Join(dir, "session", "gtx.sock")
							}
						}
						api.ConfigureExecutable(exe)
						if Path() != cfg || api.StateDir() != root {
							t.Fatalf("config/state: %s / %s want %s / %s", Path(), api.StateDir(), cfg, root)
						}
						initial, err := Load()
						if err != nil {
							t.Fatal(err)
						}
						if names == "none" && (dirs == "old-only" || dirs == "both") {
							if initial.Onboarding == nil || *initial.Onboarding {
								t.Fatal("did not read existing old config")
							}
						} else if initial.Onboarding != nil {
							t.Fatal("read a config outside the selected path")
						}
						wantSocket := socket
						if explicit || socket == "" {
							session := "default"
							if explicit {
								session = "named"
							}
							wantSocket = filepath.Join(root, session, "gtx.sock")
						}
						if got := api.APISocket(map[bool]string{false: "default", true: "named"}[explicit], explicit, os.Getenv); got != wantSocket {
							t.Fatalf("socket=%s want %s", got, wantSocket)
						}
						if err := SaveOnboarding(false); err != nil {
							t.Fatal(err)
						}
						loaded, err := Load()
						if err != nil || loaded.Onboarding == nil || *loaded.Onboarding {
							t.Fatal(loaded.Onboarding, err)
						}
						if _, err := os.Stat(cfg); err != nil {
							t.Fatal(err)
						}
						db := dispatch.DBPath()
						if db != filepath.Join(root, "gtx.db") {
							t.Fatal(db)
						}
						store, err := dispatch.Open(db)
						if err != nil {
							t.Fatal(err)
						}
						if err := store.Close(); err != nil {
							t.Fatal(err)
						}
						if _, err := os.Stat(db); err != nil {
							t.Fatal(err)
						}
						ignored, err := os.ReadFile(filepath.Join(home, "config", "ikesu", "config.toml"))
						if dirs == "new-only" || dirs == "both" {
							if err != nil || string(ignored) != "onboarding=true\n" {
								t.Fatal("modified ignored config", err, string(ignored))
							}
						} else if !os.IsNotExist(err) {
							t.Fatal("created ignored config", err)
						}
						if _, err := os.Stat(filepath.Join(home, ".local", "state", "ikesu", "gtx.db")); !os.IsNotExist(err) {
							t.Fatal("wrote ignored DB", err)
						}
						t.Logf("exe=%s dirs=%s env=%s explicit=%v socket=%s db=%s config-read-write=%s", exe, dirs, names, explicit, wantSocket, db, cfg)
					})
				}
			}
		}
	}
}
