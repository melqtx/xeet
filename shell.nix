{ pkgs ? import <nixpkgs> { } }:

pkgs.mkShell {
  name = "xeet";

  packages = with pkgs; [
    go
    gopls
    go-tools # staticcheck, which CI runs
    gnumake
    git
  ] ++ lib.optionals stdenv.isLinux [
    # Clipboard attachments shell out to these at runtime; macOS uses the
    # system pasteboard instead.
    xclip
    wl-clipboard
  ];

  shellHook = ''
    # An older nixpkgs channel may carry a go patch release behind go.mod's;
    # the pinned version matters because it usually carries stdlib security
    # fixes. Leaving GOTOOLCHAIN on auto lets the go command fetch the exact
    # toolchain rather than silently building against an older one.
    export GOTOOLCHAIN=auto
    export GOPATH="''${GOPATH:-$HOME/go}"
    export PATH="$GOPATH/bin:$PATH"
  '';
}
