package main

import (
	"bytes"
	_ "embed"
	"fmt"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	emoji "github.com/yuin/goldmark-emoji"
	emojiast "github.com/yuin/goldmark-emoji/ast"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const demoDocumentPath = "__mdshelf_demo__"

//go:embed demo.md
var demoMarkdown []byte

//go:embed demo.bib
var demoBibliography []byte

func newMarkdownRenderer() goldmark.Markdown {
	options := []goldmark.Option{
		goldmark.WithExtensions(
			extension.GFM,
			emoji.New(
				emoji.WithRenderingMethod(emoji.Func),
				emoji.WithRendererFunc(renderEmoji),
			),
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
		goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(reviewBlockRenderer{}, 200))),
	}
	options = append(options, mermaidOptions()...)
	options = append(options, mathOptions()...)
	options = append(options, calloutOptions()...)
	options = append(options, citationOptions()...)
	return goldmark.New(options...)
}

func renderEmoji(w util.BufWriter, _ []byte, node *emojiast.Emoji, _ *emoji.RendererConfig) {
	shortcode := util.EscapeHTML(node.ShortName)
	value := ":" + string(node.ShortName) + ":"
	if node.Value.IsUnicode() {
		value = string(node.Value.Unicode)
	}
	fmt.Fprintf(w, `<span class="emoji" title=":%s:">%s</span>`, shortcode, value)
}

func renderDemo(markdown goldmark.Markdown) (renderedMarkdown, error) {
	return renderMarkdownWithOptions(markdown, demoMarkdown, "demo.md", markdownRenderOptions{
		loadBibliography: func(reference string) ([]byte, error) {
			if reference != "demo.bib" {
				return nil, fmt.Errorf("unknown demo bibliography %q", reference)
			}
			return demoBibliography, nil
		},
	})
}

type renderedMarkdown struct {
	title      string
	html       string
	sourceHash string
	blocks     []markdownBlock
}

type markdownRenderOptions struct {
	rewrite          func(ast.Node)
	loadBibliography func(string) ([]byte, error)
}

func renderMarkdown(markdown goldmark.Markdown, source []byte, documentPath string, rewrite func(ast.Node)) (renderedMarkdown, error) {
	return renderMarkdownWithOptions(markdown, source, documentPath, markdownRenderOptions{rewrite: rewrite})
}

func renderMarkdownWithOptions(markdown goldmark.Markdown, source []byte, documentPath string, options markdownRenderOptions) (renderedMarkdown, error) {
	originalSource := source
	frontMatter, err := extractFrontMatter(source)
	if err != nil {
		return renderedMarkdown{}, err
	}
	source = frontMatter.body
	sourceLineOffset := bytes.Count(originalSource[:len(originalSource)-len(source)], []byte{'\n'})
	bibliography, err := loadCitationBibliography(frontMatter.values, options.loadBibliography)
	if err != nil {
		return renderedMarkdown{}, err
	}
	document := markdown.Parser().Parse(text.NewReader(source))
	if options.rewrite != nil {
		options.rewrite(document)
	}
	applyCitationBibliography(document.OwnerDocument(), bibliography)
	blocks := wrapReviewBlocks(document.OwnerDocument(), source, sourceLineOffset)
	var output bytes.Buffer
	if err := markdown.Renderer().Render(&output, source, document); err != nil {
		return renderedMarkdown{}, err
	}
	title := frontMatter.title
	if title == "" {
		title = documentTitle(document, source, documentPath)
	}
	return renderedMarkdown{
		title:      title,
		html:       injectMetadata(output.String(), frontMatter.fields),
		sourceHash: sourceHash(originalSource),
		blocks:     blocks,
	}, nil
}
