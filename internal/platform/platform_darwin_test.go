//go:build darwin

package platform

import (
	"os/exec"
	"reflect"
	"testing"
	"time"
)

func TestG9ProcArgsReadsActualWrapperArguments(t *testing.T) {
	argv := []string{"/usr/bin/python3", "-c", "import time; time.sleep(60)", "", "trailing argument"}
	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := procArgs(cmd.Process.Pid)
		if len(got) == len(argv) && got[0] != "" {
			// macOS Python can replace argv[0] with its framework executable.
			if !reflect.DeepEqual(got[1:], argv[1:]) {
				t.Fatalf("argv=%q want=%q", got, argv)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("actual process argv=%q want=%q", got, argv)
		}
		time.Sleep(time.Millisecond)
	}
}
