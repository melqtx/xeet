{ lib
, buildGoModule
, wl-clipboard
, xorg
, makeWrapper
, stdenv
}:

buildGoModule (finalAttrs: {
  pname = "xeet";
  # Keep in step with the latest release tag.
  version = "0.1.8";

  src = lib.cleanSource ./.;

  # Regenerate whenever go.mod or go.sum changes (including on dependabot
  # bumps): set this to lib.fakeHash, run `nix build .#default`, and copy the
  # hash nix reports. CI's nix job fails when it goes stale.
  vendorHash = "sha256-/Qy+oPK4BzNMl2xqVwNKdEzZ9N3zTpSzzmLCTKNV8z0=";

  # main.go reads these through -X; without them the binary reports "dev".
  ldflags = [
    "-s"
    "-w"
    "-X main.version=${finalAttrs.version}"
    "-X main.commit=nix"
    "-X main.buildTime=unknown"
  ];

  nativeBuildInputs = [ makeWrapper ];

  # golang.design/x/clipboard talks to X11 through cgo, so the linux build
  # needs Xlib to compile at all. Without it the package built on darwin and
  # failed on linux.
  buildInputs = lib.optionals stdenv.isLinux [ xorg.libX11 ];

  # On wayland the clipboard shells out to wl-paste/wl-copy, so keep those on
  # PATH for an installed xeet. X11 goes through the linked Xlib above, and
  # macOS uses the system pasteboard.
  postInstall = lib.optionalString stdenv.isLinux ''
    wrapProgram $out/bin/xeet \
      --prefix PATH : ${lib.makeBinPath [ wl-clipboard ]}
  '';

  meta = {
    description = "Post to and read X from the terminal, using your browser session";
    homepage = "https://github.com/melqtx/xeet";
    license = lib.licenses.mit;
    mainProgram = "xeet";
    platforms = lib.platforms.unix;
  };
})
