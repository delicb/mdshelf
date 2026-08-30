package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yuin/goldmark"
)

type daemonDocument struct {
	registryDocument
	title      string
	source     []byte
	removed    bool
	generation uint64
	attempt    uint64
	scheduled  bool
}

type watcherState uint8

const (
	watcherInactive watcherState = iota
	watcherStarting
	watcherActive
)

type daemonWatcherGroup struct {
	parent     string
	state      watcherState
	token      uint64
	watcher    fileWatcher
	parentInfo os.FileInfo
}

type daemonUpdater struct {
	mu sync.Mutex

	registryPath   string
	readDocument   func(string, goldmark.Markdown) ([]byte, string, error)
	newWatcher     func(string) (fileWatcher, error)
	reconcileEvery time.Duration
	watcherRetry   func(<-chan struct{}, time.Duration) bool
	sameDirectory  func(string, os.FileInfo) bool
	documents      map[string]*daemonDocument
	paths          map[string]string
	groups         map[string]*daemonWatcherGroup
	feed           *changeFeed
	markdown       goldmark.Markdown
	closing        bool
	nextAttempt    uint64
	nextWatcher    uint64
	generation     uint64
	refreshRunning bool
	refreshPending bool
	stop           chan struct{}
	started        chan struct{}
	done           chan struct{}
	closeOnce      sync.Once
	watcherWG      sync.WaitGroup
}

type daemonUpdaterOptions struct {
	readDocument   func(string, goldmark.Markdown) ([]byte, string, error)
	newWatcher     func(string) (fileWatcher, error)
	reconcileEvery time.Duration
	watcherRetry   func(<-chan struct{}, time.Duration) bool
	sameDirectory  func(string, os.FileInfo) bool
}

func newDaemonUpdater(registryPath string, registry registryFile) *daemonUpdater {
	return newDaemonUpdaterWithOptions(registryPath, registry, daemonUpdaterOptions{})
}

func newDaemonUpdaterWithOptions(registryPath string, registry registryFile, options daemonUpdaterOptions) *daemonUpdater {
	if options.readDocument == nil {
		options.readDocument = readDaemonDocument
	}
	if options.newWatcher == nil {
		options.newWatcher = newParentWatcher
	}
	if options.reconcileEvery <= 0 {
		options.reconcileEvery = 2 * time.Second
	}
	if options.watcherRetry == nil {
		options.watcherRetry = waitForWatcherRetry
	}
	if options.sameDirectory == nil {
		options.sameDirectory = sameDirectory
	}
	u := &daemonUpdater{
		registryPath:   registryPath,
		readDocument:   options.readDocument,
		newWatcher:     options.newWatcher,
		reconcileEvery: options.reconcileEvery,
		watcherRetry:   options.watcherRetry,
		sameDirectory:  options.sameDirectory,
		documents:      make(map[string]*daemonDocument),
		paths:          make(map[string]string),
		groups:         make(map[string]*daemonWatcherGroup),
		feed:           newChangeFeed(),
		markdown:       newMarkdownRenderer(),
		stop:           make(chan struct{}),
		started:        make(chan struct{}),
		done:           make(chan struct{}),
	}
	for _, stored := range registry.Documents {
		document := &daemonDocument{registryDocument: stored, title: titleFromDocumentPath(stored.Path), removed: true, generation: 1}
		u.documents[stored.ID] = document
		u.paths[stored.Path] = stored.ID
	}
	go u.run()
	return u
}

func (u *daemonUpdater) run() {
	defer close(u.done)
	u.scheduleReconcileAll()
	u.scheduleRefreshGroups()
	close(u.started)
	ticker := time.NewTicker(u.reconcileEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			u.scheduleReconcileAll()
			u.scheduleRefreshGroups()
		case <-u.stop:
			return
		}
	}
}

func (u *daemonUpdater) scheduleReconcileAll() {
	u.mu.Lock()
	ids := make([]string, 0, len(u.documents))
	for id := range u.documents {
		ids = append(ids, id)
	}
	u.mu.Unlock()
	for _, id := range ids {
		u.scheduleReconcile(id)
	}
}

