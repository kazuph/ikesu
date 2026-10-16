package endpoint

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/kazuph/goterrace/internal/kitty"
)

const DirectKittyTransport = "direct-kitty"

const (
	GraphicsFileMessage    = "graphics_file"
	GraphicsStartedMessage = "graphics_transmission_started"
	GraphicsResultMessage  = "graphics_transmission_result"
	GraphicsRetiredMessage = "graphics_transmission_retired"
)

func WithGraphicsFiles(peer Peer) Peer {
	peer.Methods = append(peer.Methods, GraphicsFileMessage, GraphicsStartedMessage, GraphicsResultMessage, GraphicsRetiredMessage)
	return peer
}

type GraphicsFile struct {
	Path         string          `json:"path"`
	ExpectedLen  uint64          `json:"expected_len"`
	ImageID      uint32          `json:"image_id"`
	TransferID   uint64          `json:"transfer_id"`
	Leading      string          `json:"leading"`
	Control      string          `json:"control"`
	SurfaceAsset json.RawMessage `json:"surface_asset,omitempty"`
}

type GraphicsTransmission struct {
	TransferID uint64 `json:"transfer_id"`
	ImageID    uint32 `json:"image_id"`
}

type GraphicsSurfaceAsset struct {
	Pane         int             `json:"pane"`
	ReplaceImage uint32          `json:"replace_image,omitempty"`
	Placement    kitty.Placement `json:"placement"`
}

type GraphicsTransmissionResult struct {
	GraphicsTransmission
	Success bool `json:"success"`
}

// Only a complete response for the expected image belongs to this transfer.
// Unrelated or malformed terminal input remains available to the pane.
func ParseGraphicsReply(raw []byte, image uint32) (matched, success bool) {
	if len(raw) > 4096 || !bytes.HasPrefix(raw, []byte("\x1b_G")) || !bytes.HasSuffix(raw, []byte("\x1b\\")) {
		return false, false
	}
	controls, payload, ok := bytes.Cut(raw[3:len(raw)-2], []byte(";"))
	if !ok {
		return false, false
	}
	found := false
	for _, field := range bytes.Split(controls, []byte(",")) {
		key, value, ok := bytes.Cut(field, []byte("="))
		if !ok || len(key) != 1 || len(value) == 0 || key[0] != 'i' && key[0] != 'I' && key[0] != 'p' {
			return false, false
		}
		for _, digit := range value {
			if digit < '0' || digit > '9' {
				return false, false
			}
		}
		number, err := strconv.ParseUint(string(value), 10, 64)
		if err != nil {
			return false, false
		}
		if key[0] == 'i' {
			if found || number != uint64(image) {
				return false, false
			}
			found = true
		}
	}
	return found, found && bytes.Equal(payload, []byte("OK"))
}
