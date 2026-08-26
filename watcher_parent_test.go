package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDaemonNativeWatcherRecoversAfterParentReplacement(t *testing.T) {
	rootContainer := t.TempDir()
	root := filepath.Join(rootContainer, "watched")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	probe, err := newParentWatcher(root)
	if err != nil {
		t.Skipf("native parent watcher is not available: %v", err)
	}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}

	path := mustWriteFile(t, root, "note.md", "# First\n")
	u := newDaemonUpdaterWithOptions(filepath.Join(t.TempDir(), "registry.json"), registryFile{Version: registryVersion}, daemonUpdaterOptions{
		reconcileEvery: time.Hour,
	})
	t.Cleanup(u.close)
	<-u.started
	document, _, err := u.add(path)
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(document.Path)
	waitForCondition(t, func() bool {
		u.mu.Lock()
		defer u.mu.Unlock()
		group := u.groups[parent]
		return group != nil && group.state == watcherActive
	})

	if err := os.Remove(document.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	u.refreshGroups()
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	u.refreshGroups()
	waitForCondition(t, func() bool {
		u.mu.Lock()
		defer u.mu.Unlock()
		group := u.groups[parent]
		return group != nil && group.state == watcherActive && sameDirectory(parent, group.parentInfo)
	})

	mustWriteFile(t, parent, filepath.Base(document.Path), "# Recovered\n")
	waitForNativeWatcher(t, func() bool {
		u.mu.Lock()
		defer u.mu.Unlock()
		stored := u.documents[document.ID]
		return stored != nil && !stored.removed && stored.title == "Recovered"
	})
}

func waitForNativeWatcher(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("native watcher did not report the change")
}
