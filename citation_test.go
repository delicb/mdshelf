package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testBibliography = `@article{lovelace1843,
  author = {Ada Lovelace},
  title = {Notes on the Analytical Engine},
  journal = {Scientific Memoirs},
  year = {1843},
  url = {https://example.com/lovelace}
}

@book{turing1950,
  author = {Alan Turing and Grace Hopper},
  title = {Computing Machinery},
  publisher = {Example Press},
  year = {1950},
  doi = {10.1000/example}
}
`

func TestRenderCitationsAndBibliography(t *testing.T) {
	source := []byte(`---
bibliography: references.bib
---
A claim [@lovelace1843, p. 10]. Repeated [@lovelace1843]. Grouped [@lovelace1843; @turing1950].
`)
	var loaded string
	rendered, err := renderMarkdownWithOptions(newMarkdownRenderer(), source, "notes.md", markdownRenderOptions{
		loadBibliography: func(reference string) ([]byte, error) {
			loaded = reference
			return []byte(testBibliography), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded != "references.bib" {
		t.Fatalf("loaded bibliography = %q", loaded)
	}
	for _, fragment := range []string{
		`<span class="citation-group">(<a class="citation" href="#` + citationID("lovelace1843") + `">Lovelace, 1843</a>, p. 10)</span>`,
		`<a class="citation" href="#` + citationID("turing1950") + `">Turing &amp; Hopper, 1950</a>`,
		`<section class="bibliography" role="doc-bibliography" aria-labelledby="mdshelf-references">`,
		`<h2 id="mdshelf-references">References</h2>`,
		`Ada Lovelace (1843). <cite>Notes on the Analytical Engine</cite>. Scientific Memoirs.`,
		`Alan Turing and Grace Hopper (1950). <cite>Computing Machinery</cite>. Example Press.`,
		`doi:10.1000/example`,
	} {
		if !strings.Contains(rendered.html, fragment) {
			t.Errorf("citation HTML does not contain %q: %s", fragment, rendered.html)
		}
	}
	if count := strings.Count(rendered.html, `<li id="`+citationID("lovelace1843")+`">`); count != 1 {
		t.Errorf("Lovelace bibliography entry count = %d, HTML = %s", count, rendered.html)
	}
}

func TestRenderMissingCitationAndLiteralWithoutBibliography(t *testing.T) {
	withBibliography, err := renderMarkdownWithOptions(newMarkdownRenderer(), []byte("---\nbibliography: refs.bib\n---\nMissing [@unknown].\n"), "notes.md", markdownRenderOptions{
		loadBibliography: func(string) ([]byte, error) { return []byte(testBibliography), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withBibliography.html, `class="citation-missing"`) || strings.Contains(withBibliography.html, `<section class="bibliography"`) {
		t.Fatalf("missing citation HTML = %s", withBibliography.html)
	}

	withoutBibliography, err := renderMarkdown(newMarkdownRenderer(), []byte("Literal [@unknown].\n"), "notes.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withoutBibliography.html, "Literal [@unknown].") {
		t.Fatalf("literal citation HTML = %s", withoutBibliography.html)
	}
}

func TestCitationSyntaxDoesNotConsumeMarkdownLinksOrImages(t *testing.T) {
	rendered, err := renderMarkdown(newMarkdownRenderer(), []byte("Contact [@account](https://example.com). ![@logo](https://example.com/logo.png)\n"), "notes.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`<a href="https://example.com">@account</a>`, `<img src="https://example.com/logo.png" alt="@logo">`} {
		if !strings.Contains(rendered.html, fragment) {
			t.Errorf("Markdown HTML does not contain %q: %s", fragment, rendered.html)
		}
	}
}

func TestRenderCitationEscapesBibTeXFieldsAndRejectsUnsafeURLs(t *testing.T) {
	bibliography := `@misc{unsafe, title = {<script>alert(1)</script>}, year = {2026}, url = {javascript:alert(1)}}`
	rendered, err := renderMarkdownWithOptions(newMarkdownRenderer(), []byte("---\nbibliography: refs.bib\n---\n[@unsafe]\n"), "notes.md", markdownRenderOptions{
		loadBibliography: func(string) ([]byte, error) { return []byte(bibliography), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.html, "<script>") || strings.Contains(rendered.html, `href="javascript:`) || !strings.Contains(rendered.html, `&lt;script&gt;alert(1)&lt;/script&gt;`) {
		t.Fatalf("unsafe bibliography HTML = %s", rendered.html)
	}
}

func TestRenderRejectsInvalidBibliography(t *testing.T) {
	_, err := renderMarkdownWithOptions(newMarkdownRenderer(), []byte("---\nbibliography: refs.bib\n---\n[@key]\n"), "notes.md", markdownRenderOptions{
		loadBibliography: func(string) ([]byte, error) { return []byte("@article{broken"), nil },
	})
	if err == nil || !strings.Contains(err.Error(), "parse bibliography") {
		t.Fatalf("error = %v", err)
	}
}

func TestCitationServingInAdHocAndDaemonModes(t *testing.T) {
	root := t.TempDir()
	source := "---\nbibliography: references.bib\n---\nChecked [@lovelace1843].\n"
	documentPath := mustWriteFile(t, root, "notes.md", source)
	mustWriteFile(t, root, "references.bib", testBibliography)

	application, err := newAppWithWatcher(root, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(application.Close)
	response := request(t, application.Handler(), http.MethodGet, apiPath("/api/render", "notes.md"), nil)
	body := readBody(t, response)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `class=\"bibliography\"`) {
		t.Fatalf("ad hoc citation status = %d, body = %s", response.StatusCode, body)
	}

	daemon, err := newDaemonServer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(daemon.close)
	document, _, err := daemon.updater.add(documentPath)
	if err != nil {
		t.Fatal(err)
	}
	response = daemonRequest(t, daemon.handler, http.MethodGet, apiPath("/api/render", document.ID), nil)
	body = readBody(t, response)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `class=\"bibliography\"`) {
		t.Fatalf("daemon citation status = %d, body = %s", response.StatusCode, body)
	}
}

func TestBibliographyLoadersStayInDocumentFolder(t *testing.T) {
	root := t.TempDir()
	documents := filepath.Join(root, "documents")
	if err := os.Mkdir(documents, 0o755); err != nil {
		t.Fatal(err)
	}
	documentPath := filepath.Join(documents, "notes.md")
	if err := os.WriteFile(documentPath, []byte("# Notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(documents, "references.bib"), []byte(testBibliography), 0o644); err != nil {
		t.Fatal(err)
	}

	application, err := newAppWithWatcher(root, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(application.Close)
	loaded, err := application.loadBibliography("documents/notes.md", "references.bib")
	if err != nil || !strings.Contains(string(loaded), "lovelace1843") {
		t.Fatalf("ad hoc bibliography = %q, %v", loaded, err)
	}
	loaded, err = loadDaemonBibliography(documentPath, "references.bib")
	if err != nil || !strings.Contains(string(loaded), "lovelace1843") {
		t.Fatalf("daemon bibliography = %q, %v", loaded, err)
	}
	for _, reference := range []string{"../references.bib", "sub/references.bib", `sub\references.bib`, "references.json"} {
		if _, err := application.loadBibliography("documents/notes.md", reference); err == nil {
			t.Errorf("ad hoc loader accepted %q", reference)
		}
		if _, err := loadDaemonBibliography(documentPath, reference); err == nil {
			t.Errorf("daemon loader accepted %q", reference)
		}
	}
}
