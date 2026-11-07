package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type oyagoiSelection struct {
	Machine    string `json:"machine"`
	Session    string `json:"session"`
	Workspace  int    `json:"workspace"`
	Generation string `json:"generation"`
}

type oyagoiStore struct{ path string }

func (s oyagoiStore) load() (*oyagoiSelection, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var selected *oyagoiSelection
	if err := json.Unmarshal(data, &selected); err != nil {
		return nil, err
	}
	if selected != nil && (selected.Machine == "" || selected.Session == "" || selected.Workspace <= 0 || selected.Generation == "") {
		return nil, fmt.Errorf("invalid oyagoi selection")
	}
	return selected, nil
}

func (s oyagoiStore) locked(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

func (s oyagoiStore) write(selection *oyagoiSelection) error {
	data, err := json.Marshal(selection)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".oyagoi-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(append(data, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), s.path)
}

func (s oyagoiStore) set(selection *oyagoiSelection) error {
	if selection != nil && (selection.Machine == "" || selection.Session == "" || selection.Workspace <= 0 || selection.Generation == "") {
		return fmt.Errorf("invalid oyagoi selection")
	}
	return s.locked(func() error { return s.write(selection) })
}

func (s oyagoiStore) clearIf(expected oyagoiSelection) (bool, error) {
	cleared := false
	err := s.locked(func() error {
		current, err := s.load()
		if err != nil {
			return err
		}
		if current == nil || *current != expected {
			return nil
		}
		if err := s.write(nil); err != nil {
			return err
		}
		cleared = true
		return nil
	})
	return cleared, err
}