func (u *daemonUpdater) scheduleReconcile(id string) {
	u.mu.Lock()
	document := u.documents[id]
	if document == nil || document.scheduled || u.closing {
		u.mu.Unlock()
		return
	}
	document.scheduled = true
	u.mu.Unlock()
	go func() {
		u.reconcile(id)
		u.mu.Lock()
		if u.documents[id] == document {
			document.scheduled = false
		}
		u.mu.Unlock()
	}()
}

func (u *daemonUpdater) scheduleRefreshGroups() {
	u.mu.Lock()
	if u.closing {
		u.mu.Unlock()
		return
	}
	if u.refreshRunning {
		u.refreshPending = true
		u.mu.Unlock()
		return
	}
	u.refreshRunning = true
	u.mu.Unlock()
	go u.runRefreshGroups()
}

func (u *daemonUpdater) runRefreshGroups() {
	for {
		u.refreshGroups()
		u.mu.Lock()
		if !u.closing && u.refreshPending {
			u.refreshPending = false
			u.mu.Unlock()
			continue
		}
		u.refreshRunning = false
		u.refreshPending = false
		u.mu.Unlock()
		return
	}
}

func (u *daemonUpdater) scheduleWatcherRetry() {
	u.mu.Lock()
	if u.closing {
		u.mu.Unlock()
		return
	}
	u.watcherWG.Add(1)
	u.mu.Unlock()
	go func() {
		defer u.watcherWG.Done()
		if u.watcherRetry(u.stop, 250*time.Millisecond) {
			u.scheduleRefreshGroups()
		}
	}()
}

func waitForWatcherRetry(stop <-chan struct{}, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-stop:
		return false
	}
}

func (u *daemonUpdater) close() {
	u.closeOnce.Do(func() {
		u.mu.Lock()
		u.closing = true
		watchers := make([]fileWatcher, 0, len(u.groups))
		for _, group := range u.groups {
			if group.watcher != nil {
				watchers = append(watchers, group.watcher)
				group.watcher = nil
			}
			group.state = watcherInactive
		}
		u.mu.Unlock()
		close(u.stop)
		for _, watcher := range watchers {
			_ = watcher.Close()
		}
		u.watcherWG.Wait()
		<-u.done
		u.feed.close()
	})
}

func (u *daemonUpdater) documentSnapshot() []*daemonDocument {
	u.mu.Lock()
	defer u.mu.Unlock()
	result := make([]*daemonDocument, 0, len(u.documents))
	for _, document := range u.documents {
		result = append(result, cloneDaemonDocument(document))
	}
	return result
}

// document returns a deep copy of the registered document with this ID, or
// nil when the ID is unknown.
func (u *daemonUpdater) document(id string) *daemonDocument {
	u.mu.Lock()
	defer u.mu.Unlock()
	return cloneDaemonDocument(u.documents[id])
}

// documentAndPaths returns a deep copy of the registered document with this
// ID plus a snapshot of the path-to-ID table for link rewriting.
func (u *daemonUpdater) documentAndPaths(id string) (*daemonDocument, map[string]string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	paths := make(map[string]string, len(u.paths))
	for path, registeredID := range u.paths {
		paths[path] = registeredID
	}
	return cloneDaemonDocument(u.documents[id]), paths
}

func (u *daemonUpdater) cloneDocument(identifier string) *daemonDocument {
	u.mu.Lock()
	defer u.mu.Unlock()
	if id := u.paths[identifier]; id != "" {
		identifier = id
	}
	return cloneDaemonDocument(u.documents[identifier])
}

func (u *daemonUpdater) withCurrentDocument(snapshot *daemonDocument, action func() error) (current, removed bool, err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	live := u.documents[snapshot.ID]
	if live == nil || live.removed {
		return false, true, nil
	}
	if live.generation != snapshot.generation {
		return false, false, nil
	}
	return true, false, action()
}

