package main

import (
	"strconv"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/util"
)

var (
	codeTitleAttribute       = []byte("title")
	highlightLinesAttribute  = []byte("hl_lines")
	lineNumberStartAttribute = []byte("linenostart")
)

func renderCodeBlockWrapper(w util.BufWriter, context highlighting.CodeBlockContext, entering bool) {
	if entering {
		_, _ = w.WriteString(`<figure class="code-block"`)
		if title := codeBlockAttribute(context, codeTitleAttribute); len(title) > 0 {
			_, _ = w.WriteString(` data-code-title="`)
			_, _ = w.Write(util.EscapeHTML(title))
			_ = w.WriteByte('"')
		}
		if language, ok := context.Language(); ok {
			_, _ = w.WriteString(` data-code-language="`)
			_, _ = w.Write(util.EscapeHTML(language))
			_ = w.WriteByte('"')
		}
		_ = w.WriteByte('>')
		if !context.Highlighted() {
			_, _ = w.WriteString("<pre><code")
			if language, ok := context.Language(); ok {
				_, _ = w.WriteString(` class="language-`)
				_, _ = w.Write(util.EscapeHTML(language))
				_ = w.WriteByte('"')
			}
			_ = w.WriteByte('>')
		}
		return
	}
	if !context.Highlighted() {
		_, _ = w.WriteString("</code></pre>")
	}
	_, _ = w.WriteString("</figure>\n")
}

func codeBlockAttribute(context highlighting.CodeBlockContext, name []byte) []byte {
	attributes := context.Attributes()
	if attributes == nil {
		return nil
	}
	value, ok := attributes.Get(name)
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []byte:
		return typed
	case string:
		return []byte(typed)
	default:
		return nil
	}
}

func codeBlockOptions(context highlighting.CodeBlockContext) []chromahtml.Option {
	value := codeBlockAttribute(context, highlightLinesAttribute)
	if len(value) == 0 {
		return nil
	}
	baseLineNumber := 1
	if attributes := context.Attributes(); attributes != nil {
		if value, ok := attributes.Get(lineNumberStartAttribute); ok {
			if number, ok := value.(float64); ok {
				baseLineNumber = int(number)
			}
		}
	}
	ranges := parseHighlightLines(string(value), baseLineNumber)
	if len(ranges) == 0 {
		return nil
	}
	return []chromahtml.Option{chromahtml.HighlightLines(ranges)}
}

func parseHighlightLines(value string, baseLineNumber int) [][2]int {
	parts := strings.FieldsFunc(value, func(character rune) bool {
		return character == ',' || character == ' ' || character == '\t'
	})
	ranges := make([][2]int, 0, len(parts))
	for _, part := range parts {
		bounds := strings.SplitN(part, "-", 2)
		start, err := strconv.Atoi(bounds[0])
		if err != nil || start < 1 {
			continue
		}
		stop := start
		if len(bounds) == 2 {
			stop, err = strconv.Atoi(bounds[1])
			if err != nil || stop < start {
				continue
			}
		}
		ranges = append(ranges, [2]int{start + baseLineNumber - 1, stop + baseLineNumber - 1})
	}
	return ranges
}
