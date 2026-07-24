{ lib
, buildGoModule
, installShellFiles
, xclip
, wl-clipboard
, makeWrapper
, stdenv
}:

buildGoModule (finalAttrs: {
  pname = "xeet";
  version = "0.1.8";

  src = lib.cleanSource ./.;

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

  # Clipboard attachments shell out to xclip/wl-clipboard on Linux; put them
  # on PATH so a nix-installed xeet works outside a dev shell. macOS uses the
  # system pasteboard and needs nothing.
  postInstall = lib.optionalString stdenv.isLinux ''
    wrapProgram $out/bin/xeet \
      --prefix PATH : ${lib.makeBinPath [ xclip wl-clipboard ]}
  '';

  meta = {
    description = "Post to and read X from the terminal, using your browser session";
    homepage = "https://github.com/melqtx/xeet";
    license = lib.licenses.mit;
    mainProgram = "xeet";
    platforms = lib.platforms.unix;
  };
})
