package machine

import "strings"

func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
func Command(argv []string) string {
	words := make([]string, len(argv))
	for i, s := range argv {
		words[i] = Quote(s)
	}
	return strings.Join(words, " ")
}

func ForwardArgs(p Profile, binary string, argv []string) []string {
	// OpenSSH joins command arguments with spaces; quote each remote word.
	if binary == "ikesu" {
		args := append([]string{"--session", p.Session}, argv...)
		command := `bin="$HOME/.local/bin/ikesu"; if [ ! -x "$bin" ]; then printf '%s%s\n' ` + Quote("ikesu missing on "+p.Target+": ") + ` "$bin" >&2; exit 127; fi; exec "$bin" ` + Command(args)
		return append([]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "--", p.Target}, command)
	}
	words := append([]string{binary, "--session", p.Session}, argv...)
	return SSHArgs(p, words)
}

func SSHArgs(p Profile, words []string) []string {
	quoted := make([]string, len(words))
	for i, s := range words {
		quoted[i] = Quote(s)
	}
	return append([]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "--", p.Target}, quoted...)
}
