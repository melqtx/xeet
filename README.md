```
██╗  ██╗███████╗███████╗████████╗
╚██╗██╔╝██╔════╝██╔════╝╚══██╔══╝
 ╚███╔╝ █████╗  █████╗     ██║
 ██╔██╗ ██╔══╝  ██╔══╝     ██║
██╔╝ ██╗███████╗███████╗   ██║
╚═╝  ╚═╝╚══════╝╚══════╝   ╚═╝
             /\_/\
            ( o.o )
             > ^ <    ready when you are ✦
```

post to x from your terminal. no api keys, no browser tab. browse your
timeline with inline images, reply, like, done.

> [!WARNING]
> xeet uses the same internal endpoints the x website uses, with your
> browser's session cookies. that probably violates x's terms of service.
> it's unofficial, not affiliated with x corp, and could break at any time.


![the xeet timeline running in a terminal: posts from the for you feed with
author, handle, and relative time, each with a full-color inline image preview
rendered in the terminal, like and reply counts underneath, and a footer
showing 1/37 with the enter, e, r, and ? key hints](docs/timeline.png)

```
╭────────────────────────────────────────╮
│ what are you thinking?                 │
╰────────────────────────────────────────╯
12/280  •  enter to xeet
```

## get it

works on macos and linux. windows isn't supported, open an issue if you
want it.

**with nix**:

```bash
nix profile install github:melqtx/xeet   # install
nix run github:melqtx/xeet               # or just try it
```

**with go** (1.26.5+, for the patched tls stack):

```bash
go install github.com/melqtx/xeet@latest
```

**from source:**

```bash
git clone https://github.com/melqtx/xeet.git
cd xeet
make install   # builds and installs to /usr/local/bin/
```

hacking on it? `nix-shell` (or `nix develop`) gets you go, gopls, and
staticcheck; `make dev` runs fmt, vet, build, and tests.

