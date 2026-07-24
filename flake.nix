{
  description = "Post to and read X from the terminal, using your browser session";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      # xeet targets macos and linux; enumerate the systems directly rather
      # than pulling in flake-utils for one helper.
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f:
        nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: {
        default = pkgs.callPackage ./default.nix { };
      });

      devShells = forAllSystems (pkgs: {
        default = import ./shell.nix { inherit pkgs; };
      });

      checks = forAllSystems (pkgs: {
        default = pkgs.callPackage ./default.nix { };
      });
    };
}
