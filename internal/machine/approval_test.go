package machine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func TestApprovalAcrossProcesses(t *testing.T) {
	if os.Getenv("IKESU_APPROVAL_TEST_CHILD") == "1" {
		err := claimApproval("saved-machine", "https://example.invalid/approval", func() error {
			f, err := os.OpenFile(os.Getenv("IKESU_APPROVAL_TEST_OPENED"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = fmt.Fprintln(f, os.Getpid())
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	root := t.TempDir()
	opened := filepath.Join(root, "opened")
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestApprovalAcrossProcesses$")
			cmd.Env = append(os.Environ(), "IKESU_STATE_DIR="+root, "IKESU_APPROVAL_TEST_CHILD=1", "IKESU_APPROVAL_TEST_OPENED="+opened)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s: %v", out, err)
			}
		}()
	}
	wg.Wait()
	data, err := os.ReadFile(opened)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}
	if count != 1 {
		t.Fatal("browser launches", count)
	}
	raw, err := os.ReadFile(filepath.Join(root, "machine-approvals", filepath.Base(approvalFile("saved-machine"))))
	if err != nil {
		t.Fatal(err)
	}
	var record ApprovalRecord
	if err = json.Unmarshal(raw, &record); err != nil || record.Client == "" || record.URL != "https://example.invalid/approval" || record.OpenedAt.IsZero() {
		t.Fatal(record, err)
	}
}

func TestApprovalClearRetryAndWriteFailure(t *testing.T) {
	t.Setenv("IKESU_STATE_DIR", t.TempDir())
	opens := 0
	open := func() error { opens++; return nil }
	for _, url := range []string{"first", "reconnect", "second-client"} {
		if err := claimApproval("id-1", url, open); err != nil {
			t.Fatal(err)
		}
	}
	if opens != 1 {
		t.Fatal(opens)
	}
	if err := ClearApproval("id-1"); err != nil {
		t.Fatal(err)
	}
	if err := claimApproval("id-1", "new-url", open); err != nil {
		t.Fatal(err)
	}
	if opens != 2 {
		t.Fatal(opens)
	}
	if err := claimApproval("id-2", "other-machine", open); err != nil {
		t.Fatal(err)
	}
	if opens != 3 {
		t.Fatal(opens)
	}
	if err := ClearApproval("id-1"); err != nil {
		t.Fatal(err)
	}
	if err := claimApproval("id-1", "failed", func() error { return fmt.Errorf("launch failed") }); err == nil {
		t.Fatal("failure hidden")
	}
	if _, err := os.Stat(approvalFile("id-1")); !os.IsNotExist(err) {
		t.Fatal("failed launch kept record", err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "machine-approvals"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IKESU_STATE_DIR", root)
	if err := claimApproval("id-1", "write-failure", open); err == nil {
		t.Fatal("write failure hidden")
	}
	if opens != 3 {
		t.Fatal("opened without durable record", opens)
	}
}
