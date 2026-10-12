# 動いている pane を保ったままサーバーを交代する

`gtx server live-handoff` は、同じ名前付き session の socket を引き継ぎ、pane の PTY と端末状態を新しいサーバーへ渡す。pane 内の shell や agent を起動し直さず、接続中の画面は自動で再接続する。交代中の Local 入力は、新しい画面を描画した後に送る。

省略時の移行先は、現在のサーバーが動いている実行ファイル。`--import-exe PATH` で移行先を指定でき、`--expected-protocol NUMBER` と `--expected-version VERSION` で移行先の確認条件を指定できる。移行先が起動できない、応答しない、確認条件が一致しないときは `handoff_failed` を返し、旧サーバーと pane を残す。API は `server.live_handoff`、引数は `import_exe`、`expected_protocol`、`expected_version`。`ping` の `capabilities.live_handoff` は対応有無を示す。

workspace・tab・pane の構成と番号、両方の端末画面と scrollback、cursor、mouse 報告形式、bracketed paste、kitty keyboard の状態、agent の報告と session 情報、plugin の登録と pane 所有、判断待ちと期限、見張りの印を引き継ぐ。新しいサーバーは起動時の保存 session の再生・agent の再起動を行わない。

## kitty 画像

`[experimental] kitty_graphics = true` を設定すると、pane は XTVERSION に `ghostty (gtx <版>)` と答える。無効時は `libghostty` と答え、画像問い合わせに `ENOTSUP` を返す。

対応する画像手順は、direct data（`t=d`）の RGB（`f=24`）、RGBA（`f=32`）、PNG（`f=100`）、zlib 圧縮、分割転送、問い合わせ（`a=q`）、転送（`a=t`）、転送と配置（`a=T`）、保存済み画像の配置（`a=p`）、全表示配置または画像 ID を対象とする削除（`a=d,d=a/A/i/I`）。配置の `c/r`、`p`、`z`、cursor 移動の `C`、応答抑制の `q` に対応する。`c/r` の省略・片方だけの指定には、外側の端末から得たセルの pixel 寸法が必要で、寸法が不明なら `ENOTSUP` を返す。

画像は pane 内のセルに配置され、scroll・分割・移動・サイズ変更に従って外側の端末に再配置される。tab・workspace 切替、pane close、画面の終了では、この画面が出力した画像を削除する。pane の境界を越える配置は全体を隠し、完全に収まると再表示する。部分描画には対応していない。

file・temporary file・shared memory の転送、画像番号（`I`）、pixel の切り出しとセル内の pixel offset、Unicode placeholder、相対配置、animation、位置を指定する削除は未対応で、`ENOTSUP` を返す。未対応の操作を成功として受け付けない。popup 以外の dialog・menu などが重なる間は画像を隠す。

動作証拠と未検証の条件は、この worktree の `.artifacts/handoff/RESULT.md` に記録する。保存マシン接続の endpoint generation と既存の入力・描画確認の契約は変更しない。
