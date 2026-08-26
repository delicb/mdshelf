package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"net/url"
	"strings"
	"unicode"

	"github.com/jschaf/bibtex"
	bibast "github.com/jschaf/bibtex/ast"
	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const citationMetadataKey = "mdshelf.citations"

var (
	kindCitation     = gast.NewNodeKind("Citation")
	kindBibliography = gast.NewNodeKind("Bibliography")
)

type citationAuthor struct {
	first  string
	prefix string
	last   string
	suffix string
}

type citationEntry struct {
	key       string
	authors   []citationAuthor
	title     string
	year      string
	container string
	publisher string
	doi       string
	url       string
}

type citationRequest struct {
	key    string
	suffix string
}

type citation struct {
	gast.BaseInline
	raw      []byte
	requests []citationRequest
}

func (n *citation) Kind() gast.NodeKind { return kindCitation }

func (n *citation) Dump(source []byte, level int) {
	gast.DumpHelper(n, source, level, map[string]string{"Source": string(n.raw)}, nil)
}

type bibliographyBlock struct {
	gast.BaseBlock
	entries []citationEntry
}

func (n *bibliographyBlock) Kind() gast.NodeKind { return kindBibliography }

func (n *bibliographyBlock) Dump(source []byte, level int) {
	gast.DumpHelper(n, source, level, map[string]string{"Entries": fmt.Sprint(len(n.entries))}, nil)
}

type citationParser struct{}

func (citationParser) Trigger() []byte { return []byte{'['} }

func (citationParser) Parse(_ gast.Node, reader text.Reader, _ parser.Context) gast.Node {
	line, _ := reader.PeekLine()
	if len(line) < 4 || line[0] != '[' || line[1] != '@' || reader.PrecendingCharacter() == '!' || reader.PrecendingCharacter() == '\\' {
		return nil
	}
	stop := findUnescapedByte(line, 2, ']')
	if stop < 3 || (stop+1 < len(line) && (line[stop+1] == '(' || line[stop+1] == '[')) {
		return nil
	}
	requests, ok := parseCitationRequests(string(line[1:stop]))
	if !ok {
		return nil
	}
	reader.Advance(stop + 1)
	return &citation{raw: bytes.Clone(line[:stop+1]), requests: requests}
}

func (citationParser) CloseBlock(_ gast.Node, _ parser.Context) {}

func parseCitationRequests(value string) ([]citationRequest, bool) {
	parts := strings.Split(value, ";")
	requests := make([]citationRequest, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) < 2 || part[0] != '@' {
			return nil, false
		}
		keyEnd := strings.IndexFunc(part[1:], func(character rune) bool {
			return unicode.IsSpace(character) || character == ','
		})
		if keyEnd < 0 {
			keyEnd = len(part) - 1
		}
		keyEnd++
		key := part[1:keyEnd]
		if key == "" {
			return nil, false
		}
		suffix := strings.TrimSpace(part[keyEnd:])
		suffix = strings.TrimSpace(strings.TrimPrefix(suffix, ","))
		requests = append(requests, citationRequest{key: key, suffix: suffix})
	}
	return requests, len(requests) > 0
}

func loadCitationBibliography(values map[string]any, loader func(string) ([]byte, error)) (map[string]citationEntry, error) {
	if loader == nil || len(values) == 0 {
		return nil, nil
	}
	var reference string
	for key, value := range values {
		if strings.EqualFold(key, "bibliography") {
			var ok bool
			reference, ok = value.(string)
			if !ok || strings.TrimSpace(reference) == "" {
				return nil, fmt.Errorf("bibliography front matter value must be a file name")
			}
			break
		}
	}
	if reference == "" {
		return nil, nil
	}
	source, err := loader(reference)
	if err != nil {
		return nil, fmt.Errorf("load bibliography %q: %w", reference, err)
	}
	entries, err := parseBibtex(source)
	if err != nil {
		return nil, fmt.Errorf("parse bibliography %q: %w", reference, err)
	}
	return entries, nil
}

