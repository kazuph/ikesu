# ikesu モバイル接続の約束（中継＋端から端の暗号）

この文書は、Android アプリと各 PC の bridge が、ユーザー自身の Cloudflare 上の中継を通して話すときの約束を決める。中継は暗号文を渡すだけで、鍵を持たない。PC にポートは開けない。

## 1. 登場するもの

| 名前 | どこにあるか | 中身 |
|---|---|---|
| 中継（relay） | `apps/relay/`（Cloudflare Worker と Durable Object） | 部屋ごとに1つの Durable Object。bridge 1本とアプリ最大4本をつなぐ |
| 中継の鍵（relay key） | Worker の secret `RELAY_KEY` と、各 bridge の `<設定フォルダ>/relay.json` | 32 バイト。接続のタグを作るだけ。端から端の鍵とは別 |
| bridge の静的な鍵 | `<設定フォルダ>/noise-key.json`（0600） | X25519 の鍵の組 |
| 部屋 ID（room） | bridge の `relay.json` | 32 バイトの乱数。base64url（43文字）で書く。資格としては扱わない |
| 許可リスト | `<設定フォルダ>/devices.json`（0600） | 登録した端末ごとに、端末 ID・Noise の公開鍵・操作用の公開鍵・名前・権限・登録日時 |
| アプリの通知用の鍵 | Android（認証なしの Keystore の鍵で包んで保存） | X25519 の鍵の組。Noise の静的な鍵 |
| アプリの操作用の鍵 | Android の Keystore の中だけ | ECDSA P-256。生体認証（BIOMETRIC_STRONG）が通ったときだけ署名できる。新しい生体の登録で無効になる |

`<設定フォルダ>` は今の bridge と同じ（`IKESU_PLUGIN_CONFIG_DIR`、既定 `~/.config/ikesu/plugins/config/kazuph.android`）。

## 2. 中継

### 2.1 エンドポイント

すべて WebSocket で、`Authorization` ヘッダーに `IKESU-RELAY <小文字16進のタグ>` を付ける。タグは `HMAC-SHA256(relay key, ラベル ‖ 中身)` で、Worker が定数時間で照合する。合わない接続は、Durable Object を作らずに 401 で断る。

| エンドポイント | 誰が使う | タグの中身 |
|---|---|---|
| `GET /v1/bridge/<room>` | bridge | `"bridge" ‖ room の32バイト` |
| `GET /v1/app/<room>/<device>` | 登録済みのアプリ | `"app" ‖ room ‖ device の16バイト` |
| `GET /v1/pair/<room>/<pairing>/<exp>` | ペアリング中のアプリ | `"pair" ‖ room ‖ pairing の16バイト ‖ exp（Unix 秒、8バイトのビッグエンディアン）`。exp を過ぎたら断る |

`<room>`・`<device>`・`<pairing>` は正規の base64url（パディングなし）。長さが合わないものや、同じバイト列でも正規と異なる表記は 400 で断る。

### 2.2 中継と bridge の間の枠

bridge の WebSocket は、複数のアプリの接続を1本で運ぶ。中身はバイナリのメッセージで、先頭の1バイトが種類、次の16バイトが接続 ID（中継が作る乱数）。

| 種類 | 向き | 続き |
|---|---|---|
| `0x01` open | 中継 → bridge | 役割1バイト（`0x01` app、`0x02` pair）、続けて device または pairing の16バイト |
| `0x02` data | 両方向 | アプリとの間の1通（Noise のメッセージ） |
| `0x03` close | 両方向 | なし。中継はアプリの接続を切る。bridge はその接続の状態を捨てる |
| `0x04` allow | bridge → 中継 | 接続 ID 欄は16バイトすべて0。続けて許可する端末 ID の全件（1件16バイト、0〜4095件） |

アプリの WebSocket の1通は、そのまま Noise のメッセージ1つ。

bridge は接続のたびに、その時点の許可一覧を全件送る。一覧を受けた部屋は、一覧にない端末の app 接続を 403 で断り、すでにつながっていれば 1008 で切る。空の一覧はすべての app を断る。まだ一覧を受けていない部屋は、接続タグが正しければ従来どおりどの端末の app も受け付ける。pair 接続には一覧を適用しない。一覧は部屋に保存され、bridge が切れても残る。

中継は置き換えられた古い bridge の枠を無視する。close は17バイトの枠だけ処理し、18バイト以上は無視する。中継は不正な allow を受けると、保存済み一覧を保持して bridge を 1002 で切る。

### 2.3 中継の上限

1つの部屋に bridge 1本、アプリ4本まで。新しい bridge の接続は古いものを置き換える。5本目のアプリは断る。1通は 65,535 バイトまで（枠の頭は除く）。超えたら、その接続を切る。bridge がいない部屋のアプリの接続は、30秒で切る。アプリの接続ごとに、毎秒200通・毎秒4 MB まで。超えたら切る。接続ごとの役割と ID は `serializeAttachment` に置く（Hibernation で消えないように）。

