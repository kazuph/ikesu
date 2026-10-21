// Package envcompat resolves the stage-one environment naming contract.
package envcompat

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

var warned sync.Map

// Lookup gives the new name priority while accepting existing callers' keys.
func Lookup(getenv func(string) string, key string) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	suffix := ""
	switch {
	case strings.HasPrefix(key, "GTX_"):
		suffix = strings.TrimPrefix(key, "GTX_")
	case strings.HasPrefix(key, "IKESU_"):
		suffix = strings.TrimPrefix(key, "IKESU_")
	default:
		return getenv(key)
	}
	newer, older := getenv("IKESU_"+suffix), getenv("GTX_"+suffix)
	if newer != "" {
		if older != "" && older != newer {
			WarnOnce("env:"+suffix, "IKESU_"+suffix+" and GTX_"+suffix+" differ; using IKESU_"+suffix)
		}
		return newer
	}
	return older
}

func Get(key string) string { return Lookup(os.Getenv, key) }

// WarnOnce never prints values, which may contain private paths or credentials.
func WarnOnce(key, message string) {
	if _, loaded := warned.LoadOrStore(key, true); !loaded {
		fmt.Fprintln(os.Stderr, "ikesu: warning:", message)
	}
}

// Both synchronizes paired names after a caller has installed fresh child values.
// Last assignment to a given name wins, as with exec.Cmd.Env.
func Both(env []string) []string {
	values := make(map[string]string, len(env))
	var keys []string
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, exists := values[key]; !exists {
			keys = append(keys, key)
		}
		values[key] = value
	}
	out := make([]string, 0, len(values)*2)
	seen := map[string]bool{}
	for _, key := range keys {
		if strings.HasPrefix(key, "GTX_") || strings.HasPrefix(key, "IKESU_") {
			suffix := strings.TrimPrefix(strings.TrimPrefix(key, "GTX_"), "IKESU_")
			if seen[suffix] {
				continue
			}
			seen[suffix] = true
			value := Lookup(func(k string) string { return values[k] }, key)
			out = append(out, "IKESU_"+suffix+"="+value, "GTX_"+suffix+"="+value)
		} else {
			out = append(out, key+"="+values[key])
		}
	}
	return out
}
