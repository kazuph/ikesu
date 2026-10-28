package guard

import (
	"io"
	"os"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// The writer and recognizer share the forwarding argv, including --via shim.
const shimForward = "guard exec --via shim --"

func shimScript(identity, executable, command string) string {
	return "#!/bin/sh\n" + identity + " exec " + quote(executable) + " " + shimForward + " " + quote(command) + " \"$@\"\n"
}

// Recognize the forwarding command, not a directory name or text in comments.
// Parsing is static: no shell substitutions or commands are evaluated.
func isForwardingShim(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	var header [2]byte
	if _, err = io.ReadFull(file, header[:]); err != nil || string(header[:]) != "#!" {
		return false
	}
	tree, err := syntax.NewParser().Parse(io.MultiReader(strings.NewReader("#!"), file), path)
	if err != nil {
		return false
	}
	for _, stmt := range tree.Stmts {
		call, ok := stmt.Cmd.(*syntax.CallExpr)
		if !ok || len(call.Args) < 2+len(strings.Fields(shimForward))+1 {
			continue
		}
		first, err := shellLiteral(nil, call.Args[0])
		if err != nil || first != "exec" {
			continue
		}
		matches := true
		for n, want := range strings.Fields(shimForward) {
			got, err := shellLiteral(nil, call.Args[n+2])
			if err != nil || got != want {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}
