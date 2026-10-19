{ pkgs }:

pkgs.mkShell {
  packages = with pkgs; [
    go
    plantuml
    wl-clipboard
    resvg
    dejavu_fonts
  ];

  # Paritaet zum nix-run-Wrap (s. nix/heron.nix postFixup): ohne die Env
  # rastert resvg im dev-shell-Pfad Diagramme ohne Text (NixOS hat keine
  # /usr/share/fonts fuer resvgs Font-DB).
  HERON_RESVG_FONTS = "${pkgs.dejavu_fonts}/share/fonts";
  HERON_RESVG_FONT = "DejaVu Sans";

  GOTOOLCHAIN = "local";

  shellHook = ''
    echo "heron dev-shell: go test ./... | go run ."
  '';
}
