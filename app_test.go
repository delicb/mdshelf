package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMarkdownFilesAreRecursiveSortedAndFiltered(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "docs")
	mustWriteFile(t, root, "z.md", "z")
	mustWriteFile(t, root, "a.markdown", "a")
	mustWriteFile(t, root, "guides/c.MD", "c")
	mustWriteFile(t, root, "guides/d.MarkDown", "d")
	mustWriteFile(t, root, "ignore.txt", "no")
	mustWriteFile(t, root, ".hidden.md", "no")
	mustWriteFile(t, root, ".private/inside.md", "no")
	mustWriteFile(t, root, "guides/.hidden.markdown", "no")

	outsideFile := mustWriteFile(t, base, "outside.md", "outside")
	outsideDir := filepath.Join(base, "outside-dir")
	mustWriteFile(t, outsideDir, "inside.md", "outside")
	trySymlink(t, outsideFile, filepath.Join(root, "linked.md"))
	trySymlink(t, outsideDir, filepath.Join(root, "linked-dir"))

	handler := mustNewHandler(t, root)
	response := request(t, handler, http.MethodGet, "/api/files", nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/files status = %d, body = %s", response.StatusCode, readBody(t, response))
	}

	var payload struct {
		Files []string `json:"files"`
	}
	decodeJSON(t, response, &payload)
	want := []string{"a.markdown", "guides/c.MD", "guides/d.MarkDown", "z.md"}
	if !reflect.DeepEqual(payload.Files, want) {
		t.Fatalf("files = %#v, want %#v", payload.Files, want)
	}
}

func TestRenderMarkdownWithGFM(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "guide.md", `# Phone *Guide* `+"`v2`"+`

~~old~~

- [x] Works

| Name | State |
| --- | --- |
| shelf | ready |

https://example.com
`)

	handler := mustNewHandler(t, root)
	response := request(t, handler, http.MethodGet, apiPath("/api/render", "guide.md"), nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("render status = %d, body = %s", response.StatusCode, readBody(t, response))
	}

	var payload struct {
		Path  string `json:"path"`
		Title string `json:"title"`
		HTML  string `json:"html"`
	}
	decodeJSON(t, response, &payload)
	if payload.Path != "guide.md" {
		t.Errorf("path = %q, want guide.md", payload.Path)
	}
	if payload.Title != "Phone Guide v2" {
		t.Errorf("title = %q, want Phone Guide v2", payload.Title)
	}
	for _, fragment := range []string{
		`<h1 id="phone-guide-v2">Phone <em>Guide</em> <code>v2</code></h1>`,
		`<del>old</del>`,
		`type="checkbox"`,
		`<table>`,
		`href="https://example.com"`,
	} {
		if !strings.Contains(payload.HTML, fragment) {
			t.Errorf("rendered HTML does not contain %q:\n%s", fragment, payload.HTML)
		}
	}
}

func TestRenderHighlightsFencedCode(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "code.md", "# Code\n\n```go\npackage main\nfunc main() { println(\"<safe>\") }\n```\n\n```madeuplanguage\n<script>alert(1)</script>\n```\n\n```\nplain <tag> & text\n```\n")

	handler := mustNewHandler(t, root)
	response := request(t, handler, http.MethodGet, apiPath("/api/render", "code.md"), nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("render status = %d, body = %s", response.StatusCode, readBody(t, response))
	}

	var payload struct {
		HTML string `json:"html"`
	}
	decodeJSON(t, response, &payload)
	for _, fragment := range []string{
		`<pre class="chroma"><code>`,
		`<span class="kn">package</span>`,
		`<span class="kd">func</span>`,
		`&#34;&lt;safe&gt;&#34;`,
		`<code class="language-madeuplanguage">&lt;script&gt;alert(1)&lt;/script&gt;`,
		`<pre><code>plain &lt;tag&gt; &amp; text`,
	} {
		if !strings.Contains(payload.HTML, fragment) {
			t.Errorf("rendered HTML does not contain %q:\n%s", fragment, payload.HTML)
		}
	}
	for _, unwanted := range []string{`style="`, `class="ln"`, `<table`} {
		if strings.Contains(payload.HTML, unwanted) {
			t.Errorf("rendered HTML contains unwanted highlighting output %q:\n%s", unwanted, payload.HTML)
		}
	}
}