func (u *daemonUpdater) add(input string) (*daemonDocument, bool, error) {
	canonical, err := canonicalDocumentPath(input)
	if err != nil {
		return nil, false, err
	}

	u.mu.Lock()
	if id, ok := u.paths[canonical]; ok {
		document := cloneDaemonDocument(u.documents[id])
		u.mu.Unlock()
		return document, false, nil
	}
	u.mu.Unlock()

	source, title, readErr := u.readDocument(canonical, u.markdown)
	if readErr != nil {
		return nil, false, readErr
	}
	id := documentID(canonical)

	u.mu.Lock()
	if u.closing {
		u.mu.Unlock()
		return nil, false, errors.New("daemon is stopping")
	}
	if existingID, ok := u.paths[canonical]; ok {
		document := cloneDaemonDocument(u.documents[existingID])
		u.mu.Unlock()
		return document, false, nil
	}
	if existing, ok := u.documents[id]; ok && existing.Path != canonical {
		u.mu.Unlock()
		return nil, false, errDocumentIDCollision
	}
	document := &daemonDocument{
		registryDocument: registryDocument{ID: id, Path: canonical},
		title:            title,
		source:           source,
		generation:       1,
	}
	proposed := u.registryDocumentsLocked(document, "")
	if err := saveRegistry(u.registryPath, proposed); err != nil {
		u.mu.Unlock()
		return nil, false, err
	}
	u.documents[id] = document
	u.paths[canonical] = id
	u.generation++
	u.feed.publish(markdownChange{Path: id, Kind: "added", Diff: unifiedMarkdownDiff(id, nil, source)})
	result := cloneDaemonDocument(document)
	u.mu.Unlock()
	u.scheduleRefreshGroups()
	return result, true, nil
}

func (u *daemonUpdater) remove(input string) (*daemonDocument, error) {
	canonical, err := canonicalDocumentPath(input)
	if err != nil {
		return nil, err
	}
	u.mu.Lock()
	id, ok := u.paths[canonical]
	if !ok {
		u.mu.Unlock()
		return nil, fs.ErrNotExist
	}
	document := u.documents[id]
	proposed := u.registryDocumentsLocked(nil, id)
	if err := saveRegistry(u.registryPath, proposed); err != nil {
		u.mu.Unlock()
		return nil, err
	}
	delete(u.documents, id)
	delete(u.paths, canonical)
	u.generation++
	u.feed.publish(markdownChange{Path: id, Kind: "removed", Diff: unifiedMarkdownDiff(id, document.source, nil)})
	result := cloneDaemonDocument(document)
	u.mu.Unlock()
	u.scheduleRefreshGroups()
	return result, nil
}

func (u *daemonUpdater) registryDocumentsLocked(add *daemonDocument, removeID string) []registryDocument {
	result := make([]registryDocument, 0, len(u.documents)+1)
	for id, document := range u.documents {
		if id != removeID {
			result = append(result, document.registryDocument)
		}
	}
	if add != nil {
		result = append(result, add.registryDocument)
	}
	return result
}

func cloneDaemonDocument(document *daemonDocument) *daemonDocument {
	if document == nil {
		return nil
	}
	copyDocument := *document
	copyDocument.source = bytes.Clone(document.source)
	return &copyDocument
}

func (u *daemonUpdater) reconcile(id string) {
	u.mu.Lock()
	document := u.documents[id]
	if document == nil || u.closing {
		u.mu.Unlock()
		return
	}
	u.nextAttempt++
	token := u.nextAttempt
	document.attempt = token
	path := document.Path
	before := bytes.Clone(document.source)
	u.mu.Unlock()

	source, title, err := u.readDocument(path, u.markdown)
	removed := errors.Is(err, fs.ErrNotExist) || errors.Is(err, errNotRegular) || errors.Is(err, errSymlink) || errors.Is(err, errInvalidPath)
	if err != nil && !removed {
		log.Printf("refresh daemon document: %v", err)
		return
	}

	u.mu.Lock()
	document = u.documents[id]
	if document == nil || document.Path != path || document.attempt != token || u.closing {
		u.mu.Unlock()
		return
	}
	wasRemoved := document.removed
	if removed {
		if wasRemoved {
			u.mu.Unlock()
			return
		}
		document.removed = true
		document.generation++
		u.feed.publish(markdownChange{Path: id, Kind: "removed", Diff: unifiedMarkdownDiff(id, before, nil)})
		u.mu.Unlock()
		return
	}
	if !wasRemoved && bytes.Equal(document.source, source) && document.title == title {
		u.mu.Unlock()
		return
	}
	document.source = source
	document.title = title
	document.removed = false
	document.generation++
	kind := "updated"
	if wasRemoved {
		kind = "added"
	}
	u.feed.publish(markdownChange{Path: id, Kind: kind, Diff: unifiedMarkdownDiff(id, before, source)})
	u.mu.Unlock()
}

