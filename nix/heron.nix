{ lib, buildGoModule, plantuml, wl-clipboard }:

buildGoModule {
  pname = "heron";
  version = "0.1.0";

  src = ../.;

  # Ignoriere Test-/Doku-Dateien, nur Go-Quellen bauen.
  buildInputs = [ plantuml wl-clipboard ];

  # Vendor-Hash der Go-Abhängigkeiten (go.sum). Update: lib.fakeHash
  # eintragen, `nix build .#default` druckt den "got:"-Hash.
  vendorHash = "sha256-tYAFOyAb4jtYXzLi1AOPcNyfExdshs0cJdQPTPpXLhM=";

  meta = with lib; {
    description = "ACP-TUI (goose-Backend) mit übersteuerbarem Markdown-Rendering";
    homepage = "https://github.com/example/heron";
    license = licenses.mit;
    mainProgram = "heron";
  };
}
