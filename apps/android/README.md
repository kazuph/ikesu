# ikesu for Android

Android から ikesu の判断待ちを確認し、回答します。Mac 上の bridge は登録済みの判断待ちを読み、Tailscale の IPv4 アドレスで待ち受けます。Android アプリのパッケージ ID は `kazuph.ikesu`、プラグイン ID は `kazuph.android` です。

## 導入

macOS に Node.js、Xcode と Tailscale が必要です。リポジトリのルートから依存物を導入し、bridge をプラグインとしてリンクします。

```sh
cd apps/android/bridge
npm ci
ikesu plugin link "$PWD"
```

判断待ちが作成されると bridge が起動します。手動では `ikesu plugin action invoke start --plugin kazuph.android` を使います。設定フォルダの `config.json` で `enabled`、`port`（既定 8776）、`tailscale_path`、`later_minutes`（既定 10）を指定できます。Tailscale のアドレスを取得できないとき、bridge は待ち受けません。

Android SDK を用意し、`apps/android` で `./gradlew assembleDebug` を実行します。USB で対象端末を接続し、端末を確認してから次を実行します。

```sh
adb devices -l
adb -s <端末ID> install -r app/build/outputs/apk/debug/app-debug.apk
```

初回の bridge 起動で、プラグインの設定フォルダに `token` が作られます。ファイルは所有者だけが読める権限です。アプリの接続設定に `ws://<MacのTailscale IPv4>:8776/` と合言葉を入力します。端末の通知許可も必要です。ADB の intent は `am start -n kazuph.ikesu/.decisions.MainActivity --es url <接続先> --es token <合言葉>` に対応します。合言葉がシェル履歴や作業ログへ残らない方法で渡してください。端末側では Android Keystore の鍵で暗号化します。

端末を紛失したときや合言葉を交換するときは `ikesu plugin action invoke rotate-token --plugin kazuph.android` を実行します。古い接続は切断されます。新しいファイルの合言葉を端末へ設定し直してください。

## 通信と保存

bridge は認証前に判断待ちを送りません。ブラウザの Origin を伴う接続を拒否します。Android が送れるのは質問の識別子、選択肢、コメント、画像、送信番号です。マシン、パス、コマンド、回答者の指定は bridge が決めます。

添付は4枚まで、1枚20MBまでです。画像を検査し、判断待ちの出どころへ保存してから、そのパスをコメントに追加します。保存失敗時は回答を送信しません。取消・期限切れでは添付を消し、回答済みの添付は7日後に消します。画像やURLの表示取得に失敗しても回答できます。

## 試験

```sh
cd apps/android/bridge && npm test
cd ../ && ./gradlew test
```

bridge の実サーバー試験は一時 HOME と隔離サーバーを使います。画像検査にはインストール済みの Mac パネルを使います。別の実パネルを使う場合は `ANDROID_TEST_NATIVE_BINARY` に実行ファイルの絶対パスを指定します。実機の検証も名前付き隔離 session と一時 HOME を使い、普段の判断待ちには接続しません。Pixel の確認では、対象端末のシリアルを明示します。
