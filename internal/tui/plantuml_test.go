package tui

import (
	"os/exec"
	"strings"
	"testing"
)

// findCode returns the code block carrying the stable id.
func findCode(t *testing.T, l BlockList, id int) CodeBlock {
	t.Helper()
	for _, it := range l.items {
		if cb, ok := it.(CodeBlock); ok && cb.id == id {
			return cb
		}
	}
	t.Fatalf("no code block with id %d", id)
	return CodeBlock{}
}

// TestPlantUMLJobsCollectedOnce (FR-6): only unconverted plantuml blocks are
// queued, and a failed conversion is never queued again.
func TestPlantUMLJobsCollectedOnce(t *testing.T) {
	r := newRig(t).seed("```plantuml\nAlice -> Bob: hi\n```")
	jobs := r.m.diagramJobs()
	if len(jobs) != 1 {
		t.Fatalf("jobs=%d want 1", len(jobs))
	}
	if jobs[0].id < 1 || strings.TrimSpace(jobs[0].code) == "" {
		t.Fatalf("bad job: %+v", jobs[0])
	}

	l, n := r.m.chat.applyDiagrams([]diagramResult{{id: jobs[0].id, ok: false}})
	if n != 1 {
		t.Fatalf("applied=%d want 1", n)
	}
	r.m.chat = l
	if again := r.m.diagramJobs(); len(again) != 0 {
		t.Fatalf("failed block queued again: %v", again)
	}
}

// TestPlantUMLApplyReplacesSource (FR-6): a successful conversion replaces
// the raw source in place, keeping the stable id.
func TestPlantUMLApplyReplacesSource(t *testing.T) {
	r := newRig(t).seed("```plantuml\nAlice -> Bob: hi\n```")
	id := r.m.diagramJobs()[0].id

	ascii := "+---+     +---+\n| A | --> | B |\n+---+     +---+"
	l, n := r.m.chat.applyDiagrams([]diagramResult{{id: id, ascii: ascii, ok: true}})
	if n != 1 {
		t.Fatalf("applied=%d want 1", n)
	}
	cb := findCode(t, l, id)
	if !cb.converted {
		t.Fatal("block not marked converted")
	}
	if !strings.Contains(cb.code, "+---+") {
		t.Fatalf("source not replaced: %q", cb.code)
	}
	if cb.id != id {
		t.Fatalf("id changed: %d want %d", cb.id, id)
	}
}

// TestPlantUMLNoPendingReturnsNil: no work -> no goroutine.
func TestPlantUMLNoPendingReturnsNil(t *testing.T) {
	r := newRig(t).seed("```go\nx := 1\n```")
	if cmd := r.m.pendingDiagrams(); cmd != nil {
		t.Fatal("expected nil cmd when nothing is pending")
	}
}

// TestPlantUMLFallbackWhenMissing (FR-6): without plantuml on PATH the
// conversion fails and the raw source stays visible — no crash, no empty box.
func TestPlantUMLFallbackWhenMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	r := newRig(t).seed("```plantuml\nAlice -> Bob: hi\n```")
	id := r.m.diagramJobs()[0].id
	before := findCode(t, r.m.chat, id).code

	cmd := r.m.pendingDiagrams()
	if cmd == nil {
		t.Fatal("expected a conversion cmd")
	}
	done, ok := cmd().(diagramsDoneMsg)
	if !ok {
		t.Fatalf("msg type %T", cmd())
	}
	if len(done.results) != 1 || done.results[0].ok {
		t.Fatalf("expected one failed result, got %+v", done.results)
	}

	l, n := r.m.chat.applyDiagrams(done.results)
	if n != 1 {
		t.Fatalf("applied=%d", n)
	}
	if got := findCode(t, l, id).code; got != before {
		t.Fatalf("raw source changed on failure:\nbefore %q\nafter  %q", before, got)
	}
	if jobs := (Model{chat: l}).diagramJobs(); len(jobs) != 0 {
		t.Fatalf("still pending after failure: %v", jobs)
	}
}

// TestPlantUMLRealConversion runs the actual plantuml binary when available —
// this is the FR-6 acceptance path (halfblock image alongside raw source).
func TestPlantUMLRealConversion(t *testing.T) {
	if _, err := exec.LookPath("plantuml"); err != nil {
		t.Skip("plantuml not installed")
	}
	r := newRig(t).seed("```plantuml\n@startuml\nAlice -> Bob: hi\n@enduml\n```")
	cmd := r.m.pendingDiagrams()
	if cmd == nil {
		t.Fatal("expected a conversion cmd")
	}
	done, ok := cmd().(diagramsDoneMsg)
	if !ok {
		t.Fatalf("msg type %T", cmd())
	}
	if len(done.results) != 1 {
		t.Fatalf("results=%d want 1", len(done.results))
	}
	if !done.results[0].ok {
		t.Fatalf("plantuml failed: %q", done.results[0].ascii)
	}
}
