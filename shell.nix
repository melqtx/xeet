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
    # go.mod may pin a patch release newer than the one in nixpkgs. Leaving
    # GOTOOLCHAIN on auto lets the go command fetch the exact toolchain
    # instead of refusing to build.
    export GOTOOLCHAIN=auto
    export GOPATH="''${GOPATH:-$HOME/go}"
    export PATH="$GOPATH/bin:$PATH"
  '';
}
