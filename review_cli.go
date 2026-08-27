package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

const reviewHelp = `Usage:
  mdshelf review show [--json] [--include-resolved] <markdown-file>
  mdshelf review address [--json] --message <text> <comment-id>
`

const reviewShowHelp = `Usage: mdshelf review show [--json] [--include-resolved] <markdown-file>
Show comments for a document.
`

const reviewAddressHelp = `Usage: mdshelf review address [--json] --message <text> <comment-id>
Reply to an open comment and mark it as addressed.
`

type reviewCommandDeps struct {
	health  func() error
	control func(string, any, any) error
}

func runReviewCommand(args []string, stdout, stderr io.Writer) error {
	return runReviewCommandWithDeps(args, stdout, stderr, reviewCommandDeps{
		health: checkDaemonReviewHealth, control: daemonControl,
	})
}

func runReviewCommandWithDeps(args []string, stdout, stderr io.Writer, deps reviewCommandDeps) error {
	if len(args) == 0 {
		return errors.New("Usage: mdshelf review <show|address>")
	}
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		_, _ = io.WriteString(stdout, reviewHelp)
		return nil
	}
	switch args[0] {
	case "show":
		return runReviewShow(args[1:], stdout, stderr, deps)
	case "address":
		return runReviewAddress(args[1:], stdout, stderr, deps)
	default:
		return fmt.Errorf("unknown review command %q", args[0])
	}
}

func runReviewShow(args []string, stdout, stderr io.Writer, deps reviewCommandDeps) error {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		_, _ = io.WriteString(stdout, reviewShowHelp)
		return nil
	}
	flags := flag.NewFlagSet("mdshelf review show", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "write one JSON object")
	includeResolved := flags.Bool("include-resolved", false, "include resolved comments")
	flags.Usage = func() { _, _ = io.WriteString(stdout, reviewShowHelp) }
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("Usage: mdshelf review show [--json] [--include-resolved] <markdown-file>")
	}
	canonical, err := canonicalDocumentPath(flags.Arg(0))
	if err != nil {
		return err
	}
	if err := deps.health(); err != nil {
		return err
	}
	var response reviewShowResponse
	if err := deps.control("review/show", showReviewRequest{Path: canonical, IncludeResolved: *includeResolved}, &response); err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(response)
	}
	return formatReviewMarkdown(stdout, response)
}

func runReviewAddress(args []string, stdout, stderr io.Writer, deps reviewCommandDeps) error {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		_, _ = io.WriteString(stdout, reviewAddressHelp)
		return nil
	}
	flags := flag.NewFlagSet("mdshelf review address", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "write one JSON object")
	message := flags.String("message", "", "agent reply")
	flags.Usage = func() { _, _ = io.WriteString(stdout, reviewAddressHelp) }
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("Usage: mdshelf review address [--json] --message <text> <comment-id>")
	}
	commentID := flags.Arg(0)
	if strings.TrimSpace(commentID) == "" {
		return errors.New("comment id is required")
	}
	if strings.TrimSpace(*message) == "" {
		return errors.New("--message must not be empty")
	}
	if err := deps.health(); err != nil {
		return err
	}
	var response reviewMutationResponse
	if err := deps.control("review/comments/address", addressReviewCommentRequest{CommentID: commentID, Message: *message}, &response); err != nil {
		return err
	}
	if response.Comment == nil {
		return errors.New("daemon returned no addressed comment")
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(response.Comment)
	}
	_, err := fmt.Fprintf(stdout, "Addressed %s.\n", commentID)
	return err
}