func parseBibtex(source []byte) (map[string]citationEntry, error) {
	biber := bibtex.New(bibtex.WithResolvers(
		bibtex.NewAuthorResolver("author"),
		bibtex.ResolverFunc(bibtex.SimplifyEscapedTextResolver),
		bibtex.NewRenderParsedTextResolver(),
	))
	file, err := biber.Parse(bytes.NewReader(source))
	if err != nil {
		return nil, err
	}
	resolved, err := biber.Resolve(file)
	if err != nil {
		return nil, err
	}
	entries := make(map[string]citationEntry, len(resolved))
	for _, item := range resolved {
		if _, exists := entries[item.Key]; exists {
			return nil, fmt.Errorf("duplicate citation key %q", item.Key)
		}
		entry := citationEntry{
			key:       item.Key,
			authors:   bibtexAuthors(item.Tags[bibtex.FieldAuthor]),
			title:     bibtexText(item.Tags[bibtex.FieldTitle]),
			year:      bibtexText(item.Tags[bibtex.FieldYear]),
			container: firstNonempty(bibtexText(item.Tags[bibtex.FieldJournal]), bibtexText(item.Tags[bibtex.FieldBookTitle])),
			publisher: firstNonempty(bibtexText(item.Tags[bibtex.FieldPublisher]), bibtexText(item.Tags[bibtex.FieldOrganization]), bibtexText(item.Tags[bibtex.FieldInstitution]), bibtexText(item.Tags[bibtex.FieldSchool])),
			doi:       bibtexText(item.Tags[bibtex.EntryDOI]),
			url:       bibtexText(item.Tags["url"]),
		}
		entries[item.Key] = entry
	}
	return entries, nil
}

func bibtexText(expression bibast.Expr) string {
	if expression == nil {
		return ""
	}
	if value, ok := expression.(*bibast.Text); ok {
		return strings.TrimSpace(value.Value)
	}
	return ""
}

