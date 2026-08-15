{ lib
, buildGoModule
, fetchurl
, go_1_26
, wl-clipboard
, libx11
, makeWrapper
, stdenv
}:

let
  go_1_26_6 = go_1_26.overrideAttrs (_: {
    version = "1.26.6";
    src = fetchurl {
      url = "https://go.dev/dl/go1.26.6.src.tar.gz";
      hash = "sha256-oHIcVMaIkBRI13rZs+x+p8R0cwdV/4kTgukuy5P/LLE=";
    };
  });
in
(buildGoModule.override { go = go_1_26_6; }) (finalAttrs: {
  pname = "xeet";
  # Keep in step with the latest release tag.
  version = "0.1.11";

  src = lib.cleanSource ./.;

  # Regenerate whenever go.mod or go.sum changes (including on dependabot
  # bumps): set this to lib.fakeHash, run `nix build .#default`, and copy the
  # hash nix reports. CI's nix job fails when it goes stale.
  vendorHash = "sha256-OD+Zi8PgMtYPYeCTHC3L2AO20MrL5bkHvsTkR3Hs0D0=";

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
