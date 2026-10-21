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
	m, active, e := ReadMarker(i)
	if e != nil {
		return authorize(c, root, i, "line", errOut, await, nil)
	}
	if !active {
		return true
	}
	c = *m.Policy
	if !c.Active() {
		return true
	}
	i.LaunchCwd = m.Cwd
	callerEnv := i.Env
	i, verdict, parseErr := lineVerdict(c, i, line)
	// Keep the caller's authorization context across env -i/-u, while retaining
	// inline request reasons and approval-file settings from the parsed command.
	parsedEnv := i.Env
	authorizationEnv := i.AuthorizationEnv
	i.Env = EnvMap(environment(callerEnv))
	for key, value := range authorizationEnv {
		if !authorizationIdentityKey(key) {
			i.Env[key] = value
		}
	}
	for key, value := range parsedEnv {
		if !authorizationIdentityKey(key) {
			i.Env[key] = value
		}
	}
	if parseErr != nil {
		fmt.Fprintln(errOut, parseErr)
	}
	if verdict.Decision == "allow" {
		return true
	}
	return authorize(c, root, i, "line", errOut, await, &verdict)
}

// Inline shell assignments cannot redirect the caller's authorization context.
func authorizationIdentityKey(key string) bool {
	switch key {
	case "GTX_GUARD_MARKER", "GTX_PANE_ID", "GTX_SOCKET_PATH", "IKESU_GUARD_MARKER", "IKESU_PANE_ID", "IKESU_SOCKET_PATH":
		return true
	}
	return false
}
