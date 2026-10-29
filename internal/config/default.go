package config

// DefaultExample contains supported settings and is shared with --default-config.
const DefaultExample = `[decisions]
screen_prompts = true

[theme]
name = "catppuccin"

# [theme.custom]
# panel_bg = "transparent" # applies to panes, sidebars, tabs and the bottom bar
# panel_bg = "#181825" # explicit background color overrides opaque_background
# surface_dim is the background fallback when panel_bg is not set.
# surface0 keeps its explicit card/control color override.
# accent = "#89b4fa"
# sidebar_border = "#a6adc8"
# sidebar_border_hover = "#89b4fa"
# Colors accept hex, names, rgb(r,g,b), reset/default/none/transparent.

[ui.sound]
enabled = true

[ui.toast]
delivery = "off"

[ui]
pane_scrollbars = "auto" # auto / always / off; legacy true / false also accepted
pane_scrollbar_hide_delay_ms = 1500
window_title = "summary"
mobile_width_threshold = 64
sidebar_min_width = 18
sidebar_max_width = 72
sidebar_width = 26
sidebar_start_collapsed = false
# Transparent terminal background is the default in every theme.
# Set true to fill panes and UI with the selected theme's background:
opaque_background = false
transparent_sidebar_cards = false
workspace_panel_density = "full"
agent_panel_scope = "all"
`
