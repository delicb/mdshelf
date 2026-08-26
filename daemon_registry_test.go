package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDocumentIDIsStableAndOpaque(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	first := documentID(path)
	if len(first) != 24 || !documentIDPattern.MatchString(first) {
		t.Fatalf("documentID() = %q, want 24 lowercase hexadecimal characters", first)
	}
	if second := documentID(path); second != first {
		t.Fatalf("documentID() changed from %q to %q", first, second)
	}
}

func TestCanonicalDocumentPathNormalizesAliasesOnCaseInsensitiveFilesystem(t *testing.T) {
	root := t.TempDir()
	actual := mustWriteFile(t, root, "Note.md", "# Note\n")
	input := filepath.Join(root, "note.md")
	if _, err := os.Lstat(input); errors.Is(err, os.ErrNotExist) {
		t.Skip("filesystem is case-sensitive")
	} else if err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalDocumentPath(input)
	if err != nil {
		t.Fatal(err)
	}
	actualCanonical, err := canonicalDocumentPath(actual)
	if err != nil {
		t.Fatal(err)
	}
	if canonical != actualCanonical {
		t.Fatalf("alias canonical path = %q, actual canonical path = %q", canonical, actualCanonical)
	}
	wantDisplay, err := filepath.EvalSymlinks(actual)
	if err != nil {
		t.Fatal(err)
	}
	if display := displayDocumentPath(canonical); display != wantDisplay {
		t.Fatalf("display path = %q, want %q", display, wantDisplay)
	}
}

func TestCanonicalDocumentPathKeepsAliasIdentityAfterDeletion(t *testing.T) {
	root := t.TempDir()
	actual := mustWriteFile(t, root, "Note.md", "# Note\n")
	alias := filepath.Join(root, "note.md")
	if _, err := os.Lstat(alias); errors.Is(err, os.ErrNotExist) {
		t.Skip("filesystem is case-sensitive")
	} else if err != nil {
		t.Fatal(err)
	}
	before, err := canonicalDocumentPath(alias)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(actual); err != nil {
		t.Fatal(err)
	}
	after, err := canonicalDocumentPath(alias)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("canonical path after deletion = %q, want %q", after, before)
	}
}

func TestCanonicalDocumentPathPreservesCaseSensitiveLookup(t *testing.T) {
	root := t.TempDir()
	actual := mustWriteFile(t, root, "Note.md", "# Note\n")
	input := filepath.Join(root, "note.md")
	if _, err := os.Lstat(input); err == nil {
		t.Skip("filesystem is case-insensitive")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	canonical, err := canonicalDocumentPath(input)
	if err != nil {
		t.Fatal(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	wantInput := filepath.Join(resolvedRoot, filepath.Base(input))
	if canonical != wantInput {
		t.Fatalf("canonical missing path = %q, want %q", canonical, wantInput)
	}
	canonical, err = canonicalDocumentPath(actual)
	if err != nil {
		t.Fatal(err)
	}
	wantActual := filepath.Join(resolvedRoot, filepath.Base(actual))
	if canonical != wantActual {
		t.Fatalf("canonical existing path = %q, want %q", canonical, wantActual)
	}
}

func TestCanonicalDocumentPathNormalizesMissingNameForFilesystem(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Missing.md")
	canonical, err := canonicalDocumentPath(path)
	if err != nil {
		t.Fatal(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	base := "Missing.md"
	if isCaseInsensitiveDirectory(resolvedRoot) {
		base = "missing.md"
	}
	want := filepath.Join(resolvedRoot, base)
	if canonical != want {
		t.Fatalf("canonical path = %q, want %q", canonical, want)
	}
}

func TestRegistryRoundTripKeepsMissingDocuments(t *testing.T) {
	state := t.TempDir()
	path := filepath.Join(t.TempDir(), "missing.md")
	registryPath := filepath.Join(state, "registry.json")
	want := []registryDocument{{ID: documentID(path), Path: path}}
	if err := saveRegistry(registryPath, want); err != nil {
		t.Fatalf("saveRegistry(): %v", err)
	}
	registry, err := loadRegistry(registryPath)
	if err != nil {
		t.Fatalf("loadRegistry(): %v", err)
	}
	if !reflect.DeepEqual(registry.Documents, want) {
		t.Fatalf("documents = %#v, want %#v", registry.Documents, want)
	}
}

func TestRegistryRejectsCorruptState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"documents":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRegistry(path); err == nil {
		t.Fatal("loadRegistry() accepted an unsupported version")
	}
}
