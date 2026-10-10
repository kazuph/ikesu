package config

// DefaultExample contains supported settings and is shared with --default-config.
const DefaultExample = `[theme]
name = "catppuccin"

# [theme.custom]
# panel_bg = "transparent"
# accent = "#89b4fa"
# Colors accept hex, names, rgb(r,g,b), reset/default/none/transparent.

[ui.sound]
enabled = true

[ui.toast]
delivery = "off"

[ui]
mobile_width_threshold = 64
sidebar_min_width = 18
sidebar_max_width = 72
sidebar_width = 26
sidebar_start_collapsed = false
transparent_sidebar_cards = false
workspace_panel_density = "full"
agent_panel_scope = "all"
`
