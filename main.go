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

const defaultPort = 7331

type options struct {
	port int
	root string
}

func main() {
	options, err := parseOptions(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		log.Fatal(err)
	}

	app, err := newApp(options.root)
	if err != nil {
		log.Fatal(err)
	}
	port := strconv.Itoa(options.port)

	server := &http.Server{
		Addr:              net.JoinHostPort("", port),
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("mdshelf is serving %s", app.root)
	log.Printf("Local:   http://localhost:%s", port)
	for _, address := range networkURLs(port) {
		log.Printf("Network: %s", address)
	}

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func parseOptions(args []string, output io.Writer) (options, error) {
	parsed := options{
		port: defaultPort,
		root: ".",
	}
	flags := flag.NewFlagSet("mdshelf", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.IntVar(&parsed.port, "port", defaultPort, "port to listen on")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: mdshelf [options] [root]")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Options:")
		flags.PrintDefaults()
	}

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
