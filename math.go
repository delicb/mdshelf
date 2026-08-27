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

var (
	kindMathInline = ast.NewNodeKind("MathInline")
	kindMathBlock  = ast.NewNodeKind("MathBlock")
)

type mathInline struct {
	ast.BaseInline
	expression []byte
}

func (n *mathInline) Kind() ast.NodeKind { return kindMathInline }

func (n *mathInline) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Expression": string(n.expression)}, nil)
}

type mathBlock struct {
	ast.BaseBlock
	expression  []byte
	sourceStart int
	sourceStop  int
}

func (n *mathBlock) Kind() ast.NodeKind { return kindMathBlock }

func (n *mathBlock) reviewSourceRange() (int, int) { return n.sourceStart, n.sourceStop }

func (n *mathBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Expression": string(n.expression)}, nil)
}

type mathTransformer struct{}

func (mathTransformer) Transform(document *ast.Document, reader text.Reader, _ parser.Context) {
	transformMathBlocks(document, reader.Source())
}

func transformMathBlocks(document *ast.Document, source []byte) {
	var paragraphs []*ast.Paragraph
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		paragraph, ok := node.(*ast.Paragraph)
		if ok {
			paragraphs = append(paragraphs, paragraph)
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})

	for _, paragraph := range paragraphs {
		expression, ok := mathBlockExpression(paragraph, source)
		if !ok {
			continue
		}
		start, stop, _ := blockSourceBounds(paragraph, source)
		block := &mathBlock{expression: expression, sourceStart: start, sourceStop: stop}
		paragraph.Parent().ReplaceChild(paragraph.Parent(), paragraph, block)
	}
}

func mathBlockExpression(paragraph *ast.Paragraph, source []byte) ([]byte, bool) {
	var value []byte
	for i := 0; i < paragraph.Lines().Len(); i++ {
		line := paragraph.Lines().At(i)
		value = append(value, line.Value(source)...)
	}
	value = bytes.TrimSpace(value)
	for _, delimiters := range [][2][]byte{{[]byte("$$"), []byte("$$")}, {[]byte(`\[`), []byte(`\]`)}} {
		open, close := delimiters[0], delimiters[1]
		if len(value) < len(open)+len(close) || !bytes.HasPrefix(value, open) || !bytes.HasSuffix(value, close) {
			continue
		}
		expression := bytes.TrimSpace(value[len(open) : len(value)-len(close)])
		if len(expression) == 0 {
			return nil, false
		}
		return bytes.Clone(expression), true
	}
	return nil, false
}

type mathInlineParser struct{}

func (mathInlineParser) Trigger() []byte { return []byte{'$', '\\'} }

func (mathInlineParser) Parse(_ ast.Node, reader text.Reader, _ parser.Context) ast.Node {
	line, _ := reader.PeekLine()
	if len(line) < 3 {
		return nil
	}

	var expression []byte
	var consumed int
	switch {
	case line[0] == '$' && line[1] != '$' && reader.PrecendingCharacter() != '\\':
		stop := findUnescapedByte(line, 1, '$')
		if stop < 2 {
			return nil
		}
		expression = line[1:stop]
		consumed = stop + 1
	case line[0] == '\\' && line[1] == '(' && reader.PrecendingCharacter() != '\\':
		stop := findSlashDelimiter(line, 2, ')')
		if stop < 2 {
			return nil
		}
		expression = line[2:stop]
		consumed = stop + 2
	default:
		return nil
	}
	if len(bytes.TrimSpace(expression)) == 0 {
		return nil
	}
	reader.Advance(consumed)
	return &mathInline{expression: bytes.Clone(expression)}
}

func (mathInlineParser) CloseBlock(_ ast.Node, _ parser.Context) {}

func findUnescapedByte(value []byte, start int, target byte) int {
	for i := start; i < len(value); i++ {
		if value[i] == target && !isEscaped(value, i) {
			return i
		}
	}
	return -1
}

func findSlashDelimiter(value []byte, start int, close byte) int {
	for i := start; i+1 < len(value); i++ {
		if value[i] == '\\' && value[i+1] == close && !isEscaped(value, i) {
			return i
		}
	}
	return -1
}

func isEscaped(value []byte, position int) bool {
	backslashes := 0
	for i := position - 1; i >= 0 && value[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

type mathRenderer struct{}

func (mathRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(kindMathInline, renderMathInline)
	register.Register(kindMathBlock, renderMathBlock)
}

func renderMathInline(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	_, _ = io.WriteString(w, `<span class="math-source" data-display="false">`)
	_, _ = w.Write(util.EscapeHTML(node.(*mathInline).expression))
	_, _ = io.WriteString(w, `</span>`)
	return ast.WalkSkipChildren, nil
}

func renderMathBlock(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	_, _ = io.WriteString(w, `<div class="math-source math-display" data-display="true">`)
	_, _ = w.Write(util.EscapeHTML(node.(*mathBlock).expression))
	_, _ = io.WriteString(w, "</div>\n")
	return ast.WalkSkipChildren, nil
}

func mathOptions() []goldmark.Option {
	return []goldmark.Option{
		goldmark.WithParserOptions(
			parser.WithInlineParsers(util.Prioritized(mathInlineParser{}, 50)),
			parser.WithASTTransformers(util.Prioritized(mathTransformer{}, 110)),
		),
		goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(mathRenderer{}, 110))),
	}
}
