package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ResolveCommand uses the user's login shell once per server, without changing
// the server environment or asking exec.LookPath to search its inherited PATH.
func (s *Supervisor) ResolveCommand(argv []string, cwd string) ([]string, string, error) {
	if len(argv) == 0 || argv[0] == "" {
		return nil, "", fmt.Errorf("agent command is empty")
	}
	s.loginPathOnce.Do(func() {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/zsh"
		}
		out, err := exec.Command(shell, "-lic", `printf '\000%s\000' "$PATH"`).Output()
		if err != nil {
			s.loginPathErr = fmt.Errorf("login shell PATH: %w", err)
			return
		}
		parts := strings.Split(string(out), "\x00")
		if len(parts) < 3 || parts[len(parts)-2] == "" {
			s.loginPathErr = fmt.Errorf("login shell returned no PATH")
			return
		}
		s.loginPath = parts[len(parts)-2]
	})
	if s.loginPathErr != nil {
		return nil, "", s.loginPathErr
	}
	resolved := append([]string(nil), argv...)
	if !strings.ContainsRune(argv[0], filepath.Separator) {
		resolved[0] = ""
		for _, dir := range filepath.SplitList(s.loginPath) {
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(cwd, dir)
			}
			candidate := filepath.Join(dir, argv[0])
			info, err := os.Stat(candidate)
			if err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
				resolved[0] = candidate
				break
			}
		}
		if resolved[0] == "" {
			return nil, "", fmt.Errorf("agent command %q not found in login shell PATH", argv[0])
		}
	}
	return resolved, s.loginPath, nil
}
