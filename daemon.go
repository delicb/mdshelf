package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	config    daemonConfig
	updater   *daemonUpdater
	reviews   *reviewStore
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
	config, err := loadDaemonConfig(stateDir)
	if err != nil {
		return nil, err
	}
	registryPath := filepath.Join(stateDir, "registry.json")
	registry, err := loadRegistry(registryPath)
	if err != nil {
		return nil, err
	}
	reviews, err := newReviewStore(filepath.Join(stateDir, "reviews.json"))
	if err != nil {
		return nil, err
	}
	web, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		return nil, fmt.Errorf("load embedded web files: %w", err)
	}
	d := &daemonServer{
		config:    config,
		updater:   newDaemonUpdaterWithOptions(registryPath, registry, updaterOptions),
		reviews:   reviews,
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
	handleMethod(mux, http.MethodGet, "/api/files", d.handleFiles)
	handleMethod(mux, http.MethodGet, "/api/render", d.handleRender)
	handleMethod(mux, http.MethodGet, "/api/asset", d.handleAsset)
	handleMethod(mux, http.MethodGet, "/api/watch", d.handleWatch)
	handleMethod(mux, http.MethodGet, "/api/health", d.handleHealth)
	handleMethod(mux, http.MethodGet, "/api/review", d.handleReview)
	handleMethod(mux, http.MethodPost, "/api/control/add", d.handleControlAdd)
	handleMethod(mux, http.MethodPost, "/api/control/list", d.handleControlList)
	handleMethod(mux, http.MethodPost, "/api/control/remove", d.handleControlRemove)
	handleMethod(mux, http.MethodPost, "/api/control/status", d.handleControlStatus)
	handleMethod(mux, http.MethodPost, "/api/control/stop", d.handleControlStop)
	handleMethod(mux, http.MethodPost, "/api/control/review/comments/add", d.handleControlReviewCommentAdd)
	handleMethod(mux, http.MethodPost, "/api/control/review/comments/reply", d.handleControlReviewCommentReply)
	handleMethod(mux, http.MethodPost, "/api/control/review/comments/address", d.handleControlReviewCommentAddress)
	handleMethod(mux, http.MethodPost, "/api/control/review/comments/resolve", d.handleControlReviewCommentResolve)
	handleMethod(mux, http.MethodPost, "/api/control/review/comments/reopen", d.handleControlReviewCommentReopen)
	handleMethod(mux, http.MethodPost, "/api/control/review/show", d.handleControlReviewShow)
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
		remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
		remoteIP := net.ParseIP(remoteHost)
		validRemote := err == nil && remoteIP != nil
		if !d.config.ListenOnAllInterfaces {
			validRemote = validRemote && remoteIP.IsLoopback()
		}
		if !validRemote || !validDaemonHost(r.Host, d.config) {
			writeJSONError(w, http.StatusForbidden, "daemon request is not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validDaemonHost(host string, config daemonConfig) bool {
	hostname, port, err := net.SplitHostPort(host)
	if err != nil || port != strconv.Itoa(config.Port) {
		return false
	}
	normalized := strings.ToLower(strings.TrimSuffix(hostname, "."))
	if normalized == "localhost" {
		return true
	}
	ipHostname := normalized
	if zone := strings.LastIndexByte(ipHostname, '%'); zone >= 0 {
		ipHostname = ipHostname[:zone]
	}
	if ip := net.ParseIP(ipHostname); ip != nil {
		return config.ListenOnAllInterfaces || ip.IsLoopback()
	}
	for _, allowed := range config.AllowedHostnames {
		if normalized == allowed {
			return true
		}
	}
	return false
}

func serveDaemon(stateDir string) error {
	d, err := newDaemonServer(stateDir)
	if err != nil {
		return err
	}
	defer d.close()
	listener, err := net.Listen("tcp", daemonListenAddress(d.config))
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
	if d.config.ListenOnAllInterfaces {
		log.Printf("MDShelf daemon listens on all interfaces on port %d", d.config.Port)
		log.Printf("Local:   %s", daemonBaseURL(d.config.Port))
		for _, address := range networkURLs(strconv.Itoa(d.config.Port)) {
			log.Printf("Network: %s", address)
		}
	} else {
		log.Printf("MDShelf daemon listens on %s", daemonBaseURL(d.config.Port))
	}
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
	type fileRow struct {
		Path         string               `json:"path"`
		Title        string               `json:"title"`
		Removed      bool                 `json:"removed"`
		ReviewStatus documentReviewStatus `json:"reviewStatus"`
		OpenComments int                  `json:"openComments"`
	}
	rows := make([]fileRow, 0)
	for _, document := range d.updater.sortedDocuments() {
		status, openComments := d.reviews.summary(document.ID, document.Path, sourceHash(document.source), document.removed)
		rows = append(rows, fileRow{
			Path: document.ID, Title: document.title, Removed: document.removed,
			ReviewStatus: status, OpenComments: openComments,
		})
	}
	writeJSON(w, http.StatusOK, struct {
		Mode  string    `json:"mode"`
		Files []fileRow `json:"files"`
	}{Mode: "daemon", Files: rows})
}

func (d *daemonServer) handleRender(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("path")
	if id == demoDocumentPath {
		rendered, err := renderDemo(d.markdown)
		if err != nil {
			log.Printf("render demo: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "could not render demo")
			return
		}
		writeJSON(w, http.StatusOK, newRenderResponse(demoDocumentPath, "", rendered))
		return
	}
	document, paths := d.updater.documentAndPaths(id)
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
	writeJSON(w, http.StatusOK, newRenderResponse(document.ID, displayDocumentPath(document.Path), rendered))
}

func (d *daemonServer) handleAsset(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("doc")
	rawPath := r.URL.Query().Get("path")
	if id == "" || rawPath == "" {
		writeJSONError(w, http.StatusBadRequest, "doc and path are required")
		return
	}
	document := d.updater.document(id)
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
		writeOpenError(w, err, "image")
		return
	}
	file, info, err := openRootedFile(root, cleanPath)
	if err != nil {
		writeOpenError(w, err, "image")
		return
	}
	defer file.Close()
	serveRasterImage(w, r, file, info, cleanPath, expectedType)
}

func (d *daemonServer) handleWatch(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, struct {
		Service  string   `json:"service"`
		Protocol int      `json:"protocol"`
		PID      int      `json:"pid"`
		Features []string `json:"features"`
	}{Service: "mdshelf-daemon", Protocol: daemonProtocol, PID: os.Getpid(), Features: []string{reviewFeature}})
}

func (d *daemonServer) handleControlAdd(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Path string `json:"path"`
	}
	if !d.decodeControl(w, r, &request) {
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
	}{Document: d.controlDocument(document), Added: added})
}

