//go:build darwin && cgo

package main

import (
	"path/filepath"
	"sync"

	"github.com/rjeczalik/notify"
)

type notifyFileWatcher struct {
	source    chan notify.EventInfo
	events    chan string
	errors    chan error
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func newFileWatcher(root string) (fileWatcher, error) {
	watcher := &notifyFileWatcher{
		source: make(chan notify.EventInfo, 256),
		events: make(chan string, 256),
		errors: make(chan error),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	if err := notify.Watch(filepath.Join(root, "..."), watcher.source, notify.Create, notify.Remove, notify.Rename, notify.Write); err != nil {
		return nil, err
	}
	go watcher.run()
	return watcher, nil
}

func (w *notifyFileWatcher) Events() <-chan string {
	return w.events
}

func (w *notifyFileWatcher) Errors() <-chan error {
	return w.errors
}

func (w *notifyFileWatcher) Close() error {
	w.closeOnce.Do(func() {
		notify.Stop(w.source)
		close(w.stop)
		<-w.done
	})
	return nil
}

func (w *notifyFileWatcher) run() {
	defer close(w.done)
	defer close(w.errors)
	defer close(w.events)

	for {
		select {
		case event := <-w.source:
			select {
			case w.events <- event.Path():
			case <-w.stop:
				return
			}
		case <-w.stop:
			return
		}
	}
}
