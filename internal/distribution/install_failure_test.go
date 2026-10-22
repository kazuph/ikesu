package distribution

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSameVersionTamperingIsNotOverwritten(t *testing.T) {
	i, r := fixtureInstaller(t)
	dir, err := i.Install(context.Background(), r, false)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "fixture")
	if err := os.WriteFile(file, []byte("different content"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Install(context.Background(), r, false); err == nil {
		t.Fatal("different same-version content accepted")
	}
	data, _ := os.ReadFile(file)
	if string(data) != "different content" {
		t.Fatal("existing version overwritten")
	}
}

func TestConcurrentInstallationAndCancelledPreparationDoNotSwitch(t *testing.T) {
	i, r := fixtureInstaller(t)
	if err := os.MkdirAll(i.Directory(), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(i.Directory(), ".install.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Install(context.Background(), r, false); err == nil {
		t.Fatal("concurrent installation accepted")
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := i.Install(ctx, r, false); err == nil {
		t.Fatal("cancelled preparation succeeded")
	}
	if _, err := os.Stat(filepath.Join(i.Directory(), "current-version")); !os.IsNotExist(err) {
		t.Fatal("cancelled preparation switched version", err)
	}
}
