{ lib, buildGoModule, plantuml, wl-clipboard }:

buildGoModule {
  pname = "heron";
  version = "0.1.0";

  src = ../.;

  # Ignoriere Test-/Doku-Dateien, nur Go-Quellen bauen.
  buildInputs = [ plantuml wl-clipboard ];

  # TODO: einmal ermitteln und eintragen:
  #   nix build --no-sandbox .#default   (druckt den erwarteten Hash)
  # bzw. `nix flake metadata` / in die nix eine feste sha256 schreiben.
  vendorHash = null;

  meta = with lib; {
    description = "ACP-TUI (goose-Backend) mit übersteuerbarem Markdown-Rendering";
    homepage = "https://github.com/example/heron";
    license = licenses.mit;
    mainProgram = "heron";
  };
}