func (d *daemonServer) handleControlList(w http.ResponseWriter, r *http.Request) {
	var request struct{}
	if !d.decodeControl(w, r, &request) {
		return
	}
	rows := make([]daemonDocumentResponse, 0)
	for _, document := range d.updater.sortedDocuments() {
		rows = append(rows, d.controlDocument(document))
	}
	writeJSON(w, http.StatusOK, struct {
		Documents []daemonDocumentResponse `json:"documents"`
	}{Documents: rows})
}

func (d *daemonServer) handleControlRemove(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Path string `json:"path"`
		ID   string `json:"id"`
	}
	if !d.decodeControl(w, r, &request) {
		return
	}
	if (request.Path == "") == (request.ID == "") {
		writeJSONError(w, http.StatusBadRequest, "set either path or id, but not both")
		return
	}
	if request.ID != "" {
		document := d.updater.document(request.ID)
		if document == nil {
			writeJSONError(w, http.StatusNotFound, "Document not registered")
			return
		}
		request.Path = document.Path
	}
	document, err := d.updater.remove(request.Path)
	if err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Document daemonDocumentResponse `json:"document"`
	}{Document: d.controlDocument(document)})
}

func (d *daemonServer) handleControlStatus(w http.ResponseWriter, r *http.Request) {
	var request struct{}
	if !d.decodeControl(w, r, &request) {
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
	}{"mdshelf-daemon", daemonProtocol, os.Getpid(), daemonBaseURL(d.config.Port), d.startedAt, len(documents), removed})
}

func (d *daemonServer) handleControlStop(w http.ResponseWriter, r *http.Request) {
	var request struct{}
	if !d.decodeControl(w, r, &request) {
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Stopping bool `json:"stopping"`
	}{Stopping: true})
	d.stopOnce.Do(func() { close(d.stop) })
}

func (d *daemonServer) decodeControl(w http.ResponseWriter, r *http.Request, target any) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "application/json" {
		writeJSONError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	if !validControlOrigin(r, d.config) {
		writeJSONError(w, http.StatusForbidden, "origin is not allowed")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxReviewWriteRequest))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSONErrorCode(w, http.StatusRequestEntityTooLarge, "JSON request is too large.", reviewCodeLimit)
			return false
		}
		writeJSONError(w, http.StatusBadRequest, "invalid JSON request")
		return false
	}
	if err := ensureJSONEnd(decoder); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON request")
		return false
	}
	return true
}

func validControlOrigin(r *http.Request, config daemonConfig) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if strings.EqualFold(parsed.Host, r.Host) {
		return true
	}
	localConfig := daemonConfig{Port: config.Port}
	return validDaemonHost(parsed.Host, localConfig) && validDaemonHost(r.Host, localConfig)
}

func (d *daemonServer) controlDocument(document *daemonDocument) daemonDocumentResponse {
	return daemonDocumentResponse{
		ID: document.ID, Path: document.Path, Title: document.title, Removed: document.removed,
		URL: daemonBaseURL(d.config.Port) + "/#/" + document.ID,
	}
}

func writeControlError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, errNotRegular):
		writeJSONError(w, http.StatusNotFound, "Markdown file not found")
	case errors.Is(err, errSymlink), errors.Is(err, errInvalidPath),
		errors.Is(err, errNotMarkdownDocument), errors.Is(err, errMarkdownTooLarge):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, errDocumentIDCollision):
		writeJSONError(w, http.StatusConflict, err.Error())
	default:
		log.Printf("daemon control request: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "could not update the daemon registry")
	}
}

func cleanDaemonAssetPath(rawPath string) (string, error) {
	return cleanRelativeFilePath(rawPath, true)
}

func rewriteDaemonImages(document ast.Node, registered *daemonDocument) {
	rewriteImages(document, func(imagePath string) *url.URL {
		imagePath = path.Clean(strings.TrimPrefix(imagePath, "/"))
		imagePath, err := cleanDaemonAssetPath(imagePath)
		if err != nil {
			return nil
		}
		return &url.URL{
			Path:     "/api/asset",
			RawQuery: url.Values{"doc": {registered.ID}, "path": {imagePath}}.Encode(),
		}
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
