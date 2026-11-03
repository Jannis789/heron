package tui

import (
	"strings"
	"testing"
)

// TestToolCardStructuredBody: die echten goose-Frames (rawInput/rawOutput)
// rendert die Karte strukturiert — $-Zeile mit dem KOMPLETTEN Kommando
// (goose kuerzt den Titel), stdout, ⚠-Zeilen fuer stderr und exit-N beim
// Fehlschlag. Vorher fielen diese Felder weg: die Karten blieben leer.
func TestToolCardStructuredBody(t *testing.T) {
	r := newRig(t)
	r.do(toolMsg{callID: "c1", name: "shell", kind: "execute",
		cmd: "ls -la /tmp && cat hello.txt", done: false})
	r.do(toolMsg{callID: "c1", name: "shell", kind: "execute",
		cmd: "ls -la /tmp && cat hello.txt",
		out: "total 4\nhello.txt", errOut: "cat: hello.txt: No such file",
		exit: 1, hasOut: true, done: true, failed: true})
	r.contains("$ ls -la /tmp && cat hello.txt").
		contains("hello.txt").
		contains("⚠ cat: hello.txt: No such file").
		contains("exit 1").
		contains("$ shell ✗")
}

// TestToolCardNoOutput: finales Update mit leerem rawOutput zeigt
// „(keine Ausgabe)“ statt einer inhaltsleeren Karte (goose schickt in dem
// Fall den Text „(no output)“ nur im content, nicht im rawOutput).
func TestToolCardNoOutput(t *testing.T) {
	r := newRig(t)
	r.do(toolMsg{callID: "c", name: "shell", kind: "execute",
		cmd: "true", hasOut: true, done: true})
	r.contains("(keine Ausgabe)").contains("$ shell ✓")
}

// TestToolCardLiveAccumulates: live_output-Chunks arrivieren verteilt auf
// mehrere tool_call_update-Frames — der Model akkumuliert sie je callID,
// das finale Update loest den Akkumulator wieder auf (kein Doppel).
func TestToolCardLiveAccumulates(t *testing.T) {
	r := newRig(t)
	r.do(toolMsg{callID: "c", name: "shell", kind: "execute",
		cmd: "build", live: "step 1\n", done: false})
	r.contains("step 1")
	r.do(toolMsg{callID: "c", name: "shell", kind: "execute",
		cmd: "build", live: "step 2\n", done: false})
	r.contains("step 1").contains("step 2")
	// Final: rawOutput ersetzt den Live-Puffer komplett.
	r.do(toolMsg{callID: "c", name: "shell", kind: "execute",
		cmd: "build", out: "ok", hasOut: true, done: true})
	r.contains("ok").notContains("step 1").notContains("step 2")
	if r.m.toolLive["c"] != "" {
		t.Fatalf("finaler Update raeumt den Live-Akkumulator: %q", r.m.toolLive["c"])
	}
}

// TestToolCardCopyCarriesStructure: der Copy-Payload (Markdown) enthaelt
// $-Zeile, Output, stderr und exit — nicht nur den Titel.
func TestToolCardCopyCarriesStructure(t *testing.T) {
	r := newRig(t)
	r.do(toolMsg{callID: "c", name: "shell", kind: "execute",
		cmd: "make test", out: "PASS", errOut: "warn", exit: 2,
		hasOut: true, done: true, failed: true})
	r.key("tab") // Fokus auf die Karte (letztes Segment) — c kopiert sie
	r.key("c")
	got := r.copiedAll()
	if len(got) == 0 {
		t.Fatal("copy payload fehlt")
	}
	last := got[len(got)-1]
	for _, want := range []string{"$ make test", "PASS", "⚠ warn", "exit 2"} {
		if !strings.Contains(last, want) {
			t.Fatalf("copy payload muss %q enthalten: %q", want, last)
		}
	}
}
