package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
)

const daemonProtocol = 1

type daemonServer struct {
	updater   *daemonUpdater
	markdown  goldmark.Markdown
	handler   http.Handler
	startedAt time.Time
	stop      chan struct{}
	stopOnce  sync.Once
}

type daemonDocumentResponse struct {
	ID      string `json:"id"`
	Path    string `json:"path,omitempty"`
	Title   string `json:"title"`
	Removed bool   `json:"removed"`
	URL     string `json:"url,omitempty"`
}

func newDaemonServer(stateDir string) (*daemonServer, error) {
	return newDaemonServerWithUpdaterOptions(stateDir, daemonUpdaterOptions{})
}

func newDaemonServerWithUpdaterOptions(stateDir string, updaterOptions daemonUpdaterOptions) (*daemonServer, error) {
	if stateDir == "" {
		var err error
		stateDir, err = daemonStateDir()
		if err != nil {
			return nil, err
		}
	}
	registryPath := filepath.Join(stateDir, "registry.json")
	registry, err := loadRegistry(registryPath)
	if err != nil {
		return nil, err
	}
	web, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		return nil, fmt.Errorf("load embedded web files: %w", err)
	}
	d := &daemonServer{
		updater:   newDaemonUpdaterWithOptions(registryPath, registry, updaterOptions),
		markdown:  newMarkdownRenderer(),
		startedAt: time.Now().UTC(),
		stop:      make(chan struct{}),
	}
	d.handler = d.routes(http.FileServer(http.FS(web)))
	return d, nil
}

func (d *daemonServer) close() { d.updater.close() }

func (d *daemonServer) routes(static http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/files", d.handleFiles)
	mux.HandleFunc("/api/render", d.handleRender)
	mux.HandleFunc("/api/asset", d.handleAsset)
	mux.HandleFunc("/api/watch", d.handleWatch)
	mux.HandleFunc("/api/health", d.handleHealth)
	mux.HandleFunc("/api/control/add", d.handleControlAdd)
	mux.HandleFunc("/api/control/list", d.handleControlList)
	mux.HandleFunc("/api/control/remove", d.handleControlRemove)
	mux.HandleFunc("/api/control/status", d.handleControlStatus)
	mux.HandleFunc("/api/control/stop", d.handleControlStop)
	mux.HandleFunc("/api", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONError(w, http.StatusNotFound, "API endpoint not found")
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONError(w, http.StatusNotFound, "API endpoint not found")
	})
	mux.Handle("/", revalidateStatic(static))
	return d.requestPolicy(securityHeaders(mux))
}

func (d *daemonServer) requestPolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		validHost := host == "localhost:"+strconv.Itoa(defaultDaemonPort) || host == "127.0.0.1:"+strconv.Itoa(defaultDaemonPort)
		remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
		remoteIP := net.ParseIP(remoteHost)
		if !validHost || err != nil || remoteIP == nil || !remoteIP.IsLoopback() {
			writeJSONError(w, http.StatusForbidden, "daemon accepts local requests only")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func serveDaemon(stateDir string) error {
	d, err := newDaemonServer(stateDir)
	if err != nil {
		return err
	}
	defer d.close()
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(defaultDaemonPort)))
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           d.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	log.Printf("MDShelf daemon listens on http://localhost:%d", defaultDaemonPort)
	<-d.stop
	_ = listener.Close()
	d.close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := server.Shutdown(ctx)
	if shutdownErr != nil {
		_ = server.Close()
	}
	serveErr := <-serveDone
	if shutdownErr != nil {
		return shutdownErr
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && !errors.Is(serveErr, net.ErrClosed) {
		return serveErr
	}
	return nil
}

func (d *daemonServer) handleFiles(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	type fileRow struct {
		Path    string `json:"path"`
		Title   string `json:"title"`
		Removed bool   `json:"removed"`
	}
	rows := make([]fileRow, 0)
	for _, document := range d.updater.sortedDocuments() {
		rows = append(rows, fileRow{Path: document.ID, Title: document.title, Removed: document.removed})
	}
	writeJSON(w, http.StatusOK, struct {
		Mode  string    `json:"mode"`
		Files []fileRow `json:"files"`
	}{Mode: "daemon", Files: rows})
}

