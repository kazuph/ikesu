package notify

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/kazuph/goterrace/internal/api"
)

//go:embed sounds/done.wav
var doneSound []byte

//go:embed sounds/request.wav
var requestSound []byte

var soundCacheWarning sync.Once

func (d Desktop) defaultSound(kind string) (string, error) {
	data := doneSound
	if kind == "needs_attention" {
		data = requestSound
	}
	cache := d.CacheDir
	if cache == nil {
		cache = os.UserCacheDir
	}
	base, err := cache()
	if err != nil {
		return "", err
	}
	namespace := api.BinaryNamespace()
	if d.Executable != "" {
		namespace = api.ExecutableNamespace(d.Executable)
	}
	dir := filepath.Join(base, namespace, "sounds")
	for _, path := range []string{filepath.Dir(dir), dir} {
		if err := os.MkdirAll(path, 0700); err != nil {
			return "", err
		}
		if err := os.Chmod(path, 0700); err != nil {
			return "", err
		}
	}
	path := filepath.Join(dir, fmt.Sprintf("%x.wav", sha256.Sum256(data)))
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	file, err := os.CreateTemp(dir, ".sound-")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	// Publish the complete file without replacing an existing hash, including
	// when two attached clients materialize the same embedded sound at once.
	if err := os.Link(file.Name(), path); err != nil && !os.IsExist(err) {
		return "", err
	}
	return path, nil
}

func (d Desktop) warnSound(err error) {
	once := &soundCacheWarning
	if d.soundWarning != nil {
		once = d.soundWarning
	}
	once.Do(func() {
		if d.Warn != nil {
			d.Warn(err)
		} else {
			fmt.Fprintln(os.Stderr, "gtx: notification:", err)
		}
	})
}
