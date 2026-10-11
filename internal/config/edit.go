package config

import "fmt"

// Edit describes existing settings operations for execution on the client host.
type Edit struct {
	Kind    string `json:"kind"`
	Key     string `json:"key,omitempty"`
	Value   string `json:"value,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
}

func (e Edit) Apply() (Config, error) {
	var err error
	switch e.Kind {
	case "theme":
		err = SaveTheme(e.Value)
	case "sound":
		err = SaveSound(e.Enabled)
	case "toast":
		err = SaveToast(ToastDelivery(e.Value))
	case "sidebar":
		err = SaveSidebarSetting(e.Key, e.Value)
	case "agent_sort":
		err = SaveAgentSort(e.Value)
	case "onboarding":
		err = SaveOnboarding(e.Enabled)
	case "reload":
	default:
		err = fmt.Errorf("unsupported configuration operation: %s", e.Kind)
	}
	if err != nil {
		return Config{}, err
	}
	return Load()
}
