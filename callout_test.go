package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestRenderCalloutTypes(t *testing.T) {
	for _, calloutType := range []string{"NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION"} {
		t.Run(calloutType, func(t *testing.T) {
			source := []byte(fmt.Sprintf("> [!%s]\n> Read **this**.\n>\n> - First item\n", calloutType))
			rendered, err := renderMarkdown(newMarkdownRenderer(), source, "callout.md", nil)
			if err != nil {
				t.Fatal(err)
			}
			name := strings.ToLower(calloutType)
			title := calloutTitles[calloutType]
			for _, fragment := range []string{
				`<aside class="callout callout-` + name + `" aria-label="` + title + `">`,
				`<p class="callout-title">` + title + `</p>`,
				`<p>Read <strong>this</strong>.</p>`,
				`<li>First item</li>`,
				`</aside>`,
			} {
				if !strings.Contains(rendered.html, fragment) {
					t.Errorf("callout HTML does not contain %q: %s", fragment, rendered.html)
				}
			}
			if strings.Contains(rendered.html, "[!"+calloutType+"]") {
				t.Errorf("callout marker remains in HTML: %s", rendered.html)
			}
		})
	}
}

func TestRenderCalloutKeepsNormalQuotes(t *testing.T) {
	rendered, err := renderMarkdown(newMarkdownRenderer(), []byte("> A normal quote.\n\n> [!UNKNOWN]\n> Still a quote.\n"), "quote.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(rendered.html, "<blockquote>") != 2 || strings.Contains(rendered.html, `class="callout`) {
		t.Fatalf("normal quote HTML = %s", rendered.html)
	}
}
