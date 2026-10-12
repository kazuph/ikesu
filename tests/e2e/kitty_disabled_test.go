package e2e

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKittyDisabledActualPTYVersionAndQuery(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	root := t.TempDir()
	probe := filepath.Join(root, "probe.py")
	answers := filepath.Join(root, "answers")
	script := `import os,sys,tty,select,time
tty.setraw(0)
os.write(1,b'\x1b[>q\x1b_Ga=q,t=d,f=24,s=1,v=1,i=31,q=1;AP8A\x1b\\')
data=b''
end=time.monotonic()+5
while time.monotonic()<end and data.count(b'\x1b\\')<2:
 if select.select([0],[],[],end-time.monotonic())[0]: data+=os.read(0,4096)
open(sys.argv[1],'wb').write(data)
os.write(1,b'DISABLED_PROBE_READY')
os.read(0,1)
`
	if err := os.WriteFile(probe, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	s.send(fmt.Sprintf("/usr/bin/python3 %q %q\r", probe, answers))
	s.waitFor("disabled query response", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "DISABLED_PROBE_READY") })
	data, err := os.ReadFile(answers)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("\x1bP>|libghostty\x1b\\")) || !bytes.Contains(data, []byte("ENOTSUP:kitty graphics is disabled")) || bytes.Contains(data, []byte(";OK")) {
		t.Fatalf("disabled advertises images: %q", data)
	}
	t.Logf("disabled XTVERSION and query responses=%s", hex.EncodeToString(data))
}
