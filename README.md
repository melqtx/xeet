# Xeet v0.1.8-alpha

A simple terminal interface for posting to and browsing X without developer
API keys.

> [!WARNING]
> Xeet uses unsupported internal X web endpoints and browser-session cookies.
> This may violate X's Terms of Service. Xeet is unofficial and is not
> affiliated with, endorsed by, or supported by X Corp.

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

- macOS and Linux: Chrome, Helium, Firefox, Brave, or Zen
- Linux installs from native packages, Snap, and common Flatpak locations are
  detected. Multiple browser profiles are supported.

`xeet auth` imports that browser session without asking for your X password.
macOS may request Keychain access. Linux may request that you unlock GNOME
Keyring, Secret Service, or KDE Wallet.

If a saved session behaves differently from the browser, inspect it without
posting:

```bash
xeet whoami            # show the account connected to the saved session
xeet doctor            # metadata plus one authenticated timeline read
xeet doctor --offline  # local metadata only
```

The diagnostic prints a short session fingerprint and the selected
browser/profile, never the cookie values.

To compare a successful browser post with Xeet without exposing the HAR's
contents:

```bash
xeet inspect-har /path/to/x.com.har
```

Export the HAR from browser developer tools after one successful website post.
Keep the HAR local because the file contains session cookies. The command
prints names and structural keys only, never values, post text, or response
bodies.

**2. Post:**

```bash
xeet                                  # browse your home timeline with images
xeet --barebones                      # browse a text-only timeline
xeet --compose                        # open only the interactive composer
xeet timeline                         # same timeline, with renderer controls
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
out of X. Run `xeet auth` again whenever you want to reconnect.

X rotates internal GraphQL operation ids periodically; xeet discovers and
caches current ids for posting, timelines, likes, and unlikes. If CreateTweet
discovery fails, Xeet explains how to set it manually from the `CreateTweet`
request in browser developer tools.

CreateTweet mutations are never retried after a transient or ambiguous outcome.
(A request explicitly rejected as an unknown persisted-query id is refreshed
and sent once with the current id.) If X returns an unclear result, Xeet
performs one read-only timeline check. When it still cannot prove whether the
post landed, it preserves the draft and asks you to check your profile before
retrying. The accompanying `details` line contains only a response-shape
fingerprint, status, type names, and rate-limit metadata; it never contains the
draft or session cookies.

## TUI keys

- **Enter**: post
- **Alt+Enter** / **Ctrl+J**: line break
- **Ctrl+V**: read an image or text from the clipboard
- **Ctrl+O**: attach an image path (you can drag a file into the prompt)
- **Tab**: move between the editor and attached images
- **Arrow keys**: select an attached image
- **Delete** / **Ctrl+X**: remove the selected image
- **B** after a restricted or unclear result: open the text in X
- **F1**: help
- **Ctrl+C** / **Esc**: quit (drafts require confirmation)

Up to four PNG, JPEG, GIF, or WebP images can be attached. The composer shows
the real format, dimensions, and size before anything is uploaded.

Unfinished composer drafts are autosaved and offered again the next time the
composer opens. File attachments are restored from their original paths;
clipboard images are copied into a private local draft directory so they can be
restored too. A successful post or an explicit discard removes the saved draft.

On Wayland, install `wl-clipboard` for clipboard image paste and link copying.
X11 and XWayland use the system X11 clipboard. In SSH or headless sessions,
clipboard operations degrade gracefully and `Ctrl+O` file attachment remains
available.

## Timeline

```bash
xeet                  # automatic image previews
xeet --barebones      # simple text-only feed
xeet --compose        # skip the feed and open the composer
xeet timeline         # explicit timeline command
```

Use **j/k** or the arrow keys to move (**Ctrl+D/U** jumps five posts),
**Enter** to open the selected post's replies, **e** or **Space** to read a
truncated post in full, **i** to zoom the selected post's
image to the whole terminal, **A** to read descriptions for all attached images
(even when previews are off), **l** to like or unlike, **r** to reply in
place, **o** to open the post in the browser, **y** to copy its link,
and **P** to write a new post. **R** refreshes in place: new posts stack on
top while you keep your position. More posts load automatically near the
bottom. Press **?** for the in-app key guide.

Inside a conversation, **j/k** moves through the replies, **r** replies to the
selected item, **R** reloads the conversation, and **Esc** returns to the same
place in the home timeline. Xeet reads conversations through the same
unsupported web endpoints as the rest of the timeline; they are read-only
requests and never retried as mutations.

Images are prefetched around your position in the feed and rendered inline
for nearby posts, so scrolling lands on already-loaded previews. Videos and
GIFs show their poster frame with a `▶` chip. Press `?` to see which image
renderer is active and why. Direct Ghostty and Kitty sessions use Kitty Unicode-placeholder
images. iTerm2 and WezTerm use the iTerm2 inline-image protocol. Zellij and
tmux use the portable ANSI renderer because they do not reliably pass these
graphics protocols through.

In auto mode xeet verifies Kitty graphics before committing to them: apps
that merely embed libghostty (cmux, for one) inherit a ghostty `TERM` but
don't reliably render placeholder graphics, so on macOS xeet checks the host
app's bundle identifier and falls back to ANSI inside embedders. A startup
probe additionally asks the terminal itself to confirm the protocol, so any
terminal that advertises graphics it cannot load also falls back instead of
leaving blank gaps where images should be. If a frame ever glitches, `ctrl+l`
redraws the screen.

Choose the renderer explicitly when needed:

```bash
xeet timeline --images auto    # native when confirmed by the terminal; otherwise ANSI
xeet timeline --images native  # trust the terminal: skip the probe, use its native backend
xeet timeline --images ansi    # portable block preview
xeet timeline --images off     # disable previews
```