**or grab a release**: download the archive for your platform from
[releases](https://github.com/melqtx/xeet/releases), check it against
`checksums.txt`, drop the binary on your `PATH`:

```bash
tar -xzf xeet_*_$(uname -s | tr A-Z a-z)_$(uname -m | sed 's/x86_64/amd64/').tar.gz
sudo install -m 0755 xeet /usr/local/bin/
```

## use it

log into x.com in your browser (chrome, helium, firefox, brave, or zen),
then:

```bash
xeet auth
```

the picker marks which browsers it can already see an x.com session in, so
you don't have to guess:

```
where are you signed in to x.com?

  › Chrome    session found
    Helium    not detected
    Firefox   session found
```

xeet borrows that browser session. it never asks for your password. macos
may prompt for keychain access; linux may ask you to unlock gnome keyring /
secret service / kde wallet. snap and flatpak browser installs are detected,
and multiple profiles work. whatever you pick is verified with x before it
replaces a session that already works, and a failure lets you try another
browser without starting over.

scripting it? `xeet auth --browser firefox` skips the picker entirely.

then just:

```bash
xeet                                  # browse your timeline, images and all
xeet --following                      # start on the following feed
xeet --bookmarks                      # start on your bookmarks
xeet lists                            # pick a list and browse it
xeet --list 1234567890                # start on a list by id
xeet search "go tui"                  # search posts and browse results
xeet --columns 2                      # show two side-by-side feeds
xeet --barebones                      # text-only feed
xeet --compose                        # skip the feed, open the composer
xeet post "hello from my shell"       # one-shot post
echo "piped in" | xeet post           # reads stdin
xeet post "photos" -i one.png -i two.jpg
xeet post --image meme.png             # image-only, no text
xeet post "a reply" --reply 1234567890
```

if your session feels off:

```bash
xeet whoami            # which account is connected
xeet doctor            # session metadata + one authenticated read
xeet doctor --offline  # local metadata only, no network
xeet logout            # delete xeet's copy of the session
```

diagnostics print a short fingerprint and the browser/profile, never the
cookie values.

`xeet --help` groups the commands by when you need them and shows a worked
example for each; `xeet <command> --help` goes deeper on any one of them.

## the tui

**composer**

| key | does |
|---|---|
| `enter` | post |
| `alt+enter` / `ctrl+j` | line break |
| `ctrl+v` | paste image or text from clipboard |
| `ctrl+o` | attach an image path (drag a file in) |
| `tab` | switch between editor and attachments |
| `←` `→` | select an attached image |
| `delete` / `ctrl+x` | remove selected image |
| `B` | after a restricted/unclear result: open the text in x |
| `F1` | help |
| `ctrl+c` / `esc` | quit (drafts ask first) |

up to four png/jpeg/gif/webp images per post, or one mp4/mov video (up
to 512 MiB, uploaded in chunks straight from disk with live progress).
the composer shows real format, dimensions, and size before anything
uploads. unfinished drafts autosave (including clipboard images) and
come back next time.

**timeline**

| key | does |
|---|---|
| `j` / `k` / arrows | move (`ctrl+d`/`ctrl+u` jumps five) |
| `f` | switch between the for you and following feeds |
| `b` | switch between bookmarks and the for you feed |
| `L` | pick a list to browse |
| `/` | search posts |
| `enter` | open the post's replies |
| `e` / `space` | read a truncated post in full |
| `i` | zoom the post's image to the whole terminal |
| `A` | read image descriptions (works with previews off) |
| `l` | like / unlike |
| `r` | reply in place |
| `o` | open in browser |
| `y` | copy link |
| `P` | write a new post |
| `R` | refresh in place, new posts stack on top, you keep your spot |
| `?` | key guide + which image renderer is active and why |

more posts load automatically near the bottom; search results and list
timelines behave like any other feed, so like, reply, and thread all work
there too. inside a
conversation, `j`/`k` moves through replies, `r` replies to the selected
item, `R` reloads, and `esc` drops you back exactly where you were in the
timeline.

### multi-column

`--columns 2` through `--columns 4` splits the timeline into equal-width
feeds. `tab` / `shift+tab` (or `]` / `[`) moves focus; navigation and post
actions apply to the focused column. if the terminal is too narrow, xeet
shows only the columns that fit and tells you how many are hidden.

ansi previews and kitty/ghostty Unicode-placeholder images compose across
columns. iterm2 and wezterm inline images rely on relative cursor movement,
which cannot be composed safely side by side, so multi-column runs fall back
to ansi even with `--images native`. the `?` help overlay shows the fallback
note.

**themes**

colors are configurable; the layout never changes. pick from tokyonight
(default), catppuccin, gruvbox, nord, rosepine, or mono:

```bash
xeet theme             # browse the palettes with a live preview
xeet theme nord        # save a default in ~/.xeet.yaml
xeet theme --list      # just the names, current one marked
xeet --theme mono      # try one for a single run
```

`xeet theme` on its own opens a picker that recolors itself as you move:
swatches, a couple of sample posts, and the composer footer, all in the
palette under the cursor. `enter` saves it, `esc` keeps what you had.

<details>
<summary>details: how images render</summary>

images are prefetched around your position so scrolling lands on
already-loaded previews. videos and gifs show their poster frame with a `▶`
chip.

- direct ghostty and kitty sessions use kitty unicode-placeholder graphics
- iterm2 and wezterm use the iterm2 inline-image protocol
- tmux 3.3+ gets native kitty graphics too when `allow-passthrough` is on
  and the terminal running tmux is kitty or ghostty; otherwise (and in
  zellij) the portable ansi renderer takes over

in auto mode xeet verifies kitty graphics before trusting them: apps that
merely embed libghostty (cmux, for one) inherit a ghostty `TERM` but don't
actually render placeholder graphics, so on macos xeet checks the host
app's bundle id and falls back to ansi inside embedders. a startup probe
also asks the terminal itself to confirm the protocol, so anything that
advertises graphics it can't load falls back instead of leaving blank gaps.
if a frame ever glitches, `ctrl+l` redraws.

pick a renderer explicitly when needed:

```bash
xeet timeline --images auto    # native when the terminal confirms it; else ansi
xeet timeline --images native  # trust the terminal, skip the probe
xeet timeline --images ansi    # portable block preview
xeet timeline --images off     # no previews
```

on wayland, install `wl-clipboard` for clipboard paste and link copying.
x11/xwayland use the x11 clipboard. over ssh, clipboard degrades gracefully
and `ctrl+o` file attachment still works.

</details>

## how it works

xeet reuses the x.com session already in your browser and speaks the same
unsupported internal graphql endpoints the website does. the imported
`auth_token` and `ct0` cookies grant account-level access, so treat them
like a password. they live in the macos keychain or linux secret service, never
in the yaml config file. `xeet logout` deletes xeet's copy (your browser
stays logged in).

<details>
<summary>details: query ids and retries</summary>

x rotates its internal graphql operation ids periodically; xeet discovers
and caches current ids for posting, timelines, likes, and unlikes. if
CreateTweet discovery fails, xeet explains how to set the id manually from
browser devtools.

CreateTweet mutations are never retried after a transient or ambiguous
outcome. (a request explicitly rejected as an unknown persisted-query id is
refreshed and sent once with the current id.) if x returns an unclear
result, xeet performs one read-only timeline check; if it still can't prove
the post landed, it preserves your draft and asks you to check your profile
before retrying. the `details` line contains only a response-shape
fingerprint, status, type names, and rate-limit metadata, never the draft
or session cookies.

to compare a successful browser post with xeet's request without exposing
secrets, export a har from devtools and run `xeet inspect-har file.har`.
it prints names and structural keys only, never values, post text, or
response bodies. keep the har local; it contains session cookies.

</details>

## wontfix

no scheduling, no bulk posting, no scraping, no automated engagement, no
mass-posting. out of scope, will not be added.

---

[MIT](LICENSE) · not affiliated with x corp · bugs go [here](https://github.com/melqtx/xeet/issues) · security goes [here](SECURITY.md)