func TestUnicodePathAndNestedRelativeImageRewrite(t *testing.T) {
	root := t.TempDir()
	documentPath := "notes/deep/Čitanje 文档.md"
	mustWriteFile(t, root, documentPath, `# Čitanje 文档

![Photo](../../images/photo%20one.png#detail)
`)
	mustWriteBytes(t, root, "images/photo one.png", encodedPNG(t))
	handler := mustNewHandler(t, root)

	filesResponse := request(t, handler, http.MethodGet, "/api/files", nil)
	defer filesResponse.Body.Close()
	if filesResponse.StatusCode != http.StatusOK {
		t.Fatalf("file list status = %d, body = %s", filesResponse.StatusCode, readBody(t, filesResponse))
	}
	var filesPayload struct {
		Files []string `json:"files"`
	}
	decodeJSON(t, filesResponse, &filesPayload)
	if !reflect.DeepEqual(filesPayload.Files, []string{documentPath}) {
		t.Fatalf("files = %#v, want %#v", filesPayload.Files, []string{documentPath})
	}

	renderResponse := request(t, handler, http.MethodGet, apiPath("/api/render", documentPath), nil)
	defer renderResponse.Body.Close()
	if renderResponse.StatusCode != http.StatusOK {
		t.Fatalf("render status = %d, body = %s", renderResponse.StatusCode, readBody(t, renderResponse))
	}
	var renderPayload struct {
		Path  string `json:"path"`
		Title string `json:"title"`
		HTML  string `json:"html"`
	}
	decodeJSON(t, renderResponse, &renderPayload)
	if renderPayload.Path != documentPath {
		t.Errorf("path = %q, want %q", renderPayload.Path, documentPath)
	}
	if renderPayload.Title != "Čitanje 文档" {
		t.Errorf("title = %q, want %q", renderPayload.Title, "Čitanje 文档")
	}
	if want := `/api/asset?path=images%2Fphoto+one.png#detail`; !strings.Contains(renderPayload.HTML, want) {
		t.Errorf("rendered HTML does not contain rewritten image URL %q:\n%s", want, renderPayload.HTML)
	}
}

func TestRenderDoesNotEmitHostileHTMLOrURLs(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "hostile.md", `# Hostile

<script>alert("raw")</script>
<img src="x" onerror="alert('raw')">

[script link](javascript:alert(1))
[data link](data:text/html;base64,PHNjcmlwdD4=)
`)

	handler := mustNewHandler(t, root)
	response := request(t, handler, http.MethodGet, apiPath("/api/render", "hostile.md"), nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("render status = %d, body = %s", response.StatusCode, readBody(t, response))
	}

	var payload struct {
		HTML string `json:"html"`
	}
	decodeJSON(t, response, &payload)
	lowerHTML := strings.ToLower(payload.HTML)
	for _, unsafe := range []string{"<script", "<img", "javascript:", "data:text/html", "onerror="} {
		if strings.Contains(lowerHTML, unsafe) {
			t.Errorf("rendered HTML contains unsafe content %q:\n%s", unsafe, payload.HTML)
		}
	}
	for _, label := range []string{"script link", "data link"} {
		if !strings.Contains(payload.HTML, label) {
			t.Errorf("rendered HTML lost link label %q:\n%s", label, payload.HTML)
		}
	}
}

func TestRenderRejectsUnsafeAndInvalidPaths(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "docs")
	mustWriteFile(t, root, "visible.md", "visible")
	mustWriteFile(t, root, "notes.txt", "not markdown")
	outside := mustWriteFile(t, base, "outside.md", "outside")

	tests := []struct {
		name string
		path string
	}{
		{name: "traversal", path: "../outside.md"},
		{name: "absolute", path: outside},
		{name: "non Markdown", path: "notes.txt"},
		{name: "missing", path: "missing.md"},
	}
	if trySymlink(t, outside, filepath.Join(root, "linked.md")) {
		tests = append(tests, struct {
			name string
			path string
		}{name: "symlink", path: "linked.md"})
	}

	handler := mustNewHandler(t, root)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := request(t, handler, http.MethodGet, apiPath("/api/render", test.path), nil)
			defer response.Body.Close()
			body := readBody(t, response)
			if response.StatusCode < 400 || response.StatusCode >= 500 {
				t.Fatalf("status = %d, want 4xx; body = %s", response.StatusCode, body)
			}
			assertNoAbsolutePath(t, body, base, root, outside)
		})
	}
}

