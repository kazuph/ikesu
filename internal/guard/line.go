package guard

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/childenv"
	"io"
	"strings"
)

// Shell lexical splitting uses zsh's parser without evaluating the input.
// This is the shell whose accept-line widget supplies the raw line.
func LineInput(i Input, line string) (Input, error) {
	cmd := childenv.Command("/bin/zsh", "-f", "-c", `for token in ${(z)1}; do print -rn -- "${(Q)token}"; print -rn -- $'\0'; done`, "guard-line", line)
	cmd.Env = childenv.Scrub(environment(i.Env))
	b, e := cmd.Output()
	if e != nil {
		return i, fmt.Errorf("unsupported shell input: %w", e)
	}
	tokens := strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
	if len(tokens) == 0 || tokens[0] == "" {
		return i, nil
	}
	i.Command = tokens[0]
	i.Args = tokens[1:]
	i.RawLine = line
	return i, nil
}
func AuthorizeLine(c Config, root string, i Input, line string, errOut io.Writer, await Await) bool {
	if !c.Active() {
		return true
	}
	_, active, e := ReadMarker(i)
	if e != nil {
		return authorize(c, root, i, "line", errOut, await, nil)
	}
	if !active {
		return true
	}
	i, e = LineInput(i, line)
	if e != nil {
		fmt.Fprintln(errOut, e)
		return c.EffectiveMode() == "record"
	}
	if i.Command == "" {
		return true
	}
	v := classify(c, i)
	if v.Decision == "allow" && i.Env["AI_GUARD_DANGER_WORD_ACK"] == "1" {
		return true
	}
	// A widget observes dangerous input; ordinary commands are logged by the
	// forwarding command, avoiding an extra record for every accepted line.
	if !danger(c, i) && v.Decision != "block" {
		return true
	}
	if v.Decision != "block" {
		v = result("ask", "danger-word-line", "deploy/publish を含む入力行は確認対象です")
	}
	return authorize(c, root, i, "line", errOut, await, &v)
}
