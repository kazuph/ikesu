# main の移植監査（2026-10-08、親 Claude）

依頼: 店長「仕様からの実装は OK、実装を port するだけは NG」（AGENTS.md に明文化）。main（8b21f45）に herdr fork（AGPL-3.0）の実装の移し替えが無いかを、親が gtx と herdr の両方のソースを読んで調べた。

調べ方: main の全 Go ファイルについて、関数名を snake_case にして herdr の全 `fn` 名と突き合わせ、一致の多いファイルは中身（処理の分け方・SQL・定数）を並べて比べた。API のメソッド名や CLI のコマンド名から決まる名前（`pane_list` など）は、外から見える約束をそろえたものなので移植の根拠にしない。

## 判定

| 区分 | ファイル | 根拠 |
|---|---|---|
| **移植（書き直す）** | `internal/dispatch/store.go`（978行。メール・ジョブの保存） | SQL 文が herdr `src/dispatch.rs` と1文字単位で同じ（例 `MarkCommandCancelling` の `WHERE kind='command' AND (external_id=?1 OR log_path LIKE '%' \|\| ?1 \|\| '.log') AND status='running'`）。関数44個中22個が herdr と同名（`mark_command_cancelling`・`insert_message`・`job_log_path` など） |
| **移植（書き直す）** | `internal/detect/identify.go`（357行。ペインで動く agent の判定） | 関数19個中16個が herdr `src/detect/mod.rs` と同名（`normalized_process_name`・`wrapped_agent_name_from_runtime_argv`・`windows_cmd_arg_agent_name`・`powershell_arg_agent_name`・`flag_matches`・`option_takes_value` など）。最初の計画 `docs/spec/PLAN.md` が herdr の `mod.rs` を参照させていた |
| **移植（書き直す）** | `internal/detect/region.go`（288行。検出ルールが見る画面の範囲の切り出し） | 関数19個中16個が herdr `src/detect/manifest.rs` と同名で、処理の分け方も同じ（例 `after_last_horizontal_rule`）。範囲の名前自体は検出ルールのデータ（TOML）が使う名前なので変えないが、切り出し方はソースから写している |
| **移植（書き直す）** | `internal/detect/manifest.go` の作業表示の判定部分（`codex_activity_fingerprint`・`claude_activity_fingerprint`・`codex_live_region`） | herdr `manifest.rs` と同名の関数。SPEC G3「15秒失効」の節に文章の仕様はあるが、実装は名前ごと対応している |
| **移植の疑い（書き直す）** | `internal/cli/comms.go`・`internal/cli/run.go`・`internal/cli/cli.go` の補助関数（`print_*_help`・`resolve_agent_name`・`shell_command_from_args`・`default_run_label`・`run_spawn_next_hint`・`parse_u32_flag` など） | herdr の CLI 実装の内部関数と同名。help の文面は herdr の help（`docs/ref/` に保存した利用者向けの約束）と同じでよいが、組み立てる関数は書き直す |
| 低い（書き直さない） | `internal/layout/layout.go`（分割の木） | 名前の一部は同じだが中身は別物（例 `splitRect` は丸め方も向きの名前も違う）。分割の木という一般的な作り方 |
| 対象外 | `internal/detect/manifests/*.toml`（22ファイル） | herdr の検出ルールと完全に同じファイル。最初の計画 `docs/spec/PLAN.md` に「店長が MIT での利用を了承済み」として意図的にコピーしたもの。コードではなくデータ。扱いを変える場合は店長が判断する |
| 対象外 | API・CLI の名前と項目名（`pane_list`・`tab_create` などのハンドラ名） | 外から見える約束をそろえたもの |

## 書き直しの進め方（案）

- 書き直す実装役は herdr のソースも、書き直す前の gtx のファイルも読まない。
- 親（herdr のソースを読んだ側）は、挙動を「入力と期待する結果」の表として仕様に書き出し、実装役にはその仕様だけを渡す（いわゆるクリーンルーム。AGENTS.md の決まりどおり）。
  - 例: 範囲 `after_last_horizontal_rule` は「画面のうち、最後の横罫線の行より下の行」、`prompt_box_body` は「…」のように、範囲ごとに定義と例を書く。
- 既存のテストのうち、外から見た挙動（入力と結果）だけを確かめるものは合否の判定に使う。herdr のテストを写したものがあれば、それも書き直す。
- 性能は SPEC.md P1 の相対検査を通す。
