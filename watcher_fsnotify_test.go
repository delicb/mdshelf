//go:build !darwin

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFSNotifyWatcherPublishesMarkdownChange(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "note.md", "old\n")
	app, err := newApp(root)
	if err != nil {
		t.Fatalf("newApp(%q): %v", root, err)
	}
	t.Cleanup(app.Close)

	mustWriteFile(t, root, "note.md", "new\n")
	batch := waitForChange(t, app.updates, 0)
	if len(batch.Changes) != 1 || batch.Changes[0].Path != "note.md" || batch.Changes[0].Kind != "updated" {
		t.Fatalf("change batch = %#v", batch)
	}
}

func TestParentWatcherDoesNotWatchChildFolders(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	watcher, err := newParentWatcher(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = watcher.Close() })
	filePath := filepath.Join(child, "note.md")
	if err := os.WriteFile(filePath, []byte("# Note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case event := <-watcher.Events():
			if filepath.Clean(event) == filePath {
				t.Fatal("parent watcher reported a child-folder file")
			}
		case <-timer.C:
			return
		}
	}
}

func TestFSNotifyWatcherReportsNestedFileChange(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "guides")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("create watched directory: %v", err)
	}
	watcher, err := newFileWatcher(root)
	if err != nil {
		t.Fatalf("newFileWatcher(%q): %v", root, err)
	}
	t.Cleanup(func() {
		if err := watcher.Close(); err != nil {
			t.Errorf("close file watcher: %v", err)
		}
	})

	filePath := filepath.Join(directory, "note.md")
	if err := os.WriteFile(filePath, []byte("# Note\n"), 0o644); err != nil {
		t.Fatalf("write watched file: %v", err)
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case eventPath, ok := <-watcher.Events():
			if !ok {
				t.Fatal("file watcher stopped before reporting the change")
			}
			if filepath.Clean(eventPath) == filePath {
				return
			}
		case err, ok := <-watcher.Errors():
			if !ok {
				t.Fatal("file watcher stopped before reporting the change")
			}
			t.Fatalf("file watcher error: %v", err)
		case <-timer.C:
			t.Fatal("timed out waiting for file watcher event")
		}
	}
}
