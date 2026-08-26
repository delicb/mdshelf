package main

import (
	"strings"
	"testing"
)

func TestRenderMathDelimiters(t *testing.T) {
	source := []byte(`# Math

Inline $x^2 + y^2$ and \(a + b\).

$$
\int_0^1 x^2\,dx
$$

\[
E = mc^2
\]
`)
	rendered, err := renderMarkdown(newMarkdownRenderer(), source, "math.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`<span class="math-source" data-display="false">x^2 + y^2</span>`,
		`<span class="math-source" data-display="false">a + b</span>`,
		`<div class="math-source math-display" data-display="true">\int_0^1 x^2\,dx</div>`,
		`<div class="math-source math-display" data-display="true">E = mc^2</div>`,
	} {
		if !strings.Contains(rendered.html, fragment) {
			t.Errorf("math HTML does not contain %q: %s", fragment, rendered.html)
		}
	}
}

func TestRenderMathLeavesCodeAndEscapedDelimitersAlone(t *testing.T) {
	source := []byte("Escaped \\$5 and `inline $code$`.\n\n```text\n$block$ and \\(block\\)\n```\n")
	rendered, err := renderMarkdown(newMarkdownRenderer(), source, "math.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.html, `class="math-source`) {
		t.Fatalf("literal math syntax was rendered: %s", rendered.html)
	}
	for _, fragment := range []string{`Escaped $5`, `<code>inline $code$</code>`, `$block$ and \(block\)`} {
		if !strings.Contains(rendered.html, fragment) {
			t.Errorf("literal HTML does not contain %q: %s", fragment, rendered.html)
		}
	}
}

func TestRenderMathEscapesExpressionHTML(t *testing.T) {
	rendered, err := renderMarkdown(newMarkdownRenderer(), []byte(`$x <script>alert(1)</script>$`), "math.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.html, "<script>") || !strings.Contains(rendered.html, `x &lt;script&gt;alert(1)&lt;/script&gt;`) {
		t.Fatalf("unsafe math HTML = %s", rendered.html)
	}
}
