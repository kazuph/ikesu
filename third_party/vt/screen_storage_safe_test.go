package vt

import (
	"sync"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestSafeScreenStorageReadersDuringWriteAndResize(t *testing.T) {
	e := NewSafeEmulator(8, 3)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			e.Write([]byte("abc界e\u0301\r\n"))
			e.Resize(8+i%2, 3)
		}
	}()
	go func() {
		defer wg.Done()
		row := make([]uv.Cell, 8)
		for i := 0; i < 200; i++ {
			e.CopyScreenRow(row, 0, i%3)
			if info := e.ScreenStorage(); info.CellBytes <= 0 || info.CellCapacity < 24 {
				t.Errorf("invalid screen storage: %+v", info)
			}
		}
	}()
	wg.Wait()
}
