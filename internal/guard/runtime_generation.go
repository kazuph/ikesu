package guard

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"golang.org/x/sys/unix"
)

var startupPayloadNames = []string{"bash-env.current", ".zshenv.current", ".zprofile.current", ".zshrc.current", ".zlogin.current"}

type runtimeGeneration struct {
	Executable string
	Native     executableIdentity
	Policy     Config
	Files      map[string]string
}

func trustedExecutable(path string) (string, os.FileInfo, error) {
	if !filepath.IsAbs(path) {
		return "", nil, fmt.Errorf("guard executable must be absolute")
	}
	if err := trustedAncestors(filepath.Dir(path)); err != nil {
		return "", nil, err
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, err
	}
	if err = trustedAncestors(filepath.Dir(target)); err != nil {
		return "", nil, err
	}
	var st unix.Stat_t
	if err = unix.Stat(target, &st); err != nil {
		return "", nil, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&0111 == 0 || st.Mode&0022 != 0 || (st.Uid != 0 && int(st.Uid) != os.Geteuid()) {
		return "", nil, fmt.Errorf("untrusted guard executable: %s", target)
	}
	return target, info, nil
}

// CheckExecutable applies the same trust checks used for guard assets.
func CheckExecutable(path string) error {
	_, _, err := trustedExecutable(path)
	return err
}

// CheckDirectory applies the guard's ancestor checks to a destination directory.
func CheckDirectory(path string) error { return trustedAncestors(path) }
func loadGeneration(shell *privateDirectory) (string, *privateDirectory, runtimeGeneration, error) {
	name, err := shell.readlink("current")
	if err != nil {
		return "", nil, runtimeGeneration{}, err
	}
	if !generationName(name) {
		return "", nil, runtimeGeneration{}, fmt.Errorf("invalid guard generation")
	}
	dir, err := shell.child(name, false)
	if err != nil {
		return "", nil, runtimeGeneration{}, err
	}
	data, err := dir.read("manifest.json")
	var generation runtimeGeneration
	if err == nil {
		err = json.Unmarshal(data, &generation)
	}
	if err != nil {
		dir.close()
		return "", nil, generation, err
	}
	return name, dir, generation, nil
}
func (a *Assets) generationMatches(dir *privateDirectory, g runtimeGeneration, files bool) bool {
	target, info, err := a.trustedSource()
	if err != nil || target != g.Executable || executableFileIdentity(info) != g.Native || !reflect.DeepEqual(a.Config, g.Policy) {
		return false
	}
	link, err := dir.readlink("ikesu")
	if err != nil || link != target {
		return false
	}
	actual, err := os.Stat(filepath.Join(dir.path, "ikesu"))
	if err != nil || !os.SameFile(actual, info) {
		return false
	}
	if files {
		if len(g.Files) != len(startupPayloadNames) {
			return false
		}
		for _, name := range startupPayloadNames {
			data, err := dir.read(name)
			if err != nil {
				return false
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != g.Files[name] {
				return false
			}
		}
	}
	return true
}
func (a *Assets) buildGeneration(shell *privateDirectory) (string, error) {
	target, info, err := a.trustedSource()
	if err != nil {
		return "", err
	}
	name := "generation-" + rand.Text()
	dir, err := shell.child(name, true)
	if err != nil {
		return "", err
	}
	defer dir.close()
	complete := false
	defer func() {
		if !complete {
			discardGeneration(shell, name)
		}
	}()
	generation := runtimeGeneration{Executable: target, Native: executableFileIdentity(info), Policy: a.Config, Files: map[string]string{}}
	generator := NewAssets(a.Root, a.Pane, target, a.Config)
	generator.meta = a.meta
	for name, data := range generator.startupPayloads(filepath.Dir(a.RuntimePath())) {
		if err = dir.write(name, data, 0600); err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		generation.Files[name] = hex.EncodeToString(sum[:])
	}
	if err = dir.link("ikesu", target); err != nil {
		return "", err
	}
	data, err := json.Marshal(generation)
	if err != nil {
		return "", err
	}
	if err = dir.write("manifest.json", data, 0600); err != nil {
		return "", err
	}
	if !a.generationMatches(dir, generation, true) {
		return "", fmt.Errorf("guard executable changed during generation")
	}
	complete = true
	return name, nil
}
func (a *Assets) stableEntries(shell *privateDirectory) error {
	prefix := "#!/bin/sh\n" + a.identity() + " exec " + quote(a.RuntimePath()) + " guard claude-prefix -- \"$@\"\n"
	if err := shell.write("claude-prefix", []byte(prefix), 0700); err != nil {
		return err
	}
	for _, name := range []string{"bash-env", ".zshenv", ".zprofile", ".zshrc", ".zlogin"} {
		if err := shell.write(name, []byte(a.startupLoader(name+".current")), 0600); err != nil {
			return err
		}
		if err := shell.link(name+".current", filepath.Join("current", name+".current")); err != nil {
			return err
		}
	}
	return shell.link("ikesu", filepath.Join("current", "ikesu"))
}

// Caller holds the cross-process exclusive lock from validation through the
// one atomic publication. A refresh never edits a published generation.
func (a *Assets) ensureGeneration(shell *privateDirectory, entries bool) (string, error) {
	name, dir, g, err := loadGeneration(shell)
	valid := err == nil
	created := false
	published := false
	defer func() {
		if created && !published {
			discardGeneration(shell, name)
		}
	}()
	if valid {
		valid = a.generationMatches(dir, g, true)
		dir.close()
	}
	if !valid {
		name, err = a.buildGeneration(shell)
		created = err == nil
		if err != nil {
			return "", err
		}
	}
	if entries {
		if err = a.stableEntries(shell); err != nil {
			return "", err
		}
	}
	if err = shell.link("current", name); err != nil {
		return "", err
	}
	published = true
	return filepath.Join(shell.path, name), nil
}

func (a *Assets) runtimeIntact() bool {
	shell, err := a.privateShell(false)
	if err != nil {
		return false
	}
	defer shell.close()
	unlock, err := shell.lock(false)
	if err != nil {
		return false
	}
	defer unlock()
	link, err := shell.readlink("ikesu")
	if err != nil || link != filepath.Join("current", "ikesu") {
		return false
	}
	_, dir, g, err := loadGeneration(shell)
	if err != nil {
		return false
	}
	defer dir.close()
	if !a.generationMatches(dir, g, true) {
		return false
	}
	for _, name := range []string{"bash-env", ".zshenv", ".zprofile", ".zshrc", ".zlogin"} {
		data, err := shell.read(name)
		if err != nil || string(data) != a.startupLoader(name+".current") {
			return false
		}
		target, err := shell.readlink(name + ".current")
		if err != nil || target != filepath.Join("current", name+".current") {
			return false
		}
	}
	return true
}

func (a *Assets) trustedSource() (string, os.FileInfo, error) {
	target, info, err := trustedExecutable(a.Executable)
	if err != nil {
		return "", nil, err
	}
	if a.native != (executableIdentity{}) && a.native != executableFileIdentity(info) {
		return "", nil, fmt.Errorf("trusted guard executable was replaced")
	}
	return target, info, nil
}

// Only unpublished directories allocated by this operation are discarded.
// Published generations remain available to already-started readers.
func discardGeneration(shell *privateDirectory, name string) {
	if !generationName(name) {
		return
	}
	if current, err := shell.readlink("current"); err == nil && current == name {
		return
	}
	dir, err := shell.child(name, false)
	if err != nil {
		return
	}
	defer dir.close()
	for _, file := range append(append([]string{}, startupPayloadNames...), "manifest.json", "ikesu") {
		unix.Unlinkat(dir.fd(), file, 0)
	}
	unix.Unlinkat(shell.fd(), name, unix.AT_REMOVEDIR)
}
