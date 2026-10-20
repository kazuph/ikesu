package server

import (
	"github.com/kazuph/ikesu/internal/notify"
	"github.com/kazuph/ikesu/internal/teststate"
	"strings"
	"testing"
)

func TestNotificationUsesEightyLogicalLinesAfterWrapping(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "logical-notify")
	p, err := s.sup.SpawnWith(1, t.TempDir(), 20, 5, nil, []string{"/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.sup.KillAll)
	p.ReplayHistory("• latest answer\r\n" + strings.Repeat(strings.Repeat("continuation ", 5)+"\r\n", 70))
	lines, err := p.ReadLines("recent_unwrapped", true)
	if err != nil {
		t.Fatal(err)
	}
	body := notify.Body(lines, "title")
	if !strings.HasPrefix(body, "latest answer ") {
		t.Fatal(body)
	}
	physical := notify.Body(append(p.ScrollbackLines(), p.ScreenLines()...), "title")
	if strings.HasPrefix(physical, "latest answer") {
		t.Fatal("fixture did not distinguish physical rows")
	}
}
