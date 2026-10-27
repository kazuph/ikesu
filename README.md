# ikesu — エージェントを池で飼う

いくつものターミナルに、ひとつの池を。ikesu はコーディングエージェントと一緒に働くためのターミナルワークスペースです。

![本実装のピクセルアートで泳ぐ錦鯉](docs/brand/koi.gif)

スペースとタブで作業を分け、サイドバーでエージェントの状態を確認できます。右の一覧の下に余白があれば、錦鯉の池が現れます。作業中は泳ぎ、待機中は底で休み、人待ちでは浮いて口を動かします。完了すると15秒間、金色に光ります。作業中のエージェントがいるスペースには、小さな鯉も泳ぎます。

エージェントが多いときは池の大きさに合わせて間引きます。人待ちの鯉を優先し、鯉ごとに帯を分けるので重なりません。詳しい状態は、これまでどおり一覧で確認できます。

kitty 画像に対応する端末では、最初に送った画像を再利用してなめらかに泳ぎます。それ以外は Unicode 16 の八分割文字で表示するため、その文字を含むフォントが必要です。アニメーションはクライアント側で動きます。

## インストール

```sh
brew install kazuph/tap/ikesu
ikesu
```

[仕様](SPEC.md) · [設定の説明](docs/ref/) · [ブランドサイトのソース](site/index.html)

## 鯉を非表示にする

設定ファイルの既存の `[ui]` セクションに追加します。省略すると表示します。

```toml
[ui]
koi = false
```

サイドバーが閉じているときや、池に表示できる鯉がいないときは、鯉のアニメーションによる再描画を止めます。エージェント一覧の場所は取りません。

---

# ikesu — Keep your agents in a pond

Many terminals. One quiet pond. ikesu is a terminal workspace for working alongside coding agents.

Organize work into spaces and tabs, and follow your agents in the sidebar. When there is room beneath the agent list, koi swim in a small pond. Working agents swim, idle agents rest near the bottom, and blocked agents surface and open their mouths. Completed agents glow gold for 15 seconds. A small koi also swims beside each space that contains a working agent.

A crowded team is represented by fewer fish, sized to the available pond. Blocked agents have priority. Separate lanes prevent fish from overlapping; the agent list keeps its detailed status display.

On terminals supported by the existing kitty image capability check, ikesu uploads the artwork once and moves the cached images. Other terminals use Unicode 16 octant characters and need a font that supports them. The animation runs in the client.

## Install

```sh
brew install kazuph/tap/ikesu
ikesu
```

## Turn off the koi

Add this to the existing `[ui]` section of your configuration file. Koi are enabled when the setting is omitted.

```toml
[ui]
koi = false
```

The koi animation stops requesting redraws when its sidebars are hidden or no fish can be shown. The pond uses spare rows and never takes space away from the agent list.

[Specification](SPEC.md) · [Configuration reference](docs/ref/) · [Brand site source](site/index.html)

The pixel-art animation and the site's embedded sprite atlas are exported from the terminal renderer with `KOI_BRAND_EXPORT="$PWD/docs/brand" go test ./internal/render -run TestExportKoiBrandAssets`. The site is a single self-contained HTML file with a custom WebGL2 pond, light reflections, caustics, refraction, touch ripples, and Japanese/English switching. Koi keep their fixed pixel palette and nearest-neighbor sampling. Without WebGL2, the same artwork appears in 2D. Reduced motion and hidden tabs stop the animation.
