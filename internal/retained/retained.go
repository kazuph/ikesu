// Package retained keeps immutable executables in the user's private release directory.
package retained

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/kazuph/ikesu/internal/guard"
)

const OriginalPathEnv = "IKESU_ORIGINAL_EXECUTABLE"
const reexecEnv = "IKESU_RETAINED_REEXEC"

// Ensure re-executes an untrusted executable once from an immutable private copy.
func Ensure(version string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err = guard.CheckExecutable(exe); err == nil {
		return os.Unsetenv(reexecEnv)
	}
	if os.Getenv(reexecEnv) != "" {
		return fmt.Errorf("retained executable %s is not trusted: %w", exe, err)
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	dest, e := Retain(exe, version, home)
	if e != nil {
		return e
	}
	original := os.Getenv(OriginalPathEnv)
	if original == "" {
		original, e = executableInvocation(os.Args[0])
		if e != nil {
			return e
		}
	}
	if e = os.Setenv(OriginalPathEnv, original); e != nil {
		return e
	}
	if e = os.Setenv(reexecEnv, "1"); e != nil {
		return e
	}
	return syscall.Exec(dest, os.Args, os.Environ())
}

func executableInvocation(arg string) (string, error) {
	if !filepath.IsAbs(arg) {
		var err error
		if filepath.Base(arg) == arg {
			arg, err = exec.LookPath(arg)
			if err != nil {
				return "", err
			}
		}
		arg, err = filepath.Abs(arg)
		if err != nil {
			return "", err
		}
	}
	return arg, nil
}

// Retain publishes source without replacing any existing release.
func Retain(source, version, home string) (string, error) {
	data, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	base := filepath.Join(home, ".local", "lib", "ikesu", "releases")
	for _, name := range []string{version, version + "+" + hex.EncodeToString(sum[:])[:12]} {
		dir := filepath.Join(base, name)
		dest := filepath.Join(dir, "ikesu")
		if err := privateDirs(home, []string{".local", "lib", "ikesu", "releases", name}); err != nil {
			return "", fmt.Errorf("retain %s: %w", dest, err)
		}
		existing, err := readRelease(dest)
		if err == nil {
			if sha256.Sum256(existing) != sum {
				continue
			}
			if err = guard.CheckExecutable(dest); err != nil {
				return "", fmt.Errorf("retained executable %s: %w", dest, err)
			}
			return dest, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("retain %s: %w", dest, err)
		}
		f, err := os.CreateTemp(dir, ".install-")
		if err != nil {
			return "", err
		}
		tmp := f.Name()
		_, err = f.Write(data)
		if err == nil {
			err = f.Sync()
		}
		if err == nil {
			err = f.Chmod(0555)
		}
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Link(tmp, dest)
		}
		os.Remove(tmp)
		if os.IsExist(err) {
			existing, readErr := readRelease(dest)
			if readErr != nil {
				return "", fmt.Errorf("retain %s: %w", dest, readErr)
			}
			if sha256.Sum256(existing) != sum {
				continue
			}
			err = nil
		}
		if err != nil {
			return "", fmt.Errorf("retain %s: %w", dest, err)
		}
		if err = guard.CheckExecutable(dest); err != nil {
			return "", fmt.Errorf("retained executable %s: %w", dest, err)
		}
		return dest, nil
	}
	return "", fmt.Errorf("retained executable collision for %s", filepath.Join(base, version+"+"+hex.EncodeToString(sum[:])[:12], "ikesu"))
}

func readRelease(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("untrusted retained executable: %s", path)
	}
	return os.ReadFile(path)
}

func privateDirs(home string, parts []string) error {
	if !filepath.IsAbs(home) {
		return fmt.Errorf("home is not absolute: %s", home)
	}
	if err := guard.CheckDirectory(home); err != nil {
		return err
	}
	path := home
	for _, part := range parts {
		path = filepath.Join(path, part)
		if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || int(st.Uid) != os.Geteuid() || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("untrusted release directory: %s", path)
		}
		if err = guard.CheckDirectory(path); err != nil {
			return err
		}
	}
	return nil
}
