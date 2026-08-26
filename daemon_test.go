package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDaemonAddIsIdempotentAndRecoversDeletion(t *testing.T) {
	state := t.TempDir()
	documentPath := mustWriteFile(t, t.TempDir(), "guide.md", "# Guide\n")
	d, err := newDaemonServer(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.close)

	first, added, err := d.updater.add(documentPath)
	if err != nil || !added {
		t.Fatalf("first add = %#v, %v, %v", first, added, err)
	}
	second, added, err := d.updater.add(documentPath)
	if err != nil || added || second.ID != first.ID {
		t.Fatalf("second add = %#v, %v, %v", second, added, err)
	}
	if err := os.Remove(documentPath); err != nil {
		t.Fatal(err)
	}
	d.updater.reconcile(first.ID)
	d.updater.mu.Lock()
	removed := d.updater.documents[first.ID].removed
	d.updater.mu.Unlock()
	if !removed {
		t.Fatal("deleted document is not marked removed")
	}
	third, added, err := d.updater.add(documentPath)
	if err != nil || added || third.ID != first.ID || !third.removed {
		t.Fatalf("missing re-add = %#v, %v, %v", third, added, err)
	}
}

func TestDaemonPersistsDocuments(t *testing.T) {
	state := t.TempDir()
	documentPath := mustWriteFile(t, t.TempDir(), "guide.md", "# Guide\n")
	d, err := newDaemonServer(state)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := d.updater.add(documentPath)
	if err != nil {
		t.Fatal(err)
	}
	d.close()

	reloaded, err := newDaemonServer(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reloaded.close)
	waitForCondition(t, func() bool {
		reloaded.updater.mu.Lock()
		defer reloaded.updater.mu.Unlock()
		stored := reloaded.updater.documents[document.ID]
		return stored != nil && !stored.removed
	})
	reloaded.updater.mu.Lock()
	stored := cloneDaemonDocument(reloaded.updater.documents[document.ID])
	reloaded.updater.mu.Unlock()
	canonical, canonicalErr := canonicalDocumentPath(documentPath)
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	if stored == nil || stored.Path != canonical || stored.removed {
		t.Fatalf("reloaded document = %#v", stored)
	}
}

func TestDaemonServingKeepsListOpaqueAndScopesAssets(t *testing.T) {
	state := t.TempDir()
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	firstPath := mustWriteFile(t, firstRoot, "first.md", "# First\n\n![Pixel](pixel.png)\n")
	secondPath := mustWriteFile(t, secondRoot, "second.md", "# Second\n")
	mustWriteBytes(t, firstRoot, "pixel.png", encodedPNG(t))
	mustWriteBytes(t, secondRoot, "secret.png", encodedPNG(t))
	d, err := newDaemonServer(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.close)
	first, _, err := d.updater.add(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.updater.add(secondPath); err != nil {
		t.Fatal(err)
	}

	files := daemonRequest(t, d.handler, http.MethodGet, "/api/files", nil)
	body := readBody(t, files)
	_ = files.Body.Close()
	if strings.Contains(body, firstRoot) || strings.Contains(body, secondRoot) {
		t.Fatalf("serving file list exposed an absolute path: %s", body)
	}

	render := daemonRequest(t, d.handler, http.MethodGet, apiPath("/api/render", first.ID), nil)
	var payload struct {
		AbsolutePath string `json:"absolutePath"`
		HTML         string `json:"html"`
	}
	decodeJSON(t, render, &payload)
	_ = render.Body.Close()
	if payload.AbsolutePath != first.Path {
		t.Errorf("absolute path = %q, want %q", payload.AbsolutePath, first.Path)
	}
	if !strings.Contains(payload.HTML, "/api/asset?doc="+first.ID+"&amp;path=pixel.png") {
		t.Fatalf("document asset URL is not scoped: %s", payload.HTML)
	}

	escapeTarget := "/api/asset?doc=" + first.ID + "&path=" + filepath.ToSlash(filepath.Join("..", filepath.Base(secondRoot), "secret.png"))
	escape := daemonRequest(t, d.handler, http.MethodGet, escapeTarget, nil)
	defer escape.Body.Close()
	if escape.StatusCode < 400 {
		t.Fatalf("cross-document asset status = %d", escape.StatusCode)
	}
}

func TestDaemonControlChecksJSONAndLocalHost(t *testing.T) {
	d, err := newDaemonServer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.close)

	request := httptest.NewRequest(http.MethodPost, "/api/control/list", strings.NewReader(`{}`))
	request.Host = "localhost:7332"
	request.RemoteAddr = "127.0.0.1:41000"
	recorder := httptest.NewRecorder()
	d.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("missing Content-Type status = %d", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	request.Host = "example.com"
	request.RemoteAddr = "127.0.0.1:41000"
	recorder = httptest.NewRecorder()
	d.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("foreign host status = %d", recorder.Code)
	}
}

func daemonRequest(t *testing.T, handler http.Handler, method, target string, body io.Reader) *http.Response {
	t.Helper()
	request := httptest.NewRequest(method, target, body)
	request.Host = "localhost:7332"
	request.RemoteAddr = "127.0.0.1:41000"
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Result()
}

func postControl(t *testing.T, handler http.Handler, endpoint string, value any) *http.Response {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return daemonRequest(t, handler, http.MethodPost, endpoint, bytes.NewReader(encoded))
}
