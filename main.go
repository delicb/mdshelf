package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"syscall"
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

Daemon mode uses http://localhost:7332 by default.
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.ListenAndServe() }()
	select {
	case err := <-serveDone:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}
	stop()
	log.Printf("mdshelf is shutting down")
	shutdownAdHocServer(server, a)
	<-serveDone
	return nil
}

// shutdownAdHocServer drains open requests, then closes the app's file watcher.
func shutdownAdHocServer(server *http.Server, a *app) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
	}
	a.Close()
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
