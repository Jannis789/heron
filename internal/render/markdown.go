// Package render is heron's overridable rendering layer: markdown fences are
// extracted and rendered as CodeBlocks (copy box + per-language routing); the
// remaining markdown goes through glamour.
package render

import (
	"context"
	"strings"

	"github.com/charmbracelet/glamour"
)

type segment struct {
	block bool
	lang  string
	code  string
	text  string
	index int
}

// Options controls rendering overrides.
type Options struct {
	RenderPlantUML bool
}

// Render renders plain markdown through glamour.
func Render(md string) (string, error) {
	r, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(100))
	if err != nil {
		return "", err
	}
	return r.Render(md)
}

// RenderBlocks renders markdown with fenced blocks as CodeBlocks. PlantUML
// blocks are rendered to ASCII when possible, otherwise shown raw.
func RenderBlocks(ctx context.Context, md string, opts Options) (string, error) {
	segs, err := splitFences(md)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, s := range segs {
		if !s.block {
			part, err := Render(s.text)
			if err != nil {
				return "", err
			}
			b.WriteString(part)
			continue
		}
		code := s.code
		if opts.RenderPlantUML && s.lang == "plantuml" {
			if ascii, err := PlantUMLToASCII(ctx, s.code); err == nil {
				code = ascii
			}
		}
		b.WriteString(CodeBlockView(s.lang, code, s.index))
	}
	return b.String(), nil
}

func splitFences(md string) ([]segment, error) {
	lines := strings.Split(md, "\n")
	var segs []segment
	var cur strings.Builder
	var inFence bool
	var lang string
	var i int
	flush := func() {
		if cur.Len() > 0 {
			segs = append(segs, segment{text: cur.String()})
			cur.Reset()
		}
	}
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "```") {
			if !inFence {
				flush()
				lang = strings.TrimSpace(strings.TrimPrefix(trim, "```"))
				inFence = true
			} else {
				code := strings.TrimSuffix(cur.String(), "\n")
				i++
				segs = append(segs, segment{block: true, lang: lang, code: code, index: i})
				cur.Reset()
				inFence = false
			}
			continue
		}
		cur.WriteString(ln + "\n")
	}
	flush()
	return segs, nil
}
