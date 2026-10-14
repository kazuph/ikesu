package machine

import (
	"slices"
	"strings"
)

// ObserveRun matches only the SSH recipe produced by RunArgs, including the
// final exec in SyncPlan's shell recipe. It never interprets arbitrary shell.
// A private remote marker reports SSH connection success without verbose SSH
// diagnostics changing the user's stderr log.
func ObserveRun(profiles []Profile, argv []string, marker string) (string, []string) {
	profiles = slices.Clone(profiles)
	slices.SortFunc(profiles, func(a, b Profile) int { return strings.Compare(a.ID, b.ID) })
	for _, p := range profiles {
		if !p.Enabled {
			continue
		}
		for _, pane := range []bool{false, true} {
			prefix := RunPrefix(p, pane)
			if len(argv) == len(prefix)+1 && slices.Equal(argv[:len(prefix)], prefix) {
				observed := slices.Clone(argv)
				observed[len(prefix)] = "printf '%s\\n' " + Quote(marker) + " >&2; " + argv[len(prefix)]
				return p.ID, observed
			}
			if len(argv) == 3 && argv[0] == "/bin/sh" && argv[1] == "-c" {
				lead := " && exec " + Command(prefix) + " "
				if at := strings.LastIndex(argv[2], lead); at >= 0 {
					observed := slices.Clone(argv)
					at += len(lead)
					observed[2] = argv[2][:at] + Quote("printf '%s\\n' "+Quote(marker)+" >&2; ") + argv[2][at:]
					return p.ID, observed
				}
			}
		}
	}
	return "", argv
}

// ObservePaneRun recognizes the same generated SSH prefix after the pane wrapper
// has received a shell command. quote is that wrapper's existing word encoder.
func ObservePaneRun(profiles []Profile, command, marker string, quote func([]string) string) (string, string) {
	profiles = slices.Clone(profiles)
	slices.SortFunc(profiles, func(a, b Profile) int { return strings.Compare(a.ID, b.ID) })
	for _, p := range profiles {
		if !p.Enabled {
			continue
		}
		for _, pane := range []bool{true, false} {
			prefix := quote(RunPrefix(p, pane)) + " "
			if strings.HasPrefix(command, prefix) {
				return p.ID, prefix + Quote("printf '%s\\n' "+Quote(marker)+" >&2; ") + command[len(prefix):]
			}
		}
		// --sync is passed through as one quoted shell recipe by the pane wrapper.
		for _, pane := range []bool{true, false} {
			prefix := " && exec " + Command(RunPrefix(p, pane)) + " "
			// The wrapper quotes the full sh -c argument; search its encoded recipe.
			encoded := quote([]string{prefix})
			// Strip only the outer quotes. All remaining escapes are part of the
			// wrapper's canonical encoding, not parsed shell input.
			encoded = strings.TrimSuffix(strings.TrimPrefix(encoded, "'"), "'")
			if at := strings.LastIndex(command, encoded); at >= 0 {
				at += len(encoded)
				insert := quote([]string{Quote("printf '%s\\n' " + Quote(marker) + " >&2; ")})
				insert = strings.TrimSuffix(strings.TrimPrefix(insert, "'"), "'")
				return p.ID, command[:at] + insert + command[at:]
			}
		}
	}
	return "", command
}
