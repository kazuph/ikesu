package dispatch

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// A CLI process opens the server's live store while the server holds a write
// transaction. Open on an existing WAL database must not need the write lock,
// so every Open finishes while the transaction is still held.
func TestOpenDuringConcurrentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ikesu.db")
	writer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	kind, name := "agent", "server"
	writer.mu.Lock()
	if err := writer.execLocked("BEGIN IMMEDIATE"); err != nil {
		writer.mu.Unlock()
		t.Fatal(err)
	}
	if _, err := writer.upsertActorLocked(&kind, &name, nil, nil, nil, nil); err != nil {
		writer.execLocked("ROLLBACK")
		writer.mu.Unlock()
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for attempt := 0; attempt < 10; attempt++ {
				s, err := Open(path)
				if err != nil {
					failures <- fmt.Errorf("open attempt %d: %w", attempt, err)
					return
				}
				if err := s.Close(); err != nil {
					failures <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	// The transaction is released only after every Open returned, so each
	// Open ran while the write lock was held.
	if err := writer.execLocked("COMMIT"); err != nil {
		t.Error(err)
	}
	writer.mu.Unlock()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}

// A server and CLI processes create a new session's database at the same
// time. Every Open must succeed even though only one of them can switch the
// new file to WAL mode at a time. All openers are released together so they
// race for the switch.
func TestOpenFreshDatabaseConcurrently(t *testing.T) {
	for round := 0; round < 20; round++ {
		path := filepath.Join(t.TempDir(), fmt.Sprintf("r%d", round), "ikesu.db")
		var ready, wg sync.WaitGroup
		start := make(chan struct{})
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			ready.Add(1)
			wg.Add(1)
			go func() {
				defer wg.Done()
				ready.Done()
				<-start
				s, err := Open(path)
				if err != nil {
					errs <- err
					return
				}
				errs <- s.Close()
			}()
		}
		ready.Wait()
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
	}
}
