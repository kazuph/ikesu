package render

import "encoding/json"

type GraphicsImageKey struct {
	Source      json.RawMessage `json:"source"`
	Width       int             `json:"image_width"`
	Height      int             `json:"image_height"`
	Format      string          `json:"format"`
	DataLength  int             `json:"data_len"`
	Fingerprint uint64          `json:"data_fingerprint"`
}

type GraphicsPatchRect struct {
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	PNG    []byte `json:"png"`
}

type GraphicsImagePatch struct {
	Key   GraphicsImageKey    `json:"key"`
	Base  GraphicsImageKey    `json:"base"`
	Rects []GraphicsPatchRect `json:"rects"`
}

type PaneGraphicsPatch struct {
	Pane, Placement int
	Patch           GraphicsImagePatch
}
