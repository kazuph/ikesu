package machine

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestCatalogCRUDAndStaleRemoval(t *testing.T) {
	c := Catalog{filepath.Join(t.TempDir(), "state", "machines.json")}
	a, err := c.AddVerified("host", "first", "default")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.AddVerified("host", "second", "other")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.ID) != 13 || a.ID[0] != 'm' {
		t.Fatal(a.ID)
	}
	renamed, err := c.Change(a.ID, "rename", "new", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Change(a.ID, "remove", "", &a); err == nil {
		t.Fatal("stale removal succeeded")
	}
	if _, err = c.Change(a.ID, "disable", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = Resolve(c.Load(), "new", true); err == nil {
		t.Fatal("disabled profile resolved")
	}
	if _, err = c.Change(a.ID, "enable", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Change(a.ID, "remove", "", &renamed); err != nil {
		t.Fatal(err)
	}
	if got := c.Load(); len(got) != 1 || got[0] != b {
		t.Fatal(got)
	}
}

func TestCatalogValidationAndAmbiguity(t *testing.T) {
	profiles := []Profile{{ID: "m1", Label: "x", Target: "host", Session: "default", Enabled: true}, {ID: "m2", Label: "x", Target: "other", Session: "default", Enabled: true}}
	for _, ref := range []string{"x", "missing"} {
		if _, err := Resolve(profiles, ref, false); err == nil {
			t.Fatal(ref)
		}
	}
	for _, v := range [][3]string{{"-bad", "a", "default"}, {"host", "", "default"}, {"host", "a", "../bad"}, {"new", "x", "default"}, {"host", "a", "default"}} {
		if ValidateAdd(profiles, v[0], v[1], v[2]) == nil {
			t.Fatal(v)
		}
	}
	c := Catalog{filepath.Join(t.TempDir(), "machines.json")}
	raw := []byte("broken")
	os.WriteFile(c.Path, raw, 0600)
	if len(c.Load()) != 0 {
		t.Fatal("corruption not empty")
	}
	if _, err := c.Change("missing", "remove", "", nil); err == nil {
		t.Fatal("unknown succeeded")
	}
	after, _ := os.ReadFile(c.Path)
	if string(after) != string(raw) {
		t.Fatal("failed update changed file")
	}
}

func TestCatalogConcurrentWritersAndSaveFailure(t *testing.T) {
	c := Catalog{filepath.Join(t.TempDir(), "machines.json")}
	var wg sync.WaitGroup
	for _, label := range []string{"a", "b", "c"} {
		wg.Add(1)
		go func(label string) {
			defer wg.Done()
			if _, err := c.AddVerified(label, label, "default"); err != nil {
				t.Error(err)
			}
		}(label)
	}
	wg.Wait()
	if len(c.Load()) != 3 {
		t.Fatal(c.Load())
	}
	bad := Catalog{filepath.Join(c.Path, "machines.json")}
	if _, err := bad.AddVerified("h", "l", "default"); err == nil {
		t.Fatal("save failure ignored")
	}
}

func TestSSHArguments(t *testing.T) {
	want := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "--", "host", "true"}
	if !reflect.DeepEqual(ProbeArgs("host"), want) {
		t.Fatal(ProbeArgs("host"))
	}
	if Probe(context.Background(), "-bad") == nil {
		t.Fatal("option target accepted")
	}
	p := Profile{Target: "host", Session: "work"}
	args := ForwardArgs(p, "/tmp/my gtx", []string{"pane", "send-text", "p1", "a'b;$HOME"})
	if !reflect.DeepEqual(args[:6], want[:6]) || args[6] != Quote("/tmp/my gtx") || args[len(args)-1] != Quote("a'b;$HOME") {
		t.Fatal(args)
	}
}