func bibtexAuthors(expression bibast.Expr) []citationAuthor {
	authors, ok := expression.(bibast.Authors)
	if !ok {
		return nil
	}
	result := make([]citationAuthor, 0, len(authors))
	for _, author := range authors {
		result = append(result, citationAuthor{
			first:  bibtexText(author.First),
			prefix: bibtexText(author.Prefix),
			last:   bibtexText(author.Last),
			suffix: bibtexText(author.Suffix),
		})
	}
	return result
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func applyCitationBibliography(document *gast.Document, entries map[string]citationEntry) {
	if len(entries) == 0 {
		return
	}
	document.Meta()[citationMetadataKey] = entries
	seen := map[string]bool{}
	ordered := make([]citationEntry, 0)
	_ = gast.Walk(document, func(node gast.Node, entering bool) (gast.WalkStatus, error) {
		if !entering {
			return gast.WalkContinue, nil
		}
		citation, ok := node.(*citation)
		if !ok {
			return gast.WalkContinue, nil
		}
		for _, request := range citation.requests {
			entry, ok := entries[request.key]
			if ok && !seen[request.key] {
				seen[request.key] = true
				ordered = append(ordered, entry)
			}
		}
		return gast.WalkSkipChildren, nil
	})
	if len(ordered) > 0 {
		document.AppendChild(document, &bibliographyBlock{entries: ordered})
	}
}

type citationRenderer struct{}

func (citationRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(kindCitation, renderCitation)
	register.Register(kindBibliography, renderBibliography)
}

func renderCitation(w util.BufWriter, _ []byte, node gast.Node, entering bool) (gast.WalkStatus, error) {
	if !entering {
		return gast.WalkContinue, nil
	}
	citation := node.(*citation)
	entries, _ := node.OwnerDocument().Meta()[citationMetadataKey].(map[string]citationEntry)
	if len(entries) == 0 {
		_, _ = w.Write(util.EscapeHTML(citation.raw))
		return gast.WalkSkipChildren, nil
	}
	_, _ = io.WriteString(w, `<span class="citation-group">(`)
	for index, request := range citation.requests {
		if index > 0 {
			_, _ = io.WriteString(w, "; ")
		}
		entry, ok := entries[request.key]
		if !ok {
			_, _ = io.WriteString(w, `<span class="citation-missing" title="Citation not found">@`)
			_, _ = io.WriteString(w, html.EscapeString(request.key))
			_, _ = io.WriteString(w, `</span>`)
			continue
		}
		_, _ = io.WriteString(w, `<a class="citation" href="#`+citationID(entry.key)+`">`)
		_, _ = io.WriteString(w, html.EscapeString(shortCitation(entry)))
		_, _ = io.WriteString(w, `</a>`)
		if request.suffix != "" {
			_, _ = io.WriteString(w, ", "+html.EscapeString(request.suffix))
		}
	}
	_, _ = io.WriteString(w, `)</span>`)
	return gast.WalkSkipChildren, nil
}

func renderBibliography(w util.BufWriter, _ []byte, node gast.Node, entering bool) (gast.WalkStatus, error) {
	if !entering {
		return gast.WalkContinue, nil
	}
	block := node.(*bibliographyBlock)
	_, _ = io.WriteString(w, `<section class="bibliography" role="doc-bibliography" aria-labelledby="mdshelf-references"><h2 id="mdshelf-references">References</h2><ol>`)
	for _, entry := range block.entries {
		_, _ = io.WriteString(w, `<li id="`+citationID(entry.key)+`">`)
		_, _ = io.WriteString(w, formatBibliographyEntry(entry))
		_, _ = io.WriteString(w, `</li>`)
	}
	_, _ = io.WriteString(w, `</ol></section>`)
	return gast.WalkSkipChildren, nil
}

func shortCitation(entry citationEntry) string {
	author := entry.title
	switch len(entry.authors) {
	case 1:
		author = entry.authors[0].last
	case 2:
		author = entry.authors[0].last + " & " + entry.authors[1].last
	default:
		if len(entry.authors) > 2 {
			author = entry.authors[0].last + " et al."
		}
	}
	if author == "" {
		author = entry.key
	}
	year := entry.year
	if year == "" {
		year = "n.d."
	}
	return author + ", " + year
}

func formatBibliographyEntry(entry citationEntry) string {
	var parts []string
	if authors := fullAuthorList(entry.authors); authors != "" {
		parts = append(parts, html.EscapeString(authors))
	}
	year := entry.year
	if year == "" {
		year = "n.d."
	}
	parts = append(parts, "("+html.EscapeString(year)+").")
	title := entry.title
	if title == "" {
		title = entry.key
	}
	parts = append(parts, "<cite>"+html.EscapeString(title)+"</cite>.")
	if entry.container != "" {
		parts = append(parts, html.EscapeString(entry.container)+".")
	}
	if entry.publisher != "" {
		parts = append(parts, html.EscapeString(entry.publisher)+".")
	}
	if entry.doi != "" {
		href := "https://doi.org/" + url.PathEscape(entry.doi)
		parts = append(parts, `<a href="`+html.EscapeString(href)+`" rel="noopener noreferrer">doi:`+html.EscapeString(entry.doi)+`</a>.`)
	} else if href, ok := safeCitationURL(entry.url); ok {
		parts = append(parts, `<a href="`+html.EscapeString(href)+`" rel="noopener noreferrer">`+html.EscapeString(href)+`</a>.`)
	}
	return strings.Join(parts, " ")
}

func fullAuthorList(authors []citationAuthor) string {
	names := make([]string, 0, len(authors))
	for _, author := range authors {
		name := strings.Join(strings.Fields(strings.Join([]string{author.first, author.prefix, author.last, author.suffix}, " ")), " ")
		if name != "" {
			names = append(names, name)
		}
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
	}
}

func safeCitationURL(value string) (string, bool) {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", false
	}
	return parsed.String(), true
}

func citationID(key string) string {
	return "ref-" + base64.RawURLEncoding.EncodeToString([]byte(key))
}

func citationOptions() []goldmark.Option {
	return []goldmark.Option{
		goldmark.WithParserOptions(parser.WithInlineParsers(util.Prioritized(citationParser{}, 150))),
		goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(citationRenderer{}, 130))),
	}
}