func TestAssetServesRasterImages(t *testing.T) {
	root := t.TempDir()
	pngData := encodedPNG(t)
	jpegData := encodedJPEG(t)
	mustWriteBytes(t, root, "images/pixel.png", pngData)
	mustWriteBytes(t, root, "images/photo.JPEG", jpegData)

	handler := mustNewHandler(t, root)
	tests := []struct {
		path        string
		contentType string
		body        []byte
	}{
		{path: "images/pixel.png", contentType: "image/png", body: pngData},
		{path: "images/photo.JPEG", contentType: "image/jpeg", body: jpegData},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			response := request(t, handler, http.MethodGet, apiPath("/api/asset", test.path), nil)
			defer response.Body.Close()
			got := []byte(readBody(t, response))
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.StatusCode, got)
			}
			if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, test.contentType) {
				t.Errorf("Content-Type = %q, want %q", contentType, test.contentType)
			}
			if !bytes.Equal(got, test.body) {
				t.Errorf("asset body differs from source")
			}
			if cacheControl := response.Header.Get("Cache-Control"); cacheControl != "private, no-cache" {
				t.Errorf("Cache-Control = %q, want private, no-cache", cacheControl)
			}
		})
	}
}

func TestRenderReadsMarkdownChanges(t *testing.T) {
	root := t.TempDir()
	filePath := mustWriteFile(t, root, "changing.md", "# First version")
	handler := mustNewHandler(t, root)

	assertTitle := func(want string) {
		t.Helper()
		response := request(t, handler, http.MethodGet, apiPath("/api/render", "changing.md"), nil)
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("render status = %d, body = %s", response.StatusCode, readBody(t, response))
		}
		var payload struct {
			Title string `json:"title"`
		}
		decodeJSON(t, response, &payload)
		if payload.Title != want {
			t.Errorf("title = %q, want %q", payload.Title, want)
		}
	}

	assertTitle("First version")
	if err := os.WriteFile(filePath, []byte("# Updated version"), 0o644); err != nil {
		t.Fatalf("update Markdown file: %v", err)
	}
	assertTitle("Updated version")
}

func TestRenderRejectsOversizedMarkdown(t *testing.T) {
	root := t.TempDir()
	filePath := mustWriteFile(t, root, "large.md", "# Large")
	if err := os.Truncate(filePath, maxMarkdownSize+1); err != nil {
		t.Fatalf("enlarge Markdown file: %v", err)
	}

	handler := mustNewHandler(t, root)
	response := request(t, handler, http.MethodGet, apiPath("/api/render", "large.md"), nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("render status = %d, want %d; body = %s", response.StatusCode, http.StatusRequestEntityTooLarge, readBody(t, response))
	}
}

func TestAssetRejectsUnsafeAndUnsupportedFiles(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "docs")
	mustWriteFile(t, root, "fake.png", "not a PNG")
	mustWriteFile(t, root, "vector.svg", `<svg xmlns="http://www.w3.org/2000/svg"></svg>`)
	outside := filepath.Join(base, "outside.png")
	mustWriteBytes(t, base, "outside.png", encodedPNG(t))

	tests := []struct {
		name string
		path string
	}{
		{name: "fake image", path: "fake.png"},
		{name: "SVG", path: "vector.svg"},
		{name: "traversal", path: "../outside.png"},
		{name: "absolute", path: outside},
		{name: "missing", path: "missing.png"},
	}
	if trySymlink(t, outside, filepath.Join(root, "linked.png")) {
		tests = append(tests, struct {
			name string
			path string
		}{name: "symlink", path: "linked.png"})
	}

	handler := mustNewHandler(t, root)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := request(t, handler, http.MethodGet, apiPath("/api/asset", test.path), nil)
			defer response.Body.Close()
			body := readBody(t, response)
			if response.StatusCode < 400 || response.StatusCode >= 500 {
				t.Fatalf("status = %d, want 4xx; body = %s", response.StatusCode, body)
			}
			assertNoAbsolutePath(t, body, base, root, outside)
		})
	}
}

