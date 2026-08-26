package main

import (
	"bytes"
	_ "embed"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

const demoDocumentPath = "__mdshelf_demo__"

//go:embed demo.md
var demoMarkdown []byte

func newMarkdownRenderer() goldmark.Markdown {
	options := []goldmark.Option{
		goldmark.WithExtensions(
			extension.GFM,
			extension.DefinitionList,
			extension.Footnote,
			highlighting.NewHighlighting(
				highlighting.WithStyle("github"),
				highlighting.WithWrapperRenderer(renderCodeBlockWrapper),
				highlighting.WithCodeBlockOptions(codeBlockOptions),
				highlighting.WithFormatOptions(
					chromahtml.WithClasses(true),
					chromahtml.WithCSSComments(false),
				),
			),
		),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	}
	options = append(options, mermaidOptions()...)
	options = append(options, mathOptions()...)
	options = append(options, calloutOptions()...)
	return goldmark.New(options...)
}

func renderDemo(markdown goldmark.Markdown) (renderedMarkdown, error) {
	return renderMarkdown(markdown, demoMarkdown, "demo.md", nil)
}

type renderedMarkdown struct {
	title    string
	html     string
	metadata map[string]any
}

func renderMarkdown(markdown goldmark.Markdown, source []byte, documentPath string, rewrite func(ast.Node)) (renderedMarkdown, error) {
	frontMatter, err := extractFrontMatter(source)
	if err != nil {
		return renderedMarkdown{}, err
	}
	source = frontMatter.body
	document := markdown.Parser().Parse(text.NewReader(source))
	if rewrite != nil {
		rewrite(document)
	}
	var output bytes.Buffer
	if err := markdown.Renderer().Render(&output, source, document); err != nil {
		return renderedMarkdown{}, err
	}
	title := frontMatter.title
	if title == "" {
		title = documentTitle(document, source, documentPath)
	}
	return renderedMarkdown{
		title:    title,
		html:     injectMetadata(output.String(), frontMatter.fields),
		metadata: frontMatter.values,
	}, nil
}
