package client

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestOyagoiStoreLastExplicitSelectionAndConditionalClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oyagoi.json")
	a, b := oyagoiStore{path}, oyagoiStore{path}
	first := oyagoiSelection{Machine: "local", Session: "one", Workspace: 1, Generation: "first"}
	second := oyagoiSelection{Machine: "m1", Session: "two", Workspace: 1, Generation: "second"}
	if err := a.set(&first); err != nil {
		t.Fatal(err)
	}
	if got, err := b.load(); err != nil || !reflect.DeepEqual(got, &first) {
		t.Fatalf("first: %v, %v", got, err)
	}
	if err := b.set(&second); err != nil {
		t.Fatal(err)
	}
	if cleared, err := a.clearIf(first); err != nil || cleared {
		t.Fatalf("stale clear: %v, %v", cleared, err)
	}
	if got, err := a.load(); err != nil || !reflect.DeepEqual(got, &second) {
		t.Fatalf("second: %v, %v", got, err)
	}
	if cleared, err := a.clearIf(second); err != nil || !cleared {
		t.Fatalf("clear: %v, %v", cleared, err)
	}
	if got, err := b.load(); err != nil || got != nil {
		t.Fatalf("empty: %v, %v", got, err)
	}
}
