# SPEC: X Lists + Multi-column + Multi-account for xeet

Status: implementation-ready. Target executor: Codex (no further clarification available — every decision is made below; where reality may diverge, §13 tells you exactly what to do instead of guessing).

Builds directly on the shipped Bookmarks/Search work (see `SPEC-bookmarks-search.md` and the seven commits on `feat/lists-columns-accounts`). Do NOT redo anything that spec covers: `FeedKind`, `setFeed`, `fetchTimelineOp`, `parseEntries`, `modeSearch`, `cmd/search.go`, `--bookmarks`, `b`/`/` keys are all live and verified against a real account.

---

## 🇯🇵 なんJまとめ（読む人向け）

ワイらの xeet フォークにデカい機能を3つ足すで:

1. **X Lists** — 自分のリストを feed として閲覧。`xeet lists` でピッカー、TUI 内 `L` キー、`--list <id>` フラグの3経路
2. **マルチカラム** — TweetDeck 風に N 本の feed を横並び。各カラム独立スクロール、スレッド/リプライ/検索/ズームは全画面オーバーレイのまま（再親化を回避してリファクタ半減）
3. **マルチアカウント** — 複数の X セッションを保存・切替。config を `version: 2` スキーマに移行、アカウントキーは **X user id**（cookie の hash やと rotate で別人扱いになるから NG）、keyring は user id suffix 化。カラムごとに別アカウントも可（最終 Phase）

フェーズ構成（1 phase = 1 commit が基本、順序は **Lists → columns → accounts** 固定）:

- **L1**: Lists API（`pkg/api/lists.go`: feed 取得 + リスト列挙）
- **L2**: `FeedList` + TUI ピッカー（`L` キー）
- **L3**: `cmd/lists.go` + `--list` フラグ + テスト
- **C1**: `column` struct 抽出（挙動不変の機械的リファクタ、msg に column id 付与）
- **C2**: N カラムレイアウト + focus 移動 + 幅不足時の自動縮退
- **C3**: 画像・preview のカラム対応（iTerm2/WezTerm は ANSI 強制）
- **C4**: `--columns` フラグ + config 永続化 + PTY テスト
- **A1**: config schema v2 + 移行 + `SaveQueryIDs` 競合修正 + `Erase` 分割
- **A2**: ブラウザ複数プロファイル収集（collapse 削除）+ auth picker にアカウント選択 phase
- **A3**: `xeet accounts` list/switch + whoami/doctor 対応
- **A4**: カラム別アカウント（stretch、A3 まで green なら着手）

規模感: **11 commits、実装 +2500〜3500行 / テスト +1500〜2500行**。Lists の GraphQL 変数だけ実セッション裏取りが必要（§13）。QID は絶対に捏造禁止、discover に任せるんや。C1 が最大の一発（26 フィールド移動）やが挙動不変やからテストが守ってくれる。A1 の移行だけはミスるとユーザーのセッション飛ぶから §8.2 の手順を一字一句守ること。

---

## 0. Ground rules (apply to every phase)

- Go **1.26.5 pinned** — do not touch the `go` directive. **No new dependencies.** Everything needed (bubbletea v1, bubbles v1, lipgloss v1, cobra, go-keyring, yaml.v3) is already a direct dep. If you add ANY dep anyway, you must update `vendorHash` in `default.nix` or the nix CI job fails. You almost certainly do not need one.
- Every phase must pass locally: `go build ./...`, `go vet ./...`, `go test -race ./...`. The operator runs staticcheck and commits — you have **no network**, `.git` is **read-only**, and you must **not create status/progress files in the repo**. Modify only the files each phase names (plus their tests).
- New endpoints stay on `x.com` only (SECURITY.md host allowlist).
- **Never fabricate a GraphQL query id.** All new operations use the TweetDetail pattern: empty fallback + `discoverOperation` on first use. `FetchBookmarks` (`pkg/api/bookmarks.go`) is the current template.
- Read-only requests need NO `X-Client-Transaction-Id` **except where §13 says a bodyless 404 proves otherwise** — that is what the `withTransactionID bool` parameter of `fetchTimelineOp` is for. Start Lists with `false`.
- Test conventions: offline mocked tests are the default and the bar (`pkg/api` has 20 test files to crib from). Live tests go in `*_live_test.go`, gated by a per-file env var (pattern: `XEET_LIVE_BOOKMARKS`, `XEET_LIVE_SEARCH`), and must compile on all four CI platforms. PTY tests re-exec via helper env vars and are build-tagged `darwin || linux`. Test names state what is guaranteed.
- Code comments: only "why not" (trade-offs, rejected alternatives). Commit messages: why, not what.

---

## 1. Phase L1 — Lists API (`pkg/api/lists.go`)

**Goal**: fetch one page of a list's timeline, and enumerate the user's own lists. No TUI change.

### 1.1 New file `pkg/api/lists.go`

```go
// FetchListTimeline retrieves one page of the given X List's feed.
func (c *WebClient) FetchListTimeline(ctx context.Context, listID, cursor string, count int) (*TimelinePage, error) {
	return c.fetchTimelineOp(ctx, "ListLatestTweetsTimeline", "", "XEET_LISTLATESTTWEETSTIMELINE_QID", count, false,
		func(count int) map[string]any {
			vars := map[string]any{
				"listId": listID,
				"count":  count,
			}
			if cursor != "" {
				vars["cursor"] = cursor
			}
			return vars
		})
}
```

Response parsing goes through the existing shared `parseEntries` walker (list timelines are instruction/entry shaped like Home). If the payload root path differs from what `parseTimeline` expects, add a thin `parseListTimeline` that locates the instructions array and delegates to `parseEntries` — do NOT fork the entry walker itself (that is the `parseConversation` exception, and it stays an exception).

### 1.2 List enumeration

```go
// ListInfo is one list the authenticated user owns or follows.
type ListInfo struct {
	ID          string
	Name        string
	MemberCount int
	IsPrivate   bool
}

// FetchOwnedLists enumerates the user's lists for the picker.
func (c *WebClient) FetchOwnedLists(ctx context.Context) ([]ListInfo, error)
```

