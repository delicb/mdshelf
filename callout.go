package main

import (
	"io"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var kindCallout = ast.NewNodeKind("Callout")

var calloutTitles = map[string]string{
	"NOTE":      "Note",
	"TIP":       "Tip",
	"IMPORTANT": "Important",
	"WARNING":   "Warning",
	"CAUTION":   "Caution",
}

type callout struct {
	ast.BaseBlock
	calloutType string
	sourceStart int
	sourceStop  int
}

func (n *callout) Kind() ast.NodeKind { return kindCallout }

func (n *callout) reviewSourceRange() (int, int) { return n.sourceStart, n.sourceStop }

func (n *callout) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Type": n.calloutType}, nil)
}

type calloutTransformer struct{}

func (calloutTransformer) Transform(document *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	var quotes []*ast.Blockquote
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		quote, ok := node.(*ast.Blockquote)
		if ok {
			quotes = append(quotes, quote)
		}
		return ast.WalkContinue, nil
	})

	for _, quote := range quotes {
		paragraph, ok := quote.FirstChild().(*ast.Paragraph)
		if !ok || paragraph.Lines().Len() == 0 {
			continue
		}
		firstLine := paragraph.Lines().At(0)
		marker := strings.ToUpper(strings.TrimSpace(string(firstLine.Value(source))))
		if len(marker) < 4 || marker[:2] != "[!" || marker[len(marker)-1] != ']' {
			continue
		}
		calloutType := marker[2 : len(marker)-1]
		if _, ok := calloutTitles[calloutType]; !ok {
			continue
		}

		for child := paragraph.FirstChild(); child != nil && child.Pos() < firstLine.Stop; {
			next := child.NextSibling()
			paragraph.RemoveChild(paragraph, child)
			child = next
		}
		if paragraph.FirstChild() == nil {
			quote.RemoveChild(quote, paragraph)
		}

		start, stop, _ := blockSourceBounds(quote, source)
		node := &callout{calloutType: strings.ToLower(calloutType), sourceStart: start, sourceStop: stop}
		for child := quote.FirstChild(); child != nil; {
			next := child.NextSibling()
			quote.RemoveChild(quote, child)
			node.AppendChild(node, child)
			child = next
		}
		quote.Parent().ReplaceChild(quote.Parent(), quote, node)
	}
}

type calloutRenderer struct{}

func (calloutRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(kindCallout, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		callout := node.(*callout)
		if entering {
			title := calloutTitles[strings.ToUpper(callout.calloutType)]
			_, _ = io.WriteString(w, `<aside class="callout callout-`+callout.calloutType+`" aria-label="`+title+`">`)
			_, _ = io.WriteString(w, `<p class="callout-title">`+title+`</p>`)
		} else {
			_, _ = io.WriteString(w, "</aside>\n")
		}
		return ast.WalkContinue, nil
	})
}

func calloutOptions() []goldmark.Option {
	return []goldmark.Option{
		goldmark.WithParserOptions(parser.WithASTTransformers(util.Prioritized(calloutTransformer{}, 120))),
		goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(calloutRenderer{}, 120))),
	}
}
