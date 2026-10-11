package session

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// History is separate from the session shape because terminal contents may
// contain credentials. It is read and written only with explicit opt-in.
type History struct {
	Version int                 `json:"version"`
	Panes   map[int]PaneHistory `json:"panes"`
}
type PaneHistory struct {
	ANSI  string `json:"ansi"`
	Lines int    `json:"lines"`
}

func HistoryPath(dir string) string { return filepath.Join(dir, "session-history.json") }
func SaveHistory(dir string, h History) error {
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}
	path := HistoryPath(dir)
	file, err := os.CreateTemp(dir, ".session-history-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
func LoadHistory(dir string) (*History, error) {
	data, err := os.ReadFile(HistoryPath(dir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var h History
	if err = json.Unmarshal(data, &h); err != nil {
		return nil, err
	}
	if h.Version != CurrentVersion {
		return nil, nil
	}
	return &h, nil
}
