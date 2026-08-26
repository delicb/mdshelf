//go:build darwin && !cgo

package main

import "errors"

func newFileWatcher(string) (fileWatcher, error) {
	return nil, errors.New("fsevents requires cgo")
}

func newParentWatcher(string) (fileWatcher, error) {
	return nil, errors.New("fsevents requires cgo")
}
