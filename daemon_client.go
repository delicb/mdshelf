package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	errDaemonNotRunning     = errors.New("MDShelf daemon is not running")
	errForeignDaemonPort    = errors.New("another service uses the MDShelf daemon port")
	errDaemonReviewsMissing = errors.New("The running MDShelf daemon does not support reviews. Run `mdshelf stop`, then retry.")
)

type daemonHealthResponse struct {
	Service  string   `json:"service"`
	Protocol int      `json:"protocol"`
	PID      int      `json:"pid"`
	Features []string `json:"features"`
}

var daemonHTTPClient = &http.Client{
	Timeout: 2 * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{Timeout: 500 * time.Millisecond}).DialContext,
	},
}

type daemonCommandDeps struct {
	health  func() error
	start   func() error
	control func(string, any, any) error
	sleep   func(time.Duration)
	now     func() time.Time
}

func runDaemonCommand(command string, args []string, stdout, stderr io.Writer) error {
	return runDaemonCommandWithOutputs(command, args, stdout, stderr, daemonCommandDeps{
		health: checkDaemonHealth, start: startDaemon, control: daemonControl, sleep: time.Sleep, now: time.Now,
	})
}

func runDaemonCommandWithOutputs(command string, args []string, stdout, stderr io.Writer, deps daemonCommandDeps) error {
	usage := map[string]string{
		"add":    "Usage: mdshelf add [--json] <markdown-file>\nRegister one Markdown file and start the daemon if needed.\n",
		"list":   "Usage: mdshelf list\nList registered Markdown files.\n",
		"remove": "Usage: mdshelf remove <markdown-file>\nRemove one Markdown file from the daemon.\n",
		"status": "Usage: mdshelf status\nShow daemon status.\n",
		"stop":   "Usage: mdshelf stop\nStop the daemon.\n",
	}
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		_, _ = io.WriteString(stdout, usage[command])
		return nil
	}
	jsonOutput := false
	if command == "add" {
		flags := flag.NewFlagSet("mdshelf add", flag.ContinueOnError)
		flags.SetOutput(stderr)
		flags.BoolVar(&jsonOutput, "json", false, "write one JSON object")
		flags.Usage = func() { _, _ = io.WriteString(stdout, usage[command]) }
		if err := flags.Parse(args); err != nil {
			return err
		}
		args = flags.Args()
	}
	requiresPath := command == "add" || command == "remove"
	if (requiresPath && len(args) != 1) || (!requiresPath && len(args) != 0) {
		return errors.New(strings.TrimSpace(strings.Split(usage[command], "\n")[0]))
	}

	if command == "add" {
		canonical, err := canonicalDocumentPath(args[0])
		if err != nil {
			return err
		}
		err = deps.health()
		if errors.Is(err, errDaemonNotRunning) {
			if err := deps.start(); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		request := struct {
			Path string `json:"path"`
		}{Path: canonical}
		if jsonOutput {
			var response struct {
				Document daemonDocumentResponse `json:"document"`
				Added    bool                   `json:"added"`
			}
			if err := deps.control("add", request, &response); err != nil {
				return err
			}
			return json.NewEncoder(stdout).Encode(struct {
				SchemaVersion int                    `json:"schemaVersion"`
				Added         bool                   `json:"added"`
				Document      daemonDocumentResponse `json:"document"`
			}{SchemaVersion: reviewAPISchemaVersion, Added: response.Added, Document: response.Document})
		}
		var response struct {
			Document daemonDocumentResponse `json:"document"`
		}
		if err := deps.control("add", request, &response); err != nil {
			return err
		}
		fmt.Fprintln(stdout, response.Document.URL)
		return nil
	}

	if err := deps.health(); err != nil {
		return err
	}
	switch command {
	case "list":
		var response struct {
			Documents []daemonDocumentResponse `json:"documents"`
		}
		if err := deps.control("list", struct{}{}, &response); err != nil {
			return err
		}
		sort.Slice(response.Documents, func(i, j int) bool { return response.Documents[i].Path < response.Documents[j].Path })
		fmt.Fprintln(stdout, "STATUS\tID\tPATH\tURL")
		for _, document := range response.Documents {
			status := "present"
			if document.Removed {
				status = "removed"
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", status, document.ID, document.Path, document.URL)
		}
	case "remove":
		canonical, err := canonicalDocumentPath(args[0])
		if err != nil {
			return err
		}
		if err := deps.control("remove", struct {
			Path string `json:"path"`
		}{canonical}, &struct{}{}); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Removed %s.\n", canonical)
	case "status":
		var response struct {
			URL              string `json:"url"`
			PID              int    `json:"pid"`
			Documents        int    `json:"documents"`
			RemovedDocuments int    `json:"removedDocuments"`
		}
		if err := deps.control("status", struct{}{}, &response); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "URL: %s\nPID: %d\nDocuments: %d\nRemoved: %d\n", response.URL, response.PID, response.Documents, response.RemovedDocuments)
	case "stop":
		if err := deps.control("stop", struct{}{}, &struct{}{}); err != nil {
			return err
		}
		deadline := deps.now().Add(5 * time.Second)
		for deps.now().Before(deadline) {
			deps.sleep(50 * time.Millisecond)
			if errors.Is(deps.health(), errDaemonNotRunning) {
				fmt.Fprintln(stdout, "MDShelf daemon stopped.")
				return nil
			}
		}
		return errors.New("MDShelf daemon did not stop")
	}
	return nil
}