Operation: `ListsManagementPageTimeline`, empty fallback, env override `XEET_LISTSMANAGEMENTPAGETIMELINE_QID`, `withTransactionID: false` to start. Variables: start with `{"count": 100}`. The response is a timeline-shaped payload whose entries contain `twitter_list` / list item content rather than tweets — this needs its own small parser (`parseListsPage`) that walks the same `instructions[].entries[]` shape but extracts list id/name from item content. §13 covers the unknowns. `fetchTimelineOp` hard-returns `*TimelinePage`, so add a small `fetchListsOp` in `lists.go` — but it must reuse the WHOLE existing pipeline, not just the request half: `operationQueryID` → `discoverOperation` on empty qid → `doTimelineOp` → `needsQueryIDRefresh`-triggered re-discover + retry → `statusToError` → `isTransientStatus` (`ServiceUnavailableError`) → non-200 error with `truncate(body)` → JSON decode → `graphQLError` → parse. That ladder is where rate-limit and auth errors get classified; reimplementing any rung of it is a bug. The ONLY divergence from `fetchTimelineOp`'s body is the final parse call (`parseListsPage` instead of `parseTimeline`).

### 1.3 Tests (Phase L1)

- `pkg/api/lists_test.go` (mocked, default): fixture JSON for a list timeline page and a lists-management page (structure per §13 R4's expected shape; update fixture if live probing corrects it). Tests:
  - `TestFetchListTimelineParsesPostsAndCursor`
  - `TestFetchListTimelineSendsListIDVariable` (assert request body contains `"listId"`)
  - `TestFetchOwnedListsParsesIDNameAndPrivacy`
  - `TestFetchListTimelineDiscoversQueryIDWhenUnset` (mirror the bookmarks discovery test)
- `pkg/api/lists_live_test.go`: gated by `XEET_LIVE_LISTS`. One test fetching `FetchOwnedLists` then the first list's first page; skip with a clear message when the account owns no lists.

**Verify phase alone**: `go test -race ./pkg/api/`; live check by the operator with `XEET_LIVE_LISTS=1`.

---

## 2. Phase L2 — `FeedList` kind + in-TUI picker

**Goal**: `L` in the timeline opens a list picker; choosing a list switches the feed via the existing `setFeed` machinery.

### 2.1 Feed kind

- Add `FeedList` to the `FeedKind` enum in `internal/timeline/model.go`.
- Add fields next to `searchQuery` (they are the same pattern — feed-kind parameters): `listID string`, `listName string` (name is display-only, for the header).
- `fetchPageSeq` gains a `listID string` parameter (alongside the existing `query string`), and its switch gains:

```go
case FeedList:
	id := listID
	fetch = func(ctx context.Context, cursor string, count int) (*api.TimelinePage, error) {
		return client.FetchListTimeline(ctx, id, cursor, count)
	}
```

Both `setFeed` and `maybeLoadMore` call sites pass `m.listID`. (In Phase C1 these params fold into the column struct; keep them positional for now — C1 is the refactor commit, not this one.)

### 2.2 Picker

New file `internal/timeline/lists.go`. New `mode` value `modeListPicker` following the `modeSearch` precedent in `search.go`:

- `beginListPicker() tea.Cmd` — sets mode, kicks a `fetchListsCmd(ctx) tea.Cmd` returning `listsMsg{lists []api.ListInfo, err error}`.
- Picker state on Model (global cluster, NOT per-feed — it is an overlay): `listPicker []api.ListInfo`, `listPickerSel int`, `listPickerErr error`, `listPickerLoading bool`, `listReturn mode` (mirror `searchReturn`).
- Keys inside picker: `j/k`/arrows move, `enter` selects (`m.listID`, `m.listName` set, then `return m, m.setFeed(FeedList)`), `esc` cancels back to `listReturn`.
- Rendering: full-screen overlay in `view.go` alongside the search prompt branch; reuse `contentWidth()` and the search prompt's frame styling. No new styling system.
- **Decision — fetch each launch, no config cache**: the picker fetches lists live every time it opens. One cheap request; caching list metadata would add config schema churn right before the multi-account migration (A1) for no real win.

### 2.3 Keys and help

- `L` (capital; lowercase `l` is taken by like) in the timeline feed opens the picker. Add to `cmd/help.go`'s `commandGroups` and the in-TUI help overlay.
- Header for `FeedList` shows `List: <name>` (crib the Search header treatment; keep it inside `TestSearchViewAndHeaderExposeQueryWithinWidth`'s width discipline).

### 2.4 Tests (Phase L2)

`internal/timeline/lists_test.go`:
- `TestListPickerOpensOnCapitalLAndClosesOnEsc`
- `TestListPickerSelectionSwitchesFeedAndResetsState` (assert posts nil, cursor cleared, feedSeq bumped — same guarantees `setFeed` gives elsewhere)
- `TestListFeedHeaderShowsListNameWithinWidth`
- `TestStalePageFromPreviousFeedIsDroppedAfterListSwitch` (seq guard still holds)

**Verify phase alone**: `go test -race ./internal/timeline/`; manual: open picker offline → error path renders, not a hang.

---

## 3. Phase L3 — `cmd/lists.go` + `--list` flag

- New `cmd/lists.go`: `xeet lists` — launches the timeline TUI directly into the picker (`timeline.Run` gains a way to start in `modeListPicker`; add an optional start-mode to the existing `Run(ctx, images, feed, query)` signature — extend it to `Run(ctx, images string, feed FeedKind, query, listID string)` and treat `feed == FeedList && listID == ""` as "open picker first"). Flags: `--images`, `--theme` (copy `cmd/search.go`).
- `--list <id>` flag on `timelineCmd` (and `rootCmd` if `--bookmarks` is mirrored there — it is; mirror it): starts directly on that list. Takes the numeric list id. **Decision — id only, no name resolution in the flag**: resolving names costs a network round-trip before the TUI even starts and introduces ambiguity; the picker is the name-based path.
- Mark `--list` mutually exclusive with `--following`/`--bookmarks` (`MarkFlagsMutuallyExclusive`).
- Help: add `lists` to `commandGroups`.
- **Decision (spec decision #1) — all three entry points ship (subcommand, flag, in-TUI key)** because each serves a distinct habit (scripted launch / muscle memory / discovery) and they all funnel into the same `setFeed(FeedList)`, so the marginal cost of each is a handful of lines.

### Tests (Phase L3)

- `cmd/lists_test.go`: `TestListsCommandRegisteredWithFlags`, `TestTimelineListFlagIsExclusiveWithOtherFeeds` (crib existing cmd tests for flag registration assertions).
- Extend `cmd/help_test.go` expectations.

**Verify phase alone**: `go build ./... && go test -race ./cmd/`; operator smoke: `xeet lists`, `xeet timeline --list <id>`.

---

## 4. Phase C1 — extract the `column` struct (behavior-preserving)

**Goal**: all per-feed state moves into one struct; the Model holds `columns []column` with exactly one element; every test still passes unchanged in meaning. This is the big mechanical commit — no layout change, no second column yet.

### 4.1 The struct — decision #3

New file `internal/timeline/column.go`:

```go
// column is one independently scrolling feed pane. With one column the TUI
// behaves exactly as before; the struct exists so state cannot leak between
// panes once there are several.
type column struct {
	id          int    // stable identity for message routing; never reused
	feed        FeedKind
	searchQuery string
	listID      string
	listName    string
	feedSeq     int
	posts       []api.TimelinePost
	cursor      string
	selected    int
	starts      []int
	ends        []int
	loading     bool
	loadingMore bool
	refreshing  bool
	expanded    bool
	err         error
	viewport    viewport.Model
	wezFrameKey string

	// thread cluster — the thread overlay is full-screen (decision #2) but its
	// STATE belongs to the column that opened it, so returning from the overlay
	// restores that column exactly.
	feedSelected  int
	threadRootID  string
	threadPosts   []api.ConversationPost
	threadCursor  string
	threadLoading bool
	threadMore    bool
	threadErr     error
	threadSeq     int
}
```

Model keeps (global): `ctx`, `width`, `height`, `imageMode`, `imageNote`, `mode`, `help`, `altText`, `altTextScroll`, `zoom`, `action`, `clipboardOK`, `toast`, `toastSeq`, `err`? — no: `err` is per-feed, it moved. `liking`, `previews`, `spinner`, the reply cluster (`replyReturn`, `replyEditor`, `replyPost`, `replyPosting`, `replyErr`, `replyNotice`), the search-prompt cluster (`searchInput`, `searchReturn`), the list-picker cluster from L2, plus new: `columns []column`, `focus int`, `nextColID int`.

**Decision #2 — `mode` stays GLOBAL on the Model; thread/reply/search/zoom/alt-text render as full-screen overlays.** Re-parenting `mode` into columns would force per-column key routing, per-column reply editors, and per-column overlay rendering — roughly doubling this refactor for a UX (side-by-side threads) nobody asked for. The thread *state* lives in the column (see struct) so `esc` returns each column to where it was; only the *presentation* is single-screen.

### 4.2 Accessors

- `func (m *Model) cur() *column { return &m.columns[m.focus] }` — the single funnel. Mechanically rewrite every `m.posts` → `m.cur().posts` etc. `currentPost()` reads from `m.cur()`; this retargets `openSelected`, `showAltText`, `zoomSelected`, `toggleSelectedLike`, `copySelectedLink`, enter→thread, `viewZoom`, `altTextRows`, `zoomLoading`, `evictDistantPreviews` in one stroke.
- `setFeed`, `switchFeed`, `maybeLoadMore`, `applyFeedPage`, `syncViewport`, `ensureSelectedVisible`, `moveSelection`, `activePosts`, thread functions: all become operations on `*column` (method on Model that takes `c *column`, or method on `*column` taking what it needs — prefer methods on Model operating on `m.cur()` where they touch global state like spinner/previews, plain `*column` methods where they do not).
- `applyLike` fans out over **all** columns' `posts` and `threadPosts` (For You and Following overlap heavily; cross-column duplicates are guaranteed).

### 4.3 Message routing — decision #4

`pageMsg`, `threadMsg`, `previewMsg`, and `likeMsg`-adjacent flows gain column identity:

```go
type pageMsg struct {
	page  *api.TimelinePage
	err   error
	more  bool
	seq   int
	colID int
}
```

- **`feedSeq` stays PER-COLUMN** (it moved into the struct) **and messages carry `colID`.** Rationale: seq answers "is this page from before my last feed switch", which is a per-column question; colID answers "which column asked", which seq cannot — a global tagged counter would conflate the two and reintroduce the silent-drop bug the brief documents.
- Routing helper: `func (m *Model) columnByID(id int) *column` returning nil when the column is gone (removed columns drop their in-flight pages silently — that is correct, mirror the existing stale-seq drop).
- `fetchPageSeq` signature becomes `fetchPageSeq(parent context.Context, feed FeedKind, query, listID, cursor string, more bool, seq, colID int)`; all five `return pageMsg{...}` sites carry `colID`. Same treatment for `threadMsg` construction in `thread.go` and `previewMsg` in `preview.go`. Update the four routing sites each in `model.go` Update / `thread.go` / `reply.go` / `search.go` to route via `columnByID` instead of assuming the singleton.
- `applyFeedPage(msg)` first resolves the column: nil → drop; then the existing `msg.seq != c.feedSeq` guard.

### 4.4 View (still one column)

`View`/`shell`/`syncViewport`/`contentWidth` untouched in behavior this phase — they read from `m.cur()`. `imageFrameKey` hashes `m.cur()` state as before.

### 4.5 Tests (Phase C1)

All existing `internal/timeline` tests must pass with mechanical-only edits (constructor changes, field paths). Add:
- `TestPageForRemovedColumnIsDropped` (construct pageMsg with unknown colID → no panic, no state change)
- `TestPageRoutedToNonFocusedColumnDoesNotTouchFocusedColumn` (two columns wired manually in the test even though the UI can't create them yet — this is THE regression test for the feedSeq collision bug; write it now, it is cheap insurance for C2)

**Verify phase alone**: full `go test -race ./...` green; `xeet` behaves identically by hand.

---

## 5. Phase C2 — N-column layout, focus, narrow fallback

### 5.1 Width — decision #5

`contentWidth()` becomes two functions in `view.go`:

```go
// columnContentWidth is the width of one column's text frame.
func columnContentWidth(totalWidth, ncols int) int
// (m Model) contentWidth() int  — kept, now returns columnContentWidth(m.width, len(m.columns))
```

Keeping the zero-arg method (now delegating) preserves the ~15 call sites; only sites that render a specific non-focused column call the parameterized form. Per-column width = `(totalWidth - gutters) / ncols`, gutter = 2 cells, clamped to `[30, 76]` as today. Overlays (thread/reply/search/zoom/alt-text/help) are full-screen and use `columnContentWidth(m.width, 1)`.

**Too-narrow behavior (defined, tested)**: effective column count `effectiveCols = min(configuredCols, max(1, usable/32))` where usable accounts for gutters and the two right-edge safety cells `shell` already keeps clear. When `effectiveCols < len(m.columns)`, render only columns `[first..first+effectiveCols)` chosen so the focused column is always visible (slide the window), and show a footer note `"+N more (widen terminal)"`. Never render a column below 30 cells; never overflow; the PTY resize-to-42 test must see exactly one column.

### 5.2 Composition

- `renderFeedContent` renders ONE column's content string at its width (parameterized). `View` composes: render each visible column into its own `viewport`-backed block, then `lipgloss.JoinHorizontal(lipgloss.Top, blocks...)`, then the existing `shell(center, footer)`.
- Each column keeps its own `viewport.Model` (already moved in C1); `resize()` sets every visible column's viewport to the per-column width and shared height.
- Focused column is indicated by the existing selected-post highlight plus a header accent on the focused column's header; non-focused columns still show their selection dimmed (theme's muted style). No new theme entries — reuse existing styles.
- Keys: `tab` / `shift+tab` cycle focus (also `[`/`]` as vim-adjacent synonyms). ALL existing keys (j/k, f, b, /, L, l, o, r, enter, …) act on the focused column via `m.cur()` — zero per-key changes thanks to C1.
- `footer()` describes the focused column; the `< 50` degraded branch keys off the focused column's width.

### 5.3 Tests (Phase C2)

- `TestTwoColumnsRenderSideBySideWithoutOverflowAt100Cols`
- `TestNarrowTerminalCollapsesToOneColumnAndNotesHiddenColumns` (width 42)
- `TestFocusCycleMovesActionTargetBetweenColumns` (like in col 2 does not touch col 1's posts — except the deliberate `applyLike` fan-out for the SAME post id, which gets its own test: `TestLikeFansOutToDuplicatePostAcrossColumns`)
- `TestFocusedColumnStaysVisibleWhenWindowSlides`
- Update the frame-invariant tests the brief lists (`TestViewIsFixedHeightAndDoesNotDuplicateRows` etc.): keep their single-column assertions by running them with one column, and add a two-column variant of the fixed-height/no-duplicate assertion (`strings.Count(view, "Alice") == 1` becomes: Alice appears once *per column that contains her*).

**Verify phase alone**: `go test -race ./...`; operator smoke in Ghostty at ≥100 cols with 2–3 columns.

---

## 6. Phase C3 — images and previews across columns

### 6.1 Image mode policy — decision #7

| protocol | columns == 1 | columns > 1 |
|---|---|---|
| ANSI half-block | as today | as today (width-parameterized, composes) |
| Kitty native (Unicode placeholders) | as today | supported — placeholder cells are ordinary text; verify width maths (§6.3) |
| iTerm2/WezTerm (`ESC ]1337;File=`) | as today | **forced to ANSI** with `imageNote = "multi-column: iTerm2/WezTerm images fall back to ANSI"` |

The WezTerm path moves the cursor with relative `ESC[nD`/`ESC[nA` around reserved cells; `JoinHorizontal` interleaves the neighbour column's text between reservation and motion, and CSI sequences measure zero-width so lipgloss padding is wrong on exactly the image rows. There is no fix inside bubbletea's `View() string` contract (it would need absolute addressing and pane origins). Enforcement point: where `imageMode` is resolved at startup and wherever columns can grow past 1 at runtime — `if m.imageMode == imageWezTerm && len(m.columns) > 1 { downgrade to ANSI + note }`. Explicit `--images native` on that path with `--columns` ≥ 2 downgrades with the visible note rather than erroring (an error would make `--columns 2` unusable in a whole terminal family; a note is honest and recoverable). Document in README under a "Multi-column" heading and in `--images` flag help.

`imageFrameKey()` (which triggers `tea.ClearScreen` for iTerm2 and hashes all of `starts`): with the forced-ANSI rule it only ever runs with one column, so leave its logic alone; assert that with a test (`TestWezTermPathNeverRunsMultiColumn`).

### 6.2 Preview cache — decision #6

**Shared cache, keyed by post id, unchanged key** — legal because all columns share ONE width (equal split, §5.1), so the "refetch when cached preview is wider than the frame" invariant still cannot ping-pong: widths in play are {column width, full-screen overlay widths}, and refetch only ever *shrinks* toward the narrowest, which is the (uniform) column width.

**EXPLICIT INVARIANT (load-bearing, must be written into the code): all feed columns are equal width; the shared post-id-keyed preview cache is only correct because of this.** If anyone later introduces unequal column widths (resizable panes, a wide "main" column), the width-based refetch loop returns: a preview cached at the wide column's width is "too wide" for the narrow one, gets refetched narrower, then looks degraded in the wide column, forever. Requirement: put this as a "why not per-column cache" comment directly on the `previews` map declaration AND on `columnContentWidth` ("all columns equal width — the preview cache depends on this; see previews map"), so a future width change trips over it at the two places it would be made. Changes needed:

- `evictDistantPreviews`: keep-set becomes the **union over all visible columns** of their `activePosts()` window plus each column's `selected` — not just the focused column. This is the fix for the evict-then-refetch churn the brief flags.
- `maxCachedPreviews`: `48 * len(m.columns)` capped at 144. Scaling matters because N columns hold N viewports of posts on screen at once.
- Kitty image ids stay `fnv32(postID) & 0xFFFFFF` — same post in two columns intentionally shares the image id and the transmitted image (no collision, and dedup is a feature).
- `requestPreviews` iterates visible columns, requesting at the shared column width; `previewMsg` carries `colID` only for routing the repaint, the stored state stays global.

### 6.3 Kitty width caution

U+10EEEE is Plane-16 PUA (go-runewidth reports width 1) and the row/column diacritics are combining marks (width 0). `columnContentWidth`-based padding around `nativePreviewBlock` output must measure with the same function lipgloss uses today — add `TestNativePlaceholderRowsMeasureAtDeclaredWidth` locking the measured width of a placeholder row to the declared image cell width, so a future runewidth bump fails loudly instead of skewing columns.

### 6.4 Tests (Phase C3)

- `TestMultiColumnForcesANSIOnWezTermWithVisibleNote`
- `TestEvictionKeepsPreviewsVisibleInNonFocusedColumns`
- `TestPreviewBudgetScalesWithColumnCount`
- `TestNativePlaceholderRowsMeasureAtDeclaredWidth`
- Update `TestThreadRefetchesAPreviewTooWideForItsRail`, `TestNativePrefetchIncludesVisiblePosts`, `TestWezRepaintClearsOnlyWhenFrameMoves` for the new accessors (semantics unchanged, single-column).

---

## 7. Phase C4 — configuring columns (decision #8)

**v1 configuration surface: `--columns` flag + optional config key. No in-TUI add/remove.** In-TUI layout editing is UI polish that should ride on a stable base; the flag delivers the whole feature value now.

- Flag on `timelineCmd` and `rootCmd`: `--columns "foryou,following,bookmarks,list:1234567890,search:golang"` — comma-separated column specs, each `foryou | following | bookmarks | list:<id> | search:<query>`. Parser in `cmd/timeline.go` → `[]timeline.ColumnSpec{Kind FeedKind; Query, ListID string}`; `timeline.Run` gains the spec slice (default: one spec derived from the existing feed/query args, preserving every current invocation).
- Config persistence: `columns: ["foryou", "following"]` (list of the same spec strings) in `~/.xeet.yaml`, read at startup when the flag is absent, written **only** by explicit `xeet columns save` (a tiny subcommand that snapshots nothing at runtime — it just validates and writes the given specs: `xeet columns save "foryou,list:123"`). No implicit writes: the TUI never persists layout on its own (write-on-exit would collide with the A1 write-contention work and surprise users).
- Cap: max 4 columns (76-min-width terminals rarely fit more; parser rejects >4 with a clear error).
- `--columns` mutually exclusive with `--following`/`--bookmarks`/`--list` (they are the 1-column shorthands).

### Tests (Phase C4)

- `cmd`: `TestColumnsFlagParsesSpecsAndRejectsUnknownKinds`, `TestColumnsFlagExclusiveWithSingleFeedFlags`, `TestColumnsConfigKeyIsUsedWhenFlagAbsent`
- PTY (`darwin || linux`, re-exec helper env pattern from `cmd/pty_test.go` / `internal/timeline/pty_test.go`): extend `TestTimelinePTYNavigationResizeAndHelp`-style coverage with a run at 120 cols → resize to 42 → assert single-column fallback text appears and no line exceeds the terminal width.

---

## 8. Phase A1 — multi-account config schema v2 (decision #9)

**This is the migration-risk phase. It lands after columns so it sits on a stable base, and nothing in it touches `pkg/api` — `NewWebClient(cfg)` copies scalars, has no package state, and already supports N clients.**

### 8.1 Schema — one file, nested map keyed by X user id, version key

**The account key is the X user id (`api.Account.ID`, the numeric rest-id), NOT a hash of the cookies.** `sessionFingerprint(authToken, ct0)` (`cmd/doctor.go:84`) hashes the two session cookies — and those cookies ROTATE (that is why `session_expires` exists and why re-running `xeet auth` is the documented fix for a stale session). Keying by fingerprint would mean every cookie refresh of the SAME account creates a new key: duplicate `accounts:` entries, orphaned keyring items, `active` drifting to the newest twin. Handle is also not a key (users rename). The user id never changes, and A2's reordered `verifyAndSave` has it in hand (from `FetchViewer`) at exactly the moment the key is chosen. `sessionFingerprint` survives only as a doctor display diagnostic — never a lookup key.

`~/.xeet.yaml` (same path — a new filename would orphan theme/QIDs and double the migration surface; the `version:` key is the downgrade tripwire):

```yaml
version: 2
active: "783214"                 # X user id of the active account
theme: dracula                   # stays global
create_tweet_qid: ...            # ALL query-id fields stay global, top-level (decision below)
...other *_qid...
accounts:
  "783214":                      # api.Account.ID (numeric rest-id) — quote it, YAML must keep it a string
    handle: someuser             # display only; refreshed on every successful viewer fetch, so a
    session_browser: chrome      #   rename UPDATES the entry instead of forking it
    session_profile: Default
    session_domain: .x.com
    session_expires: 2027-01-01T00:00:00Z
    session_imported: 2026-07-27T00:00:00Z
  "legacy":                      # provisional key — only ever present right after migration (§8.2),
    ...                          #   rekeyed to the real user id on the first successful viewer fetch
```

- **Query ids stay GLOBAL** — they are X's deployment-wide persisted-query ids scraped from public JS, identical for every account; per-account storage would mean N× discovery for zero benefit and would churn `Config`, `NewWebClient`, `ApplyRefreshedQueryIDs`, and `cmd/setqid.go` for nothing.
- Keyring (decision #9 cont.): service stays `"xeet"`; per-account keys are `auth_token:<userid>` / `ct0:<userid>` (and `auth_token:legacy` / `ct0:legacy` for the provisional slot). Turn the `keyAuthToken`/`keyCT0` constants into `func authTokenKey(id string) string` etc. — `SecretStore` interface unchanged. The legacy unsuffixed keys are read ONLY by migration and deleted by it.
- New/changed API in `pkg/config`:
  - `Config` gains `UserID string` and `Handle string` (both yaml:"-"; filled from the resolved account entry); represents the ACTIVE account, so all 20 `NewConfigManager()`+`Load()` call sites keep working unchanged.
  - `Load()` → loads file, resolves `active`, pulls that account's secrets. v1 file (no `version:`) with legacy keyring entries → **migrate** (§8.2). `migrateLegacy` (pre-keyring, plaintext `auth_token:` in file) chains into the same path — do not reintroduce top-level `auth_token:` writes.
  - `LoadAccount(userID string) (*Config, error)` — same as Load but for a named account (A4 uses this).
  - `Accounts() ([]AccountInfo, error)` — file-only listing (`AccountInfo{UserID, Handle, SessionBrowser, SessionProfile, SessionImported}`); **no keyring, no network** so `doctor --offline` and `accounts` stay cheap.
  - `SetActive(userID string) error` — validates the account exists, rewrites file.
  - `SaveAccount(cfg *Config) error` — upserts the account keyed by `cfg.UserID` (keyring pair + accounts entry) and sets it active; errors if `UserID` is empty (only migration may write the `legacy` slot). Keeps the existing snapshot/rollback discipline per key pair.
  - `RekeyAccount(oldID, newID string) error` — moves the accounts entry and its keyring pair from one key to the other (write new pair → rewrite file with new key + `active` → delete old pair, delete-last for crash recoverability, same discipline as §8.2). Used to promote `legacy` → real user id; if `newID` already exists (the migrated session belongs to an account that was re-authed meanwhile), MERGE: keep the newer `session_imported`, drop the loser's keyring pair.
  - **`SaveQueryIDs(cfg *Config) error` — REQUIRED, fixes the write-contention bug**: re-read the file, patch ONLY the `*_qid` fields from `cfg`, write back atomically. All eight `Load → use → ApplyRefreshedQueryIDs → Save` call sites (both closures in `internal/timeline/model.go` shown above, `verifyAndSave`, `cmd/whoami.go`, `cmd/post.go`, etc. — grep `ApplyRefreshedQueryIDs`) switch their `Save(cfg)` to `SaveQueryIDs(cfg)`. Without this, a `setLike` holding a stale in-memory `Config` can resurrect a just-logged-out account or clobber a just-added one. `Save` in its full-overwrite form becomes internal to auth/migration paths only.
  - `EraseAccount(userID string) error` — deletes that account's keyring pair and map entry; if it was active, promote another account or clear `active`. `EraseAll() error` — current `Erase` behavior (all keys, file, legacy key file). `cmd/logout.go` gets `--all`; default logs out only the active account.
- Downgrade hazard (documented, mitigated, not fully preventable): an old binary saving over a v2 file silently erases `accounts:` (plain `yaml.Unmarshal`, full-marshal `writeFile`, no unknown-key round-trip). Mitigations shipped: the `version:` key (new binaries refuse to *full-overwrite* a file whose version is higher than they know: `Save` checks and errors "config written by a newer xeet"), and `doctor` prints the version + account count so damage is visible. Preserving unknown YAML nodes via `yaml.Node` is explicitly out of scope (high-effort, low-payoff once version-gating exists). Note this limitation in README.

### 8.2 Migration (exact, ordered) — provisional key + rekey

**Decision: migrate OFFLINE under the provisional key `legacy`, and rekey to the real user id on the first successful viewer fetch** — migration must never fail when offline, and the rekey rides on network calls that already happen (`verifyAndSave`, `whoami`'s viewer fetch), so no new failure mode is introduced. (The alternative — one `FetchViewer` call during migration — is allowed as an OPTIMIZATION: attempt it with a short timeout, use the id on success, fall back to `legacy` on any error. Implement the fallback path first; the optimization is optional.)

On `Load()` finding no `version:` key:
1. Read legacy keyring `auth_token`/`ct0`. If absent and no legacy in-file tokens → write nothing yet; first `SaveAccount` creates v2.
2. If present: write `auth_token:legacy`/`ct0:legacy`; build the single `accounts:` entry under key `legacy` from the existing top-level `session_*` fields; set `active: legacy`, `version: 2`; write the file **before** deleting the old unsuffixed keyring keys (delete-last so a crash mid-migration leaves a recoverable state, not an orphaned session); then delete the old keys (and the dead `session_cookies`).
3. Rekey: every code path that completes a successful `FetchViewer` while the active account key is `legacy` (that is `verifyAndSave` and `whoami`; add the check in one shared helper in `pkg/config`, not scattered) calls `RekeyAccount("legacy", account.ID)` and sets `handle`. Until then, `accounts` listing shows `legacy` with its fingerprint as the display id — functional but unnamed.
4. Handle backfill: migration itself does NOT call the network (modulo the optional optimization above); `handle` is filled by the same viewer fetches that trigger the rekey.

### 8.3 Tests (Phase A1)

`pkg/config/config_test.go` additions (in-memory SecretStore fake already exists):
- `TestLoadMigratesV1KeyringSessionToV2UnderProvisionalKey` (old unsuffixed keys gone, `auth_token:legacy` present, file has version+accounts+active=legacy, QIDs preserved)
- `TestMigrationCrashBeforeLegacyDeleteIsRecoverable` (simulate by asserting file+new keys are complete before delete step — order-of-operations test)
- `TestRekeyAccountMovesEntryAndKeyringPairDeleteLast` (crash between write and delete leaves both readable, new one wins on next Load)
- `TestRekeyIntoExistingUserIDMergesKeepingNewerSession`
- `TestSaveAccountRejectsEmptyUserID`
- `TestSaveQueryIDsPatchesOnlyQIDFields` (mutate accounts on disk between Load and SaveQueryIDs; assert accounts survive)
- `TestSaveRefusesFileFromNewerVersion`
- `TestEraseAccountRemovesOnlyThatAccountAndPromotesActive`
- `TestLoadAccountReturnsRequestedSessionNotActive`
- `TestPreKeyringLegacyFileMigratesStraightToV2` (chained migration)

**Verify phase alone**: `go test -race ./pkg/config/ ./...`; operator: run new binary once against a real v1 install copy, then `xeet whoami` + `xeet doctor`.

---

## 9. Phase A2 — multi-profile browser import + auth UI

**Goal**: importing can see every logged-in profile, not just the "best" one; `xeet auth` lets you pick which; re-auth of a different account no longer silently replaces the previous one (it now lands in its own slot keyed by X user id).

- `pkg/api`: replace the `betterLoginResult` reduction with collection. New exported entry point `ScanSessions() ([]LoginResult, error)` (or extend the existing scan APIs — follow whatever `cmd/auth_ui.go`'s `scanBrowsersCmd` currently calls) returning ALL per-profile results across `cookies_darwin.go`, `cookies_linux.go`, `cookies_gecko.go` (+ keep `cookies_other.go` stub compiling). `LoginResult.Profile` is already populated per-DB before the collapse — you are deleting a reduction, not building extraction. Keep `betterLoginResult` only as the sort comparator (prefer `x.com` domain → later `LastUsedAt` → later `ExpiresAt` → smaller profile name) so the list is best-first and the plain-mode default (`runAuthPlain`) remains "the best one".
- Dedup happens at TWO levels, because the true account id is only knowable after a network call:
  - **Scan time (offline)**: collapse only entries whose cookie pair is byte-identical (same session copied between profiles) — `sessionFingerprint` is fine for THIS, it is comparing the cookies themselves, not identifying the account. Two profiles holding the same account at different cookie ages legitimately remain two picker rows (they ARE two distinct live sessions; the user picks the one to import — the best-first sort puts the fresher one on top).
  - **Save time (authoritative)**: `verifyAndSave` keys by the user id from `FetchViewer`, so importing either of those rows upserts the SAME `accounts:` entry — the account list can never grow a duplicate per cookie age. This is where the real dedup lives.
- `cmd/auth_ui.go`: insert phase `authPhaseChooseProfile` between `authPhasePick` (browser) and `authPhaseImport` — skipped when the scan yields exactly one session. Renders profile name + cookie domain + expiry.
- `cmd/auth.go` `verifyAndSave` reorder (**required**): `Verify` → **`FetchViewer` (now load-bearing: it supplies the account key)** → `cfg.UserID = account.ID`, `cfg.Handle = account.Handle` → `SaveAccount`. The viewer fetch is no longer "a courtesy". On `FetchViewer` FAILURE the save must NOT proceed under a guessed key: retry once, then fail the auth with "verified but could not identify the account; try again" — an unidentified save is exactly the duplicate-slot bug this phase exists to kill. (Exception: the migration `legacy` slot, §8.2, which is owned by `pkg/config`, not this path.) Also stop seeding `candidate := *cfg` from the ACTIVE account's session metadata — build the candidate from the `LoginResult` + global fields only, so a second account does not inherit the first's `session_*` (QIDs/theme are global anyway and stay).
- `xeet auth` of an already-known user id updates that account's entry and keyring pair in place — cookie refresh for the same account is an UPDATE, never a new slot. This is the safe re-auth story replacing today's unconditional overwrite, and it also resolves a pending `legacy` slot naturally (rekey/merge per §8.2 step 3).

### Tests (Phase A2)

- Per-platform cookie tests already exist — extend each (`cookies_darwin_test.go` etc.) with `Test<Platform>ScanReturnsAllLoggedInProfiles` using two fixture profile DBs.
- `TestScanCollapsesOnlyByteIdenticalCookiePairs` (same account, different cookie ages ⇒ two rows)
- `cmd/auth_ui_test.go`: `TestAuthSkipsProfileChoiceWithSingleSession`, `TestAuthProfileChoiceListsAllSessionsBestFirst`
- `cmd/auth_test.go`: `TestVerifyAndSaveKeysByViewerUserIDAndDoesNotInheritSessionMetadata`, `TestReauthOfKnownAccountUpdatesInPlaceNotNewSlot`, `TestVerifyAndSaveFailsClosedWhenViewerUnidentifiable`

---

## 10. Phase A3 — `xeet accounts` + switch

- New `cmd/accounts.go`:
  - `xeet accounts` — table: active marker, handle (or user id when handle is empty; `legacy` shown with its fingerprint), browser/profile, imported date. File-only (`Accounts()`), no network, no keyring — same cheapness contract as `doctor --offline`.
  - `xeet accounts switch <handle|user-id-prefix>` — `SetActive`. Exact match on handle; unique-prefix on user id.
  - `xeet accounts remove <…>` — `EraseAccount` with a y/N confirm (destructive).
- `cmd/whoami.go` prints which account (handle + user id) it verified — and this fetch is a rekey trigger for a pending `legacy` slot (§8.2). `cmd/doctor.go` adds account count + active user id (fingerprint stays as the session diagnostic it always was); `--offline` still does zero `Verify` calls (verify only the ACTIVE account in online mode — N live verifies is exactly the slowness the brief forbids).
- In-TUI: `A` key cycles the ACTIVE account for the whole app (all columns) — full re-`setFeed` on every column (bump every seq; in-flight pages from the old account die on the seq guard). Footer/header shows the active handle when more than one account exists.

### Tests (Phase A3)

- `cmd/accounts_test.go`: `TestAccountsListsWithoutTouchingKeyringOrNetwork` (fake store instrumented), `TestSwitchByHandleAndByUniqueUserIDPrefix`, `TestRemoveActivePromotesNextAccount`
- `internal/timeline`: `TestAccountCycleResetsEveryColumnAndDropsInFlightPages`

---

## 11. Phase A4 (stretch) — per-column accounts (decision #10)

**Yes, columns can carry different accounts**, and the race is solved the same way feed switches already are: **resolve the account inside the closure from data captured at dispatch time, guarded by seq.**

- `column` gains `accountID string` (X user id; "" = follow the global active account).
- `fetchPageSeq` gains `accountID string`, captured like `query`/`listID`. Inside the closure: `cfg` comes from `LoadAccount(accountID)` (or `Load()` when empty). A mid-flight account switch on the column bumps `feedSeq` (it goes through `setFeed`), so a page fetched under the old account fails the seq guard and is dropped — identical mechanism, zero new synchronization. `fetchPageSeq` already carries `seq` for exactly this class of problem.
- Column spec syntax grows an optional prefix: `--columns "@alice:foryou,@bob:following"` (`@handle:` resolved at startup via `Accounts()`; unknown → startup error listing known accounts).
- `setLike`/reply closures triggered from a column use that column's account (thread the user id the same way); `applyLike` fan-out must NOT cross accounts — fan out only across columns sharing the acting account's id (a like by @alice must not flip @bob's rendered like state; same post id, different per-account like status).
- `previews` stay shared (images are account-independent). `liking` map keys become `accountID + ":" + postID`.
- SaveQueryIDs from any account's fetch is fine — QIDs are global by design.

### Tests (Phase A4)

- `TestColumnFetchUsesItsOwnAccountSession` (fake config dir with two accounts; assert auth header per request via the mocked transport)
- `TestAccountSwitchMidFlightDropsStalePageViaSeqGuard`
- `TestLikeFanOutStaysWithinTheActingAccount`
- `cmd`: `TestColumnsFlagParsesAccountPrefixes`

---

## 12. Decisions index (the ten, one line each)

1. **List selection = subcommand + flag + `L` key; metadata fetched per launch, not cached** — three entry points share one `setFeed` funnel so each costs little; caching would churn the config schema right before A1's migration.
2. **Thread/reply/search/zoom/alt-text stay full-screen overlays; `mode` stays global** — halves the refactor by avoiding per-column key routing and overlay rendering; per-column thread *state* still restores correctly because it lives in the column.
3. **`column` struct, unexported, `internal/timeline/column.go`, 26 fields as listed in §4.1** — everything guarded by `feedSeq` or indexed parallel to `posts` moves; overlay/global clusters stay on Model.
4. **Messages carry `colID`; `feedSeq` is per-column** — seq answers "before my last feed switch?", colID answers "whose page?"; one global tagged counter would conflate them and reintroduce the misrouting bug.
5. **`columnContentWidth(totalWidth, ncols)` + delegating zero-arg method; too-narrow ⇒ render only the columns that fit (≥30 each), sliding window keeps focus visible, footer notes the hidden count** — defined and PTY-tested at 42 cols.
6. **Preview cache stays shared and post-id-keyed** — equal-split columns share one width so the no-ping-pong invariant survives (this equal-width dependency is an EXPLICIT invariant, commented at both the cache and `columnContentWidth`, §6.2); eviction keep-set unions all visible columns; budget scales `48×cols` cap 144.
7. **Images: ANSI and Kitty native compose; iTerm2/WezTerm forced to ANSI when cols>1 with a visible note** — relative-cursor-motion escape art cannot survive `JoinHorizontal`, and fixing it needs absolute addressing bubbletea's View contract hides.
8. **Columns configured by `--columns` flag + config key written only by explicit `xeet columns save`; no in-TUI editing in v1; max 4** — smallest surface that delivers the value; implicit write-on-exit would collide with A1's write-contention fix.
9. **One `~/.xeet.yaml` with `version: 2`, `accounts:` map keyed by X user id (never the cookie fingerprint — cookies rotate, the user id doesn't), global QIDs, keyring keys `auth_token:<userid>`/`ct0:<userid>`, offline migration under a provisional `legacy` key rekeyed on first viewer fetch** — same file keeps theme/QIDs; version key gates newer-file overwrites; the provisional key keeps migration network-free while the stable id arrives on calls that already happen.
10. **Per-column accounts: yes (A4); the account's user id captured in the fetch closure at dispatch, mid-flight switches invalidated by the existing per-column seq guard** — the same mechanism that already makes feed switches race-free, no new locking.

---

## 13. Risk register (genuinely unverified — probe, don't guess)

Recovery procedure for every row, exactly as it worked for Bookmarks/Search:

> Run the live test with the gate env var. On a GraphQL 400, **read the error body** — X names the missing/unknown variable or feature switch explicitly. Add exactly what it names to `buildVars` / the features map, nothing more, and re-run. `xeet inspect-har` prints structural keys but **never values**, so a HAR from the operator's browser can confirm variable *names* and feature-switch *keys* safely. A **bodyless 404 with a decrementing `x-rate-limit-remaining` header means the endpoint is alive and a required HEADER is missing** — that is exactly how the `SearchTimeline` per-request `X-Client-Transaction-Id` requirement was found; the fix is flipping `withTransactionID` to `true`, not touching variables.

| # | Risk | Likelihood | Plan |
|---|---|---|---|
| R1 | `ListLatestTweetsTimeline` needs more variables than `{listId, count, cursor}` (feature switches, `withCommunity` etc.) | high | Recovery procedure above; expect 1–2 iterations of adding named feature switches. |
| R2 | `ListsManagementPageTimeline` is the wrong operation name for enumerating lists (candidates: `CombinedLists`, `ListsManagementPageTimeline`) | medium | `discoverOperation` fails ⇒ the name is not in the JS bundle map: grep the operator-provided HAR structural dump for `List` operations, use the one the web client actually fires on x.com/…/lists. |
| R3 | Lists ops require `X-Client-Transaction-Id` (SearchTimeline precedent) | medium | Bodyless-404 signature ⇒ flip `withTransactionID: true`. One-line change by design. |
| R4 | Lists-management payload entry shape (list items vs tweet items) differs from the fixture guess | high | Fixture is a guess until first live run; `parseListsPage` walks defensively (skip non-list entries), live test asserts only non-empty id+name. Correct the fixture from the live structural dump, never from invented values. |
| R5 | go-runewidth / lipgloss width maths break Kitty placeholder columns after a version bump | low | `TestNativePlaceholderRowsMeasureAtDeclaredWidth` (§6.3) fails loudly; fix is a measurement shim, not silent skew. |
| R6 | Old binary run against a v2 config erases `accounts:` (downgrade) | certain-if-it-happens | Cannot be fully prevented without yaml.Node round-tripping (out of scope). Mitigated: version-gate on full `Save`, doctor visibility, README warning; keyring items survive and re-auth restores. |
| R7 | Keyring backends that key strictly by (service, account) may treat `auth_token:<userid>` names differently (length/charset limits on some Linux Secret Service impls) | low | User ids are numeric, ≤ 20 digits today; total key ≤ 31 ASCII chars — well inside every backend's limits. If a backend still balks, fall back to service `"xeet:<userid>"` with unchanged key names (one function edit, covered by the fake-store tests). |
| R8 | Two accounts rate-limit-share per IP; multi-account columns double request volume | medium | Not a code change: existing 429 handling (`statusToError`) surfaces per-column errors independently. Note in README. |

---

## 14. Phase → verification recap

| Phase | Verify with |
|---|---|
| L1 | `go test -race ./pkg/api/`; operator: `XEET_LIVE_LISTS=1 go test -run Live ./pkg/api/` |
| L2 | `go test -race ./internal/timeline/`; manual `L` picker offline error path |
| L3 | `go test -race ./cmd/`; `xeet lists`, `xeet timeline --list <id>` |
| C1 | FULL suite green with mechanical-only test edits; hand-check identical behavior |
| C2 | new layout tests + updated frame invariants; smoke at ≥100 cols |
| C3 | image-policy tests; smoke in Ghostty (Kitty native, 2 cols) and WezTerm (ANSI fallback note) |
| C4 | flag/config tests + PTY resize-to-42 fallback |
| A1 | `pkg/config` migration tests; operator runs against a COPY of a real v1 install |
| A2 | per-platform multi-profile fixtures; auth picker phase tests |
| A3 | accounts cmd tests; `A` cycle test |
| A4 | per-column session tests with instrumented transport |
