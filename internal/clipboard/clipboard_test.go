package clipboard

import "testing"

func TestOsc52Frame(t *testing.T) {
	w := osc52Frame("hi")
	expected := "\x1b]52;c;aGk=\x07"
	if w != expected {
		t.Fatalf("got %q want %q", w, expected)
	}
}
