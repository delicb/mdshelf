package main

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed skills/mdshelf/SKILL.md
var mdshelfSkill []byte

const skillHelp = `Usage:
  mdshelf skill print
  mdshelf skill install [--force] <skills-directory>
`

func runSkillCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("Usage: mdshelf skill <print|install>")
	}
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		_, _ = io.WriteString(stdout, skillHelp)
		return nil
	}

	switch args[0] {
	case "print":
		flags := flag.NewFlagSet("mdshelf skill print", flag.ContinueOnError)
		flags.SetOutput(stderr)
		flags.Usage = func() { _, _ = io.WriteString(stderr, "Usage: mdshelf skill print\n") }
		if err := flags.Parse(args[1:]); errors.Is(err, flag.ErrHelp) {
			return nil
		} else if err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("Usage: mdshelf skill print")
		}
		_, err := stdout.Write(mdshelfSkill)
		return err
	case "install":
		flags := flag.NewFlagSet("mdshelf skill install", flag.ContinueOnError)
		flags.SetOutput(stderr)
		force := flags.Bool("force", false, "replace a different installed skill")
		flags.Usage = func() {
			_, _ = io.WriteString(stderr, "Usage: mdshelf skill install [--force] <skills-directory>\n")
		}
		if err := flags.Parse(args[1:]); errors.Is(err, flag.ErrHelp) {
			return nil
		} else if err != nil {
			return err
		}
		if flags.NArg() != 1 {
			return errors.New("Usage: mdshelf skill install [--force] <skills-directory>")
		}
		destination, current, err := installMDShelfSkill(flags.Arg(0), *force)
		if err != nil {
			return err
		}
		if current {
			fmt.Fprintf(stdout, "MDShelf skill is current at %s.\n", destination)
		} else {
			fmt.Fprintf(stdout, "Installed MDShelf skill at %s.\n", destination)
		}
		return nil
	default:
		return fmt.Errorf("unknown skill command %q", args[0])
	}
}

func installMDShelfSkill(rootPath string, force bool) (string, bool, error) {
	directory := filepath.Join(rootPath, "mdshelf")
	destination := filepath.Join(directory, "SKILL.md")
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		return destination, false, fmt.Errorf("create skills directory: %w", err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return destination, false, fmt.Errorf("open skills directory: %w", err)
	}
	defer root.Close()

	info, err := root.Lstat("mdshelf")
	switch {
	case err == nil && info.Mode()&fs.ModeSymlink != 0:
		return destination, false, errors.New("mdshelf skill directory must not be a symbolic link")
	case err == nil && !info.IsDir():
		return destination, false, errors.New("mdshelf skill path is not a directory")
	case errors.Is(err, fs.ErrNotExist):
		if err := root.Mkdir("mdshelf", 0o755); err != nil {
			return destination, false, fmt.Errorf("create skill directory: %w", err)
		}
	case err != nil:
		return destination, false, fmt.Errorf("inspect skill directory: %w", err)
	}

	directoryFile, err := root.Open("mdshelf")
	if err != nil {
		return destination, false, fmt.Errorf("open skill directory: %w", err)
	}
	if err := directoryFile.Chmod(0o755); err != nil {
		_ = directoryFile.Close()
		return destination, false, fmt.Errorf("set skill directory permissions: %w", err)
	}
	_ = directoryFile.Close()

	const destinationName = "mdshelf/SKILL.md"
	existing, err := root.ReadFile(destinationName)
	if err == nil {
		if bytes.Equal(existing, mdshelfSkill) {
			return destination, true, nil
		}
		if !force {
			return destination, false, fmt.Errorf("%s exists with different content; use --force to replace it", destination)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return destination, false, fmt.Errorf("read installed skill: %w", err)
	}

	temporary, temporaryName, err := createSkillTemporaryFile(root)
	if err != nil {
		return destination, false, err
	}
	remove := true
	defer func() {
		_ = temporary.Close()
		if remove {
			_ = root.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o644); err != nil {
		return destination, false, fmt.Errorf("set skill permissions: %w", err)
	}
	if _, err := temporary.Write(mdshelfSkill); err != nil {
		return destination, false, fmt.Errorf("write skill: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return destination, false, fmt.Errorf("flush skill: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return destination, false, fmt.Errorf("close skill: %w", err)
	}
	if err := root.Rename(temporaryName, destinationName); err != nil {
		return destination, false, fmt.Errorf("replace skill: %w", err)
	}
	remove = false
	if parent, err := root.Open("mdshelf"); err == nil {
		_ = parent.Sync()
		_ = parent.Close()
	}
	return destination, false, nil
}

func createSkillTemporaryFile(root *os.Root) (*os.File, string, error) {
	for range 10 {
		var random [12]byte
		if _, err := io.ReadFull(rand.Reader, random[:]); err != nil {
			return nil, "", fmt.Errorf("create skill temporary name: %w", err)
		}
		name := "mdshelf/.SKILL-" + hex.EncodeToString(random[:]) + ".tmp"
		file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
		if err == nil {
			return file, name, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, "", fmt.Errorf("create skill temporary file: %w", err)
		}
	}
	return nil, "", errors.New("could not create a unique skill temporary file")
}
