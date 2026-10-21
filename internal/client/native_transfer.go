package client

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/render"
)

type nativeViewerTransfer struct {
	identity   endpoint.GraphicsTransmission
	asset      endpoint.GraphicsSurfaceAsset
	epoch      uint64
	connection *endpointConnection
	result     *endpoint.GraphicsTransmissionResult
	sent       bool
}

func nativePaneVisible(in render.Input, pane int) bool {
	if in.State == nil || in.Panes[pane] == nil {
		return false
	}
	if in.State.PopupVisible() && in.State.Popup.PaneID == pane {
		return true
	}
	for _, rect := range in.State.DisplayPaneRects() {
		if rect.ID == pane && rect.Rect.W > 0 && rect.Rect.H > 0 {
			return true
		}
	}
	return false
}

func decodeNativeAsset(file endpoint.GraphicsFile, in render.Input) (endpoint.GraphicsSurfaceAsset, error) {
	var asset endpoint.GraphicsSurfaceAsset
	if err := json.Unmarshal(file.SurfaceAsset, &asset); err != nil {
		return asset, err
	}
	image := asset.Placement.Image
	if !nativePaneVisible(in, asset.Pane) || image.NativeID != file.ImageID || image.ID == 0 || image.Format != 32 || image.Width <= 0 || image.Height <= 0 || len(image.Data) != 0 || asset.Placement.Virtual || asset.Placement.Cols <= 0 || asset.Placement.Rows <= 0 {
		return asset, fmt.Errorf("invalid native graphics surface asset")
	}
	width, height := uint64(image.Width), uint64(image.Height)
	if width > api.MaxGraphicsFileDirectBytes/4/height || width*height*4 != file.ExpectedLen {
		return asset, fmt.Errorf("native surface does not match file dimensions")
	}
	controls := map[string]string{}
	for _, item := range strings.Split(file.Control, ",") {
		key, value, _ := strings.Cut(item, "=")
		controls[key] = value
	}
	if file.Leading != "" || controls["a"] != "t" || controls["s"] != strconv.Itoa(image.Width) || controls["v"] != strconv.Itoa(image.Height) {
		return asset, fmt.Errorf("native surface does not match endpoint upload controls")
	}
	for _, placement := range in.Panes[asset.Pane].Graphics {
		if placement.Image.ID == image.ID && placement.Image.ID != asset.ReplaceImage {
			return asset, fmt.Errorf("native surface image conflicts with another source")
		}
	}
	return asset, nil
}

func snapshotHasNative(in *render.Input, image uint32) bool {
	if in != nil {
		for _, pane := range in.Panes {
			for _, placement := range pane.Graphics {
				if placement.Image.NativeID == image {
					return true
				}
			}
		}
	}
	return false
}

// Apply only to the copied paint input; the server commits after placed success.
func (m *machineClient) nativePaintInput(in render.Input) (render.Input, bool, error) {
	pending := m.nativeTransfer
	if pending == nil || pending.result == nil {
		return in, false, nil
	}
	local := m.endpoints["local"]
	if pending.sent && local != nil && snapshotHasNative(local.snapshot, pending.identity.ImageID) {
		m.nativeTransfer = nil
		return in, false, nil
	}
	valid := m.active == "local" && !m.blurred && local != nil && local.connection == pending.connection && local.epoch == pending.epoch && nativePaneVisible(in, pending.asset.Pane)
	if !valid {
		if !pending.sent {
			result := *pending.result
			result.Success = false
			return in, false, m.sendGraphicsFileResult(pending.connection, &result)
		}
		return in, false, nil
	}
	panes := make(map[int]*render.PaneSnap, len(in.Panes))
	for pid, pane := range in.Panes {
		panes[pid] = pane
	}
	pane := *panes[pending.asset.Pane]
	var graphics []kitty.Placement
	for _, placement := range pane.Graphics {
		if (pending.asset.ReplaceImage == 0 || placement.Image.ID != pending.asset.ReplaceImage) && placement.Image.NativeID != pending.identity.ImageID {
			graphics = append(graphics, placement)
		}
	}
	pane.Graphics = append(graphics, pending.asset.Placement)
	panes[pending.asset.Pane] = &pane
	in.Panes = panes
	return in, true, nil
}

func (m *machineClient) nativePainted() error {
	if pending := m.nativeTransfer; pending != nil && pending.result != nil && !pending.sent {
		if err := pending.connection.enqueue(msg{Type: endpoint.GraphicsResultMessage, GraphicsResult: pending.result}); err != nil {
			return err
		}
		pending.sent = true
	}
	return nil
}

func (m *machineClient) cancelNativeTransfer(identity endpoint.GraphicsTransmission) error {
	m.fileGraphics.retired(identity, time.Now())
	if pending := m.nativeTransfer; pending != nil && pending.identity == identity {
		m.nativeTransfer = nil
		local := m.endpoints["local"]
		if local == nil || !snapshotHasNative(local.snapshot, identity.ImageID) {
			_, err := m.out.Write(m.frames.ReleaseNativeImage(identity.ImageID))
			return err
		}
	}
	return nil
}