## 3. 端から端の暗号

方式: `Noise_IK_25519_ChaChaPoly_BLAKE2b`。アプリが始める側（initiator）で、bridge の静的な公開鍵を知っている。prologue: `"ikesu-mobile/1"` の14バイト ‖ room の32バイト。1通目（アプリ → bridge）の中身は空。2通目（bridge → アプリ）の中身も空。握手が終わるまで、bridge は何も実行しない。握手ハッシュ `h`（64バイト。BLAKE2b の出力）を、ペアリングの証明・6桁・操作の署名に使う。握手の後の1通は、平文が 60,000 バイトまで。復号に1回でも失敗したら、その接続を切る。送った通数が 2^31 に届くか、つないでから24時間たったら、つなぎ直す。握手の失敗が、同じ端末 ID（またはペアリング ID）で毎分10回を超えたら、bridge はその ID の open を握手せずに閉じる。ほかの ID の open は閉じない。

### 3.1 平文の形

平文の先頭1バイトで形を分ける。

| 先頭 | 意味 |
|---|---|
| `0x00` | 残り全部が UTF-8 の JSON 1つ |
| `0x01` | 分割の一部。続けて、メッセージ ID（4バイト）・最後なら1／途中なら0（1バイト）・中身 |

分割を受けた側は、同じメッセージ ID の中身を順につなぎ、最後の一部で JSON として読む。1つのメッセージは 128 MB まで。
送る側は、最後でない一部を必ず満杯（中身 59,994 バイト）で送る。満杯でない途中の一部を受けたら、受けた側は接続を切る。
アプリから bridge への分割は、顔・指紋で開いた後（操作できる状態）だけ送ってよい。それより前の分割を受けたら、bridge は接続を切る。

### 3.2 ペアリング

1. PC でユーザーが `node apps/android/bridge/main.mjs pair --name <PC の名前>` を実行する。bridge は pairing ID（16バイト）・ペアリングの秘密（32バイト）・期限（5分後）を作り、端末に QR を出す。
2. QR の中身は JSON 1行。`{"v":1,"relay":"wss://…","room":"…","bridge":"<公開鍵の base64url>","pairing":"…","secret":"…","exp":<Unix 秒>,"tag":"<pair のエンドポイントのタグ>","name":"<PC の名前>"}`
3. アプリはアプリ内のカメラだけで QR を読む。PC の名前と bridge の鍵の指紋（公開鍵の SHA-256 の先頭8バイトを16進で）を見せ、ユーザーが確かめてから進む。
4. アプリは pair のエンドポイントへつなぎ、握手する。握手の後の最初のメッセージは次の JSON。
   `{"type":"pair","proof":"<HMAC-SHA256(secret, "pair" ‖ h) の base64url>","name":"<端末の名前>","operate":"<操作用の公開鍵の SubjectPublicKeyInfo（DER）の base64url>"}`
5. bridge は proof を定数時間で照合する。合わなければ切る。5回外れたら、その QR を無効にする。
6. 両方が6桁を作る。`HMAC-SHA256(secret, "sas" ‖ h)` の先頭4バイトをビッグエンディアンの整数として、1,000,000 で割った余りを、ゼロ埋めの6桁にする。アプリは画面に出し、ユーザーが PC の端末に打つ。
7. 打った6桁と合う候補（1つの QR につき4本まで）だけを許可リストに入れる。bridge は端末 ID（16バイトの乱数）を作り、`{"type":"paired","device":"…","tag":"<app のエンドポイントのタグ>","name":"<PC の名前>","permissions":["notify","answer","screen","input"]}` を送る。QR の秘密はここで捨てる。期限を過ぎたときも捨てる。
8. 権限は、PC の端末でペアリングのときに選ぶ。既定は全部。

### 3.3 ふだんの接続

アプリは app のエンドポイントへつなぎ、握手する。bridge は、1通目で分かったアプリの公開鍵が、その device の許可リストの鍵と一致しなければ、返事をせずに切る。握手の後、この接続でできるのは通知だけ（3.4）。回答・あとで・画面・キー入力の前に、アプリは操作の証明を送る。
  `{"type":"operate","hashes":["<h の base64url>",…],"sig":"<署名する文の ECDSA P-256／SHA-256 の署名（DER）の base64url>"}`
  署名する文は `"ikesu-operate/1"` の15バイト ‖ hashes の数（1バイト、1〜8）‖ hashes の各64バイトを並べたもの。アプリは顔・指紋を1回通すだけで、つながっている全部の PC の h を1つの署名に入れる（操作用の鍵は1回の認証で1回だけ署名できるため）。bridge は、自分の接続の h が hashes に入っていることと、署名を確かめる。
  bridge は、登録した操作用の公開鍵で確かめる。通ったら、その接続を操作できる状態にし、`{"type":"operate_ok","until":<Unix ミリ秒>}` を返す。