func checkDaemonHealth() error {
	_, err := readDaemonHealth()
	return err
}

func checkDaemonReviewHealth() error {
	health, err := readDaemonHealth()
	if err != nil {
		return err
	}
	for _, feature := range health.Features {
		if feature == reviewFeature {
			return nil
		}
	}
	return errDaemonReviewsMissing
}

func readDaemonHealth() (daemonHealthResponse, error) {
	baseURL, err := configuredDaemonBaseURL()
	if err != nil {
		return daemonHealthResponse{}, err
	}
	response, err := daemonHTTPClient.Get(baseURL + "/api/health")
	if err != nil {
		var networkError net.Error
		if errors.As(err, &networkError) {
			return daemonHealthResponse{}, errDaemonNotRunning
		}
		return daemonHealthResponse{}, err
	}
	defer response.Body.Close()
	var health daemonHealthResponse
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&health) != nil || health.Service != "mdshelf-daemon" || health.Protocol != daemonProtocol {
		return daemonHealthResponse{}, errForeignDaemonPort
	}
	return health, nil
}

func daemonControl(endpoint string, request, response any) error {
	baseURL, err := configuredDaemonBaseURL()
	if err != nil {
		return err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}
	httpRequest, err := http.NewRequest(http.MethodPost, baseURL+"/api/control/"+endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpResponse, err := daemonHTTPClient.Do(httpRequest)
	if err != nil {
		return err
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		var payload struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(httpResponse.Body, 64<<10)).Decode(&payload)
		if payload.Error == "" {
			payload.Error = httpResponse.Status
		}
		return errors.New(payload.Error)
	}
	return json.NewDecoder(httpResponse.Body).Decode(response)
}

func configuredDaemonBaseURL() (string, error) {
	stateDir, err := daemonStateDir()
	if err != nil {
		return "", err
	}
	return daemonBaseURLFromStateDir(stateDir)
}

func daemonBaseURLFromStateDir(stateDir string) (string, error) {
	config, err := loadDaemonConfig(stateDir)
	if err != nil {
		return "", err
	}
	return daemonBaseURL(config.Port), nil
}

func startDaemon() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	stateDir, err := daemonStateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	logPath := filepath.Join(stateDir, "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open daemon log: %w", err)
	}
	command := exec.Command(executable, "__daemon")
	command.Stdin = nil
	command.Stdout = logFile
	command.Stderr = logFile
	configureDetachedProcess(command)
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start daemon: %w", err)
	}
	_ = command.Process.Release()
	_ = logFile.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		err := checkDaemonHealth()
		if err == nil {
			return nil
		}
		if errors.Is(err, errForeignDaemonPort) {
			return err
		}
	}
	return fmt.Errorf("daemon did not start; see %s", logPath)
}
