package dispatch

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// A CLI process opens the server's live store while the server holds write
// transactions. Open must wait for the writer instead of failing with
// "database is locked". The writer keeps each transaction open for a short
// while so the openers really contend for the write lock.
func TestOpenDuringConcurrentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ikesu.db")
	writer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	kind := "agent"
	stop := make(chan struct{})
	writes := make(chan error, 1)
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				writes <- nil
				return
			default:
			}
			name := fmt.Sprintf("server-%d", i)
			if err := holdWrite(writer, &kind, &name); err != nil {
				writes <- err
				return
			}
		}
	}()
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
	close(stop)
	if err := <-writes; err != nil {
		t.Error(err)
	}
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}

// holdWrite inserts one actor inside a write transaction held for 10 ms.
func holdWrite(s *Store, kind, name *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.execLocked("BEGIN IMMEDIATE"); err != nil {
		return err
	}
	if _, err := s.upsertActorLocked(kind, name, nil, nil, nil, nil); err != nil {
		s.execLocked("ROLLBACK")
		return err
	}
	time.Sleep(10 * time.Millisecond)
	return s.execLocked("COMMIT")
}

// A server and CLI processes create a new session's database at the same
// time. Every Open must succeed even though only one of them can switch the
// new file to WAL mode at a time.
func TestOpenFreshDatabaseConcurrently(t *testing.T) {
	for round := 0; round < 20; round++ {
		path := filepath.Join(t.TempDir(), fmt.Sprintf("r%d", round), "ikesu.db")
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s, err := Open(path)
				if err != nil {
					errs <- err
					return
				}
				errs <- s.Close()
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
	}
}
