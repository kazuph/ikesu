package server

import (
	"bufio"

	"net"

	"os/exec"

	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	r := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "init"}, {"branch", "existing"}} {
		if b, e := exec.Command("git", append([]string{"-C", r}, args...)...).CombinedOutput(); e != nil {
			t.Fatalf("%v %s", e, b)
		}
	}
	return r
}

func subscribe(t *testing.T, dir string, kinds []string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, e := net.Dial("unix", api.APISocketPath(dir))
	if e != nil {
		t.Fatal(e)
	}
	subs := []api.Subscription{}
	for _, k := range kinds {
		subs = append(subs, api.Subscription{Type: k})
	}
	if e := api.WriteRequest(conn, api.Request{ID: "wt-events", Method: api.MethodEventsSubscribe, Params: api.MarshalParams(api.EventsSubscribeParams{Subscriptions: subs})}); e != nil {
		t.Fatal(e)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	rd := bufio.NewReader(conn)
	line, e := rd.ReadBytes('\n')
	if e != nil || !strings.Contains(string(line), "subscription_started") {
		t.Fatalf("%s %v", line, e)
	}
	return conn, rd
}
