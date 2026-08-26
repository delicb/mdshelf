package main

import (
	"strings"
	"testing"
)

func TestRenderAdvancedCodeBlockOptions(t *testing.T) {
	source := []byte("```go {title=\"main.go\" linenos=true hl_lines=\"2 4\"}\npackage main\n\nfunc main() {\n\tprintln(\"ready\")\n}\n```\n")
	rendered, err := renderMarkdown(newMarkdownRenderer(), source, "code.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`<figure class="code-block" data-code-title="main.go" data-code-language="go">`,
		`class="chroma"`,
		`class="ln"`,
		`class="line hl"`,
		`</figure>`,
	} {
		if !strings.Contains(rendered.html, fragment) {
			t.Errorf("advanced code HTML does not contain %q: %s", fragment, rendered.html)
		}
	}
	if count := strings.Count(rendered.html, `class="line hl"`); count != 2 {
		t.Errorf("highlighted line count = %d, HTML = %s", count, rendered.html)
	}
}

func TestRenderCodeBlockEscapesTitleAndWrapsPlainCode(t *testing.T) {
	source := []byte("```unknown {title=\"<script>alert(1)</script>\"}\nplain text\n```\n")
	rendered, err := renderMarkdown(newMarkdownRenderer(), source, "code.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`data-code-title="&lt;script&gt;alert(1)&lt;/script&gt;"`,
		`<pre><code class="language-unknown">plain text`,
		`</code></pre></figure>`,
	} {
		if !strings.Contains(rendered.html, fragment) {
			t.Errorf("plain code HTML does not contain %q: %s", fragment, rendered.html)
		}
	}
	if strings.Contains(rendered.html, "<script>") {
		t.Fatalf("unsafe title HTML = %s", rendered.html)
	}
}

func TestParseHighlightLines(t *testing.T) {
	got := parseHighlightLines("2 4-6,invalid 0 8-7", 10)
	want := [][2]int{{11, 11}, {13, 15}}
	if len(got) != len(want) {
		t.Fatalf("ranges = %#v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("ranges = %#v", got)
		}
	}
}
