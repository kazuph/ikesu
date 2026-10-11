package cli

import (
	"bytes"
	"encoding/json"
	"github.com/kazuph/goterrace/internal/guard"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"testing"
)

func TestGuardCheckJSON(t *testing.T) {
	teststate.Lock(t)
	d := t.TempDir()
	teststate.Setenv(t, "HOME", d)
	teststate.Setenv(t, "GTX_CONFIG", filepath.Join(d, "config.toml"))
	teststate.Setenv(t, "GTX_STATE_DIR", filepath.Join(d, "state"))
	var out, errout bytes.Buffer
	code := Run([]string{"guard", "check", "--cwd", d, "--launch-cwd", d, "--", "git", "push", "--force"}, Options{Stdout: &out, Stderr: &errout})
	var v guard.Verdict
	if e := json.Unmarshal(out.Bytes(), &v); e != nil || code != 0 || v.Decision != "block" {
		t.Fatal(code, out.String(), errout.String(), e)
	}
	if _, e := os.Stat(filepath.Join(d, "state")); !os.IsNotExist(e) {
		t.Fatal("check created state", e)
	}
}

func TestGuardCheckObservedOutputContract(t *testing.T) {
	teststate.Lock(t)
	d := t.TempDir()
	teststate.Setenv(t, "HOME", d)
	teststate.Setenv(t, "GTX_CONFIG", filepath.Join(d, "config.toml"))
	var out, errs bytes.Buffer
	code := Run([]string{"guard", "check", "--", "echo", "hello", "--cwd", d, "--launch-dir", d}, Options{Stdout: &out, Stderr: &errs})
	var v map[string]any
	if e := json.Unmarshal(out.Bytes(), &v); e != nil || code != 0 {
		t.Fatal(out.String(), errs.String(), e)
	}
	if len(v) != 5 || v["verdict"] != "allow" || v["reason"] != "" || v["rule"] != "default" || v["approval_key"] != nil || v["suppressed_by"] != nil {
		t.Fatal(v)
	}
}
