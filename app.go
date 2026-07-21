package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

const maxMarkdownSize = 8 << 20

var (
	errInvalidPath = errors.New("invalid path")
	errNotRegular  = errors.New("not a regular file")
	errSymlink     = errors.New("symlinks are not allowed")
)

//go:embed web/*
var embeddedWeb embed.FS

type app struct {
	root     string
	markdown goldmark.Markdown
	handler  http.Handler
}

func newApp(root string) (*app, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}

	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}

	info, err := os.Stat(resolvedRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect root: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("root is not a directory")
	}

	web, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		return nil, fmt.Errorf("load embedded web files: %w", err)
	}

	a := &app{
		root: resolvedRoot,
		markdown: goldmark.New(
			goldmark.WithExtensions(
				extension.GFM,
				highlighting.NewHighlighting(
					highlighting.WithStyle("github"),
					highlighting.WithFormatOptions(
						chromahtml.WithClasses(true),
						chromahtml.WithCSSComments(false),
					),
				),
			),
			goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		),
	}
	a.handler = a.routes(http.FileServer(http.FS(web)))
	return a, nil
}

func (a *app) Handler() http.Handler {
	return a.handler
}

func (a *app) routes(static http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/files", a.handleFiles)
	mux.HandleFunc("/api/render", a.handleRender)
	mux.HandleFunc("/api/asset", a.handleAsset)
	notFound := func(w http.ResponseWriter, r *http.Request) {
		writeJSONError(w, http.StatusNotFound, "API endpoint not found")
	}
	mux.HandleFunc("/api", notFound)
	mux.HandleFunc("/api/", notFound)
	mux.Handle("/", static)
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; img-src 'self' data: http: https:")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (a *app) handleFiles(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}

	files, err := a.markdownFiles()
	if err != nil {
		log.Printf("list Markdown files: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "could not list Markdown files")
		return
	}

	writeJSON(w, http.StatusOK, struct {
		Files []string `json:"files"`
	}{Files: files})
}

func (a *app) handleRender(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}

	rawPath := r.URL.Query().Get("path")
	if rawPath == "" {
		writeJSONError(w, http.StatusBadRequest, "path is required")
		return
	}

	ext := strings.ToLower(path.Ext(rawPath))
	if ext != ".md" && ext != ".markdown" {
		writeJSONError(w, http.StatusBadRequest, "path must point to a Markdown file")
		return
	}

	file, cleanPath, info, err := a.openFile(rawPath)
	if err != nil {
		a.writeOpenError(w, err, "Markdown file")
		return
	}
	defer file.Close()

	if info.Size() > maxMarkdownSize {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "Markdown file is too large")
		return
	}

	source, err := io.ReadAll(io.LimitReader(file, maxMarkdownSize+1))
	if err != nil {
		log.Printf("read Markdown file %q: %v", cleanPath, err)
		writeJSONError(w, http.StatusInternalServerError, "could not read Markdown file")
		return
	}
	if len(source) > maxMarkdownSize {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "Markdown file is too large")
		return
	}

	document := a.markdown.Parser().Parse(text.NewReader(source))
	rewriteLocalImages(document, cleanPath)

	var rendered bytes.Buffer
	if err := a.markdown.Renderer().Render(&rendered, source, document); err != nil {
		log.Printf("render Markdown file %q: %v", cleanPath, err)
		writeJSONError(w, http.StatusInternalServerError, "could not render Markdown file")
		return
	}

	title := documentTitle(document, source, cleanPath)
	writeJSON(w, http.StatusOK, struct {
		Path  string `json:"path"`
		Title string `json:"title"`
		HTML  string `json:"html"`
	}{Path: cleanPath, Title: title, HTML: rendered.String()})
}

func (a *app) handleAsset(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}

	rawPath := r.URL.Query().Get("path")
	if rawPath == "" {
		writeJSONError(w, http.StatusBadRequest, "path is required")
		return
	}

	expectedType, ok := rasterTypes[strings.ToLower(path.Ext(rawPath))]
	if !ok {
		writeJSONError(w, http.StatusUnsupportedMediaType, "unsupported image type")
		return
	}

	file, cleanPath, info, err := a.openFile(rawPath)
	if err != nil {
		a.writeOpenError(w, err, "image")
		return
	}
	defer file.Close()

	var header [512]byte
	n, err := io.ReadFull(file, header[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		log.Printf("read image %q: %v", cleanPath, err)
		writeJSONError(w, http.StatusInternalServerError, "could not read image")
		return
	}

	detectedType := http.DetectContentType(header[:n])
	if detectedType != expectedType {
		writeJSONError(w, http.StatusUnsupportedMediaType, "file content is not a supported image")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		log.Printf("seek image %q: %v", cleanPath, err)
		writeJSONError(w, http.StatusInternalServerError, "could not read image")
		return
	}

	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Type", detectedType)
	http.ServeContent(w, r, path.Base(cleanPath), info.ModTime(), file)
}

