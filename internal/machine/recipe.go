package machine

import "strings"

// DecodeCommand reads literal words emitted by ikesu's shell encoders. It rejects
// expansion, substitutions and operators; it is not a general shell parser.
func DecodeCommand(text string) ([]string, bool) {
	var words []string
	for text != "" {
		word, rest, ok := literalWord(text)
		if !ok {
			return nil, false
		}
		words = append(words, word)
		if rest == "" {
			return words, true
		}
		if rest[0] != ' ' {
			return nil, false
		}
		text = rest[1:]
		if text == "" || text[0] == ' ' {
			return nil, false
		}
	}
	return nil, false
}

func literalWord(text string) (string, string, bool) {
	var word strings.Builder
	consumed := false
	for len(text) > 0 && text[0] != ' ' {
		switch text[0] {
		case '\'':
			end := strings.IndexByte(text[1:], '\'')
			if end < 0 {
				return "", "", false
			}
			word.WriteString(text[1 : end+1])
			text = text[end+2:]
			consumed = true
		case '\\':
			if len(text) < 2 || text[1] != '\'' {
				return "", "", false
			}
			word.WriteByte('\'')
			text = text[2:]
			consumed = true
		default:
			c := text[0]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("@%_+=:,./-~", rune(c))) {
				return "", "", false
			}
			word.WriteByte(c)
			text = text[1:]
			consumed = true
		}
	}
	return word.String(), text, consumed
}

// syncStages accepts only SyncPlan's canonical quoted commands followed by
// exec and a final quoted SSH command. A separator inside a word is literal.
func syncStages(argv []string) ([][]string, bool) {
	if len(argv) != 3 || argv[0] != "/bin/sh" || argv[1] != "-c" {
		return nil, false
	}
	text := argv[2]
	var stages [][]string
	for {
		final := strings.HasPrefix(text, "exec ")
		if final {
			text = strings.TrimPrefix(text, "exec ")
		}
		start := text
		var words []string
		for {
			word, rest, ok := literalWord(text)
			if !ok {
				return nil, false
			}
			words = append(words, word)
			if rest == "" || strings.HasPrefix(rest, " && ") {
				used := start[:len(start)-len(rest)]
				if Command(words) != used {
					return nil, false
				}
				stages = append(stages, words)
				if final {
					return stages, rest == "" && len(stages) > 1
				}
				if !strings.HasPrefix(rest, " && ") {
					return nil, false
				}
				text = rest[4:]
				break
			}
			if rest[0] != ' ' {
				return nil, false
			}
			text = rest[1:]
		}
	}
}
