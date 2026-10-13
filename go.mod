module github.com/kazuph/goterrace

go 1.27.0

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/charmbracelet/ultraviolet v0.0.0-20261001125412-878653296cfd
	github.com/charmbracelet/x/ansi v0.11.8
	github.com/charmbracelet/x/vt v0.0.0-20261004011457-ad85c59fdf4e
	github.com/creack/pty v1.1.24
	github.com/ncruces/go-sqlite3 v0.35.6
	golang.org/x/sync v0.23.0
	golang.org/x/sys v0.48.0
	golang.org/x/term v0.46.0
)

require (
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/x/exp/ordered v0.1.0 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-runewidth v0.0.30 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/ncruces/go-sqlite3-wasm/v6 v6.3.35304 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/xo/terminfo v1.0.0 // indirect
)

replace github.com/charmbracelet/ultraviolet => ./third_party/ultraviolet

replace github.com/charmbracelet/x/vt => ./third_party/vt
replace github.com/charmbracelet/x/ansi => ./third_party/ansi
