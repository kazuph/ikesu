package config

import "fmt"

var ThemeNames = []string{"catppuccin", "catppuccin-latte", "terminal", "tokyo-night", "tokyo-night-day", "dracula", "nord", "gruvbox", "gruvbox-light", "one-dark", "one-light", "solarized", "solarized-light", "kanagawa", "rose-pine", "vesper"}

func ValidateTheme(name string) error {
	for _, n := range ThemeNames {
		if n == name {
			return nil
		}
	}
	return fmt.Errorf("unsupported theme: %s", name)
}
