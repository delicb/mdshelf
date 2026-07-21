package main

import (
	"bytes"
	"errors"
	"flag"
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
			if got != test.want {
				t.Fatalf("parseOptions() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestParseOptionsHelp(t *testing.T) {
	var output bytes.Buffer
	_, err := parseOptions([]string{"-help"}, &output)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("parseOptions(-help) error = %v, want flag.ErrHelp", err)
	}
	for _, want := range []string{"Usage: mdshelf [options] [root]", "-port int", "(default 7331)"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("help output does not contain %q:\n%s", want, output.String())
		}
	}
}
