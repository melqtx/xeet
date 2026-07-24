# Xeet TODO

## Done

- Browser-cookie authentication only, with no password or developer API key flow
- macOS and Linux browser import: Chrome, Helium, Firefox, Brave, and Zen
- Linux Secret Service, GNOME Keyring, and KDE Wallet support
- Multiple browser profiles plus common native, Snap, and Flatpak paths
- Session storage in macOS Keychain or Linux Secret Service
- Session migration, atomic `0600` config writes, symlink refusal, and `xeet logout`
- CreateTweet query ID discovery and stale-ID recovery
- Composer with multiline editing, image attachments, upload progress, and draft-safe failures
- Home timeline with pagination, replies, likes, refresh, browser opening, and link copying
- X11 and Wayland clipboard support with a file-attachment fallback
- `xeet doctor` session source, fingerprint, expiry, and authenticated-read diagnostics
- Ambiguous post reconciliation without retrying transient/unclear CreateTweet outcomes
- Lazy inline truecolor image previews in the timeline
- multi-image timeline collages and optional native Kitty/iTerm2 previews
- Bounded API retries, rate-limit errors, session-expiry detection, and cookie-leak tests
- Draft autosave and restore across process exits, including clipboard images
- Actionable offline, timeout, service outage, rate-limit, and expired-session recovery
- Persistent operation-ID caching for posting, timelines, likes, and unlikes
- Keyboard-accessible image descriptions, including text-only mode
- `xeet whoami` session/account display
- Following timeline (`--following`, `f` key in the timeline)
- Configurable color themes without layout changes (`xeet theme`, `--theme`)
- tmux passthrough (DCS-wrapped Kitty graphics) for native images inside tmux 3.3+
- Chunked upload for video and large media
- Interactive `xeet auth` that marks which browsers hold an x.com session
- Live-preview theme picker (`xeet theme`) and a grouped, colorized help

## Next

- Animated GIF playback via the Kitty graphics animation protocol
- Windows browser-cookie import using DPAPI

Scheduling, bulk posting, scraping, automated engagement, and mass-posting
features are intentionally out of scope.