操作できる状態は、接続が切れる・10分間操作が無い・アプリが `{"type":"operate_end"}` を送る、のどれかで終わる。終わったら bridge は `{"type":"operate_expired"}` を送り、その接続を閉じる。アプリはつなぎ直し、新しい握手ハッシュに署名し直す。同じ署名を送り直して開き直すことはできない。

### 3.4 送ってよいもの

| 状態 | bridge → アプリ | アプリ → bridge |
|---|---|---|
| 握手の後（通知だけ） | `{"type":"notice","decisions":[{"id","title","origin","created_unix_ms","notify"}…]}`（`notify` が false のものは鳴らさない。PC で見ている・「あとで」にしたもの） | `shown`・`operate` |
| 操作できる状態 | `snapshot`・`video_chunk`・`video_end`・`answer_result`・`error`・`beat`・`operate_expired`、画面（4章） | `answer`・`later`・`shown`・`operate_end`、画面（4章） |

メッセージの形（bridge は、ここに無い項目を混ぜた `answer`・`shown` を断る。検査は `bridge/messages.mjs`）:

- `answer`: `{"type":"answer","id","options":[選んだ選択肢の id…],"text","images":[base64…],"sequence"}`。`beat` を足してよい。画像は4枚まで、1枚20MBまで。`sequence` は送信番号で、同じ番号の送り直しは1回だけ保存する。マシン・パス・コマンド・回答者はアプリから指定できない。
- `later`: `{"type":"later","id"}`。`shown`: `{"type":"shown","ids":[…]}`（200件まで）。`operate_end`: `{"type":"operate_end"}`。
- `snapshot`: `{"type":"snapshot","decisions":[…]}`。各判断待ちは、題名・本文・選択肢・画像・カード・動画の key・`notify` など、PC の判断待ちパネルと同じ中身を持つ。
- `answer_result`: `{"type":"answer_result","sequence","status"}`。`status` は `answered`・`answered_elsewhere`・`cancelled`・`expired`・`failed`・`unknown`。`failed`・`unknown` には `message` が付く。
- `error`: `{"type":"error","message"}`。回答への返事なら `sequence` が付く。
- `video_chunk`: `{"type":"video_chunk","id","key","index","data"}`（`data` は 32KB ずつの base64、`index` は0から順）。`video_end`: `{"type":"video_end","id","key"}`。

操作できない状態で操作のメッセージが来たら、`{"type":"error","message":"operate required"}` を返して実行しない。

### 3.5 心拍とキー入力の鮮度

bridge は操作できる接続へ5秒ごとに `{"type":"beat","n":<通し番号>}` を送る。アプリのキー入力と画面の操作には、最後に受けた `n` を `beat` として付ける。bridge は、今の番号より2つ以上古い `beat` の入力を捨てる（10秒より古い）。分けて届いたメッセージは、届き終えた時点の番号で比べる。例外は `screen_file` と `answer` で、そのメッセージの最初の1通が届いた時点の番号で比べる（大きなファイルや画像は届き終えるまでに番号が進むため）。つなぎ直した後、アプリは送れていないキー入力を送り直さずに捨てる。

接続の数と権限: bridge は同時に16本までの接続を受ける。中継が同じ接続 ID で開き直したら、前の接続（開いていた画面を含む）を閉じてから受ける。devices.json で端末の鍵か権限が変わったら、その端末の接続を切る（アプリはつなぎ直し、新しい権限で顔・指紋の署名をし直す）。画面のサーバーの確認は接続ごとに1本ずつで、確認中に届いた `screen_open` は最後の大きさだけを使う。アプリは 65,535 バイトを超える1通を受けずに接続を閉じる。

## 4. 画面（ペインの画面を見て操作する）

アプリ → bridge: `{"type":"screen_open","cols":<20〜300>,"rows":<10〜200>}`。bridge は `ikesu` の画面をスマホ用の大きさで PTY の中に開き、その出力を送る。アプリの文字列を `ikesu` の引数に入れない。bridge → アプリ: `{"type":"screen_data","screen":<画面 ID>,"seq":<通し番号>,"data":"<PTY の出力の base64>"}`。30ms ごとにまとめて送る。アプリ → bridge: `{"type":"screen_input","screen":<画面 ID>,"beat":<n>,"data":"<キー入力の base64>"}`、`{"type":"screen_resize","screen":…,"cols":…,"rows":…}`、`{"type":"screen_close","screen":…}`。端末部品が自動で返す応答（端末の問い合わせへの返事、OSC 52 のクリップボード）は、アプリが送らない。アプリが裏へ回ったら `screen_close` を送る。操作できる状態が終わったら、bridge が画面を閉じる。

