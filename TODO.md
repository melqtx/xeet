# Xeet TODO

## Done

- Browser-cookie authentication only, with no password or developer API key flow
- macOS Chromium import: Dia, Arc, Chrome, Chromium, Brave, and Edge
- macOS Firefox-family import: Firefox and Zen
- Linux Chromium import: Chrome, Chrome Beta, Chromium, Brave, and Edge
- Linux Firefox-family import: Firefox and Zen
- Linux Secret Service, GNOME Keyring, and KDE Wallet support
- Multiple browser profiles plus common native, Snap, and Flatpak paths
- Session storage in macOS Keychain or Linux Secret Service
- Session migration, atomic `0600` config writes, symlink refusal, and `xeet logout`
- CreateTweet query ID discovery and stale-ID recovery
- Composer with multiline editing, image attachments, upload progress, and draft-safe failures
- Home timeline with pagination, replies, likes, refresh, browser opening, and link copying
- X11 and Wayland clipboard support with a file-attachment fallback
- [done] lazy inline truecolor image previews in the timeline
- multi-image timeline collages and optional native Kitty/iTerm2 previews
- Bounded API retries, rate-limit errors, session-expiry detection, and cookie-leak tests

## Next

- Draft autosave and restore across process exits
- `xeet doctor` environment and keyring diagnostics
- `xeet whoami` session/account display
- Graceful offline handling
- Accessible image alt text
- Following timeline option
- Configurable theme without changing layout
- Optional inline image previews for Kitty, iTerm2, and WezTerm
- Chunked upload for video and large media
- Animated GIF playback via the Kitty graphics animation protocol
- tmux passthrough (DCS-wrapped Kitty graphics) for native images inside tmux 3.3+
- Windows browser-cookie import using DPAPI

Scheduling, bulk posting, scraping, automated engagement, and mass-posting
features are intentionally out of scope.
