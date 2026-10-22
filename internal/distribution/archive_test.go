package distribution

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeArchive(t *testing.T, headers []tar.Header) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "package.tar.gz")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, h := range headers {
		if h.Typeflag == tar.TypeReg {
			h.Size = 4
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte("test")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestArchiveAllEntriesValidatedBeforeAnyExtraction(t *testing.T) {
	for _, h := range []tar.Header{
		{Name: "/outside", Typeflag: tar.TypeReg},
		{Name: "terminal-browser/a/../outside", Typeflag: tar.TypeReg},
		{Name: "terminal-browser/link", Typeflag: tar.TypeSymlink, Linkname: "../../outside"},
		{Name: "terminal-browser/link", Typeflag: tar.TypeSymlink, Linkname: "/outside"},
		{Name: "terminal-browser/device", Typeflag: tar.TypeChar},
		{Name: "terminal-browser/data", Typeflag: tar.TypeReg},
	} {
		t.Run(h.Name+h.Linkname, func(t *testing.T) {
			file := makeArchive(t, []tar.Header{{Name: "terminal-browser/data", Typeflag: tar.TypeReg}, h})
			destination := t.TempDir()
			if err := ExtractArchive(file, "terminal-browser", destination); err == nil {
				t.Fatal("unsafe archive accepted")
			}
			items, _ := os.ReadDir(destination)
			if len(items) != 0 {
				t.Fatal("wrote files before completing inspection")
			}
		})
	}
}

func TestArchivePreservesInternalSymlinksAndHardlinks(t *testing.T) {
	file := makeArchive(t, []tar.Header{
		{Name: "terminal-browser/data", Typeflag: tar.TypeReg, Mode: 0755},
		{Name: "terminal-browser/alias", Typeflag: tar.TypeSymlink, Linkname: "data"},
		{Name: "terminal-browser/hard", Typeflag: tar.TypeLink, Linkname: "terminal-browser/data"},
	})
	destination := t.TempDir()
	if err := ExtractArchive(file, "terminal-browser", destination); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"data", "alias", "hard"} {
		value, err := os.ReadFile(filepath.Join(destination, name))
		if err != nil || string(value) != "test" {
			t.Fatal(name, string(value), err)
		}
	}
	info, _ := os.Stat(filepath.Join(destination, "data"))
	if info.Mode().Perm() != 0755 {
		t.Fatal(info.Mode())
	}
}

func TestArchiveRejectsSymlinkChainEscapeBeforeExtraction(t *testing.T) {
	file := makeArchive(t, []tar.Header{
		{Name: "terminal-browser/alias", Typeflag: tar.TypeSymlink, Linkname: "."},
		{Name: "terminal-browser/escape", Typeflag: tar.TypeSymlink, Linkname: "alias/../outside"},
	})
	destination := t.TempDir()
	if err := ExtractArchive(file, "terminal-browser", destination); err == nil {
		t.Fatal("symlink chain escape accepted")
	}
	items, _ := os.ReadDir(destination)
	if len(items) != 0 {
		t.Fatal(items)
	}
}

func TestChecksumRejectsMissingDuplicateMalformedEntries(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, value := range []string{"", digest + "  other\n", digest + "  file\n" + digest + "  file\n", "invalid  file", digest + " file extra", digest + " file\ninvalid other"} {
		if _, err := Checksum([]byte(value), "file"); err == nil {
			t.Fatal(value)
		}
	}
	if got, err := Checksum([]byte(digest+" *file\n"), "file"); err != nil || got != digest {
		t.Fatal(got, err)
	}
}
