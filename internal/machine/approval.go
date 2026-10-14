package machine

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/childenv"
)

const ApprovalStopped = "承認待ち。クリックで承認ページを開く"

// ApprovalRecord is shared across sessions and client processes for one saved machine ID.
type ApprovalRecord struct {
	Client   string    `json:"client"`
	URL      string    `json:"url"`
	OpenedAt time.Time `json:"opened_at"`
}

func approvalFile(machineID string) string {
	return filepath.Join(api.StateDir(), "machine-approvals", fmt.Sprintf("%x.json", sha256.Sum256([]byte(machineID))))
}

func withApproval(machineID string, action func(string) error) error {
	path := approvalFile(machineID)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return action(path)
}

// OpenApproval holds the cross-process lock through browser launch, so a second
// client cannot open a duplicate even while the first launch is in progress.
func OpenApproval(machineID, url string) error {
	return claimApproval(machineID, url, func() error {
		cmd := childenv.Command("open", "--", url)
		if err := cmd.Start(); err != nil {
			return err
		}
		go cmd.Wait()
		return nil
	})
}

func claimApproval(machineID, url string, open func() error) error {
	return withApproval(machineID, func(path string) error {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		data, err := json.Marshal(ApprovalRecord{Client: fmt.Sprintf("pid:%d", os.Getpid()), URL: url, OpenedAt: time.Now().UTC()})
		if err != nil {
			return err
		}
		tmp, err := os.CreateTemp(filepath.Dir(path), ".approval-")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		_, err = tmp.Write(data)
		closeErr := tmp.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if err := os.Rename(tmp.Name(), path); err != nil {
			return err
		}
		if err := open(); err != nil {
			_ = os.Remove(path)
			return err
		}
		return nil
	})
}

// ClearApproval is used after connection success and an explicit header retry.
func ClearApproval(machineID string) error {
	return withApproval(machineID, func(path string) error {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	})
}
