package render

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// RenderText parses Markdown with goldmark and renders text nodes plus known
// block appliers (tables for now) in the theme style. Per-block appliers
// (code/copy-box etc.) come later; this validates text, tables + theme JSON.
func RenderText(src string, th *Theme) (string, error) {
	if th == nil {
		return src, nil
	}
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	doc := md.Parser().Parse(text.NewReader([]byte(src)))
	style := th.TextStyle()
	var b strings.Builder
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := n.(type) {
		case *extast.Table:
			b.WriteString(renderTable(t, []byte(src), th))
			b.WriteString("\n")
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			if t.SoftLineBreak() {
				b.WriteByte('\n')
				return ast.WalkContinue, nil
			}
			if seg := t.Text([]byte(src)); len(seg) > 0 {
				b.WriteString(style.Render(string(seg)))
			}
		}
		return ast.WalkContinue, nil
	})
	return b.String(), nil
}
