package main

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuin/goldmark"
)

type fakeDaemonWatcher struct {
	events     chan string
	errors     chan error
	eventsOnce sync.Once
	errorsOnce sync.Once
}

func newFakeDaemonWatcher() *fakeDaemonWatcher {
	return &fakeDaemonWatcher{events: make(chan string, 8), errors: make(chan error, 8)}
}

func (w *fakeDaemonWatcher) Events() <-chan string { return w.events }
func (w *fakeDaemonWatcher) Errors() <-chan error  { return w.errors }
func (w *fakeDaemonWatcher) Close() error {
	w.closeEvents()
	w.closeErrors()
	return nil
}
func (w *fakeDaemonWatcher) closeEvents() { w.eventsOnce.Do(func() { close(w.events) }) }
func (w *fakeDaemonWatcher) closeErrors() { w.errorsOnce.Do(func() { close(w.errors) }) }

func TestDaemonStartupDoesNotWaitForPersistedFiles(t *testing.T) {
	state := t.TempDir()
	documentPath := filepath.Join(t.TempDir(), "slow.md")
	readStarted := make(chan struct{})
	releaseRead := make(chan struct{})
	watcherStarted := make(chan struct{})
	releaseWatcher := make(chan struct{})
	var releaseReadOnce sync.Once
	var releaseWatcherOnce sync.Once
	t.Cleanup(func() {
		releaseReadOnce.Do(func() { close(releaseRead) })
		releaseWatcherOnce.Do(func() { close(releaseWatcher) })
	})

	if err := saveRegistry(filepath.Join(state, "registry.json"), []registryDocument{{ID: documentID(documentPath), Path: documentPath}}); err != nil {
		t.Fatal(err)
	}
	d, err := newDaemonServerWithUpdaterOptions(state, daemonUpdaterOptions{
		readDocument: func(string, goldmark.Markdown) ([]byte, string, error) {
			select {
			case <-readStarted:
			default:
				close(readStarted)
			}
			<-releaseRead
			return []byte("# Slow\n"), "Slow", nil
		},
		newWatcher: func(string) (fileWatcher, error) {
			close(watcherStarted)
			<-releaseWatcher
			return newFakeDaemonWatcher(), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-readStarted:
	case <-time.After(time.Second):
		t.Fatal("persisted-file reconcile did not start")
	}
	select {
	case <-watcherStarted:
	case <-time.After(time.Second):
		t.Fatal("watcher setup did not start")
	}
	response := daemonRequest(t, d.handler, "GET", "/api/health", nil)
	if response.StatusCode != 200 {
		t.Fatalf("health status = %d", response.StatusCode)
	}
	_ = response.Body.Close()

	closed := make(chan struct{})
	go func() {
		d.close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("daemon close waited for background startup work")
	}
	releaseReadOnce.Do(func() { close(releaseRead) })
	releaseWatcherOnce.Do(func() { close(releaseWatcher) })
}

func TestWatcherErrorAndClosureInstallReplacement(t *testing.T) {
	for _, closeChannel := range []string{"error", "events"} {
		t.Run(closeChannel, func(t *testing.T) {
			root := t.TempDir()
			path := mustWriteFile(t, root, "note.md", "# Note\n")
			var mu sync.Mutex
			var watchers []*fakeDaemonWatcher
			factory := func(string) (fileWatcher, error) {
				watcher := newFakeDaemonWatcher()
				mu.Lock()
				watchers = append(watchers, watcher)
				mu.Unlock()
				return watcher, nil
			}
			u := newDaemonUpdaterWithOptions(filepath.Join(t.TempDir(), "registry.json"), registryFile{Version: registryVersion}, daemonUpdaterOptions{
				newWatcher: factory, reconcileEvery: time.Hour,
			})
			t.Cleanup(u.close)
			<-u.started
			document, _, err := u.add(path)
			if err != nil {
				t.Fatal(err)
			}
			parent := filepath.Dir(document.Path)
			waitForCondition(t, func() bool {
				mu.Lock()
				defer mu.Unlock()
				return len(watchers) == 1
			})
			mu.Lock()
			first := watchers[0]
			mu.Unlock()
			u.mu.Lock()
			group := u.groups[parent]
			if group == nil {
				u.mu.Unlock()
				t.Fatal("watcher group was not created")
			}
			firstToken := group.token
			u.mu.Unlock()

			if closeChannel == "error" {
				first.errors <- errors.New("watch failed")
			} else {
				first.closeEvents()
			}
			waitForCondition(t, func() bool {
				mu.Lock()
				defer mu.Unlock()
				return len(watchers) >= 2
			})
			mu.Lock()
			second := watchers[1]
			mu.Unlock()
			u.mu.Lock()
			group = u.groups[parent]
			if group == nil || group.watcher != second || group.token == firstToken || group.state != watcherActive {
				t.Fatalf("replacement group = %#v", group)
			}
			secondToken := group.token
			u.mu.Unlock()

			if u.deactivateGroup(parent, firstToken, first) {
				t.Fatal("stale watcher deactivated its replacement")
			}
			u.mu.Lock()
			group = u.groups[parent]
			active := group != nil && group.watcher == second && group.token == secondToken && group.state == watcherActive
			u.mu.Unlock()
			if !active {
				t.Fatal("replacement watcher is not active")
			}
		})
	}
}

func TestBlockedRefreshGroupsCoalescesConcurrentRequests(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{}, 2)
	var active atomic.Int32
	var maximum atomic.Int32
	check := func(string, os.FileInfo) bool {
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		return true
	}
	u := newDaemonUpdaterWithOptions(filepath.Join(t.TempDir(), "registry.json"), registryFile{Version: registryVersion}, daemonUpdaterOptions{
		newWatcher:     func(string) (fileWatcher, error) { return nil, errors.New("not used") },
		reconcileEvery: time.Hour,
		sameDirectory:  check,
	})
	t.Cleanup(u.close)
	<-u.started
	waitForCondition(t, func() bool {
		u.mu.Lock()
		defer u.mu.Unlock()
		return !u.refreshRunning
	})

	watcher := newFakeDaemonWatcher()
	u.mu.Lock()
	u.groups["blocked"] = &daemonWatcherGroup{parent: "blocked", state: watcherActive, watcher: watcher}
	u.documents["document"] = &daemonDocument{registryDocument: registryDocument{ID: "document", Path: filepath.Join("blocked", "note.md")}}
	u.mu.Unlock()
	u.scheduleRefreshGroups()
	<-entered
	for range 20 {
		u.scheduleRefreshGroups()
	}
	select {
	case <-entered:
		t.Fatal("a second refresh started while the first refresh was blocked")
	default:
	}
	release <- struct{}{}
	<-entered
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent refreshes = %d, want 1", got)
	}
	release <- struct{}{}
	waitForCondition(t, func() bool {
		u.mu.Lock()
		defer u.mu.Unlock()
		return !u.refreshRunning
	})
}

