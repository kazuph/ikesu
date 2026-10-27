package guard

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// Do not interpret client configuration as shell argv. Even an empty or
// unreadable configuration stays subject to approval unless loading is disabled.
func noHTTPDefaultConfig(i Input, cmd string) bool {
	if cmd == "curl" {
		first := arg(i.Args, 0)
		if first == "--disable" || strings.HasPrefix(first, "-q") {
			return true
		}
	}
	home := i.Env["HOME"]
	if home == "" {
		current, err := user.Current()
		if err != nil {
			return false
		}
		home = current.HomeDir
	}
	if !filepath.IsAbs(home) {
		return false
	}
	absent := func(base, relative string) bool {
		if !filepath.IsAbs(base) {
			return false
		}
		_, err := os.Lstat(filepath.Join(base, relative))
		return os.IsNotExist(err)
	}
	if cmd == "curl" {
		if base := i.Env["CURL_HOME"]; base != "" && (!absent(base, ".curlrc") || !absent(base, ".config/curlrc")) {
			return false
		}
		if base := i.Env["XDG_CONFIG_HOME"]; base != "" && (!absent(base, "curlrc") || !absent(base, ".curlrc")) {
			return false
		}
		// curl can fall back to getpwuid even with an isolated HOME.
		current, err := user.Current()
		return err == nil && absent(current.HomeDir, ".curlrc") && absent(home, ".curlrc") && absent(home, ".config/curlrc")
	}
	if base := i.Env["HTTPIE_CONFIG_DIR"]; base != "" {
		return absent(base, "config.json")
	}
	if base, set := i.Env["XDG_CONFIG_HOME"]; set && !absent(base, "httpie/config.json") {
		return false
	}
	return absent(home, ".httpie/config.json") && absent(home, ".config/httpie/config.json")
}
