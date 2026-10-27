# ikesu

English | [日本語](README.ja.md)

A terminal multiplexer for watching many AI coding agents side by side. ikesu shows the workspaces, tabs and panes of your local machine and of SSH machines in one screen, and lets agents exchange messages and run long jobs without blocking.

- One sidebar for the spaces of the local machine and SSH machines; move between them with the mouse or keys
- Agent status (waiting for a decision, working, done) in the sidebar and the terminal title
- Durable messages with `ikesu send` / `ikesu inbox`, background jobs with completion notices via `ikesu run`
- Running panes keep going when the server is handed off to a new version (live handoff)
- On macOS, a native panel for answering agents' decisions

The specification is in [SPEC.md](SPEC.md) (Japanese).

## Platforms

macOS (Apple Silicon and Intel) and Linux (arm64 and amd64). The decision panel is available only on Apple Silicon macOS.

## Install

With Homebrew:

```sh
brew install kazuph/tap/ikesu
ikesu --version
```

Binaries for each platform are also available from [GitHub Releases](https://github.com/kazuph/ikesu/releases).

## Usage

```sh
ikesu                      # open the default session (created if missing)
ikesu --session work       # open a named session
ikesu help                 # commands and usage
```

The default prefix key is `ctrl+b`. Press `prefix ?` to list key bindings.

### Running agents

```sh
ikesu agent start reviewer -- claude          # start an agent in a pane
ikesu send <pane> "please fix the tests"      # send a message to an agent
ikesu run --label tests -- go test ./...      # run without waiting; the caller is notified on exit
ikesu log <job_id>                            # read the job result
```

### Combining machines

```sh
ikesu machine add mini.local --label mini
ikesu machine list
```

Spaces of added machines appear in the sidebar. `keys.next_workspace` / `keys.previous_workspace` (for example `cmd+j` / `cmd+k`) move through spaces across machines. The other machine needs the same ikesu version. SSH keys and authentication come from your OpenSSH configuration.

### Decision panel (macOS)

```sh
ikesu mac-dialog install
ikesu mac-dialog status
```

Lets you answer agents' confirmations and questions in a native window. The panel never takes the keyboard. While you are looking at the space of the pane that asked, the window is placed behind other windows.

## Configuration

The configuration file is `~/.config/ikesu/config.toml` (override with `IKESU_CONFIG`).

```toml
[keys]
prefix = "ctrl+t"
next_workspace = "cmd+j"
previous_workspace = "cmd+k"
switch_workspace = "cmd+1..9"
```

## Terminal setup: switch tabs with ⌃Tab / ⌃⇧Tab

By default ikesu binds ⌃Tab to "next tab" and ⌃⇧Tab to "previous tab" (`prefix n` / `prefix p` also work). Many terminal apps use ⌃Tab for their own tabs, so the key does not reach ikesu as is. Configure your terminal to send ⌃Tab as a kitty keyboard protocol sequence.

| Key | Sequence the terminal sends |
|---|---|
| ⌃Tab | `\x1b[9;5u` |
| ⌃⇧Tab | `\x1b[9;6u` |

### Ghostty

Add the following to the configuration file (on macOS, `~/Library/Application Support/com.mitchellh.ghostty/config` or `~/.config/ghostty/config`) and reload it with ⌘⇧,.

```
keybind = ctrl+tab=text:\x1b[9;5u
keybind = ctrl+shift+tab=text:\x1b[9;6u
```

- Unbinding alone (`keybind = ctrl+tab=unbind`) is not enough. With macOS native tabs (`macos-titlebar-style = tabs`), the key press is consumed by the "Show Next Tab" menu item and only the key release reaches the app.
- After this change, use ⌘⇧[ / ⌘⇧] to switch Ghostty's own tabs.

### Other terminals

Configure ⌃Tab / ⌃⇧Tab to send the sequences in the table above. Examples for terminals other than Ghostty have not been verified yet.

### ⌘Tab cannot be used

macOS takes ⌘Tab for app switching before any terminal app sees it.

## License

[MIT](LICENSE)
