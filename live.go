package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pmezard/go-difflib/difflib"
	"github.com/rjeczalik/notify"
)

const (
	changeDebounce        = 120 * time.Millisecond
	changePollTimeout     = 20 * time.Second
	maxChangeHistory      = 128
	maxChangeHistoryBytes = 16 << 20
)

var errMarkdownTooLarge = errors.New("markdown file is too large")

type markdownChange struct {
	Revision uint64 `json:"revision"`
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Diff     string `json:"diff"`
}

type changeBatch struct {
	Revision uint64           `json:"revision"`
	Reset    bool             `json:"reset,omitempty"`
	Changes  []markdownChange `json:"changes"`
}

type liveUpdates struct {
	app       *app
	events    chan notify.EventInfo
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once

	mu           sync.Mutex
	snapshots    map[string][]byte
	revision     uint64
	history      []markdownChange
	historyBytes int
	changed      chan struct{}
	closed       bool
}

func newLiveUpdates(a *app) (*liveUpdates, error) {
	u := &liveUpdates{
		app:       a,
		events:    make(chan notify.EventInfo, 256),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		snapshots: make(map[string][]byte),
		changed:   make(chan struct{}),
	}

	files, err := a.markdownFiles()
	if err != nil {
		return nil, fmt.Errorf("list initial Markdown files: %w", err)
	}
	for _, filePath := range files {
		source, err := a.readMarkdownSource(filePath)
		if err != nil {
			log.Printf("watch Markdown file %q: %v", filePath, err)
			continue
		}
		u.snapshots[filePath] = source
	}

	watchPath := filepath.Join(a.root, "...")
	if err := notify.Watch(watchPath, u.events, notify.Create, notify.Remove, notify.Rename, notify.Write); err != nil {
		return nil, fmt.Errorf("start file watcher: %w", err)
	}

	go u.run()
	return u, nil
}

func (u *liveUpdates) Close() {
	u.closeOnce.Do(func() {
		notify.Stop(u.events)
		close(u.stop)
		<-u.done

		u.mu.Lock()
		u.closed = true
		close(u.changed)
		u.mu.Unlock()
	})
}

func (u *liveUpdates) run() {
	defer close(u.done)

	pending := make(map[string]struct{})
	rescan := false
	var timer *time.Timer
	var timerC <-chan time.Time

	for {
		select {
		case event := <-u.events:
			if u.markdownDirectoryChanged(event.Path()) {
				rescan = true
			} else if filePath, ok := u.markdownPath(event.Path()); ok {
				pending[filePath] = struct{}{}
			} else {
				continue
			}
			if timer == nil {
				timer = time.NewTimer(changeDebounce)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(changeDebounce)
			}
			timerC = timer.C
		case <-timerC:
			if rescan {
				u.recordAllChanges()
			} else {
				paths := make([]string, 0, len(pending))
				for filePath := range pending {
					paths = append(paths, filePath)
				}
				sort.Strings(paths)
				for _, filePath := range paths {
					u.recordChange(filePath)
				}
			}
			clear(pending)
			rescan = false
			timerC = nil
		case <-u.stop:
			if timer != nil {
				timer.Stop()
			}
			return
		}
	}
}

func (u *liveUpdates) markdownPath(filePath string) (string, bool) {
	cleanPath, ok := u.relativePath(filePath)
	return cleanPath, ok && isMarkdownPath(cleanPath)
}

func (u *liveUpdates) markdownDirectoryChanged(filePath string) bool {
	cleanPath, ok := u.relativePath(filePath)
	if !ok {
		return false
	}
	info, err := os.Lstat(filePath)
	if err == nil {
		return info.IsDir()
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return false
	}

	prefix := cleanPath + "/"
	u.mu.Lock()
	defer u.mu.Unlock()
	for trackedPath := range u.snapshots {
		if strings.HasPrefix(trackedPath, prefix) {
			return true
		}
	}
	return false
}

func (u *liveUpdates) relativePath(filePath string) (string, bool) {
	relative, err := filepath.Rel(u.app.root, filePath)
	if err != nil || filepath.IsAbs(relative) {
		return "", false
	}
	cleanPath, err := cleanRelativePath(filepath.ToSlash(relative))
	return cleanPath, err == nil
}

