package plugin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func localGitInstall(t *testing.T, manifest string) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "gtx-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", repo}, {"-C", repo, "add", "gtx-plugin.toml"}, {"-C", repo, "-c", "user.name=Plugin Test", "-c", "user.email=plugin@example.invalid", "commit", "-m", "fixture"}} {
		c := exec.Command(realGit, args...)
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatal(string(out), err)
		}
	}
	bin := filepath.Join(root, "bin")
	if err = os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	// The explicitly requested fake git routes the GitHub clone to a real local
	// repository; every operation is still performed by the real git binary.
	wrapper := "#!/bin/sh\nif [ \"$1\" = clone ]; then exec \"$PLUGIN_TEST_REAL_GIT\" clone -- \"$PLUGIN_TEST_REPO\" \"$4\"; fi\nexec \"$PLUGIN_TEST_REAL_GIT\" \"$@\"\n"
	if err = os.WriteFile(filepath.Join(bin, "git"), []byte(wrapper), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PLUGIN_TEST_REPO", repo)
	t.Setenv("PLUGIN_TEST_REAL_GIT", realGit)
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	return &Store{ConfigRoot: filepath.Join(root, "config"), StateRoot: filepath.Join(root, "state"), Version: "0.1.0"}, repo
}
func TestInstallBuildReinstallAndUninstallRealGit(t *testing.T) {
	s, _ := localGitInstall(t, minimal+`platforms=["macos","linux"]
[[build]]
command=["/bin/sh","-c","test -z \"$GTX_PLUGIN_CONTEXT_JSON\" && printf built > built"]
`)
	t.Setenv("GTX_PLUGIN_CONTEXT_JSON", "inherited-context")
	confirm := func(p InstallPreview) bool { return p.Manifest.ID == "example.local" && len(p.Manifest.Build) == 1 }
	item, err := s.Install("owner/repo", "", confirm)
	if err != nil {
		t.Fatal(err)
	}
	if item.Source.Kind != "github" || item.Source.ResolvedCommit == "" || item.Source.ManagedPath == "" {
		t.Fatal(item)
	}
	if data, err := os.ReadFile(filepath.Join(item.Root, "built")); err != nil || string(data) != "built" {
		t.Fatal(string(data), err)
	}
	if _, err = s.Install("owner/repo", "", confirm); err != nil {
		t.Fatal(err)
	}
	config, _ := s.ConfigDir(item.ID)
	state, _ := s.StateDir(item.ID)
	id, removed, err := s.Uninstall("owner/repo")
	if err != nil || !removed || id != item.ID {
		t.Fatal(id, removed, err)
	}
	if _, err = os.Stat(item.Root); !os.IsNotExist(err) {
		t.Fatal("checkout remains", err)
	}
	for _, dir := range []string{config, state} {
		if _, err = os.Stat(dir); err != nil {
			t.Fatal("durable user data removed", err)
		}
	}
}
func TestInstallFailureNeverRegisters(t *testing.T) {
	for _, body := range []string{`[[build]]
command=["/bin/sh","-c","exit 3"]
`, `[[build]]
command=["/bin/sh","-c","printf '\\n# changed' >> gtx-plugin.toml"]
`} {
		t.Run(body, func(t *testing.T) {
			s, _ := localGitInstall(t, minimal+body)
			if _, err := s.Install("owner/repo", "", func(InstallPreview) bool { return true }); err == nil {
				t.Fatal("accepted failed/changed build")
			}
			list, err := s.List("")
			if err != nil || len(list) != 0 {
				t.Fatal(list, err)
			}
		})
	}
}
func TestInstallRefusesLocalAndUnconfirmed(t *testing.T) {
	s, repo := localGitInstall(t, minimal)
	if _, err := s.Install("owner/repo", "", nil); err == nil {
		t.Fatal("missing confirmation accepted")
	}
	if _, err := s.Link(repo, true, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Install("owner/repo", "", func(InstallPreview) bool { return true }); err == nil || !strings.Contains(err.Error(), "locally linked") {
		t.Fatal(err)
	}
	if _, err := os.Stat(repo); err != nil {
		t.Fatal("source removed", err)
	}
}
func TestGitHubShorthandValidation(t *testing.T) {
	for _, v := range []string{"owner", "/repo", "https://github.com/o/r", "o/r/../x", "o/r//x", "o/r/."} {
		if _, err := ParseGitHubSource(v); err == nil {
			t.Fatal("accepted", v)
		}
	}
	if src, err := ParseGitHubSource("owner/repo/sub/dir"); err != nil || src.Subdir != "sub/dir" {
		t.Fatal(src, err)
	}
}
