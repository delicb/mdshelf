package main

import (
	"strings"
	"testing"
)

func TestRenderYAMLFrontMatter(t *testing.T) {
	source := []byte(`---
title: Release notes
date: 2026-08-26
tags:
  - markdown
  - local
draft: false
---
# Visible heading

Document text.
`)
	rendered, err := renderMarkdown(newMarkdownRenderer(), source, "notes.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rendered.title != "Release notes" {
		t.Fatalf("title = %q", rendered.title)
	}
	for _, fragment := range []string{
		`<h1 id="visible-heading">Visible heading</h1>`,
		`<section class="document-metadata" aria-label="Document metadata"><dl>`,
		`<dt>Date</dt><dd>2026-08-26</dd>`,
		`<dt>Draft</dt><dd>false</dd>`,
		`<dt>Tags</dt><dd>markdown, local</dd>`,
		`<p>Document text.</p>`,
	} {
		if !strings.Contains(rendered.html, fragment) {
			t.Errorf("YAML front matter HTML does not contain %q: %s", fragment, rendered.html)
		}
	}
	if strings.Index(rendered.html, "document-metadata") < strings.Index(rendered.html, "</h1>") {
		t.Fatalf("metadata panel is not after the first heading: %s", rendered.html)
	}
	if strings.Contains(rendered.html, "title: Release notes") || strings.Contains(rendered.html, "<hr>") {
		t.Fatalf("front matter source remains in HTML: %s", rendered.html)
	}
}

func TestRenderTOMLFrontMatter(t *testing.T) {
	source := []byte(`+++
title = "TOML document"
authors = ["Ada", "Linus"]
category = "Guide"
+++
Document text.
`)
	rendered, err := renderMarkdown(newMarkdownRenderer(), source, "notes.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rendered.title != "TOML document" {
		t.Fatalf("title = %q", rendered.title)
	}
	for _, fragment := range []string{`<dt>Authors</dt><dd>Ada, Linus</dd>`, `<dt>Category</dt><dd>Guide</dd>`, `<p>Document text.</p>`} {
		if !strings.Contains(rendered.html, fragment) {
			t.Errorf("TOML front matter HTML does not contain %q: %s", fragment, rendered.html)
		}
	}
}

func TestRenderFrontMatterEscapesMetadata(t *testing.T) {
	source := []byte("---\ntitle: Safe\ndescription: '<script>alert(1)</script>'\n---\nText.\n")
	rendered, err := renderMarkdown(newMarkdownRenderer(), source, "notes.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.html, "<script>") || !strings.Contains(rendered.html, `&lt;script&gt;alert(1)&lt;/script&gt;`) {
		t.Fatalf("unsafe metadata HTML = %s", rendered.html)
	}
}

func TestRenderUnclosedFrontMatterAsMarkdown(t *testing.T) {
	rendered, err := renderMarkdown(newMarkdownRenderer(), []byte("---\n\n# Heading\n"), "notes.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.html, "<hr>") || !strings.Contains(rendered.html, "<h1") {
		t.Fatalf("unclosed front matter HTML = %s", rendered.html)
	}
}

func TestRenderRejectsInvalidFrontMatter(t *testing.T) {
	_, err := renderMarkdown(newMarkdownRenderer(), []byte("---\ntitle: [invalid\n---\nText\n"), "notes.md", nil)
	if err == nil || !strings.Contains(err.Error(), "parse front matter") {
		t.Fatalf("error = %v", err)
	}
}
