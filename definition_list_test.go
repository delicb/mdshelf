package main

import (
	"strings"
	"testing"
)

func TestRenderDefinitionLists(t *testing.T) {
	source := []byte(`MDShelf
: A local Markdown reader.
: A server with live updates.

Goldmark
: The Markdown parser.
`)
	rendered, err := renderMarkdown(newMarkdownRenderer(), source, "terms.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`<dl>`,
		`<dt>MDShelf</dt>`,
		`<dd>A local Markdown reader.</dd>`,
		`<dd>A server with live updates.</dd>`,
		`<dt>Goldmark</dt>`,
		`</dl>`,
	} {
		if !strings.Contains(rendered.html, fragment) {
			t.Errorf("definition list HTML does not contain %q: %s", fragment, rendered.html)
		}
	}
}

func TestRenderDefinitionListDoesNotConsumeNormalColons(t *testing.T) {
	rendered, err := renderMarkdown(newMarkdownRenderer(), []byte("Time: noon\n"), "terms.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.html, "<dl>") || !strings.Contains(rendered.html, "<p>Time: noon</p>") {
		t.Fatalf("normal colon HTML = %s", rendered.html)
	}
}
