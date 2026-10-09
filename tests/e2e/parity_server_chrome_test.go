package e2e

import (
	"strings"
	"testing"
	"time"
)

func TestServerCopyStatusConsumesButtonAndYanksOSC52(t *testing.T) {
	s := start(t)
	s.waitFor("server boot", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	var raw strings.Builder
	s.observeOutput = func(b []byte) { raw.Write(b) }
	s.send("printf 'COPY_%s\\n' SERVER\r")
	s.waitFor("copy source", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "COPY_SERVER") })
	s.send("\x02[")
	s.waitFor("server copy status", 5*time.Second, func(tx string) bool {
		return strings.Contains(strings.Split(tx, "\n")[termH-1], " COPY ") && strings.Contains(tx, "y/enter copy")
	})
	before := s.text()
	x, y := s.find(" COPY ")
	s.click(x, y)
	s.pump(time.Now().Add(200 * time.Millisecond))
	if s.text() != before {
		t.Fatal("server copy status click changed the mode", s.text())
	}
	s.send("/COPY_SERVER\r llll\r")
	s.waitFor("OSC52 and copy mode exit", 5*time.Second, func(tx string) bool {
		return strings.Contains(raw.String(), "\x1b]52;c;") && !strings.Contains(tx, "y/enter copy")
	})
	if strings.Contains(s.text(), "Copied") {
		t.Fatal("server displayed the local-only success flash", s.text())
	}
}

func TestServerTabStripHasNoAddControl(t *testing.T) {
	s := start(t)
	s.waitFor("server tab", 10*time.Second, func(tx string) bool { return hasTabLabel(tx, "1") && strings.Contains(tx, "❯") })
	if strings.Contains(strings.Split(s.text(), "\n")[0], " + ") {
		t.Fatal("server tab strip exposes a local-only add control")
	}
	s.send("\x02c")
	s.waitFor("new tab prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "new tab") })
	s.send("PARITY\r")
	s.waitFor("new tab name replaced default", 5*time.Second, func(tx string) bool { return hasTabLabel(tx, "PARITY") })
	if hasTabLabel(s.text(), "2PARITY") {
		t.Fatal("new tab input appended to its initial number")
	}
}
