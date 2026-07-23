# Xeet (older version)

A simple terminal interface for posting to and browsing X without developer
API keys.

> [!WARNING]
> Xeet uses unsupported internal X web endpoints and browser-session cookies.
> This may violate X's Terms of Service and can result in account limits,
> suspension, or loss. The endpoints can change or stop working without notice.
> Xeet is not affiliated with, endorsed by, or supported by X Corp. Use it only
> with an account you are prepared to risk. Do not use it for spam, scraping,
> bulk posting, or automated engagement.

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

**1. Connect your account:**

```bash
xeet auth
```

First, log into x.com in a supported browser:

- macOS: Chrome, Chromium, Brave, Edge, Arc, Dia, Firefox, or Zen
- Linux: Chrome, Chrome Beta, Chromium, Brave, Edge, Firefox, or Zen
- Linux installs from native packages, Snap, and common Flatpak locations are
  detected. Multiple browser profiles are supported.

`xeet auth` imports that browser session without asking for your X password.
macOS may request Keychain access. Linux may request that you unlock GNOME
Keyring, Secret Service, or KDE Wallet.

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

Xeet reuses the x.com session already in your browser and talks to unsupported
internal endpoints used by the website. This is not the official developer API.
X can rate-limit these requests, change the endpoints, or reject the session at
any time.

The imported `auth_token` and `ct0` cookies grant account-level access. Treat
them like a password. Xeet stores them in macOS Keychain or Linux Secret
Service. It does not store them in the YAML config file. Run `xeet logout` to
delete the saved session. This removes Xeet's copy but does not log your browser
out of X.

X rotates the internal `CreateTweet` id periodically; xeet discovers the current
one automatically and caches it. If discovery fails, Xeet explains how to set
it manually from the `CreateTweet` request in browser developer tools.

## TUI keys

- **Enter**: post
- **Alt+Enter** / **Ctrl+J**: line break
- **Ctrl+V**: read an image or text from the clipboard
- **Ctrl+O**: attach an image path (you can drag a file into the prompt)
- **Tab**: move between the editor and attached images
- **Arrow keys**: select an attached image
- **Delete** / **Ctrl+X**: remove the selected image
- **F1**: help
- **Ctrl+C** / **Esc**: quit (drafts require confirmation)

Up to four PNG, JPEG, GIF, or WebP images can be attached. The composer shows
the real format, dimensions, and size before anything is uploaded.

On Wayland, install `wl-clipboard` for clipboard image paste and link copying.
X11 and XWayland use the system X11 clipboard. In SSH or headless sessions,
clipboard operations degrade gracefully and `Ctrl+O` file attachment remains
available.

## Timeline

```bash
xeet timeline
```

Use **j/k** or the arrow keys to move, **l** to like or unlike, **r** to reply
in place, **R** to refresh, **Enter** to open a post, **y** to copy its link,
and **P** to write a new post. More posts load automatically near the
bottom. Press **?** for the in-app key guide.
