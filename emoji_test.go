package main

import (
	"strings"
	"testing"
)

func TestRenderEmojiShortcodes(t *testing.T) {
	rendered, err := renderMarkdown(newMarkdownRenderer(), []byte("Launch :rocket: Smile :smile: Approve :+1:. Unknown :not_a_real_emoji:.\n"), "emoji.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`Launch <span class="emoji" title=":rocket:">🚀</span>`,
		`Smile <span class="emoji" title=":smile:">😄</span>`,
		`Approve <span class="emoji" title=":+1:">👍</span>`,
		"Unknown :not_a_real_emoji:",
	} {
		if !strings.Contains(rendered.html, fragment) {
			t.Errorf("emoji HTML does not contain %q: %s", fragment, rendered.html)
		}
	}
}

func TestRenderEmojiTooltipUsesOriginalAlias(t *testing.T) {
	rendered, err := renderMarkdown(newMarkdownRenderer(), []byte(":laughing: :satisfied:\n"), "emoji.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, shortcode := range []string{"laughing", "satisfied"} {
		fragment := `title=":` + shortcode + `:">😆</span>`
		if !strings.Contains(rendered.html, fragment) {
			t.Errorf("emoji HTML does not contain %q: %s", fragment, rendered.html)
		}
	}
}

func TestRenderEmojiShortcodesStayLiteralInCode(t *testing.T) {
	source := []byte("`:rocket:`\n\n```text\n:smile:\n```\n")
	rendered, err := renderMarkdown(newMarkdownRenderer(), source, "emoji.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.html, "🚀") || strings.Contains(rendered.html, "😄") {
		t.Fatalf("code emoji shortcodes were rendered: %s", rendered.html)
	}
	for _, fragment := range []string{"<code>:rocket:</code>", ":smile:"} {
		if !strings.Contains(rendered.html, fragment) {
			t.Errorf("code HTML does not contain %q: %s", fragment, rendered.html)
		}
	}
}

func TestRenderUnknownLegacyEmojiPreservesShortcode(t *testing.T) {
	rendered, err := renderMarkdown(newMarkdownRenderer(), []byte("Mascot :shipit:.\n"), "emoji.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.html, "Mascot :shipit:.") {
		t.Fatalf("legacy emoji HTML = %s", rendered.html)
	}
}