func readDaemonDocument(path string, markdown goldmark.Markdown) ([]byte, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, "", errSymlink
	}
	if !info.Mode().IsRegular() {
		return nil, "", errNotRegular
	}
	file, fileInfo, err := openRootedFile(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	if fileInfo.Size() > maxMarkdownSize {
		return nil, "", errMarkdownTooLarge
	}
	source, err := io.ReadAll(io.LimitReader(file, maxMarkdownSize+1))
	if err != nil {
		return nil, "", err
	}
	if len(source) > maxMarkdownSize {
		return nil, "", errMarkdownTooLarge
	}
	rendered, err := renderMarkdown(markdown, source, filepath.Base(path), nil)
	if err != nil {
		return nil, "", fmt.Errorf("read document title: %w", err)
	}
	return source, rendered.title, nil
}

func titleFromDocumentPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func (u *daemonUpdater) refreshGroups() {
	type groupCheck struct {
		parent  string
		token   uint64
		watcher fileWatcher
		info    os.FileInfo
	}
	u.mu.Lock()
	if u.closing {
		u.mu.Unlock()
		return
	}
	checks := make([]groupCheck, 0, len(u.groups))
	for parent, group := range u.groups {
		if group.state == watcherActive {
			checks = append(checks, groupCheck{parent: parent, token: group.token, watcher: group.watcher, info: group.parentInfo})
		}
	}
	u.mu.Unlock()

	invalid := make([]groupCheck, 0)
	for _, check := range checks {
		if !u.sameDirectory(check.parent, check.info) {
			invalid = append(invalid, check)
		}
	}

	u.mu.Lock()
	if u.closing {
		u.mu.Unlock()
		return
	}
	needed := make(map[string]struct{})
	for _, document := range u.documents {
		needed[filepath.Dir(document.Path)] = struct{}{}
	}
	var closeWatchers []fileWatcher
	for parent, group := range u.groups {
		if _, ok := needed[parent]; ok {
			continue
		}
		delete(u.groups, parent)
		if group.watcher != nil {
			closeWatchers = append(closeWatchers, group.watcher)
		}
	}
	for _, check := range invalid {
		group := u.groups[check.parent]
		if group == nil || group.state != watcherActive || group.token != check.token || group.watcher != check.watcher {
			continue
		}
		group.watcher = nil
		group.state = watcherInactive
		closeWatchers = append(closeWatchers, check.watcher)
	}
	var starts []string
	for parent := range needed {
		group := u.groups[parent]
		if group == nil {
			group = &daemonWatcherGroup{parent: parent}
			u.groups[parent] = group
		}
		if group.state == watcherInactive {
			u.nextWatcher++
			group.token = u.nextWatcher
			group.state = watcherStarting
			starts = append(starts, parent)
		}
	}
	u.mu.Unlock()
	for _, watcher := range closeWatchers {
		_ = watcher.Close()
	}
	for _, parent := range starts {
		u.startGroup(parent)
	}
}

