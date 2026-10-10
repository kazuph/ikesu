package config

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// CustomColorKeys follows the published configuration reference.
var CustomColorKeys = []string{"accent", "panel_bg", "surface0", "surface1", "surface_dim", "overlay0", "overlay1", "text", "subtext0", "mauve", "green", "yellow", "red", "blue", "teal", "peach"}

func (c Config) ValidateCustomColors() error {
	for key, value := range c.Theme.Custom {
		known := false
		for _, allowed := range CustomColorKeys {
			known = known || key == allowed
		}
		if !known {
			return fmt.Errorf("unsupported theme.custom key: %s", key)
		}
		if _, err := ParseThemeColor(value); err != nil {
			return fmt.Errorf("theme.custom.%s: %w", key, err)
		}
	}
	return nil
}

// A nil color means the terminal default, rather than an alpha-blended fill.
func ParseThemeColor(value string) (color.Color, error) {
	s := strings.ToLower(strings.TrimSpace(value))
	switch s {
	case "reset", "default", "none", "transparent":
		return nil, nil
	}
	if strings.HasPrefix(s, "#") {
		hex := s[1:]
		if len(hex) == 3 {
			hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
		}
		if len(hex) == 6 {
			if n, err := strconv.ParseUint(hex, 16, 24); err == nil {
				return color.RGBA{uint8(n >> 16), uint8(n >> 8), uint8(n), 255}, nil
			}
		}
	}
	if strings.HasPrefix(s, "rgb(") && strings.HasSuffix(s, ")") {
		parts := strings.Split(s[4:len(s)-1], ",")
		if len(parts) == 3 {
			var rgb [3]uint8
			for i, part := range parts {
				n, err := strconv.ParseUint(strings.TrimSpace(part), 10, 8)
				if err != nil {
					return nil, fmt.Errorf("invalid color: %s", value)
				}
				rgb[i] = uint8(n)
			}
			return color.RGBA{rgb[0], rgb[1], rgb[2], 255}, nil
		}
	}
	names := map[string]ansi.BasicColor{
		"black": ansi.Black, "red": ansi.Red, "green": ansi.Green, "yellow": ansi.Yellow,
		"blue": ansi.Blue, "magenta": ansi.Magenta, "cyan": ansi.Cyan, "white": ansi.BrightWhite,
		"gray": ansi.White, "darkgray": ansi.BrightBlack,
		"lightred": ansi.BrightRed, "lightgreen": ansi.BrightGreen, "lightyellow": ansi.BrightYellow,
		"lightblue": ansi.BrightBlue, "lightmagenta": ansi.BrightMagenta, "lightcyan": ansi.BrightCyan,
	}
	if c, ok := names[s]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("invalid color: %s", value)
}
