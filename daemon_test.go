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

func TestDaemonRendersEmbeddedDemo(t *testing.T) {
	d, err := newDaemonServer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.close)

	response := daemonRequest(t, d.handler, http.MethodGet, apiPath("/api/render", demoDocumentPath), nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("render status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var payload struct {
		Path         string `json:"path"`
		AbsolutePath string `json:"absolutePath"`
		Title        string `json:"title"`
		HTML         string `json:"html"`
	}
	decodeJSON(t, response, &payload)
	if payload.Path != demoDocumentPath || payload.Title != "MDShelf feature demo" || payload.AbsolutePath != "" {
		t.Fatalf("demo response = %#v", payload)
	}
	if !strings.Contains(payload.HTML, `class="mermaid"`) {
		t.Fatal("demo response does not contain a Mermaid diagram")
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

func TestDaemonControlRemoveAcceptsDocumentID(t *testing.T) {
	stateDir := t.TempDir()
	documentPath := mustWriteFile(t, t.TempDir(), "note.md", "# Note\n")
	d, err := newDaemonServer(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.close)
	document, _, err := d.updater.add(documentPath)
	if err != nil {
		t.Fatal(err)
	}

	response := postControl(t, d.handler, "/api/control/remove", map[string]string{"id": document.ID})
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("remove status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	if documents := d.updater.documentSnapshot(); len(documents) != 0 {
		t.Fatalf("documents after remove = %#v", documents)
	}
	registry, err := loadRegistry(filepath.Join(stateDir, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Documents) != 0 {
		t.Fatalf("registry after remove = %#v", registry.Documents)
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

func TestDaemonDefaultRequestPolicyRejectsNetworkRequest(t *testing.T) {
	d, err := newDaemonServer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.close)

	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	request.Host = "192.0.2.10:7332"
	request.RemoteAddr = "192.0.2.20:41000"
	recorder := httptest.NewRecorder()
	d.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("network request status = %d", recorder.Code)
	}
}

func TestDaemonConfiguredRequestPolicyAllowsNetworkRequest(t *testing.T) {
	stateDir := t.TempDir()
	configPath := filepath.Join(stateDir, daemonConfigFileName)
	config := `{"listenOnAllInterfaces":true,"port":7444,"allowedHostnames":["mentat:7332"]}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := newDaemonServer(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.close)

	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	request.Host = "192.0.2.10:7444"
	request.RemoteAddr = "192.0.2.20:41000"
	recorder := httptest.NewRecorder()
	d.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("network request status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/control/status", strings.NewReader(`{}`))
	request.Host = "mentat:7444"
	request.RemoteAddr = "192.0.2.20:41000"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://mentat:7444")
	recorder = httptest.NewRecorder()
	d.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("same-origin control status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var status struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.URL != "http://localhost:7444" {
		t.Fatalf("status URL = %q", status.URL)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/control/list", strings.NewReader(`{}`))
	request.Host = "mentat:7444"
	request.RemoteAddr = "192.0.2.20:41000"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://other:7444")
	recorder = httptest.NewRecorder()
	d.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-origin control status = %d", recorder.Code)
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
