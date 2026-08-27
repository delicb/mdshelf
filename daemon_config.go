package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

const daemonConfigFileName = "config.json"

type daemonConfig struct {
	ListenOnAllInterfaces bool     `json:"listenOnAllInterfaces"`
	Port                  int      `json:"port"`
	AllowedHostnames      []string `json:"allowedHostnames"`
}

func defaultDaemonConfig() daemonConfig {
	return daemonConfig{Port: defaultDaemonPort}
}

func loadDaemonConfig(stateDir string) (daemonConfig, error) {
	path := filepath.Join(stateDir, daemonConfigFileName)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultDaemonConfig(), nil
	}
	if err != nil {
		return daemonConfig{}, fmt.Errorf("open daemon config: %w", err)
	}
	defer file.Close()

	config := defaultDaemonConfig()
	decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return daemonConfig{}, fmt.Errorf("decode daemon config: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return daemonConfig{}, fmt.Errorf("decode daemon config: %w", err)
	}
	if config.Port < 1 || config.Port > 65535 {
		return daemonConfig{}, errors.New("daemon port must be between 1 and 65535")
	}

	var allowed []string
	if len(config.AllowedHostnames) > 0 {
		allowed = make([]string, 0, len(config.AllowedHostnames))
	}
	seen := make(map[string]struct{}, len(config.AllowedHostnames))
	for index, value := range config.AllowedHostnames {
		hostname, err := normalizeAllowedHostname(value)
		if err != nil {
			return daemonConfig{}, fmt.Errorf("allowed hostname %d: %w", index+1, err)
		}
		if _, ok := seen[hostname]; ok {
			continue
		}
		seen[hostname] = struct{}{}
		allowed = append(allowed, hostname)
	}
	config.AllowedHostnames = allowed
	return config, nil
}

func normalizeAllowedHostname(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("hostname must not be empty")
	}
	if strings.ContainsAny(value, "/\\?#@[]") {
		return "", errors.New("hostname is invalid")
	}
	hostname := value
	if strings.Contains(value, ":") {
		host, _, err := net.SplitHostPort(value)
		if err != nil {
			return "", errors.New("hostname must have an optional port")
		}
		hostname = host
	}
	hostname = strings.TrimSuffix(hostname, ".")
	if hostname == "" || strings.ContainsAny(hostname, "/\\?#@[]") {
		return "", errors.New("hostname is invalid")
	}
	for _, character := range hostname {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return "", errors.New("hostname is invalid")
		}
	}
	return strings.ToLower(hostname), nil
}

func daemonListenAddress(config daemonConfig) string {
	host := "127.0.0.1"
	if config.ListenOnAllInterfaces {
		host = ""
	}
	return net.JoinHostPort(host, strconv.Itoa(config.Port))
}

func daemonBaseURL(port int) string {
	return "http://localhost:" + strconv.Itoa(port)
}
