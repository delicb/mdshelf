package main

import (
	"bytes"
	"errors"
	"flag"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestParseOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    options
		wantErr string
	}{
		{
			name: "defaults",
			want: options{port: defaultPort, root: "."},
		},
		{
			name: "root",
			args: []string{"/tmp/phone notes"},
			want: options{port: defaultPort, root: "/tmp/phone notes"},
		},
		{
			name: "port and root",
			args: []string{"-port", "9123", "/tmp/notes"},
			want: options{port: 9123, root: "/tmp/notes"},
		},
		{
			name: "lowest port",
			args: []string{"-port", "1"},
			want: options{port: 1, root: "."},
		},
		{
			name: "highest port",
			args: []string{"-port", "65535"},
			want: options{port: 65535, root: "."},
		},
		{
			name:    "port too low",
			args:    []string{"-port", "0"},
			wantErr: "port must be between 1 and 65535",
		},
		{
			name:    "port too high",
			args:    []string{"-port", "65536"},
			wantErr: "port must be between 1 and 65535",
		},
		{
			name:    "invalid port",
			args:    []string{"-port", "nope"},
			wantErr: "invalid value",
		},
		{
			name:    "too many roots",
			args:    []string{"one", "two"},
			wantErr: "accepts at most one root folder",
		},
		{
			name: "allowed hostname",
			args: []string{"-allow-hostname", "mentat"},
			want: options{port: defaultPort, root: ".", allowedHostnames: []string{"mentat"}},
		},
		{
			name: "allowed hostnames are normalized and deduplicated",
			args: []string{"-allow-hostname", "Mentat:7331", "-allow-hostname", "notes.example.ts.net", "-allow-hostname", "mentat"},
			want: options{port: defaultPort, root: ".", allowedHostnames: []string{"mentat", "notes.example.ts.net"}},
		},
		{
			name:    "invalid allowed hostname",
			args:    []string{"-allow-hostname", "http://mentat"},
			wantErr: "hostname is invalid",
		},
		{
			name:    "empty allowed hostname",
			args:    []string{"-allow-hostname", ""},
			wantErr: "hostname must not be empty",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseOptions(test.args, &bytes.Buffer{})
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("parseOptions() error = %v, want error containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseOptions() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("parseOptions() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestShutdownAdHocServer(t *testing.T) {
	a, err := newAppWithWatcher(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: a.Handler()}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()

	shutdownAdHocServer(server, a)

	if err := <-serveDone; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Serve() error = %v, want http.ErrServerClosed", err)
	}
	a.updates.feed.mu.Lock()
	closed := a.updates.feed.closed
	a.updates.feed.mu.Unlock()
	if !closed {
		t.Fatal("live updates are not closed after shutdown")
	}
}

func TestVersionFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-version"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(-version) error = %v", err)
	}
	if got, want := stdout.String(), "mdshelf "+version+"\n"; got != want {
		t.Fatalf("run(-version) output = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("run(-version) stderr = %q, want empty", stderr.String())
	}
}

func TestParseOptionsHelp(t *testing.T) {
	var output bytes.Buffer
	_, err := parseOptions([]string{"-help"}, &output)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("parseOptions(-help) error = %v, want flag.ErrHelp", err)
	}
	for _, want := range []string{"mdshelf [options] [root]", "mdshelf add [--json] <markdown-file>", "mdshelf review show", "mdshelf skill install", "-allow-hostname value", "-port int", "(default 7331)", "-version"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("help output does not contain %q:\n%s", want, output.String())
		}
	}
}
