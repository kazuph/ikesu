package render

import "github.com/kazuph/ikesu/internal/config"

func CustomPalette(name string, custom map[string]string) Palette {
	p := ThemePalette(name)
	for key, value := range custom {
		c, err := config.ParseThemeColor(value)
		if err != nil {
			continue // Config.Load rejects invalid colors before rendering.
		}
		switch key {
		case "panel_bg":
			p.PopupBG = c
		case "surface_dim":
			p.SurfDim, p.PanelBG = c, c
		case "surface0":
			p.Surface = c
		case "surface1":
			p.NavigatorDivider = c
		case "overlay0":
			p.Dim = c
		case "overlay1":
			p.Overlay1 = c
		case "text":
			p.Text = c
		case "subtext0":
			p.Subtle = c
		case "accent":
			p.Accent = c
		case "red":
			p.Red = c
		case "green":
			p.Green = c
		case "yellow":
			p.Yellow = c
		case "mauve":
			p.Mauve = c
		case "teal":
			p.NavigatorDone = c
		case "blue":
			p.Blue = c
		case "peach":
			p.Peach = c
		}
	}
	return p
}

func ConfigPalette(c config.Config) Palette {
	p := CustomPalette(c.ThemeName(), c.Theme.Custom)
	if _, override := c.Theme.Custom["accent"]; !override && c.UI.Accent != "" {
		if value, err := config.ParseThemeColor(c.UI.Accent); err == nil {
			p.Accent = value
		}
	}
	return p
}
