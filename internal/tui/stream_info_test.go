package tui

import "testing"

func TestStreamBuffersUntilAttach(t *testing.T) {
	var s Stream
	s.publish(chunkMsg{text: "x"})
	s.Info("glm-5.3", "max")

	s.mu.Lock()
	n := len(s.pending)
	s.mu.Unlock()
	if n != 2 {
		t.Fatalf("pending = %d, want 2", n)
	}

	got := s.takePending()
	if len(got) != 2 {
		t.Fatalf("takePending = %d, want 2", len(got))
	}
	if im, ok := got[1].(infoMsg); !ok || im.model != "glm-5.3" || im.effort != "max" {
		t.Fatalf("second pending = %#v, want infoMsg glm-5.3/max", got[1])
	}

	s.mu.Lock()
	n = len(s.pending)
	s.mu.Unlock()
	if n != 0 {
		t.Fatalf("pending after drain = %d, want 0", n)
	}
}