func (d *daemonServer) handleRender(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	id := r.URL.Query().Get("path")
	if id == demoDocumentPath {
		rendered, err := renderDemo(d.markdown)
		if err != nil {
			log.Printf("render demo: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "could not render demo")
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Path  string `json:"path"`
			Title string `json:"title"`
			HTML  string `json:"html"`
		}{Path: demoDocumentPath, Title: rendered.title, HTML: rendered.html})
		return
	}
	d.updater.mu.Lock()
	document := cloneDaemonDocument(d.updater.documents[id])
	paths := make(map[string]string, len(d.updater.paths))
	for filePath, registeredID := range d.updater.paths {
		paths[filePath] = registeredID
	}
	d.updater.mu.Unlock()
	if document == nil {
		writeJSONError(w, http.StatusNotFound, "Document not registered")
		return
	}
	if document.removed {
		writeJSONError(w, http.StatusNotFound, "Markdown file not found")
		return
	}
	rendered, err := renderMarkdownWithOptions(d.markdown, document.source, filepath.Base(document.Path), markdownRenderOptions{
		rewrite: func(node ast.Node) {
			rewriteDaemonImages(node, document)
			rewriteDaemonLinks(node, document, paths)
		},
		loadBibliography: func(reference string) ([]byte, error) {
			return loadDaemonBibliography(document.Path, reference)
		},
	})
	if err != nil {
		log.Printf("render daemon document: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "could not render Markdown file")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Path         string `json:"path"`
		AbsolutePath string `json:"absolutePath"`
		Title        string `json:"title"`
		HTML         string `json:"html"`
	}{
		Path:         document.ID,
		AbsolutePath: displayDocumentPath(document.Path),
		Title:        rendered.title,
		HTML:         rendered.html,
	})
}

func (d *daemonServer) handleAsset(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	id := r.URL.Query().Get("doc")
	rawPath := r.URL.Query().Get("path")
	if id == "" || rawPath == "" {
		writeJSONError(w, http.StatusBadRequest, "doc and path are required")
		return
	}
	d.updater.mu.Lock()
	document := cloneDaemonDocument(d.updater.documents[id])
	d.updater.mu.Unlock()
	if document == nil {
		writeJSONError(w, http.StatusNotFound, "Document not registered")
		return
	}
	if document.removed {
		writeJSONError(w, http.StatusNotFound, "Markdown file not found")
		return
	}
	cleanPath, err := cleanDaemonAssetPath(rawPath)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid path")
		return
	}
	expectedType, ok := rasterTypes[strings.ToLower(path.Ext(cleanPath))]
	if !ok {
		writeJSONError(w, http.StatusUnsupportedMediaType, "unsupported image type")
		return
	}
	root := filepath.Dir(document.Path)
	if err := checkPathSegments(root, cleanPath); err != nil {
		writeDaemonOpenError(w, err, "image")
		return
	}
	file, info, err := openRootedFile(root, cleanPath)
	if err != nil {
		writeDaemonOpenError(w, err, "image")
		return
	}
	defer file.Close()
	var header [512]byte
	n, err := io.ReadFull(file, header[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		writeJSONError(w, http.StatusInternalServerError, "could not read image")
		return
	}
	detectedType := http.DetectContentType(header[:n])
	if detectedType != expectedType {
		writeJSONError(w, http.StatusUnsupportedMediaType, "file content is not a supported image")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not read image")
		return
	}
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Type", detectedType)
	http.ServeContent(w, r, path.Base(cleanPath), info.ModTime(), file)
}

