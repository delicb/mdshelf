package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"
)

const (
	defaultPort       = 7331
	defaultDaemonPort = 7332
)

const topLevelHelp = `Usage:
  mdshelf [options] [root]
  mdshelf add [--json] <markdown-file>
  mdshelf review show [--json] [--include-resolved] <markdown-file>
  mdshelf review address [--json] --message <text> <comment-id>
  mdshelf skill print
  mdshelf skill install [--force] <skills-directory>
  mdshelf list
  mdshelf remove <markdown-file>
  mdshelf status
  mdshelf stop

Options:
  -port int
        port to listen on in ad-hoc mode (default 7331)

Daemon mode uses http://localhost:7332.
`

type options struct {
	port int
	root string
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		log.Fatal(err)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "add", "list", "remove", "status", "stop":
			return runDaemonCommand(args[0], args[1:], stdout, stderr)
		case "review":
			return runReviewCommand(args[1:], stdout, stderr)
		case "skill":
			return runSkillCommand(args[1:], stdout, stderr)
		case "__daemon":
			if len(args) != 1 {
				return errors.New("Usage: mdshelf __daemon")
			}
			return serveDaemon("")
		}
	}

	parsed, err := parseOptions(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	return serveAdHoc(parsed)
}

func serveAdHoc(options options) error {
	a, err := newApp(options.root)
	if err != nil {
		return err
	}
	defer a.Close()
	port := strconv.Itoa(options.port)
	server := &http.Server{
		Addr:              net.JoinHostPort("", port),
		Handler:           a.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("mdshelf is serving %s", a.root)
	log.Printf("Local:   http://localhost:%s", port)
	for _, address := range networkURLs(port) {
		log.Printf("Network: %s", address)
	}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func parseOptions(args []string, output io.Writer) (options, error) {
	parsed := options{port: defaultPort, root: "."}
	flags := flag.NewFlagSet("mdshelf", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.IntVar(&parsed.port, "port", defaultPort, "port to listen on in ad-hoc mode")
	flags.Usage = func() { fmt.Fprint(output, topLevelHelp) }

	if err := flags.Parse(args); err != nil {
		return parsed, err
	}
	if flags.NArg() > 1 {
		return parsed, errors.New("accepts at most one root folder")
	}
	if flags.NArg() == 1 {
		parsed.root = flags.Arg(0)
	}
	if parsed.port < 1 || parsed.port > 65535 {
		return parsed, errors.New("port must be between 1 and 65535")
	}
	return parsed, nil
}

func networkURLs(port string) []string {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}

	urls := make([]string, 0)
	seen := make(map[string]struct{})
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err != nil || ip.IsLoopback() || !ip.IsGlobalUnicast() {
			continue
		}
		url := "http://" + net.JoinHostPort(ip.String(), port)
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		urls = append(urls, url)
	}
	sort.Strings(urls)
	return urls
}
