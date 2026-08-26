package main

import (
	"context"
	"sync"
	"time"
)

type changeFeed struct {
	mu           sync.Mutex
	revision     uint64
	history      []markdownChange
	historyBytes int
	changed      chan struct{}
	closed       bool
}

func newChangeFeed() *changeFeed {
	return &changeFeed{changed: make(chan struct{})}
}

func (f *changeFeed) publish(change markdownChange) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.revision++
	change.Revision = f.revision
	f.history = append(f.history, change)
	f.historyBytes += len(change.Diff)
	for len(f.history) > 1 && (len(f.history) > maxChangeHistory || f.historyBytes > maxChangeHistoryBytes) {
		f.historyBytes -= len(f.history[0].Diff)
		f.history = f.history[1:]
	}
	close(f.changed)
	f.changed = make(chan struct{})
}

func (f *changeFeed) wait(ctx context.Context, since uint64) changeBatch {
	timer := time.NewTimer(changePollTimeout)
	defer timer.Stop()
	for {
		f.mu.Lock()
		batch, ready := f.batchAfter(since)
		changed := f.changed
		closed := f.closed
		f.mu.Unlock()
		if ready || closed {
			return batch
		}
		select {
		case <-changed:
		case <-timer.C:
			f.mu.Lock()
			batch = changeBatch{Revision: f.revision, Changes: []markdownChange{}}
			f.mu.Unlock()
			return batch
		case <-ctx.Done():
			return changeBatch{}
		}
	}
}

func (f *changeFeed) batchAfter(since uint64) (changeBatch, bool) {
	batch := changeBatch{Revision: f.revision, Changes: []markdownChange{}}
	if since == f.revision {
		return batch, false
	}
	if since > f.revision || len(f.history) == 0 || since+1 < f.history[0].Revision {
		batch.Reset = true
		return batch, true
	}
	for _, change := range f.history {
		if change.Revision > since {
			batch.Changes = append(batch.Changes, change)
		}
	}
	return batch, true
}

func (f *changeFeed) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.closed = true
	close(f.changed)
}
