package guard

import (
	"fmt"
	"io"
)

// LineInput exposes the first parsed command for existing callers.
func LineInput(i Input, line string) (Input, error) {
	commands, err := lineCommands(i, line)
	if len(commands) > 0 {
		return commands[0], err
	}
	i.Command, i.Args, i.RawLine = "", nil, line
	return i, err
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
	i, verdict, parseErr := lineVerdict(c, i, line)
	if parseErr != nil {
		fmt.Fprintln(errOut, parseErr)
	}
	if verdict.Decision == "allow" {
		return true
	}
	return authorize(c, root, i, "line", errOut, await, &verdict)
}