func TestImmediateWatcherFailureWaitsBeforeEachRetryAndStopsOnClose(t *testing.T) {
	retryStarted := make(chan struct{}, 4)
	releaseRetry := make(chan struct{}, 4)
	var calls atomic.Int32
	u := newDaemonUpdaterWithOptions(filepath.Join(t.TempDir(), "registry.json"), registryFile{Version: registryVersion}, daemonUpdaterOptions{
		newWatcher: func(string) (fileWatcher, error) {
			calls.Add(1)
			watcher := newFakeDaemonWatcher()
			_ = watcher.Close()
			return watcher, nil
		},
		reconcileEvery: time.Hour,
		watcherRetry: func(stop <-chan struct{}, _ time.Duration) bool {
			retryStarted <- struct{}{}
			select {
			case <-releaseRetry:
				return true
			case <-stop:
				return false
			}
		},
	})
	<-u.started
	path := mustWriteFile(t, t.TempDir(), "note.md", "# Note\n")
	if _, _, err := u.add(path); err != nil {
		t.Fatal(err)
	}
	<-retryStarted
	if got := calls.Load(); got != 1 {
		t.Fatalf("watcher starts before first retry = %d, want 1", got)
	}
	releaseRetry <- struct{}{}
	<-retryStarted
	if got := calls.Load(); got != 2 {
		t.Fatalf("watcher starts before second retry = %d, want 2", got)
	}

	u.close()
	releaseRetry <- struct{}{}
	if got := calls.Load(); got != 2 {
		t.Fatalf("watcher starts after close = %d, want 2", got)
	}
}

