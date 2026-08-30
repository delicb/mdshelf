package main

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func serveIndexHTML(t *testing.T) string {
	t.Helper()
	handler, err := embeddedWebHandler()
	if err != nil {
		t.Fatalf("embeddedWebHandler() = %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("GET / content type = %q, want text/html", contentType)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read index.html body: %v", err)
	}
	return string(body)
}

func TestServedIndexCarriesAssetContentHash(t *testing.T) {
	web, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		t.Fatalf("load embedded web files: %v", err)
	}
	version, err := webAssetVersion(web)
	if err != nil {
		t.Fatalf("webAssetVersion() = %v", err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(version) {
		t.Fatalf("webAssetVersion() = %q, want 12 lowercase hex characters", version)
	}

	body := serveIndexHTML(t)
	for _, reference := range []string{
		`href="./app.css?v=` + version + `"`,
		`href="./chroma.css?v=` + version + `"`,
		`href="./vendor/fonts/fonts.css?v=` + version + `"`,
		`src="./app.js?v=` + version + `"`,
		`src="./text-selection.js?v=` + version + `"`,
	} {
		if !strings.Contains(body, reference) {
			t.Errorf("served index.html does not contain %s", reference)
		}
	}
}

func TestServedIndexHasNoStaleCacheBusters(t *testing.T) {
	body := serveIndexHTML(t)
	if strings.Contains(body, assetVersionPlaceholder) {
		t.Errorf("served index.html still contains the %q placeholder", assetVersionPlaceholder)
	}
	// Hand-bumped numeric tokens must be gone; vendored libraries keep their
	// dotted upstream versions.
	if stale := regexp.MustCompile(`\?v=\d+"`).FindString(body); stale != "" {
		t.Errorf("served index.html contains stale hand-maintained cache buster %q", stale)
	}
	for _, vendored := range []string{
		`href="./vendor/katex/katex.min.css?v=0.18.4"`,
		`src="./vendor/mermaid.min.js?v=11.17.2"`,
	} {
		if !strings.Contains(body, vendored) {
			t.Errorf("served index.html lost the vendored reference %s", vendored)
		}
	}
}
