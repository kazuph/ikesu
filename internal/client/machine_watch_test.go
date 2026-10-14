package client

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMachineCatalogWatchFollowsAtomicReplacementDeletionAndRecreation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "machines.json")
	if err := os.WriteFile(path, []byte("initial"), 0600); err != nil {
		t.Fatal(err)
	}
	changes := make(chan string, 8)
	stop, err := watchMachineCatalog(context.Background(), path, func(ctx context.Context, err error) {
		value := "missing"
		if err != nil {
			value = err.Error()
		} else if data, err := os.ReadFile(path); err == nil {
			value = string(data)
		}
		select {
		case changes <- value:
		case <-ctx.Done():
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	await := func(want string) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case got := <-changes:
				if got == want {
					return
				}
				t.Fatalf("catalog change=%q want=%q", got, want)
			case <-deadline:
				t.Fatalf("catalog change %q was not observed", want)
			}
		}
	}
	for _, value := range []string{"replacement-one", "replacement-two"} {
		if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		replacement := filepath.Join(dir, ".machines-next")
		if err := os.WriteFile(replacement, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, path); err != nil {
			t.Fatal(err)
		}
		await(value)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	await("missing")
	// The catalog is always written as a temporary file renamed into place.
	// A plain WriteFile creates an empty file first, which a directory
	// watch may observe before the bytes arrive.
	recreated := filepath.Join(dir, ".machines-recreated")
	if err := os.WriteFile(recreated, []byte("recreated"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(recreated, path); err != nil {
		t.Fatal(err)
	}
	await("recreated")
	ended := make(chan struct{})
	go func() { stop(); close(ended) }()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("catalog watcher survived cancellation")
	}
}
