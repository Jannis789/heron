{ lib, buildGoModule, makeWrapper, plantuml, wl-clipboard, resvg }:

buildGoModule {
  pname = "heron";
  version = "0.1.0";

  src = ../.;

  # Ignoriere Test-/Doku-Dateien, nur Go-Quellen bauen.
  buildInputs = [ plantuml wl-clipboard resvg ];
  nativeBuildInputs = [ makeWrapper ];

  # Der committete vendor/-Baum ist die Quelle (go mod vendor haelt ihn
  # konsistent) — kein Vendor-Fetch-Derivation, kein Hash-Pflege.
  vendorHash = null;

  # plantuml/wl-clipboard/resvg sind Laufzeit-Werkzeuge (FR-6/FR-5/FR-27),
  # nicht nur Build-Inputs — ohne Wrap fehlen sie im PATH von "nix run".
  # resvg rastert die PlantUML-Vektoren auf die exakte Terminalgroesse
  # (0x Skalierung = scharf, Metrik 2).
  postFixup = ''
    wrapProgram $out/bin/heron --prefix PATH : ${lib.makeBinPath [ plantuml wl-clipboard resvg ]}
  '';

  meta = with lib; {
    description = "ACP-TUI (goose-Backend) mit übersteuerbarem Markdown-Rendering";
    homepage = "https://github.com/example/heron";
    license = licenses.mit;
    mainProgram = "heron";
  };
}