var rasterTypes = map[string]string{
	".gif":  "image/gif",
	".jpeg": "image/jpeg",
	".jpg":  "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
}

func (a *app) markdownFiles() ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(a.root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == a.root {
			return nil
		}

		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".md" && ext != ".markdown" {
			return nil
		}

		relative, err := filepath.Rel(a.root, filePath)
		if err != nil {
			return err
		}
		cleanPath, err := cleanRelativePath(filepath.ToSlash(relative))
		if err != nil {
			return nil
		}
		files = append(files, cleanPath)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

func (a *app) openFile(rawPath string) (*os.File, string, os.FileInfo, error) {
	cleanPath, err := cleanRelativePath(rawPath)
	if err != nil {
		return nil, "", nil, err
	}

	current := a.root
	parts := strings.Split(cleanPath, "/")
	for index, part := range parts {
		current = filepath.Join(current, filepath.FromSlash(part))
		info, err := os.Lstat(current)
		if err != nil {
			return nil, "", nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, "", nil, errSymlink
		}
		if index < len(parts)-1 && !info.IsDir() {
			return nil, "", nil, fs.ErrNotExist
		}
		if index == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, "", nil, errNotRegular
		}
	}

	resolved, err := filepath.EvalSymlinks(current)
	if err != nil {
		return nil, "", nil, err
	}
	if !isWithinRoot(a.root, resolved) {
		return nil, "", nil, errInvalidPath
	}

	file, err := os.Open(resolved)
	if err != nil {
		return nil, "", nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, "", nil, err
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, "", nil, errNotRegular
	}
	return file, cleanPath, info, nil
}

func cleanRelativePath(rawPath string) (string, error) {
	if rawPath == "" || strings.ContainsRune(rawPath, '\x00') || strings.Contains(rawPath, "\\") || strings.HasPrefix(rawPath, "/") {
		return "", errInvalidPath
	}

	for _, part := range strings.Split(rawPath, "/") {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") {
			return "", errInvalidPath
		}
	}

	cleanPath := path.Clean(rawPath)
	localPath := filepath.FromSlash(cleanPath)
	if cleanPath == "." || filepath.IsAbs(localPath) || filepath.VolumeName(localPath) != "" {
		return "", errInvalidPath
	}
	return cleanPath, nil
}

func isWithinRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func rewriteLocalImages(document ast.Node, documentPath string) {
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

		var resolved string
		if strings.HasPrefix(imagePath, "/") {
			resolved = path.Clean(strings.TrimPrefix(imagePath, "/"))
		} else {
			resolved = path.Clean(path.Join(path.Dir(documentPath), imagePath))
		}
		resolved, err = cleanRelativePath(resolved)
		if err != nil {
			return ast.WalkContinue, nil
		}

		assetURL := url.URL{
			Path:     "/api/asset",
			RawQuery: url.Values{"path": []string{resolved}}.Encode(),
			Fragment: destination.Fragment,
		}
		image.Destination = []byte(assetURL.String())
		return ast.WalkContinue, nil
	})
}

func documentTitle(document ast.Node, source []byte, documentPath string) string {
	var title string
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || title != "" {
			return ast.WalkContinue, nil
		}
		heading, ok := node.(*ast.Heading)
		if ok && heading.Level == 1 {
			title = inlineText(heading, source)
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	if title != "" {
		return title
	}
	return strings.TrimSuffix(path.Base(documentPath), path.Ext(documentPath))
}

func inlineText(node ast.Node, source []byte) string {
	var value strings.Builder
	var appendChildren func(ast.Node)
	appendChildren = func(parent ast.Node) {
		for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
			switch child := child.(type) {
			case *ast.Text:
				value.Write(child.Value(source))
				if child.SoftLineBreak() || child.HardLineBreak() {
					value.WriteByte(' ')
				}
			case *ast.String:
				value.Write(child.Value)
			case *ast.AutoLink:
				value.Write(child.Label(source))
			default:
				appendChildren(child)
			}
		}
	}
	appendChildren(node)
	return strings.Join(strings.Fields(value.String()), " ")
}

func (a *app) writeOpenError(w http.ResponseWriter, err error, noun string) {
	switch {
	case errors.Is(err, errInvalidPath):
		writeJSONError(w, http.StatusBadRequest, "invalid path")
	case errors.Is(err, errSymlink):
		writeJSONError(w, http.StatusBadRequest, "symlinks are not allowed")
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, errNotRegular):
		writeJSONError(w, http.StatusNotFound, noun+" not found")
	case errors.Is(err, fs.ErrPermission):
		writeJSONError(w, http.StatusForbidden, noun+" cannot be read")
	default:
		log.Printf("open %s: %v", noun, err)
		writeJSONError(w, http.StatusInternalServerError, "could not open "+noun)
	}
}

func requireGET(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet {
		return true
	}
	w.Header().Set("Allow", http.MethodGet)
	writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
