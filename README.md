# Xeet (older version)

A simple, beautiful terminal interface for posting to X — no API keys, no
posting limits.

```
██╗  ██╗███████╗███████╗████████╗
╚██╗██╔╝██╔════╝██╔════╝╚══██╔══╝
 ╚███╔╝ █████╗  █████╗     ██║
 ██╔██╗ ██╔══╝  ██╔══╝     ██║
██╔╝ ██╗███████╗███████╗   ██║
╚═╝  ╚═╝╚══════╝╚══════╝   ╚═╝
             /\_/\
            ( o.o )
             > ^ <
         ready when you are  ✦

╭────────────────────────────────────────╮
│ what are you thinking?                 │
╰────────────────────────────────────────╯
12/280  •  enter to xeet
```

## Install

```bash
git clone https://github.com/melqtx/xeet.git
cd xeet
make install
```

Installs `xeet` to `/usr/local/bin/`, so you can run it from anywhere.

## Use it

**1. Connect your account — one command:**

```bash
xeet auth
```

Just make sure you're logged into x.com in your browser first (Chrome, Arc,
Brave, Dia, or Edge). `xeet auth` finds that session and connects — no
passwords, no API keys, no login screen. On macOS it asks once to read the
browser's Keychain key; click Allow.

**2. Post:**

```bash
xeet                                  # open the interactive composer
xeet timeline                         # browse your home timeline
xeet post "hello from my shell"       # one-shot from the terminal
echo "piped in" | xeet post           # reads stdin
xeet post "photos" -i one.png -i two.jpg
xeet post --image meme.png             # image-only post
xeet post "a reply" --reply 1234567890
```

## How it works

xeet reuses the x.com session already in your browser and talks to the same
internal endpoints the website does — so it isn't metered like the paid
developer API. Your session cookie is stored encrypted (AES-256-GCM) in
`~/.xeet.yaml`; the key lives in `~/.xeet.key` (created automatically).

X rotates the internal `CreateTweet` id periodically; xeet discovers the current
one automatically and caches it. If discovery ever fails it'll tell you to grab
it manually — open x.com, post a tweet, copy the id from the `CreateTweet`
request in DevTools → Network, then `xeet setqid <id>`.

**Note:** this uses X's internal endpoints, which is against X's Terms of
Service. It's fine for posting your own stuff; don't use it to spam or automate
at scale. Currently macOS only.

## TUI keys

- **Enter** — post
- **Alt+Enter** / **Ctrl+J** — line break
- **Ctrl+V** — read an image or text from the clipboard
- **Ctrl+O** — attach an image path (you can drag a file into the prompt)
- **Tab** — move between the editor and attached images
- **Arrow keys** — select an attached image
- **Delete** / **Ctrl+X** — remove the selected image
- **F1** — help
- **Ctrl+C** / **Esc** — quit (drafts require confirmation)

Up to four PNG, JPEG, GIF, or WebP images can be attached. The composer shows
the real format, dimensions, and size before anything is uploaded.

## Timeline

```bash
xeet timeline
```

Use **j/k** or the arrow keys to move, **l** to like or unlike, **r** to reply
in place, **R** to refresh, **Enter** to open a post, **y** to copy its link,
and **P** to write a new post. More posts load automatically near the
bottom. Press **?** for the in-app key guide.
