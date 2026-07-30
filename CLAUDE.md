# xeet fork — Claude Code 向けコンテキスト

このリポは melqtx/xeet の fork（origin: github.com/rrrrnmtsu/xeet）。
改修は必ずこのリポ（/Users/remma/dev/xeet）で行う。他リポのセッションから
xeet を触らない（コンテキストがここにあるため）。

## ブランチ・出荷状況

作業ブランチ: `feat/lists-multi-column-multi-account`（main への PR 先は main）

upstream には無い fork 独自機能（全部このブランチにコミット済み）:

| 機能 | コミット | 入口 |
|---|---|---|
| 通知カラム | 1b36073 | NotificationsTimeline、`--columns notifications,...` |
| リポスト/解除 | d1cb8c6 | TUI `t` キー（トグル・楽観更新） |
| 引用ポスト | d7b364e | TUI `Q` キー、`xeet post --quote <id>` |
| プロフィールTL | 2157677 | TUI `u` キー（フォーカスカラムのフィード切替） |
| ブックマーク登録/解除 | 3880d89 | TUI `B` キー |
| URLからポスト取得 | a9596b1 | `xeet fetch <url-or-id>`（JSON 既定、`--text`/`--replies`/`--account`） |

## エージェントからポストを読む

セッションで X のポスト URL を共有されたら:

```bash
xeet fetch <url> | jq .text        # 本文
xeet fetch <url> | jq .article     # X長文記事（あれば title/text）
xeet fetch <url> --replies 5       # 返信込み
```

見つからない・権限がない場合は終了コード 1。WebFetch は X の JS
レンダリングで詰むので使わない。

## 鉄則（壊すと静かに死ぬ所）

1. **QID 捏造禁止**。空フォールバック + eager `discoverOperation` +
   env override（`XEET_<OP>_QID`）+ config 永続化の既存経路だけ使う
2. **config QID 配管は1オペレーション7点**: Config struct と fileConfig
   struct の**両方**（片方だけだと Save() が静かに落とす）+
   fileConfigFor + configFromFile + SaveQueryIDs + web.go の
   operationQIDs 初期化 + ApplyRefreshedQueryIDs case
3. **Transaction ID が要るオペレーション**: SearchTimeline、
   CreateRetweet/DeleteRetweet、CreateBookmark/DeleteBookmark
   （ヘッダ無しだと bodyless 404。likes・UserByScreenName・UserTweets は不要）
4. **冪等性でリトライを決める**: likes/CreateBookmark = 冪等 →
   transient リトライあり。CreateRetweet/DeleteBookmark = 非冪等 →
   単発（リプレイすると GraphQL エラー。TUI は楽観状態をロールバック）
5. **GraphQL 400/422 が来たら**: エラーボディが指名した変数だけ足す。
   推測で変数を増やさない。新規依存も禁止
6. **fieldToggles は共有マップを直接いじらない**。読み取り拡張が要る
   エンドポイントはコピーに opt-in させる（例: tweetDetailFieldToggles の
   withArticlePlainText。pkg/api/conversation.go の Why not コメント参照）
7. **共有パーサに載せる**: ポストの legacy パースは parseTimelineItem
   （pkg/api/timeline.go）に集約。エンドポイント個別にパースを書かない

## コード/テスト/コミット/コメントの軸

- コード本体 = How、テスト = What、コミットログ = Why、
  コードコメント = Why not（見送った代替案）のみ

## live テスト（全て env ゲート）

`XEET_LIVE_TIMELINE` / `XEET_LIVE_CONVERSATION` / `XEET_LIVE_SEARCH` /
`XEET_LIVE_BOOKMARKS` / `XEET_LIVE_BOOKMARK`（+`_TWEET_ID`）/
`XEET_LIVE_RETWEET` / `XEET_LIVE_QUOTE` / `XEET_LIVE_PROFILE` /
`XEET_LIVE_LISTS` / `XEET_LIVE_NOTIFICATIONS` / `XEET_LIVE_ARTICLE`（+`_TWEET_ID`）/
`XEET_LIVE_VERIFY` / `XEET_LIVE_DISCOVER`

新しい GraphQL オペレーションを足したら unit + live 両方を書くこと。
live でしか分からん挙動（transaction header 要否、非冪等性など）が必ずある。

## 検証コマンド

```bash
go build ./... && go vet ./... && go test -race ./... -count=1
```

TUI のヘルプ表示は 15 行端末で center-crop される。short 版は feed 8 行・
thread 6 行予算、各行 28 文字以内（view.go）。

## 未着手の残件

- 引用ライブテストの残骸ポスト **2082616681940341043** を X 上で手動削除
  （delete API はスコープ外。ユーザー作業）

## 参考

- `SPEC-lists-columns-accounts.md` — マルチカラム/マルチアカウントの設計仕様
- `PROGRESS.md` — 作業ログ
