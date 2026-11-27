package machine

import "strings"

var shellQuoteEscapes = strings.NewReplacer(`\`, `'\\'`, `'`, `'\''`)

// Quote encodes one word for both POSIX shells and fish. Fish also decodes
// escaped backslashes inside single quotes, so escape them outside the quotes.
func Quote(s string) string { return "'" + shellQuoteEscapes.Replace(s) + "'" }

// Command keeps the POSIX encoding used by local recipes and DecodeCommand.
func Command(argv []string) string {
	words := make([]string, len(argv))
	for i, s := range argv {
		words[i] = "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return strings.Join(words, " ")
}

// remoteScript lets sshd's login shell parse only a portable command line.
func remoteScript(script string) string { return "exec /bin/sh -c " + Quote(script) }

func ForwardArgs(p Profile, binary string, argv []string) []string {
	if binary == "ikesu" {
		args := append([]string{"--session", p.Session}, argv...)
		command := `bin="$HOME/.local/bin/ikesu"; if [ ! -x "$bin" ]; then printf '%s%s\n' ` + Quote("ikesu missing on "+p.Target+": ") + ` "$bin" >&2; exit 127; fi; exec "$bin" ` + Command(args)
		return append(sshTarget(p.Target), remoteScript(command))
	}
	words := append([]string{binary, "--session", p.Session}, argv...)
	return SSHArgs(p, words)
}

func SSHArgs(p Profile, words []string) []string {
	// OpenSSH joins command arguments with spaces; quote each remote word.
	args := sshTarget(p.Target)
	for _, word := range words {
		args = append(args, Quote(word))
	}
	return args
}

func sshTarget(target string) []string {
	return []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "--", target}
}
