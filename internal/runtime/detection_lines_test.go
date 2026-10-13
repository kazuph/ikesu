package runtime

import (
	"strings"
	"testing"
)

func TestDetectionLinesUnwrapCurrentScreenWithoutHistory(t *testing.T) {
	s:=NewSupervisor()
	p,err:=s.SpawnWith(1,t.TempDir(),8,4,nil,[]string{"/bin/sh","-c","printf 'HISTORY\\nabc def ghi\\nEND\\n'; exec /bin/cat"})
	if err!=nil { t.Fatal(err) }; defer s.KillAll()
	waitReadMarker(t,p,"END")
	got:=strings.Join(p.DetectionLines(),"\n")
	if !strings.Contains(got,"abc def ghi") || strings.Contains(got,"HISTORY") { t.Fatalf("detection=%q",got) }
	history,_:=p.ReadLines("recent_unwrapped",true)
	if !strings.Contains(strings.Join(history,"\n"),"HISTORY") { t.Fatal("history setup did not scroll") }
}
