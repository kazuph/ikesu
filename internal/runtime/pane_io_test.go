package runtime

import (
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/teststate"
	"strings"
	"testing"
	"time"
)

func TestPaneInputEcho(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SHELL", "/bin/cat")
	s := NewSupervisor()
	p, err := s.Spawn(1, "/tmp", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer s.KillAll()
	p.WithEmu(func(e *vt.Emulator) { e.Paste("echo HELLO\n") })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, l := range p.ScreenLines() {
			if strings.Contains(l, "HELLO") {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("never saw HELLO; screen=%q", strings.Join(p.ScreenLines(), "|"))
}
