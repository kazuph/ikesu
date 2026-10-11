package machine

import (
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedSSHPrivateConfigPreservesUserKeepaliveAndCleansUp(t *testing.T) {
	teststate.Lock(t)
	home := t.TempDir()
	teststate.Setenv(t, "HOME", home)
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("Host example\n  ServerAliveInterval 91\n  ServerAliveCountMax 7\n")
	path := filepath.Join(home, ".ssh", "config")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	m, err := PrepareSSH(config.Config{}, "example")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, tc := range []struct{ target, want string }{{"example", "serveraliveinterval 91\n"}, {"other.invalid", "serveraliveinterval 15\n"}} {
		out, err := exec.Command("/usr/bin/ssh", m.Args([]string{"-G", tc.target})...).Output()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), tc.want) || !strings.Contains(string(out), "controlmaster auto\n") {
			t.Fatal(string(out))
		}
	}
	st, err := os.Stat(filepath.Join(m.Dir, "config"))
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal(st, err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != string(original) {
		t.Fatal("user config changed")
	}
	dir := m.Dir
	m.Close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	no := false
	c := config.Config{}
	c.Remote.ManageSSHConfig = &no
	plain, err := PrepareSSH(c, "example")
	if err != nil || plain.Dir != "" || len(plain.Args([]string{"example"})) != 1 {
		t.Fatal(plain, err)
	}
}
