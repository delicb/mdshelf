package main

import (
	"bytes"
	"io"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var kindMermaidBlock = ast.NewNodeKind("MermaidBlock")

type mermaidBlock struct {
	ast.BaseBlock
	source []byte
}

func (n *mermaidBlock) Kind() ast.NodeKind { return kindMermaidBlock }

func (n *mermaidBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type mermaidTransformer struct{}

func (mermaidTransformer) Transform(document *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	var blocks []*ast.FencedCodeBlock
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		block, ok := node.(*ast.FencedCodeBlock)
		if ok && bytes.EqualFold(block.Language(source), []byte("mermaid")) {
			blocks = append(blocks, block)
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	for _, block := range blocks {
		var value []byte
		for i := 0; i < block.Lines().Len(); i++ {
			line := block.Lines().At(i)
			value = append(value, line.Value(source)...)
		}
		node := &mermaidBlock{source: value}
		block.Parent().ReplaceChild(block.Parent(), block, node)
	}
}

type mermaidRenderer struct{}

func (mermaidRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(kindMermaidBlock, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		block := node.(*mermaidBlock)
		_, _ = io.WriteString(w, `<pre class="mermaid">`)
		_, _ = w.Write(util.EscapeHTML(block.source))
		_, _ = io.WriteString(w, "</pre>\n")
		return ast.WalkSkipChildren, nil
	})
}

func mermaidOptions() []goldmark.Option {
	return []goldmark.Option{
		goldmark.WithParserOptions(parser.WithASTTransformers(util.Prioritized(mermaidTransformer{}, 100))),
		goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(mermaidRenderer{}, 100))),
	}
}