bridge は開いたら `{"type":"screen_opened","screen","cols","rows"}`、閉じたら `{"type":"screen_closed","screen"}` を返す。見るには `screen`、打つには `input` の権限と新しい心拍が要る。bridge は ikesu run のジョブとして動くので、画面の ikesu には持ち主の設定の写しに `[experimental] allow_nested = true` と `[ui] native_menus = true` を足したものを渡す（写しは閉じたら消す）。狭い画面を広く使うため、写しには `[ui] sidebar_start_collapsed = true` と `sidebar_collapsed_mode = "hidden"` も足し、`[keys] toggle_sidebar` に持ち主の割り当て（無ければ `prefix+b`）と `f12` を並べる。アプリの ≡ は F12（`ESC [ 2 4 ~`）を送って、スペース一覧を出し入れする。`native_menus` の ikesu は、サイドバーの開閉や幅を共有の保存ファイルに書かず（読みもしない）、毎回この設定から始める。ikesu のサーバーが動いていないときは開かない。

メニューとダイアログ: `[ui] native_menus = true` の ikesu の画面は、開いているメニューかダイアログが変わるたびに `ESC ] 7767 ; ikesu-ui ; <JSON の base64> BEL` を出力に足す（既定は false で、Mac の画面には出ない）。JSON は `{"kind":"menu","title","sel","items":[{"label","detail","status","disabled","separator"}],"filterable"}`、`{"kind":"dialog","title","detail","lines","confirm","cancel","input","continue_only"}`（`input` は文字を受けるダイアログだけ。それ以外は null）、閉じたら `{"kind":"none"}`。アプリはこの合図を端末部品に渡さず、Android の一覧とダイアログで出す。答えはふつうのキーで返す: 一覧は ↑/↓ を1つ送るたびに次の合図で位置を確かめ、目的の行で Enter。入力は Ctrl+U・文字・Enter。取り消しは Esc。

長押し: 同じ合図に `{"kind":"panes","cols","rows","spaces","panes":[{"id","x","y","w","h"}]}` を足す。画面のセル座標は左上を 0 とし、枠線を除いたペイン本文の範囲だけを送る。合図は画面の大きさや範囲が変わったときに送り、メニューやダイアログが画面を覆う間は空の範囲を送る。アプリは受け取った画面サイズと現在のサイズが一致する場合だけ範囲を使い、長押ししたセルが属するペインの本文・リンクを読む。bridge は画面のバイト列を中継する。 `spaces` はスマホでスペース一覧が開いている間は true。音量＋長押しで F12 を送り、一覧を開閉する。一覧が閉じている間は＋短押しで録音を開始・終了し、一覧では＋／−短押しで F9／F10 を送って前後のスペースへ移る。F9／F10 はスマホの一覧操作としてだけ扱う。

アプリのキー: ⌘ は kitty のキーの形（`ESC [ 文字の番号 ; 1+修飾 u`、⌘ は 8）で送る。タップは SGR のマウスのクリック（押して離す）で送る。続けて2回タップしたら、2回目も1回目と同じマスへ送る（ダブルクリック）。上下に指を動かすと、3行ごとに SGR のホイール（上は 64、下は 65）を送る。貼り付けは bracketed paste で送る。

ファイル: アプリ → bridge `{"type":"screen_file","screen","beat","name","data":"<base64>"}`（20 MB まで、`input` の権限と、送り始めの時点で新しい心拍が要る）。bridge は `~/Downloads/ikesu-phone/<日時>-<名前>` に 0600 で保存し（名前からフォルダと制御文字を消し、上書きしない）、そのパスをシェルの1語にクォートし、貼り付けの形（bracketed paste）で画面に打つ（Enter は押さない）。返事は `{"type":"screen_file_saved","screen","path"}`。

## 5. 取り消しと入れ替え

`node apps/android/bridge/main.mjs devices` で登録済みの端末を出し、`… revoke <device>` で外す。PC 側の bridge は手動登録と revoke をファイル監視または定期確認（間隔は `pollMs`、既定1秒）で読み込み、再起動なしに外した端末の接続を切る。

中継は更新した一覧を受け取って保存した時点から、外した端末の新しい app 接続を断る。bridge が中継につながっていない間は保存済みの一覧が残り、次に接続したときに bridge が全件を送り直す。許可一覧に対応する前の bridge に戻した部屋でも、中継には保存済みの一覧が残るため、新しく登録した端末が 403 で断られることがある。一覧に対応した bridge を再接続すると、現在の一覧が反映される。

端末を無くして PC に触れないときは、Cloudflare で `RELAY_KEY` を入れ替える。すべての端末がつながらなくなる。bridge の鍵を入れ替えたら、すべての端末をペアリングし直す。
