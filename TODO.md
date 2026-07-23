## xeet todo.md

- tweet scheduling
- [done] don't use the paid xapi — auth is now ONLY the browser cookie import:
    - `xeet auth` reads + decrypts the x.com session from your browser
      (Dia/Arc/Chrome/Brave/Edge), no login, never trips X's login-limiting.
    - posts via internal CreateTweet GraphQL; queryId is auto-discovered from
      X's live JS bundles and cached, so rotations self-heal.
    - image upload works in cookie mode (TUI Ctrl+V paste).
    - removed all the fluff: OAuth1 API keys, PIN flow, headless login, chromedp
      browser-launch login. One command to connect.
- browser cookie import for Windows (DPAPI) + Linux (libsecret/kwallet)
- chunked upload for video/large media (simple upload only for now)
- [done] responsive Bubble Tea composer with multiline editing, attachment cards,
  clipboard/file image import, multi-image upload progress, and draft-safe errors
- optional inline image previews for Kitty/iTerm2/WezTerm
- [done] cozy read-only home timeline with pagination, browser opening, link copying,
  refresh, and a shortcut into the composer
- [done] optimistic like/unlike from the timeline with rollback on API errors
- timeline interactions: replies, reposts, Following feed, and conversations
- anything

---

*This is a living document. Feel free to add your own ideas and vote on priorities!*