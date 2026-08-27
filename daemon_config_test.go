package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadDaemonConfig(t *testing.T) {
	tests := []struct {
		name    string
		content string
		missing bool
		want    daemonConfig
		wantErr string
	}{
		{name: "missing", missing: true, want: defaultDaemonConfig()},
		{name: "defaults", content: `{}`, want: defaultDaemonConfig()},
		{
			name:    "network settings",
			content: `{"listenOnAllInterfaces":true,"port":7444,"allowedHostnames":["Mentat:7332","mentat","notes.example.ts.net:9000"]}`,
			want: daemonConfig{
				ListenOnAllInterfaces: true,
				Port:                  7444,
				AllowedHostnames:      []string{"mentat", "notes.example.ts.net"},
			},
		},
		{name: "port too low", content: `{"port":0}`, wantErr: "daemon port must be between 1 and 65535"},
		{name: "port too high", content: `{"port":65536}`, wantErr: "daemon port must be between 1 and 65535"},
		{name: "invalid hostname", content: `{"allowedHostnames":["http://mentat"]}`, wantErr: "hostname is invalid"},
		{name: "unknown field", content: `{"listenEverywhere":true}`, wantErr: "unknown field"},
		{name: "wrong type", content: `{"listenOnAllInterfaces":"yes"}`, wantErr: "cannot unmarshal"},
		{name: "multiple values", content: `{} {}`, wantErr: "multiple JSON values"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stateDir := t.TempDir()
			if !test.missing {
				path := filepath.Join(stateDir, daemonConfigFileName)
				if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := loadDaemonConfig(stateDir)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("loadDaemonConfig() error = %v, want error containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("loadDaemonConfig() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestDaemonListenAddress(t *testing.T) {
	if got := daemonListenAddress(defaultDaemonConfig()); got != "127.0.0.1:7332" {
		t.Fatalf("default address = %q", got)
	}
	config := daemonConfig{ListenOnAllInterfaces: true, Port: 7444}
	if got := daemonListenAddress(config); got != ":7444" {
		t.Fatalf("all-interface address = %q", got)
	}
}

func TestDaemonBaseURLFromStateDir(t *testing.T) {
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, daemonConfigFileName)
	if err := os.WriteFile(path, []byte(`{"port":7444}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := daemonBaseURLFromStateDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://localhost:7444" {
		t.Fatalf("daemon base URL = %q", got)
	}
}

func TestValidDaemonHost(t *testing.T) {
	config := daemonConfig{
		ListenOnAllInterfaces: true,
		Port:                  7444,
		AllowedHostnames:      []string{"mentat", "notes.example.ts.net"},
	}
	tests := []struct {
		host string
		want bool
	}{
		{host: "localhost:7444", want: true},
		{host: "192.0.2.10:7444", want: true},
		{host: "mentat:7444", want: true},
		{host: "MENTAT.:7444", want: true},
		{host: "notes.example.ts.net:7444", want: true},
		{host: "other:7444", want: false},
		{host: "mentat:7332", want: false},
	}
	for _, test := range tests {
		if got := validDaemonHost(test.host, config); got != test.want {
			t.Errorf("validDaemonHost(%q) = %t, want %t", test.host, got, test.want)
		}
	}
}
