package server

import (
	"testing"

	"github.com/kazuph/ikesu/internal/runtime"
)

func TestMailWriteWaitsForRecentExternalInput(t *testing.T) {
	sup := runtime.NewSupervisor()
	pane, err := sup.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	defer sup.Kill(1)
	s := &Server{sup: sup}
	pane.RecordExternalInput()
	written, _ := s.deliverMailWrite(mailWrite{pane: 1, owner: pane, payload: []byte("interrupted message\n")})
	if written {
		t.Fatal("mail was written to the PTY while external input was recent")
	}
}
