package main

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
)

// runDaemonCommandWithDeps is a test shorthand for runDaemonCommandWithOutputs
// that discards stderr.
func runDaemonCommandWithDeps(command string, args []string, stdout io.Writer, deps daemonCommandDeps) error {
	return runDaemonCommandWithOutputs(command, args, stdout, io.Discard, deps)
}

func TestDaemonAddStartsOnlyWhenNotRunning(t *testing.T) {
	path := mustWriteFile(t, t.TempDir(), "note.md", "# Note\n")
	canonical, err := canonicalDocumentPath(path)
	if err != nil {
		t.Fatal(err)
	}
	startCalls := 0
	controlCalls := 0
	deps := daemonCommandDeps{
		health: func() error { return errDaemonNotRunning },
		start: func() error {
			startCalls++
			return nil
		},
		control: func(endpoint string, request, response any) error {
			controlCalls++
			if endpoint != "add" {
				t.Fatalf("endpoint = %q", endpoint)
			}
			pathField := reflect.ValueOf(request).FieldByName("Path").String()
			if pathField != canonical {
				t.Fatalf("request path = %q, want %q", pathField, canonical)
			}
			result := response.(*struct {
				Document daemonDocumentResponse `json:"document"`
			})
			result.Document.URL = "http://localhost:7332/#/abc"
			return nil
		},
		sleep: func(time.Duration) {},
		now:   time.Now,
	}
	var output bytes.Buffer
	if err := runDaemonCommandWithDeps("add", []string{path}, &output, deps); err != nil {
		t.Fatal(err)
	}
	if startCalls != 1 || controlCalls != 1 || output.String() != "http://localhost:7332/#/abc\n" {
		t.Fatalf("start=%d control=%d output=%q", startCalls, controlCalls, output.String())
	}
}

func TestDaemonListDoesNotStartDaemon(t *testing.T) {
	started := false
	deps := daemonCommandDeps{
		health: func() error { return errDaemonNotRunning },
		start: func() error {
			started = true
			return nil
		},
		control: func(string, any, any) error { return errors.New("control must not run") },
		sleep:   func(time.Duration) {},
		now:     time.Now,
	}
	if err := runDaemonCommandWithDeps("list", nil, &bytes.Buffer{}, deps); !errors.Is(err, errDaemonNotRunning) {
		t.Fatalf("error = %v", err)
	}
	if started {
		t.Fatal("list started the daemon")
	}
}

func TestDaemonStopWaitsForHealthFailure(t *testing.T) {
	healthCalls := 0
	current := time.Unix(0, 0)
	deps := daemonCommandDeps{
		health: func() error {
			healthCalls++
			if healthCalls >= 3 {
				return errDaemonNotRunning
			}
			return nil
		},
		start: func() error { return errors.New("start must not run") },
		control: func(endpoint string, _, _ any) error {
			if endpoint != "stop" {
				t.Fatalf("endpoint = %q", endpoint)
			}
			return nil
		},
		sleep: func(duration time.Duration) { current = current.Add(duration) },
		now:   func() time.Time { return current },
	}
	var output bytes.Buffer
	if err := runDaemonCommandWithDeps("stop", nil, &output, deps); err != nil {
		t.Fatal(err)
	}
	if output.String() != "MDShelf daemon stopped.\n" || healthCalls != 3 {
		t.Fatalf("health calls=%d output=%q", healthCalls, output.String())
	}
}
