package distribution

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kazuph/ikesu/internal/plugin"
)

type Installer struct {
	Product Product
	Home    string
	Store   *plugin.Store
	Fetch   Fetcher
	// Register uses the same plugin.link request as the ordinary plugin CLI.
	// sent distinguishes a connect failure from an ambiguous saved response.
	Register func(context.Context, string) (sent bool, err error)
	Verify   func(string) error
}

func (i *Installer) Directory() string {
	return filepath.Join(i.Home, ".local", "share", "ikesu", i.Product.Name)
}

func (i *Installer) Install(ctx context.Context, release Release, takeOver bool) (string, error) {
	p := i.Product
	if !strings.HasPrefix(release.Tag, p.TagPrefix) {
		return "", fmt.Errorf("unexpected release tag")
	}
	v := strings.TrimPrefix(release.Tag, p.TagPrefix)
	if _, err := version(v); err != nil {
		return "", err
	}
	if p.Minimum != "" && newer(p.Minimum, v) {
		return "", fmt.Errorf("%s requires %s or newer", p.Name, p.Minimum)
	}
	archiveURL, sumsURL, err := p.URLs(release)
	if err != nil {
		return "", err
	}
	root := i.Directory()
	if err := os.MkdirAll(root, 0755); err != nil {
		return "", err
	}
	lock, err := os.OpenFile(filepath.Join(root, ".install.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return "", fmt.Errorf("another %s installation is active: %w", p.Name, err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	work, err := os.MkdirTemp(root, ".staging-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	archive, sums := filepath.Join(work, "archive.tar.gz"), filepath.Join(work, "SHA256SUMS")
	if err := i.Fetch.Fetch(ctx, sumsURL, sums); err != nil {
		return "", err
	}
	data, err := os.ReadFile(sums)
	if err != nil {
		return "", err
	}
	expected, err := Checksum(data, p.Asset)
	if err != nil {
		return "", err
	}
	if err := i.Fetch.Fetch(ctx, archiveURL, archive); err != nil {
		return "", err
	}
	actual, err := FileSHA256(archive)
	if err != nil {
		return "", err
	}
	if actual != expected {
		return "", fmt.Errorf("SHA256 mismatch for %s", p.Asset)
	}
	stage := filepath.Join(work, "package")
	if err := os.Mkdir(stage, 0755); err != nil {
		return "", err
	}
	if err := ExtractArchive(archive, p.Root, stage); err != nil {
		return "", err
	}
	if err := i.validate(stage, v); err != nil {
		return "", err
	}
	destination := filepath.Join(root, v)
	if _, err := os.Lstat(destination); err == nil {
		old, err := TreeSHA256(destination)
		if err != nil {
			return "", err
		}
		fresh, err := TreeSHA256(stage)
		if err != nil {
			return "", err
		}
		if old != fresh {
			return "", fmt.Errorf("refusing different contents for installed version %s", v)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	} else if err := os.Rename(stage, destination); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// SIGINT/SIGTERM cancellation is observed above. The switch and registration
	// now reach a committed or rolled-back boundary before returning.
	if err := i.activate(destination, v, takeOver); err != nil {
		return "", err
	}
	return destination, nil
}

func (i *Installer) validate(root, v string) error {
	m, _, err := plugin.ReadManifest(filepath.Join(root, i.Product.Manifest), i.Store.Version)
	if err != nil {
		return fmt.Errorf("required ikesu plugin: %w", err)
	}
	if i.Product.PluginID != "" && m.ID != i.Product.PluginID {
		return fmt.Errorf("unexpected plugin id %q", m.ID)
	}
	if len(m.Build) != 0 {
		return fmt.Errorf("release plugin must not require a build")
	}
	{
		data, err := os.ReadFile(filepath.Join(root, "VERSION"))
		if err != nil || strings.TrimSpace(string(data)) != v {
			return fmt.Errorf("release VERSION does not match %s", v)
		}
	}
	if i.Product.Name == "terminal-browser" {
		info, err := os.Stat(filepath.Join(root, "bin", "terminal-browser"))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("release executable missing or not executable")
		}
	}
	if i.Product.Name == "mac-dialog-decisions" && i.Verify == nil {
		return fmt.Errorf("Developer ID signature verification is required")
	}
	if i.Verify != nil {
		return i.Verify(root)
	}
	return nil
}

func (i *Installer) activate(destination, v string, takeOver bool) error {
	manifest := filepath.Join(destination, i.Product.Manifest)
	expected, err := i.Store.PreparedLink(manifest, true, nil)
	if err != nil {
		return err
	}
	previous, err := i.Store.Registration(expected.ID)
	if err != nil {
		return err
	}
	currentPath := filepath.Join(i.Directory(), "current-version")
	current, err := snapshotFile(currentPath)
	if err != nil {
		return err
	}
	changes := []fileChange{{path: currentPath, previous: current, next: []byte(v + "\n"), mode: 0600}}
	if i.Product.Name == "terminal-browser" {
		wrapperPath := filepath.Join(i.Home, ".local", "bin", "tb")
		wrapper, err := snapshotFile(wrapperPath)
		if err != nil {
			return err
		}
		if !wrapper.exists || wrapper.link == "" && strings.HasPrefix(string(wrapper.data), wrapperMarker) || takeOver {
			changes = append(changes, fileChange{path: wrapperPath, previous: wrapper, next: wrapperBytes(destination), mode: 0755})
		}
	}
	changed := 0
	rollbackFiles := func() error {
		var failures []error
		for n := changed - 1; n >= 0; n-- {
			if err := changes[n].restore(); err != nil {
				failures = append(failures, err)
			}
		}
		return errors.Join(failures...)
	}
	for n := range changes {
		if err := changes[n].apply(); err != nil {
			return errors.Join(err, rollbackFiles())
		}
		changed++
	}
	var sent bool
	if i.Register != nil {
		sent, err = i.Register(context.Background(), manifest)
	} else {
		sent = true
		_, err = i.Store.Link(manifest, true, nil)
	}
	if err == nil {
		return nil
	}
	if sent {
		err = errors.Join(err, i.Store.RestoreRegistration(expected, previous))
	}
	return errors.Join(err, rollbackFiles())
}
