package distribution

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func archivePath(name, root string) (string, error) {
	if strings.Contains(name, "\\") || path.IsAbs(name) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	for _, component := range strings.Split(name, "/") {
		if component == ".." {
			return "", fmt.Errorf("parent reference in archive %q", name)
		}
	}
	name = path.Clean(name)
	if name == root {
		return ".", nil
	}
	if !strings.HasPrefix(name, root+"/") {
		return "", fmt.Errorf("archive entry outside %s: %q", root, name)
	}
	return strings.TrimPrefix(name, root+"/"), nil
}

func readArchive(file string, visit func(*tar.Header, io.Reader) error) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer z.Close()
	r := tar.NewReader(z)
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if err := visit(h, r); err != nil {
			return err
		}
	}
	// Read through the gzip trailer; a truncated stream must never be installed.
	_, err = io.Copy(io.Discard, z)
	return err
}

func InspectArchive(file, root string) (map[string]tar.Header, error) {
	entries := map[string]tar.Header{}
	err := readArchive(file, func(h *tar.Header, _ io.Reader) error {
		name, err := archivePath(h.Name, root)
		if err != nil {
			return err
		}
		if _, exists := entries[name]; exists {
			return fmt.Errorf("duplicate archive entry %q", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir, tar.TypeReg:
		case tar.TypeSymlink:
			if path.IsAbs(h.Linkname) || strings.Contains(h.Linkname, "\\") {
				return fmt.Errorf("unsafe symlink %q", h.Name)
			}
			resolved := path.Join(path.Dir(name), h.Linkname)
			if resolved == ".." || strings.HasPrefix(resolved, "../") {
				return fmt.Errorf("escaping symlink %q", h.Name)
			}
		case tar.TypeLink:
			if _, err := archivePath(h.Linkname, root); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported archive entry type %d at %q", h.Typeflag, h.Name)
		}
		entries[name] = *h
		return nil
	})
	if err != nil {
		return nil, err
	}
	for name, h := range entries {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if p, exists := entries[parent]; exists && p.Typeflag != tar.TypeDir {
				return nil, fmt.Errorf("entry %q descends through non-directory %q", name, parent)
			}
		}
		if h.Typeflag == tar.TypeLink {
			target, _ := archivePath(h.Linkname, root)
			if entries[target].Typeflag != tar.TypeReg {
				return nil, fmt.Errorf("hardlink must point to a regular archive file")
			}
		}
		if h.Typeflag == tar.TypeSymlink {
			if err := inspectLinkTarget(strings.Split(path.Dir(name), "/"), strings.Split(h.Linkname, "/"), entries, map[string]bool{name: true}); err != nil {
				return nil, err
			}
		}
	}
	return entries, nil
}

// Resolve archive links component by component: lexical cleaning alone would
// miss `alias/..` when alias itself points at the package root.
func inspectLinkTarget(base, components []string, entries map[string]tar.Header, visiting map[string]bool) error {
	stack := append([]string{}, base...)
	if len(stack) == 1 && stack[0] == "." {
		stack = nil
	}
	for len(components) > 0 {
		component := components[0]
		components = components[1:]
		switch component {
		case "", ".":
			continue
		case "..":
			if len(stack) == 0 {
				return fmt.Errorf("symlink chain escapes archive root")
			}
			stack = stack[:len(stack)-1]
		default:
			name := strings.Join(append(append([]string{}, stack...), component), "/")
			if h, ok := entries[name]; ok && h.Typeflag == tar.TypeSymlink {
				if visiting[name] {
					return fmt.Errorf("cyclic archive symlink %q", name)
				}
				next := make(map[string]bool, len(visiting)+1)
				for k, v := range visiting {
					next[k] = v
				}
				next[name] = true
				return inspectLinkTarget(stack, append(strings.Split(h.Linkname, "/"), components...), entries, next)
			}
			stack = append(stack, component)
		}
	}
	return nil
}

func ExtractArchive(file, root, destination string) error {
	entries, err := InspectArchive(file, root)
	if err != nil {
		return err
	}
	// The caller provides an empty staging directory on the target filesystem.
	items, err := os.ReadDir(destination)
	if err != nil {
		return err
	}
	if len(items) != 0 {
		return fmt.Errorf("archive staging directory is not empty")
	}
	err = readArchive(file, func(h *tar.Header, r io.Reader) error {
		name, _ := archivePath(h.Name, root)
		target := filepath.Join(destination, filepath.FromSlash(name))
		if h.Typeflag == tar.TypeDir {
			return os.MkdirAll(target, 0755)
		}
		if h.Typeflag != tar.TypeReg {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(h.Mode)&0777)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, r)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	if err != nil {
		return err
	}
	for name, h := range entries {
		if h.Typeflag != tar.TypeLink && h.Typeflag != tar.TypeSymlink {
			continue
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if h.Typeflag == tar.TypeSymlink {
			err = os.Symlink(h.Linkname, target)
		} else {
			linked, _ := archivePath(h.Linkname, root)
			err = os.Link(filepath.Join(destination, filepath.FromSlash(linked)), target)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func FileSHA256(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func TreeSHA256(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(file string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%q %v ", name, info.Mode())
		if info.Mode().IsRegular() {
			value, err := FileSHA256(file)
			if err != nil {
				return err
			}
			fmt.Fprintln(hash, value)
		} else if info.Mode()&os.ModeSymlink != 0 {
			value, err := os.Readlink(file)
			if err != nil {
				return err
			}
			fmt.Fprintf(hash, "%q\n", value)
		} else if !info.IsDir() {
			return fmt.Errorf("unsupported installed file %q", file)
		}
		return nil
	})
	return hex.EncodeToString(hash.Sum(nil)), err
}
