package client

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
)

func TestOyagoiWatchIgnoresPreferencesAndTracksReplacements(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "oyagoi.json")
	if err := os.WriteFile(path, []byte("initial"), 0600); err != nil {
		t.Fatal(err)
	}
	changes := make(chan error, 32)
	stop, err := watchMachineCatalog(context.Background(), path, func(_ context.Context, err error) { changes <- err }, true)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if err := endpoint.SaveViewPreferences(filepath.Join(dir, "local-test.json"), endpoint.ViewPreferences{SidebarWidth: 20}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-changes:
		t.Fatalf("preferences triggered HQ notification: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	await := func() {
		t.Helper()
		select {
		case err := <-changes:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("HQ change was not observed")
		}
	}
	for _, value := range []string{"one", "two"} {
		tmp := filepath.Join(dir, ".next")
		if err := os.WriteFile(tmp, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, path); err != nil {
			t.Fatal(err)
		}
		await()
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	await()
	if err := os.WriteFile(filepath.Join(dir, ".next"), []byte("recreated"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, ".next"), path); err != nil {
		t.Fatal(err)
	}
	await()
}