func TestAPIRejectsUnsupportedMethods(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "doc.md", "# Document")
	mustWriteBytes(t, root, "pixel.png", encodedPNG(t))
	handler := mustNewHandler(t, root)

	for _, target := range []string{
		"/api/files",
		apiPath("/api/render", "doc.md"),
		apiPath("/api/asset", "pixel.png"),
	} {
		t.Run(target, func(t *testing.T) {
			response := request(t, handler, http.MethodPost, target, strings.NewReader("ignored"))
			defer response.Body.Close()
			if response.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("POST %s status = %d, want %d; body = %s", target, response.StatusCode, http.StatusMethodNotAllowed, readBody(t, response))
			}
			if allow := response.Header.Get("Allow"); allow != http.MethodGet {
				t.Errorf("Allow = %q, want GET", allow)
			}
		})
	}
}

func TestEmbeddedWebShell(t *testing.T) {
	root := t.TempDir()
	handler := mustNewHandler(t, root)

	tests := []struct {
		path                string
		contentTypePrefixes []string
		contains            []string
	}{
		{path: "/", contentTypePrefixes: []string{"text/html"}, contains: []string{`<meta name="viewport"`, `href="./chroma.css"`}},
		{path: "/app.css", contentTypePrefixes: []string{"text/css"}, contains: []string{":root"}},
		{path: "/chroma.css", contentTypePrefixes: []string{"text/css"}, contains: []string{".chroma .kd", "prefers-color-scheme: dark"}},
		{path: "/app.js", contentTypePrefixes: []string{"text/javascript", "application/javascript"}, contains: []string{`"use strict"`}},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			response := request(t, handler, http.MethodGet, test.path, nil)
			defer response.Body.Close()
			body := readBody(t, response)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("GET %s status = %d, body = %s", test.path, response.StatusCode, body)
			}
			contentType := response.Header.Get("Content-Type")
			validContentType := false
			for _, prefix := range test.contentTypePrefixes {
				if strings.HasPrefix(contentType, prefix) {
					validContentType = true
					break
				}
			}
			if !validContentType {
				t.Errorf("Content-Type = %q, want one of prefixes %q", contentType, test.contentTypePrefixes)
			}
			for _, fragment := range test.contains {
				if !strings.Contains(body, fragment) {
					t.Errorf("GET %s body does not contain %q", test.path, fragment)
				}
			}
		})
	}
}

func mustNewHandler(t *testing.T, root string) http.Handler {
	t.Helper()
	app, err := newApp(root)
	if err != nil {
		t.Fatalf("newApp(%q): %v", root, err)
	}
	return app.Handler()
}

func request(t *testing.T, handler http.Handler, method, target string, body io.Reader) *http.Response {
	t.Helper()
	request := httptest.NewRequest(method, target, body)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Result()
}

func apiPath(endpoint, filePath string) string {
	return endpoint + "?" + url.Values{"path": []string{filePath}}.Encode()
}

func decodeJSON(t *testing.T, response *http.Response, target any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode JSON response: %v", err)
	}
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return string(body)
}

func mustWriteFile(t *testing.T, root, name, content string) string {
	t.Helper()
	return mustWriteBytes(t, root, name, []byte(content))
}

func mustWriteBytes(t *testing.T, root, name string, content []byte) string {
	t.Helper()
	filePath := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("create directory for %q: %v", filePath, err)
	}
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("write %q: %v", filePath, err)
	}
	return filePath
}

func trySymlink(t *testing.T, target, link string) bool {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Logf("symlink checks skipped for %q: %v", link, err)
		return false
	}
	return true
}

func encodedPNG(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 30, G: 90, B: 150, A: 255})
	if err := png.Encode(&output, img); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return output.Bytes()
}

func encodedJPEG(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 30, G: 90, B: 150, A: 255})
	if err := jpeg.Encode(&output, img, nil); err != nil {
		t.Fatalf("encode JPEG: %v", err)
	}
	return output.Bytes()
}

func assertNoAbsolutePath(t *testing.T, body string, paths ...string) {
	t.Helper()
	for _, candidate := range paths {
		if candidate != "" && strings.Contains(body, candidate) {
			t.Errorf("response leaks absolute path %q: %s", candidate, body)
		}
	}
}
