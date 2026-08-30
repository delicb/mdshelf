package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const registryVersion = 1

var documentIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

type registryDocument struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

type registryFile struct {
	Version   int                `json:"version"`
	Documents []registryDocument `json:"documents"`
}

func daemonStateDir() (string, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(config, "mdshelf"), nil
}

func canonicalDocumentPath(input string) (string, error) {
	if input == "" || !isMarkdownPath(input) {
		return "", errNotMarkdownDocument
	}
	absolute, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("resolve document path: %w", err)
	}
	absolute = filepath.Clean(absolute)
	parent := filepath.Dir(absolute)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err == nil {
		base, err := actualDirectoryEntryName(resolvedParent, filepath.Base(absolute))
		if err != nil {
			return "", fmt.Errorf("resolve document name: %w", err)
		}
		if isCaseInsensitiveDirectory(resolvedParent) {
			base = strings.ToLower(base)
		}
		return filepath.Join(resolvedParent, base), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("resolve document parent: %w", err)
	}
	return absolute, nil
}

func actualDirectoryEntryName(parent, name string) (string, error) {
	path := filepath.Join(parent, name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return name, nil
	}
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.Name() == name {
			return name, nil
		}
	}
	for _, entry := range entries {
		if !strings.EqualFold(entry.Name(), name) {
			continue
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return "", err
		}
		if os.SameFile(info, entryInfo) {
			return entry.Name(), nil
		}
	}
	return name, nil
}

func displayDocumentPath(path string) string {
	name, err := actualDirectoryEntryName(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return path
	}
	return filepath.Join(filepath.Dir(path), name)
}

func isCaseInsensitiveDirectory(path string) bool {
	for {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() {
			return false
		}
		name, ok := swapASCIICase(filepath.Base(path))
		if ok {
			other, err := os.Lstat(filepath.Join(filepath.Dir(path), name))
			return err == nil && os.SameFile(info, other)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
}

func swapASCIICase(value string) (string, bool) {
	bytes := []byte(value)
	for index, char := range bytes {
		switch {
		case char >= 'a' && char <= 'z':
			bytes[index] = char - ('a' - 'A')
			return string(bytes), true
		case char >= 'A' && char <= 'Z':
			bytes[index] = char + ('a' - 'A')
			return string(bytes), true
		}
	}
	return value, false
}

func documentID(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:12])
}

func loadRegistry(path string) (registryFile, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return registryFile{Version: registryVersion, Documents: []registryDocument{}}, nil
	}
	if err != nil {
		return registryFile{}, fmt.Errorf("open registry: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4<<20))
	decoder.DisallowUnknownFields()
	var registry registryFile
	if err := decoder.Decode(&registry); err != nil {
		return registryFile{}, fmt.Errorf("decode registry: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return registryFile{}, fmt.Errorf("decode registry: %w", err)
	}
	if err := validateRegistry(registry); err != nil {
		return registryFile{}, err
	}
	return registry, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateRegistry(registry registryFile) error {
	if registry.Version != registryVersion {
		return fmt.Errorf("unsupported registry version %d", registry.Version)
	}
	ids := make(map[string]string)
	paths := make(map[string]struct{})
	for _, document := range registry.Documents {
		if !documentIDPattern.MatchString(document.ID) {
			return fmt.Errorf("invalid document id %q", document.ID)
		}
		if !filepath.IsAbs(document.Path) || filepath.Clean(document.Path) != document.Path || !isMarkdownPath(document.Path) {
			return fmt.Errorf("invalid document path %q", document.Path)
		}
		if other, ok := ids[document.ID]; ok {
			return fmt.Errorf("duplicate document id for %q and %q", other, document.Path)
		}
		if _, ok := paths[document.Path]; ok {
			return fmt.Errorf("duplicate document path %q", document.Path)
		}
		if document.ID != documentID(document.Path) {
			return fmt.Errorf("document id does not match path %q", document.Path)
		}
		ids[document.ID] = document.Path
		paths[document.Path] = struct{}{}
	}
	return nil
}

func saveRegistry(path string, documents []registryDocument) error {
	sorted := append([]registryDocument(nil), documents...)
	sort.Slice(sorted, func(i, j int) bool { return strings.Compare(sorted[i].Path, sorted[j].Path) < 0 })
	registry := registryFile{Version: registryVersion, Documents: sorted}
	if err := validateRegistry(registry); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".registry-*.tmp")
	if err != nil {
		return fmt.Errorf("create registry temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	remove := true
	defer func() {
		_ = temporary.Close()
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("set registry permissions: %w", err)
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(registry); err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("flush registry: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close registry: %w", err)
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("replace registry: %w", err)
	}
	remove = false
	if directory, err := os.Open(filepath.Dir(path)); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}
