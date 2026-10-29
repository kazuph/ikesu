package config

import (
	"fmt"
	"time"
)

type ScrollbarMode string

func (m *ScrollbarMode) UnmarshalTOML(value any) error {
	switch v := value.(type) {
	case bool:
		if v {
			*m = "always"
		} else {
			*m = "off"
		}
	case string:
		if v != "auto" && v != "always" && v != "off" {
			return fmt.Errorf("expected auto, always or off")
		}
		*m = ScrollbarMode(v)
	default:
		return fmt.Errorf("expected auto, always, off or boolean")
	}
	return nil
}

func (c Config) PaneScrollbarMode() ScrollbarMode {
	if c.UI.PaneScrollbars == "" {
		return "auto"
	}
	return c.UI.PaneScrollbars
}

// The requested idle interval is 1.5 seconds; configuration uses milliseconds.
func (c Config) PaneScrollbarHideDelay() time.Duration {
	if c.UI.PaneScrollbarHideDelayMs != nil && *c.UI.PaneScrollbarHideDelayMs > 0 {
		return time.Duration(*c.UI.PaneScrollbarHideDelayMs) * time.Millisecond
	}
	return 1500 * time.Millisecond
}
