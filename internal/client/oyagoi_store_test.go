package client

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOyagoiInvalidSettingsKeepFileAndCanBeReplacedReview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oyagoi.json")
	for _, text := range []string{"{", `{"machine":"local","session":"x","workspace":1}`} {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		m := &machineClient{oyagoiStore: oyagoiStore{path}, oyagoi: &oyagoiSelection{Machine: "old"}, endpoints: map[string]*managedEndpoint{}}
		if err := m.refreshOyagoi(); err == nil || m.oyagoi != nil {
			t.Fatalf("invalid selection remained active: %v %+v", err, m.oyagoi)
		}
		data, _ := os.ReadFile(path)
		if string(data) != text {
			t.Fatal("invalid file changed")
		}
		want := &oyagoiSelection{Machine: "local", Session: "x", Workspace: 1, Generation: "new"}
		if err := m.oyagoiStore.set(want); err != nil {
			t.Fatal(err)
		}
		if err := m.refreshOyagoi(); err != nil || !reflect.DeepEqual(m.oyagoi, want) {
			t.Fatalf("set failed: %+v %v", m.oyagoi, err)
		}
	}
}

func TestOyagoiCollapsedSharedAcrossReplacementAndConditionalClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oyagoi.json")
	if err := os.WriteFile(path, []byte(`{"machine":"local","session":"one","workspace":1,"generation":"g"}`), 0600); err != nil {
		t.Fatal(err)
	}
	a, b := oyagoiStore{path}, oyagoiStore{path}
	old, err := a.load()
	if err != nil || old.Collapsed {
		t.Fatalf("legacy: %+v %v", old, err)
	}
	if err := a.toggleCollapsed(); err != nil {
		t.Fatal(err)
	}
	got, err := b.load()
	if err != nil || !got.Collapsed {
		t.Fatalf("shared: %+v %v", got, err)
	}
	next := oyagoiSelection{Machine: "remote", Session: "two", Workspace: 2, Generation: "h"}
	if err := b.set(&next); err != nil {
		t.Fatal(err)
	}
	got, err = a.load()
	if err != nil || !got.Collapsed {
		t.Fatalf("replacement: %+v %v", got, err)
	}
	if cleared, err := a.clearIf(*old); err != nil || cleared {
		t.Fatalf("stale clear: %v %v", cleared, err)
	}
	next.Collapsed = false
	if cleared, err := a.clearIf(next); err != nil || !cleared {
		t.Fatalf("identity clear: %v %v", cleared, err)
	}
}
