package main

import (
	"sort"
	"time"
)

type fileWatcher interface {
	Events() <-chan string
	Errors() <-chan error
	Close() error
}

// debouncer coalesces bursts of watcher events: arm (re)starts the delay, and
// C fires once the delay passes without another arm.
type debouncer struct {
	delay time.Duration
	timer *time.Timer

	// C is nil until arm is called and after fired, so a select case reading
	// from it blocks unless a flush is due.
	C <-chan time.Time
}

func newDebouncer(delay time.Duration) *debouncer {
	return &debouncer{delay: delay}
}

// arm (re)starts the debounce delay.
func (d *debouncer) arm() {
	if d.timer == nil {
		d.timer = time.NewTimer(d.delay)
	} else {
		if !d.timer.Stop() {
			select {
			case <-d.timer.C:
			default:
			}
		}
		d.timer.Reset(d.delay)
	}
	d.C = d.timer.C
}

// fired marks the delay as consumed after a receive from C.
func (d *debouncer) fired() {
	d.C = nil
}

// stop releases the timer.
func (d *debouncer) stop() {
	if d.timer != nil {
		d.timer.Stop()
	}
}

// drainPending empties a debounced set and returns its keys in sorted order.
func drainPending(pending map[string]struct{}) []string {
	keys := make([]string, 0, len(pending))
	for key := range pending {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	clear(pending)
	return keys
}
