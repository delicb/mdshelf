package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

const maxBibliographySize = 4 << 20

func validateBibliographyReference(reference string) error {
	if reference == "" || strings.ContainsAny(reference, `/\`) || path.Base(reference) != reference {
		return errors.New("bibliography must be a sibling file")
	}
	if !strings.EqualFold(path.Ext(reference), ".bib") {
		return errors.New("bibliography must be a BibTeX file")
	}
	return nil
}

func (a *app) loadBibliography(documentPath, reference string) ([]byte, error) {
	if err := validateBibliographyReference(reference); err != nil {
		return nil, err
	}
	file, _, info, err := a.openFile(path.Join(path.Dir(documentPath), reference))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readBibliography(file, info)
}

func loadDaemonBibliography(documentPath, reference string) ([]byte, error) {
	if err := validateBibliographyReference(reference); err != nil {
		return nil, err
	}
	root := filepath.Dir(documentPath)
	if err := checkPathSegments(root, reference); err != nil {
		return nil, err
	}
	file, info, err := openRootedFile(root, reference)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readBibliography(file, info)
}

func readBibliography(file io.Reader, info fs.FileInfo) ([]byte, error) {
	if info.Size() > maxBibliographySize {
		return nil, errors.New("bibliography is too large")
	}
	source, err := io.ReadAll(io.LimitReader(file, maxBibliographySize+1))
	if err != nil {
		return nil, fmt.Errorf("read bibliography: %w", err)
	}
	if len(source) > maxBibliographySize {
		return nil, errors.New("bibliography is too large")
	}
	return source, nil
}