func (d *daemonServer) handleWatch(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	since, err := parseRevision(r.URL.Query().Get("since"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	batch := d.updater.feed.wait(r.Context(), since)
	if r.Context().Err() == nil {
		writeJSON(w, http.StatusOK, batch)
	}
}

func (d *daemonServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Service  string `json:"service"`
		Protocol int    `json:"protocol"`
		PID      int    `json:"pid"`
	}{Service: "mdshelf-daemon", Protocol: daemonProtocol, PID: os.Getpid()})
}

func (d *daemonServer) handleControlAdd(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Path string `json:"path"`
	}
	if !decodeControl(w, r, &request) {
		return
	}
	document, added, err := d.updater.add(request.Path)
	if err != nil {
		writeControlError(w, err)
		return
	}
	status := http.StatusOK
	if added {
		status = http.StatusCreated
	}
	writeJSON(w, status, struct {
		Document daemonDocumentResponse `json:"document"`
		Added    bool                   `json:"added"`
	}{Document: controlDocument(document), Added: added})
}

func (d *daemonServer) handleControlList(w http.ResponseWriter, r *http.Request) {
	var request struct{}
	if !decodeControl(w, r, &request) {
		return
	}
	rows := make([]daemonDocumentResponse, 0)
	for _, document := range d.updater.sortedDocuments() {
		rows = append(rows, controlDocument(document))
	}
	writeJSON(w, http.StatusOK, struct {
		Documents []daemonDocumentResponse `json:"documents"`
	}{Documents: rows})
}

func (d *daemonServer) handleControlRemove(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Path string `json:"path"`
	}
	if !decodeControl(w, r, &request) {
		return
	}
	document, err := d.updater.remove(request.Path)
	if err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Document daemonDocumentResponse `json:"document"`
	}{Document: controlDocument(document)})
}

func (d *daemonServer) handleControlStatus(w http.ResponseWriter, r *http.Request) {
	var request struct{}
	if !decodeControl(w, r, &request) {
		return
	}
	documents := d.updater.documentSnapshot()
	removed := 0
	for _, document := range documents {
		if document.removed {
			removed++
		}
	}
	writeJSON(w, http.StatusOK, struct {
		Service          string    `json:"service"`
		Protocol         int       `json:"protocol"`
		PID              int       `json:"pid"`
		URL              string    `json:"url"`
		StartedAt        time.Time `json:"startedAt"`
		Documents        int       `json:"documents"`
		RemovedDocuments int       `json:"removedDocuments"`
	}{"mdshelf-daemon", daemonProtocol, os.Getpid(), daemonBaseURL(), d.startedAt, len(documents), removed})
}

func (d *daemonServer) handleControlStop(w http.ResponseWriter, r *http.Request) {
	var request struct{}
	if !decodeControl(w, r, &request) {
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Stopping bool `json:"stopping"`
	}{Stopping: true})
	d.stopOnce.Do(func() { close(d.stop) })
}

func decodeControl(w http.ResponseWriter, r *http.Request, target any) bool {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "application/json" {
		writeJSONError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	origin := r.Header.Get("Origin")
	if origin != "" && origin != daemonBaseURL() && origin != "http://127.0.0.1:"+strconv.Itoa(defaultDaemonPort) {
		writeJSONError(w, http.StatusForbidden, "origin is not allowed")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON request")
		return false
	}
	if err := ensureJSONEnd(decoder); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON request")
		return false
	}
	return true
}

func controlDocument(document *daemonDocument) daemonDocumentResponse {
	return daemonDocumentResponse{
		ID: document.ID, Path: document.Path, Title: document.title, Removed: document.removed,
		URL: daemonBaseURL() + "/#/" + document.ID,
	}
}

func writeControlError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, errNotRegular):
		writeJSONError(w, http.StatusNotFound, "Markdown file not found")
	case errors.Is(err, errSymlink), errors.Is(err, errInvalidPath):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case strings.Contains(err.Error(), "collision"):
		writeJSONError(w, http.StatusConflict, err.Error())
	default:
		writeJSONError(w, http.StatusBadRequest, err.Error())
	}
}

func daemonBaseURL() string { return "http://localhost:" + strconv.Itoa(defaultDaemonPort) }

