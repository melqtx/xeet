{ lib
, buildGoModule
, wl-clipboard
, libx11
, makeWrapper
, stdenv
}:

buildGoModule (finalAttrs: {
  pname = "xeet";
  # Keep in step with the latest release tag.
  version = "0.1.11";

  src = lib.cleanSource ./.;

  # Regenerate whenever go.mod or go.sum changes (including on dependabot
  # bumps): set this to lib.fakeHash, run `nix build .#default`, and copy the
  # hash nix reports. CI's nix job fails when it goes stale.
  vendorHash = "sha256-Hij56rK+qINQYCjGdLUgM/5N7e0XAfh/zvcOYDs6gek=";

  # main.go reads these through -X; without them the binary reports "dev".
  ldflags = [
    "-s"
    "-w"
    "-X main.version=${finalAttrs.version}"
    "-X main.commit=nix"
    "-X main.buildTime=unknown"
  ];

  nativeBuildInputs = [ makeWrapper ];

  # golang.design/x/clipboard compiles X11 through cgo, so the linux build
  # needs Xlib's headers. Without them the package built on darwin and failed
  # on linux.
  buildInputs = lib.optionals stdenv.isLinux [ libx11 ];

  # The clipboard links only -ldl and dlopens "libX11.so" at runtime, so
  # nothing records libx11 in the binary's rpath and the lookup would fail on
  # a machine without X11 in the default loader paths. LD_LIBRARY_PATH points
  # it at the unversioned symlink in libx11's lib directory. Wayland shells
  # out to wl-paste/wl-copy instead; macOS uses the system pasteboard.
  postInstall = lib.optionalString stdenv.isLinux ''
    wrapProgram $out/bin/xeet \
      --prefix PATH : ${lib.makeBinPath [ wl-clipboard ]} \
      --prefix LD_LIBRARY_PATH : ${lib.makeLibraryPath [ libx11 ]}
  '';

  meta = {
    description = "Post to and read X from the terminal, using your browser session";
    homepage = "https://github.com/melqtx/xeet";
    license = lib.licenses.mit;
    mainProgram = "xeet";
    platforms = lib.platforms.unix;
  };
})
