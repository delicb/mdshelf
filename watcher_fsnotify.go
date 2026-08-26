//go:build !darwin

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

type fsnotifyFileWatcher struct {
	native    *fsnotify.Watcher
	events    chan string
	errors    chan error
	stop      chan struct{}
	done      chan struct{}
	watched   map[string]struct{}
	recursive bool
	closeOnce sync.Once
	closeErr  error
}

func newFileWatcher(root string) (fileWatcher, error) {
	return newFSNotifyWatcher(root, true)
}

func newParentWatcher(parent string) (fileWatcher, error) {
	return newFSNotifyWatcher(parent, false)
}

func newFSNotifyWatcher(root string, recursive bool) (fileWatcher, error) {
	native, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	watcher := &fsnotifyFileWatcher{
		native:    native,
		events:    make(chan string, 256),
		errors:    make(chan error, 16),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		watched:   make(map[string]struct{}),
		recursive: recursive,
	}
	var watchErr error
	if recursive {
		watchErr = watcher.addDirectoryTree(root)
	} else {
		watchErr = watcher.addDirectory(root)
	}
	if watchErr != nil {
		_ = native.Close()
		return nil, watchErr
	}
	go watcher.run()
	return watcher, nil
}

func (w *fsnotifyFileWatcher) Events() <-chan string {
	return w.events
}

func (w *fsnotifyFileWatcher) Errors() <-chan error {
	return w.errors
}

func (w *fsnotifyFileWatcher) Close() error {
	w.closeOnce.Do(func() {
		close(w.stop)
		w.closeErr = w.native.Close()
		<-w.done
	})
	return w.closeErr
}

func (w *fsnotifyFileWatcher) run() {
	defer close(w.done)
	defer close(w.errors)
	defer close(w.events)

	for {
		select {
		case event, ok := <-w.native.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename|fsnotify.Write) == 0 {
				continue
			}
			if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				w.forgetDirectoryTree(event.Name)
			}
			if event.Op&(fsnotify.Create|fsnotify.Rename) != 0 && w.recursive {
				if info, err := os.Lstat(event.Name); err == nil && info.IsDir() {
					if err := w.addDirectoryTree(event.Name); err != nil {
						w.report(err)
					}
				}
			}
			select {
			case w.events <- event.Name:
			case <-w.stop:
				return
			}
		case err, ok := <-w.native.Errors:
			if !ok {
				return
			}
			w.report(err)
		case <-w.stop:
			return
		}
	}
}

func (w *fsnotifyFileWatcher) addDirectoryTree(root string) error {
	return filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath != root && strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		return w.addDirectory(filePath)
	})
}

func (w *fsnotifyFileWatcher) addDirectory(path string) error {
	path = filepath.Clean(path)
	if _, ok := w.watched[path]; ok {
		return nil
	}
	if err := w.native.Add(path); err != nil {
		return err
	}
	w.watched[path] = struct{}{}
	return nil
}

func (w *fsnotifyFileWatcher) forgetDirectoryTree(root string) {
	root = filepath.Clean(root)
	prefix := root + string(filepath.Separator)
	for watchedPath := range w.watched {
		if watchedPath != root && !strings.HasPrefix(watchedPath, prefix) {
			continue
		}
		delete(w.watched, watchedPath)
		_ = w.native.Remove(watchedPath)
	}
}

func (w *fsnotifyFileWatcher) report(err error) {
	select {
	case w.errors <- err:
	case <-w.stop:
	}
}