func cleanDaemonAssetPath(rawPath string) (string, error) {
	if rawPath == "" || strings.ContainsRune(rawPath, '\x00') || strings.Contains(rawPath, "\\") || strings.HasPrefix(rawPath, "/") {
		return "", errInvalidPath
	}
	for _, part := range strings.Split(rawPath, "/") {
		if part == "" || part == "." || part == ".." {
			return "", errInvalidPath
		}
	}
	clean := path.Clean(rawPath)
	local := filepath.FromSlash(clean)
	if clean == "." || filepath.IsAbs(local) || filepath.VolumeName(local) != "" {
		return "", errInvalidPath
	}
	return clean, nil
}

func checkPathSegments(root, cleanPath string) error {
	current := root
	parts := strings.Split(cleanPath, "/")
	for index, part := range parts {
		current = filepath.Join(current, filepath.FromSlash(part))
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errSymlink
		}
		if index < len(parts)-1 && !info.IsDir() {
			return fs.ErrNotExist
		}
		if index == len(parts)-1 && !info.Mode().IsRegular() {
			return errNotRegular
		}
	}
	if !isWithinRoot(root, current) {
		return errInvalidPath
	}
	return nil
}

func writeDaemonOpenError(w http.ResponseWriter, err error, noun string) {
	switch {
	case errors.Is(err, errInvalidPath), errors.Is(err, errSymlink):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, errNotRegular):
		writeJSONError(w, http.StatusNotFound, noun+" not found")
	case errors.Is(err, fs.ErrPermission):
		writeJSONError(w, http.StatusForbidden, noun+" cannot be read")
	default:
		writeJSONError(w, http.StatusInternalServerError, "could not open "+noun)
	}
}

func rewriteDaemonImages(document ast.Node, registered *daemonDocument) {
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		image, ok := node.(*ast.Image)
		if !ok {
			return ast.WalkContinue, nil
		}
		destination, err := url.Parse(string(image.Destination))
		if err != nil || destination.Scheme != "" || destination.Host != "" || destination.Path == "" {
			return ast.WalkContinue, nil
		}
		imagePath, err := url.PathUnescape(destination.Path)
		if err != nil || strings.Contains(imagePath, "\\") {
			return ast.WalkContinue, nil
		}
		imagePath = path.Clean(strings.TrimPrefix(imagePath, "/"))
		imagePath, err = cleanDaemonAssetPath(imagePath)
		if err != nil {
			return ast.WalkContinue, nil
		}
		asset := url.URL{Path: "/api/asset", RawQuery: url.Values{"doc": {registered.ID}, "path": {imagePath}}.Encode(), Fragment: destination.Fragment}
		image.Destination = []byte(asset.String())
		return ast.WalkContinue, nil
	})
}

func rewriteDaemonLinks(document ast.Node, registered *daemonDocument, paths map[string]string) {
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		link, ok := node.(*ast.Link)
		if !ok {
			return ast.WalkContinue, nil
		}
		destination, err := url.Parse(string(link.Destination))
		if err != nil || destination.Scheme != "" || destination.Host != "" {
			return ast.WalkContinue, nil
		}
		if destination.Path == "" && destination.Fragment != "" {
			link.Destination = []byte("#/" + registered.ID + "#" + url.PathEscape(destination.Fragment))
			return ast.WalkContinue, nil
		}
		target, err := url.PathUnescape(destination.Path)
		if err != nil || !isMarkdownPath(target) {
			return ast.WalkContinue, nil
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(registered.Path), filepath.FromSlash(target))
		}
		canonical, err := canonicalDocumentPath(target)
		if err != nil {
			return ast.WalkContinue, nil
		}
		id := paths[canonical]
		if id == "" {
			return ast.WalkContinue, nil
		}
		route := "#/" + id
		if destination.Fragment != "" {
			route += "#" + url.PathEscape(destination.Fragment)
		}
		link.Destination = []byte(route)
		return ast.WalkContinue, nil
	})
}
