package api

const (
	MethodPaneGraphicsInfo   = "pane.graphics.info"
	MethodPaneGraphicsSet    = "pane.graphics.set"
	MethodPaneGraphicsClear  = "pane.graphics.clear"
	MethodPaneGraphicsStream = "pane.graphics.stream"
	// The producer contract and the reference binary both allow 16 layers.
	MaxGraphicsLayers = 16
)

type PaneGraphicsPlacement struct {
	ViewportCol int32  `json:"viewport_col"`
	ViewportRow int32  `json:"viewport_row"`
	GridCols    uint32 `json:"grid_cols"`
	GridRows    uint32 `json:"grid_rows"`
}

type PaneGraphicsSetParams struct {
	PaneID      string                `json:"pane_id"`
	Format      string                `json:"format"`
	ImageWidth  uint32                `json:"image_width"`
	ImageHeight uint32                `json:"image_height"`
	DataBase64  string                `json:"data_base64"`
	LayerID     *string               `json:"layer_id"`
	Placement   PaneGraphicsPlacement `json:"placement"`
	ZIndex      int32                 `json:"z_index"`
}

type PaneGraphicsClearParams struct {
	PaneID  string  `json:"pane_id"`
	LayerID *string `json:"layer_id"`
}

type PaneGraphicsInfoResult struct {
	Type             string `json:"type"`
	CellWidthPX      int    `json:"cell_width_px"`
	CellHeightPX     int    `json:"cell_height_px"`
	PaneVisible      bool   `json:"pane_visible"`
	FileFrameDamage  bool   `json:"file_frame_damage"`
	MaxLayersPerPane int    `json:"max_layers_per_pane"`
	PixelMouse       bool   `json:"pixel_mouse"`
}
