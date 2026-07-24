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
    # The wayland clipboard shells out to these at runtime; macOS uses the
    # system pasteboard instead.
    wl-clipboard
  ];

  # `go build` on linux compiles the cgo clipboard, which needs Xlib's
  # headers. Without them the dev shell cannot build the project at all.
  buildInputs = pkgs.lib.optionals pkgs.stdenv.isLinux [ pkgs.libx11 ];

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