func (u *liveUpdates) recordAllChanges() {
	files, err := u.app.markdownFiles()
	if err != nil {
		log.Printf("rescan Markdown files: %v", err)
		return
	}

	paths := make(map[string]struct{}, len(files))
	for _, filePath := range files {
		paths[filePath] = struct{}{}
	}
	u.mu.Lock()
	for filePath := range u.snapshots {
		paths[filePath] = struct{}{}
	}
	u.mu.Unlock()

	sortedPaths := make([]string, 0, len(paths))
	for filePath := range paths {
		sortedPaths = append(sortedPaths, filePath)
	}
	sort.Strings(sortedPaths)
	for _, filePath := range sortedPaths {
		u.recordChange(filePath)
	}
}

func (u *liveUpdates) recordChange(filePath string) {
	after, err := u.app.readMarkdownSource(filePath)
	removed := errors.Is(err, fs.ErrNotExist) || errors.Is(err, errNotRegular) || errors.Is(err, errSymlink)
	if err != nil && !removed {
		log.Printf("refresh Markdown file %q: %v", filePath, err)
		return
	}

	u.mu.Lock()
	before, existed := u.snapshots[filePath]
	if removed {
		if !existed {
			u.mu.Unlock()
			return
		}
		delete(u.snapshots, filePath)
	} else {
		if existed && bytes.Equal(before, after) {
			u.mu.Unlock()
			return
		}
		u.snapshots[filePath] = after
	}
	u.mu.Unlock()

	kind := "updated"
	if removed {
		kind = "removed"
	} else if !existed {
		kind = "added"
	}
	u.publish(markdownChange{
		Path: filePath,
		Kind: kind,
		Diff: unifiedMarkdownDiff(filePath, before, after),
	})
}

func (u *liveUpdates) publish(change markdownChange) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return
	}

	u.revision++
	change.Revision = u.revision
	u.history = append(u.history, change)
	u.historyBytes += len(change.Diff)
	for len(u.history) > 1 && (len(u.history) > maxChangeHistory || u.historyBytes > maxChangeHistoryBytes) {
		u.historyBytes -= len(u.history[0].Diff)
		u.history = u.history[1:]
	}

	close(u.changed)
	u.changed = make(chan struct{})
}

func (u *liveUpdates) waitForChanges(ctx context.Context, since uint64) changeBatch {
	timer := time.NewTimer(changePollTimeout)
	defer timer.Stop()

	for {
		u.mu.Lock()
		batch, ready := u.batchAfter(since)
		changed := u.changed
		closed := u.closed
		u.mu.Unlock()
		if ready || closed {
			return batch
		}

		select {
		case <-changed:
		case <-timer.C:
			u.mu.Lock()
			batch = changeBatch{Revision: u.revision, Changes: []markdownChange{}}
			u.mu.Unlock()
			return batch
		case <-ctx.Done():
			return changeBatch{}
		}
	}
}

func (u *liveUpdates) batchAfter(since uint64) (changeBatch, bool) {
	batch := changeBatch{Revision: u.revision, Changes: []markdownChange{}}
	if since == u.revision {
		return batch, false
	}
	if since > u.revision || len(u.history) == 0 || since+1 < u.history[0].Revision {
		batch.Reset = true
		return batch, true
	}

	for _, change := range u.history {
		if change.Revision > since {
			batch.Changes = append(batch.Changes, change)
		}
	}
	return batch, true
}

func (a *app) readMarkdownSource(filePath string) ([]byte, error) {
	if !isMarkdownPath(filePath) {
		return nil, errInvalidPath
	}

	file, _, info, err := a.openFile(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	if info.Size() > maxMarkdownSize {
		return nil, errMarkdownTooLarge
	}

	source, err := io.ReadAll(io.LimitReader(file, maxMarkdownSize+1))
	if err != nil {
		return nil, err
	}
	if len(source) > maxMarkdownSize {
		return nil, errMarkdownTooLarge
	}
	return source, nil
}

func isMarkdownPath(filePath string) bool {
	extension := strings.ToLower(filepath.Ext(filePath))
	return extension == ".md" || extension == ".markdown"
}

func unifiedMarkdownDiff(filePath string, before, after []byte) string {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(before)),
		B:        difflib.SplitLines(string(after)),
		FromFile: "a/" + filepath.ToSlash(filePath),
		ToFile:   "b/" + filepath.ToSlash(filePath),
		Context:  3,
	})
	if err != nil {
		return ""
	}
	return diff
}

func parseRevision(value string) (uint64, error) {
	if value == "" {
		return 0, errors.New("since is required")
	}
	revision, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, errors.New("since must be a non-negative integer")
	}
	return revision, nil
}
