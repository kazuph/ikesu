# 設定の再読込（server reload-config）の段階の計画

作成: 2026-10-13（親 Claude）。

## 依頼

店長（2026-10-13）: herdr にあって gtx に無いサーバー操作のうち、「設定を live reload させる的なやつであれば欲しい」。herdr の `herdr server reload-config`（API `server.reload_config`）は、動いているサーバーに `config.toml` を読み直させ、pane を止めずに再読込できる設定を反映する操作で、これに当たる。`server agent-manifests`・`reload-agent-manifests` は作らない（店長の決定）。

## 0. 作り方の決まり（最優先）

- herdr のソースコードを読まない・移植しない（AGENTS.md）。挙動は利用者向け文書（`~/src/github.com/kazuph/herdr/docs/next/website/src/content/docs/configuration.mdx` の「Reload config」節、`cli-reference.mdx` の server の説明）、`docs/ref/herdr-api-schema.json`、mini の herdr 0.2.30 配布用ビルド `~/.local/share/herdr-builds/0.2.30/herdr` を一時の HOME・XDG・state・名前付き session で ptyrun の中で動かした観察で決める。店長の稼働中の herdr と gtx に触らない。
- gtx にはすでに、画面のメニューの「reload config」と `prefix+shift+r` による再読込（`internal/tui/sidebar.go` の `reloadSidebarConfig`）がある。新しく作る CLI と API は、この既存の再読込と同じ結果になる経路にする（二重の実装を作らない）。

## 1. やること

1. CLI `gtx server reload-config`、API `server.reload_config`。`gtx server --help` に載せる。出力・終了 code・設定に誤りがあるときの扱い（前の設定を保つか、警告の出し方）は herdr の実物に合わせる。
2. 再読込で反映される設定の範囲を herdr の実物と比べる。herdr で再読込すると反映されるのに gtx では反映されない設定（例: テーマ、通知、音、sidebar、キー割り当て、pane の枠、tab bar など。実物で確かめて決める）を反映するようにする。起動時だけ効く設定は herdr と同じく再起動が必要なままにし、その一覧を RESULT.md に書く。
3. 接続中の画面（複数の画面がつながっているときは全部）に、再読込の結果がすぐ反映されること。メニューと `prefix+shift+r` の再読込も同じ範囲を反映すること。

## 2. 守ること

- 変えてはいけないもの（変えたいときは実装前に親へ報告して止まる）: go.mod、CLI 名 `gtx server reload-config`、API 名 `server.reload_config`、既存の設定ファイルの形と既存の API・event・CLI・保存マシン接続・handoff・判断待ち・見張りの挙動。1ファイル 800 行まで。計画1章に無い機能を作らない。
- 反映できない設定を反映したことにしない。未対応は未対応として RESULT.md の未達に書く。
- 店長の本物の設定・state・稼働中の herdr と gtx に触らない。確かめは一時の HOME と名前付き session で行い、自分で起動したサーバーは確かめのたびに止める。
- 普段の動きの性能を悪くしない（性能は親が最後に測る）。go build はメインの Mac で可。テスト・race・ptyrun は mini。

## 3. 進め方と確認

- 区切り: 実物の観察メモ → CLI と API → 反映範囲をそろえる。区切りごとに mini で gofmt・go build・go vet・go test を通して積み上げコミット。最後に race。
- `.artifacts/reload-config/RESULT.md` に、上の各項目ごとの表（条件・確かめたテスト名か保存した出力・状態）と、herdr と gtx で再読込後に反映される設定の比較表を書く。確かめていない条件は未達と書く。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
