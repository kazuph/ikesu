# ikesu-relay

ikesu の Android アプリと、各 PC の bridge をつなぐ中継です。Cloudflare Worker と Durable Object で動きます。約束は `apps/android/PROTOCOL.md` の「2. 中継」です。

中継は Noise の鍵を持たずに暗号文を渡します。HMAC の接続タグを照合し、部屋ごとに端末の許可一覧と接続の上限を適用します。

## 仕組み

- `GET /v1/bridge/<room>`、`GET /v1/app/<room>/<device>`、`GET /v1/pair/<room>/<pairing>/<exp>` の3つ。すべて WebSocket で、`Authorization: IKESU-RELAY <16進のタグ>` を付けます。
- Worker が `RELAY_KEY` で HMAC-SHA256 を定数時間で照合します。合わなければ Durable Object を作らずに 401、ID の長さや base64url の正規表記が不正なら 400、pair の期限切れは 401 です。
- 部屋（room）ごとに Durable Object が1つ。bridge 1本（新しいものが古いものを置き換える）とアプリ4本まで。Hibernation の WebSocket API を使い、役割と ID は `serializeAttachment` に置きます。
- bridge から受けた許可一覧を部屋に保存します。一覧外の app 接続は 403 で断り、接続中なら 1008 で切ります。一覧を受けていない部屋は従来どおり有効なタグを持つ app を受け付けます。pair 接続には一覧を適用しません。
- 1通は 65,535 バイトまで。アプリごとに毎秒200通・毎秒4MBまで。bridge がいない部屋のアプリは 30 秒（`APP_WAIT_MS`）で切ります。

## 試験

```sh
bun install
bun run test
```

workerd をローカルで動かして、27 件の試験を流します（`@cloudflare/vitest-plugin`）。

## ボタンでデプロイ（自分の Cloudflare アカウントへ）

[![Deploy to Cloudflare](https://deploy.workers.cloudflare.com/button)](https://deploy.workers.cloudflare.com/?url=https://github.com/kazuph/ikesu/tree/main/apps/relay)

中継は、使う人がそれぞれ自分の Cloudflare アカウントに置きます。中継の鍵（`RELAY_KEY`）は Worker 全体で1つで、持つ人はどの部屋にも PC として入れるため、他人の中継は使いません。

1. PC で鍵を作り、控えます。

   ```sh
   node -e 'console.log(require("crypto").randomBytes(32).toString("base64url"))'
   ```

2. 上のボタンを押します。Cloudflare にログインし、GitHub か GitLab に置くリポジトリの名前を決め、`RELAY_KEY` の欄に1の値を貼って、デプロイします。
3. デプロイが終わると `https://ikesu-relay.<サブドメイン>.workers.dev` が出ます。
4. PC で bridge に中継を設定します。`https://` を `wss://` に変え、1と同じ鍵を渡します。

   ```sh
   node apps/android/bridge/main.mjs relay-setup --url wss://ikesu-relay.<サブドメイン>.workers.dev --key <1の値>
   ```

5. スマホを登録します。`node apps/android/bridge/main.mjs pair --name <PC の名前>` の QR をアプリで読みます（`apps/android/README.md`）。

## デプロイ（コマンドで）

1. `npm install`
2. Cloudflare にログインします。`npx wrangler login`
3. `RELAY_KEY`（base64url の32バイト）を作って secret に入れます。

   ```sh
   node -e 'console.log(require("crypto").randomBytes(32).toString("base64url"))'
   npx wrangler secret put RELAY_KEY
   ```

   同じ値を bridge の relay.json にも入れます（`main.mjs relay-setup`）。アプリには入れません。アプリは端末ごとの入場タグだけを持ちます。
4. `npx wrangler deploy`
5. 出た `https://ikesu-relay.<サブドメイン>.workers.dev` を、`wss://` にして QR の `relay` に使います。

`RELAY_KEY` を入れ替えると、すべての端末がつながらなくなります（`PROTOCOL.md` の 5 章）。

## ローカルで動かす

`.dev.vars` に `RELAY_KEY=<base64url の32バイト>` を書いて `npx wrangler dev` を実行します。`.dev.vars` はコミットしません。
