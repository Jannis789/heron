package render

import (
	"context"
	"strings"
	"testing"
)

func TestRenderProducesANSIMarkup(t *testing.T) {
	out, err := Render("**fett** und `code`\n")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "fett") {
		t.Fatalf("expected text in output, got %q", out)
	}
}

func TestSplitFences(t *testing.T) {
	segs, err := splitFences("vor\n```go\nfmt.Println(x)\n```\nnach\n")
	if err != nil {
		t.Fatal(err)
	}
	var blocks, rest int
	for _, s := range segs {
		if s.block {
			blocks++
			if s.lang != "go" {
				t.Fatalf("expected lang go, got %q", s.lang)
			}
		} else {
			rest++
		}
	}
	if blocks != 1 || rest < 2 {
		t.Fatalf("blocks=%d rest=%d", blocks, rest)
	}
}

func TestRenderBlocksIssuesCopyBox(t *testing.T) {
	out, err := RenderBlocks(context.Background(), "vor\n```python\nx = 1\n```\n", Options{})
	if err != nil {
		t.Fatalf("RenderBlocks: %v", err)
	}
	if !strings.Contains(out, "[copy 1]") {
		t.Fatalf("expected copy box, got %q", out)
	}
}
