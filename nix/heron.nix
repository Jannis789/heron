{ lib, buildGoModule, makeWrapper, plantuml, wl-clipboard }:

buildGoModule {
  pname = "heron";
  version = "0.1.0";

  src = ../.;

  # Ignoriere Test-/Doku-Dateien, nur Go-Quellen bauen.
  buildInputs = [ plantuml wl-clipboard ];
  nativeBuildInputs = [ makeWrapper ];

  # Der committete vendor/-Baum ist die Quelle (go mod vendor haelt ihn
  # konsistent) — kein Vendor-Fetch-Derivation, kein Hash-Pflege.
  vendorHash = null;

  # plantuml/wl-clipboard sind Laufzeit-Werkzeuge (FR-6/FR-5), nicht nur
  # Build-Inputs — ohne Wrap fehlen sie im PATH von "nix run".
  postFixup = ''
    wrapProgram $out/bin/heron --prefix PATH : ${lib.makeBinPath [ plantuml wl-clipboard ]}
  '';

  meta = with lib; {
    description = "ACP-TUI (goose-Backend) mit übersteuerbarem Markdown-Rendering";
    homepage = "https://github.com/example/heron";
    license = licenses.mit;
    mainProgram = "heron";
  };
}
