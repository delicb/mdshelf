package main

type fileWatcher interface {
	Events() <-chan string
	Errors() <-chan error
	Close() error
}
