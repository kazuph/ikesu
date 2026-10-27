# ikesu

AI のコーディングエージェントをたくさん並べて見守るための端末多重化ツール（terminal multiplexer）。手元と SSH 先の複数マシンのワークスペース・タブ・ペインを1つの画面にまとめて表示し、エージェント同士のメッセージ送信や、待たずに走らせる長時間ジョブを扱えます。

- 手元と SSH 先のマシンのスペースを1つのサイドバーに並べ、マウスやキーで行き来できる
- エージェントの状態（判断待ち・作業中・完了）をサイドバーと端末のタイトルに表示する
- `ikesu send` / `ikesu inbox` による消えないメッセージ、`ikesu run` による背景ジョブと完了通知
- サーバーを新しい版へ引き継いでも、動いているペインは止まらない（live handoff）
- macOS では、エージェントの判断待ちに答えるネイティブのパネルを使える

仕様は [SPEC.md](SPEC.md) にあります。

## 対応環境

macOS（Apple Silicon・Intel）と Linux（arm64・amd64）。判断待ちパネルは Apple Silicon の macOS のみです。

## インストール

Homebrew で入れます。

```sh
brew install kazuph/tap/ikesu
ikesu --version
```

[GitHub Releases](https://github.com/kazuph/ikesu/releases) から各環境の実行ファイルを直接取得することもできます。

## 使い方

```sh
ikesu                      # 既定のセッションを開く（無ければ作る）
ikesu --session work       # 名前付きのセッションを開く
ikesu help                 # コマンドの一覧と使い方
```

prefix キーの既定は `ctrl+b` です。`prefix ?` でキー操作の一覧を表示します。

### エージェントを動かす

```sh
ikesu agent start reviewer -- claude          # ペインでエージェントを起動
ikesu send <pane> "テストを直して"             # エージェントへメッセージを送る
ikesu run --label tests -- go test ./...      # 待たずに走らせ、終わったら呼び出し元へ通知
ikesu log <job_id>                            # ジョブの結果を読む
```

### 複数のマシンをまとめる

```sh
ikesu machine add mini.local --label mini
ikesu machine list
```

追加したマシンのスペースはサイドバーに並びます。`keys.next_workspace` / `keys.previous_workspace`（例: `cmd+j` / `cmd+k`）は、マシンをまたいでスペースを順に移ります。相手のマシンも同じ版の ikesu が必要です。SSH の鍵や認証は OpenSSH の設定をそのまま使います。

### 判断待ちパネル（macOS）

```sh
ikesu mac-dialog install
ikesu mac-dialog status
```

エージェントの確認や質問を、ネイティブの窓で答えられるようにします。パネルはキーボードを奪いません。その質問を出したペインのスペースを今見ているときは、窓を背面に置きます。

## 設定

設定ファイルは `~/.config/ikesu/config.toml` です（`IKESU_CONFIG` で変えられます）。

```toml
[keys]
prefix = "ctrl+t"
next_workspace = "cmd+j"
previous_workspace = "cmd+k"
switch_workspace = "cmd+1..9"
```

## 端末アプリの設定: ⌃Tab / ⌃⇧Tab でタブを移動する

ikesu は既定で ⌃Tab を「次のタブ」、⌃⇧Tab を「前のタブ」に割り当てています（`prefix n` / `prefix p` も使えます）。ただし多くの端末アプリは ⌃Tab を自分のタブ切替に使うため、そのままでは ikesu までキーが届きません。端末アプリ側で、⌃Tab を kitty キー符号の文字列として送るように設定してください。

| キー | 端末から送る文字列 |
|---|---|
| ⌃Tab | `\x1b[9;5u` |
| ⌃⇧Tab | `\x1b[9;6u` |

### Ghostty

設定ファイル（macOS では `~/Library/Application Support/com.mitchellh.ghostty/config` または `~/.config/ghostty/config`）に追記し、⌘⇧, で設定を読み直します。

```
keybind = ctrl+tab=text:\x1b[9;5u
keybind = ctrl+shift+tab=text:\x1b[9;6u
```

- 割当を外すだけ（`keybind = ctrl+tab=unbind`）では足りません。macOS 標準のタブ（`macos-titlebar-style = tabs`）を使っていると、キーを押した信号がメニューの「次のタブを表示」に吸われ、離した信号だけが届きます。
- この設定の後は、Ghostty 自体のタブ切替に ⌘⇧[ / ⌘⇧] を使います。

### そのほかの端末

上の表の文字列を ⌃Tab / ⌃⇧Tab で送る設定にしてください。Ghostty 以外の端末の設定例はまだ検証していません。

### ⌘Tab は使えません

⌘Tab は macOS がアプリ切替のために先に取るため、どの端末アプリにも届きません。

## ライセンス

[MIT](LICENSE)
