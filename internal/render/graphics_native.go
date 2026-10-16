package render

import (
	"bytes"
	"fmt"
)

// Reserve before writing a file upload, so inline rendering cannot reuse its ID.
func (e *Encoder) ReserveNativeImage(id uint32) error {
	if id == 0 {
		return fmt.Errorf("native image ID must be positive")
	}
	if _, exists := e.nativeImages[id]; exists {
		return fmt.Errorf("native image ID is already reserved")
	}
	for _, old := range e.graphicsIDs {
		if old == id {
			return fmt.Errorf("native image ID belongs to an inline image")
		}
	}
	if e.nativeImages == nil {
		e.nativeImages = map[uint32]bool{}
	}
	e.nativeImages[id] = false
	return nil
}

// Observe all local panes, including hidden workspaces, before encoding a frame.
// Append the returned deletions after that frame to preserve replacement order.
func (e *Encoder) ObserveNativeImages(panes map[int]*PaneSnap) ([]byte, error) {
	present := map[uint32]bool{}
	for _, pane := range panes {
		if pane == nil {
			return nil, fmt.Errorf("native image snapshot contains a nil pane")
		}
		for _, placement := range pane.Graphics {
			id := placement.Image.NativeID
			if id == 0 {
				continue
			}
			if placement.Image.Format != 32 || placement.Image.Width <= 0 || placement.Image.Height <= 0 || len(placement.Image.Data) != 0 {
				return nil, fmt.Errorf("native image descriptor contains invalid dimensions, format, or inline pixels")
			}
			if _, reserved := e.nativeImages[id]; !reserved {
				return nil, fmt.Errorf("native image was not received by this viewer")
			}
			present[id] = true
		}
	}
	var retired bytes.Buffer
	for id, seen := range e.nativeImages {
		if present[id] {
			e.nativeImages[id] = true
		} else if seen {
			retired.Write(e.ReleaseNativeImage(id))
		}
	}
	return retired.Bytes(), nil
}

func (e *Encoder) ReleaseNativeImage(id uint32) []byte {
	if _, exists := e.nativeImages[id]; !exists {
		return nil
	}
	delete(e.nativeImages, id)
	return []byte(fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2;\x1b\\", id))
}
