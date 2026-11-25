package snapshot

import (
	"regexp"
	"strings"

	"github.com/kazuph/ikesu/internal/guard"
)

// Match a complete quoted value (including YAML's doubled single quotes), or
// conservatively redact the rest of an unquoted or incomplete value.
var snapshotSecretField = regexp.MustCompile(`(?i)(\b(?:password|passwd|secret|token|api_key|apikey|access_token|refresh_token|client_secret|authorization|private_key|[a-z0-9_]*_(?:token|secret|key))\b["']?[ \t]*[:=][ \t]*)("(?:[^"\\]|\\.)*"|'(?:[^']|'')*'|.+)`)
var privateKeyMarker = regexp.MustCompile(`-----(BEGIN|END) (?:[A-Z0-9-]+ )*PRIVATE KEY-----`)
var yamlBlockValue = regexp.MustCompile(`^[|>](?:[+-][1-9]?|[1-9][+-]?)?(?:[ \t]+#.*)?[ \t]*$`)

func redactLines(lines []string) []string {
	inPrivateKey := false
	prefixEnd := 0
	yamlBlockIndent := -1
	for i, original := range lines {
		// Guard redaction can consume a marker after a field assignment.
		markers := privateKeyMarker.FindAllStringSubmatch(original, -1)
		hiddenKey := inPrivateKey || len(markers) != 0
		for _, marker := range markers {
			if marker[1] == "END" && !inPrivateKey {
				prefixEnd = i + 1
			}
			inPrivateKey = marker[1] == "BEGIN"
		}

		indent := len(original) - len(strings.TrimLeft(original, " \t"))
		blank := strings.TrimSpace(original) == ""
		if yamlBlockIndent >= 0 && !blank && indent <= yamlBlockIndent {
			yamlBlockIndent = -1
		}
		if hiddenKey || (yamlBlockIndent >= 0 && !blank) {
			lines[i] = "***"
			continue
		}
		lines[i] = snapshotSecretField.ReplaceAllStringFunc(guard.RedactSecrets(original), func(match string) string {
			field := snapshotSecretField.FindStringSubmatch(match)
			value := field[2]
			if yamlBlockValue.MatchString(value) {
				yamlBlockIndent = indent
			}
			if len(value) >= 2 && strings.ContainsAny(value[:1], "\"'") && value[0] == value[len(value)-1] {
				return field[1] + value[:1] + "***" + value[:1]
			}
			return field[1] + "***"
		})
	}
	for i := range prefixEnd {
		lines[i] = "***"
	}
	return lines
}

func safeText(text string) string {
	return redactLines([]string{cleanText(text)})[0]
}
