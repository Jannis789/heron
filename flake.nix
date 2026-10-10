{
  description = "heron — ACP-TUI (goose-Backend) mit übersteuerbarem Markdown-Rendering";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils, ... }:
    {
      # Overlay: macht pkgs.heron in jedem System verfügbar.
      overlays.default = final: prev: {
        heron = final.callPackage ./nix/heron.nix { };
      };
    }
    // flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ self.overlays.default ];
        };
      in
      {
        packages.default = pkgs.heron;
        packages.heron = pkgs.heron;
        devShells.default = import ./nix/shell.nix { inherit pkgs; };
      }) // { }
  ;
}