func sameDirectory(path string, expected os.FileInfo) bool {
	if expected == nil {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir() && os.SameFile(info, expected)
}

func (u *daemonUpdater) startGroup(parent string) {
	u.mu.Lock()
	group := u.groups[parent]
	if group == nil || group.state != watcherStarting {
		u.mu.Unlock()
		return
	}
	token := group.token
	u.mu.Unlock()

	before, err := os.Lstat(parent)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		u.failGroupStart(parent, token)
		return
	}
	watcher, err := u.newWatcher(parent)
	if err != nil {
		u.failGroupStart(parent, token)
		return
	}
	after, err := os.Lstat(parent)
	if err != nil || !os.SameFile(before, after) {
		_ = watcher.Close()
		u.failGroupStart(parent, token)
		return
	}

	u.mu.Lock()
	group = u.groups[parent]
	if group == nil || group.token != token || group.state != watcherStarting || u.closing {
		u.mu.Unlock()
		_ = watcher.Close()
		return
	}
	group.watcher = watcher
	group.parentInfo = after
	group.state = watcherActive
	u.watcherWG.Add(1)
	u.mu.Unlock()
	go func() {
		defer u.watcherWG.Done()
		u.consumeWatcher(parent, token, watcher)
	}()
}

func (u *daemonUpdater) failGroupStart(parent string, token uint64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	group := u.groups[parent]
	if group != nil && group.token == token && group.state == watcherStarting {
		group.state = watcherInactive
	}
}

func (u *daemonUpdater) consumeWatcher(parent string, token uint64, watcher fileWatcher) {
	events := watcher.Events()
	errorsChannel := watcher.Errors()
	pending := make(map[string]struct{})
	debounce := newDebouncer(changeDebounce)
	defer debounce.stop()
	for events != nil || errorsChannel != nil || debounce.C != nil {
		select {
		case event, ok := <-events:
			if !ok {
				u.recoverGroup(parent, token, watcher)
				return
			}
			if filepath.Clean(event) == filepath.Clean(parent) {
				u.mu.Lock()
				group := u.groups[parent]
				valid := group != nil && group.token == token && group.watcher == watcher
				parentInfo := groupParentInfo(group)
				u.mu.Unlock()
				if !valid {
					return
				}
				if sameDirectory(parent, parentInfo) {
					continue
				}
				u.recoverGroup(parent, token, watcher)
				return
			}
			if filepath.Clean(filepath.Dir(event)) != filepath.Clean(parent) {
				continue
			}
			u.mu.Lock()
			id := u.paths[filepath.Join(parent, filepath.Base(event))]
			current := u.groups[parent]
			valid := current != nil && current.token == token && current.watcher == watcher
			u.mu.Unlock()
			if !valid || id == "" {
				continue
			}
			pending[id] = struct{}{}
			debounce.arm()
		case <-debounce.C:
			for _, id := range drainPending(pending) {
				u.scheduleReconcile(id)
			}
			debounce.fired()
		case err, ok := <-errorsChannel:
			if !ok {
				u.recoverGroup(parent, token, watcher)
				return
			}
			log.Printf("daemon file watcher: %v", err)
			u.recoverGroup(parent, token, watcher)
			return
		}
	}
	u.recoverGroup(parent, token, watcher)
}

func groupParentInfo(group *daemonWatcherGroup) os.FileInfo {
	if group == nil {
		return nil
	}
	return group.parentInfo
}

func (u *daemonUpdater) recoverGroup(parent string, token uint64, watcher fileWatcher) {
	if u.deactivateGroup(parent, token, watcher) {
		u.scheduleWatcherRetry()
	}
}

func (u *daemonUpdater) deactivateGroup(parent string, token uint64, watcher fileWatcher) bool {
	u.mu.Lock()
	group := u.groups[parent]
	if group == nil || group.token != token || group.watcher != watcher {
		u.mu.Unlock()
		return false
	}
	group.watcher = nil
	group.state = watcherInactive
	u.mu.Unlock()
	_ = watcher.Close()
	return true
}

func (u *daemonUpdater) sortedDocuments() []*daemonDocument {
	result := u.documentSnapshot()
	sort.Slice(result, func(i, j int) bool {
		if result[i].title == result[j].title {
			return result[i].ID < result[j].ID
		}
		return strings.ToLower(result[i].title) < strings.ToLower(result[j].title)
	})
	return result
}