func TestReconcileRejectsOlderCompletion(t *testing.T) {
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	releaseSecond := make(chan struct{})
	var calls atomic.Int32
	read := func(string, goldmark.Markdown) ([]byte, string, error) {
		switch calls.Add(1) {
		case 1:
			close(firstStarted)
			<-releaseFirst
			return []byte("# Old\n"), "Old", nil
		case 2:
			close(secondStarted)
			<-releaseSecond
			return []byte("# New\n"), "New", nil
		default:
			return nil, "", errors.New("unexpected read")
		}
	}
	u := newDaemonUpdaterWithOptions(filepath.Join(t.TempDir(), "registry.json"), registryFile{Version: registryVersion}, daemonUpdaterOptions{
		readDocument:   read,
		newWatcher:     func(string) (fileWatcher, error) { return nil, errors.New("watch disabled") },
		reconcileEvery: time.Hour,
	})
	t.Cleanup(u.close)
	<-u.started
	path := filepath.Join(t.TempDir(), "note.md")
	id := documentID(path)
	u.mu.Lock()
	u.documents[id] = &daemonDocument{registryDocument: registryDocument{ID: id, Path: path}, title: "note", removed: true, generation: 1}
	u.paths[path] = id
	u.mu.Unlock()

	firstDone := make(chan struct{})
	go func() { u.reconcile(id); close(firstDone) }()
	<-firstStarted
	secondDone := make(chan struct{})
	go func() { u.reconcile(id); close(secondDone) }()
	<-secondStarted
	close(releaseSecond)
	<-secondDone
	close(releaseFirst)
	<-firstDone

	u.mu.Lock()
	document := cloneDaemonDocument(u.documents[id])
	u.mu.Unlock()
	if string(document.source) != "# New\n" || document.title != "New" || document.removed || document.generation != 2 {
		t.Fatalf("document after reversed reconcile = %#v", document)
	}
	batch, ready := u.feed.batchAfter(0)
	if !ready || batch.Revision != 1 || len(batch.Changes) != 1 || batch.Changes[0].Kind != "added" {
		t.Fatalf("change batch = %#v", batch)
	}
}

func TestRemoveKeepsWorkingForMissingRegisteredPath(t *testing.T) {
	input := filepath.Join(t.TempDir(), "Missing.md")
	path, err := canonicalDocumentPath(input)
	if err != nil {
		t.Fatal(err)
	}
	id := documentID(path)
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	u := newDaemonUpdaterWithOptions(registryPath, registryFile{
		Version:   registryVersion,
		Documents: []registryDocument{{ID: id, Path: path}},
	}, daemonUpdaterOptions{
		newWatcher:     func(string) (fileWatcher, error) { return nil, errors.New("watch disabled") },
		reconcileEvery: time.Hour,
	})
	t.Cleanup(u.close)
	<-u.started
	removed, err := u.remove(input)
	if err != nil {
		t.Fatal(err)
	}
	if removed.ID != id || removed.Path != path {
		t.Fatalf("removed document = %#v", removed)
	}
	registry, err := loadRegistry(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Documents) != 0 {
		t.Fatalf("registry documents = %#v, want none", registry.Documents)
	}
}

func TestConcurrentAddAndRemoveKeepRegistryConsistent(t *testing.T) {
	root := t.TempDir()
	path := mustWriteFile(t, root, "note.md", "# Note\n")
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	u := newDaemonUpdaterWithOptions(registryPath, registryFile{Version: registryVersion}, daemonUpdaterOptions{
		newWatcher:     func(string) (fileWatcher, error) { return nil, errors.New("watch disabled") },
		reconcileEvery: time.Hour,
	})
	t.Cleanup(u.close)
	<-u.started
	if _, _, err := u.add(path); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _, _ = u.add(path)
		}()
		go func() {
			defer wg.Done()
			_, _ = u.remove(path)
		}()
	}
	wg.Wait()

	registry, err := loadRegistry(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	u.mu.Lock()
	memoryCount := len(u.documents)
	pathCount := len(u.paths)
	u.mu.Unlock()
	if len(registry.Documents) != memoryCount || pathCount != memoryCount || memoryCount > 1 {
		t.Fatalf("registry=%d documents=%d paths=%d", len(registry.Documents), memoryCount, pathCount)
	}
}

func TestDaemonCloseReleasesActivePoll(t *testing.T) {
	u := newDaemonUpdaterWithOptions(filepath.Join(t.TempDir(), "registry.json"), registryFile{Version: registryVersion}, daemonUpdaterOptions{
		newWatcher:     func(string) (fileWatcher, error) { return nil, errors.New("watch disabled") },
		reconcileEvery: time.Hour,
	})
	<-u.started
	pollDone := make(chan changeBatch, 1)
	go func() { pollDone <- u.feed.wait(t.Context(), 0) }()
	u.close()
	select {
	case <-pollDone:
	case <-time.After(time.Second):
		t.Fatal("active poll did not stop")
	}
}

func waitForCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met")
}
