package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"time"
)

// index.html links its own stylesheets and scripts with a "?v=dev" cache
// buster. At startup the placeholder is replaced with a short hash over the
// content of those assets, so a rebuilt binary busts browser caches without
// hand-maintained version numbers. Vendored libraries keep their upstream
// version numbers in index.html and are not rewritten.

const assetVersionPlaceholder = "?v=dev"

// hashedWebAssets are the files, relative to the embedded web root, whose
// content feeds the cache-busting hash. Keep this list in sync with the
// "?v=dev" references in web/index.html.
var hashedWebAssets = []string{
	"app.css",
	"app.js",
	"chroma.css",
	"text-selection.js",
	"vendor/fonts/fonts.css",
}

// webAssetVersion returns the first 12 hex characters of a SHA-256 over the
// hashed asset files.
func webAssetVersion(web fs.FS) (string, error) {
	digest := sha256.New()
	for _, name := range hashedWebAssets {
		data, err := fs.ReadFile(web, name)
		if err != nil {
			return "", fmt.Errorf("hash embedded asset %s: %w", name, err)
		}
		digest.Write([]byte(name))
		digest.Write([]byte{0})
		digest.Write(data)
		digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))[:12], nil
}

// versionedIndexHTML returns index.html with every cache-buster placeholder
// replaced by the content hash. A missing placeholder is an error, so a
// stale copy of index.html fails at startup instead of shipping stale assets.
func versionedIndexHTML(web fs.FS) ([]byte, error) {
	raw, err := fs.ReadFile(web, "index.html")
	if err != nil {
		return nil, fmt.Errorf("load embedded index.html: %w", err)
	}
	if !bytes.Contains(raw, []byte(assetVersionPlaceholder)) {
		return nil, fmt.Errorf("index.html has no %q cache-buster placeholder to rewrite", assetVersionPlaceholder)
	}
	version, err := webAssetVersion(web)
	if err != nil {
		return nil, err
	}
	return bytes.ReplaceAll(raw, []byte(assetVersionPlaceholder), []byte("?v="+version)), nil
}

// embeddedWebHandler serves the embedded web assets, with index.html's asset
// links carrying the content hash.
func embeddedWebHandler() (http.Handler, error) {
	web, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		return nil, fmt.Errorf("load embedded web files: %w", err)
	}
	index, err := versionedIndexHTML(web)
	if err != nil {
		return nil, err
	}
	return http.FileServer(http.FS(replacedFileFS{FS: web, name: "index.html", data: index})), nil
}

// replacedFileFS serves one file from memory and everything else from the
// wrapped fs.FS.
type replacedFileFS struct {
	fs.FS
	name string
	data []byte
}

func (r replacedFileFS) Open(name string) (fs.File, error) {
	if name != r.name {
		return r.FS.Open(name)
	}
	return &memFile{
		Reader: bytes.NewReader(r.data),
		info:   memFileInfo{name: r.name, size: int64(len(r.data))},
	}, nil
}

type memFile struct {
	*bytes.Reader
	info memFileInfo
}

func (f *memFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *memFile) Close() error               { return nil }

type memFileInfo struct {
	name string
	size int64
}

func (i memFileInfo) Name() string       { return i.name }
func (i memFileInfo) Size() int64        { return i.size }
func (i memFileInfo) Mode() fs.FileMode  { return 0o444 }
func (i memFileInfo) ModTime() time.Time { return time.Time{} }
func (i memFileInfo) IsDir() bool        { return false }
func (i memFileInfo) Sys() any           { return nil }
