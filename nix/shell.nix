{ pkgs }:

pkgs.mkShell {
  packages = with pkgs; [
    go
    plantuml
    wl-clipboard
  ];

  GOTOOLCHAIN = "local";

  shellHook = ''
    echo "heron dev-shell: go test ./... | go run ."
  '';
}
