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
			if stages, ok := syncStages(argv); ok {
				final := stages[len(stages)-1]
				if len(final) == len(prefix)+1 && slices.Equal(final[:len(prefix)], prefix) {
					observed := slices.Clone(argv)
					final = slices.Clone(final)
					final[len(prefix)] = "printf '%s\\n' " + Quote(marker) + " >&2; " + final[len(prefix)]
					stages[len(stages)-1] = final
					observed[2] = syncRecipe(stages)
					return p.ID, observed
				}
			}
		}
	}
	return "", argv
}

// ObservePaneRun receives the wrapper's canonical literal argv, preserving its
// existing encoder for the final command reconstruction.
func ObservePaneRun(profiles []Profile, command, marker string, quote func([]string) string) (string, string) {
	argv, ok := DecodeCommand(command)
	if !ok || quote(argv) != command {
		return "", command
	}
	id, observed := ObserveRun(profiles, argv, marker)
	if id == "" {
		return "", command
	}
	return id, quote(observed)
}

func syncRecipe(stages [][]string) string {
	var commands []string
	for i, stage := range stages {
		command := Command(stage)
		if i == len(stages)-1 {
			command = "exec " + command
		}
		commands = append(commands, command)
	}
	return strings.Join(commands, " && ")
}
