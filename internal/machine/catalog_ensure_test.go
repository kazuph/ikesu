package machine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCatalogEnsurePreservesConcurrentFirstAddAndExistingFile(t *testing.T) {
	c := Catalog{Path: filepath.Join(t.TempDir(), "session", "machines.json")}
	start := make(chan struct{})
	errors := make(chan error, 3)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() { defer workers.Done(); <-start; errors <- c.Ensure() }()
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		_, err := c.AddVerified("own-host", "saved", "own-session")
		errors <- err
	}()
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.ReadFile(c.Path)
	if err != nil {
		t.Fatal(err)
	}
	var profiles []Profile
	if err := json.Unmarshal(before, &profiles); err != nil || len(profiles) != 1 {
		t.Fatal("initialization lost the concurrently saved profile", string(before), err)
	}
	if profiles[0].Label != "saved" || profiles[0].Session != "own-session" {
		t.Fatal(profiles)
	}
	if err := c.Ensure(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(c.Path)
	if err != nil || string(before) != string(after) {
		t.Fatal("existing catalog changed", err)
	}
}
