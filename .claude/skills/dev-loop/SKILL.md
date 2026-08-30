---
name: dev-loop
description: Use when building, testing, or manually verifying mdshelf changes - the CI check list, ad hoc serving, and safe daemon or review testing.
---

# MDShelf development loop

## Checks

Run the same checks as CI before you push:

```sh
gofmt -l .                     # must print nothing
go vet ./...
go test -race -count=1 ./...
go mod tidy && git diff --exit-code -- go.mod go.sum
node --check web/text-selection.js
node --check web/app.js
node --test web/app.test.cjs web/text-selection.test.cjs
```

Pass the frontend test files to `node --test` by name. A bare `node --test web/` does not find the `.cjs` tests on Node 22.

Frontend changes need no build step. `web/` is embedded with `go:embed`, so rebuild the Go binary to pick them up.

## Ad hoc mode against a scratch folder

```sh
go build -o /tmp/mdshelf .
mkdir -p /tmp/mdshelf-scratch && cp demo.md demo.bib /tmp/mdshelf-scratch/
/tmp/mdshelf -port 7451 /tmp/mdshelf-scratch
```

Open `http://localhost:7451`. Ad hoc mode is read-only for comments; the embedded Demo document (`#/__mdshelf_demo__`) accepts in-memory comments until the page reloads.

## Daemon and review flow

The daemon state dir is `os.UserConfigDir()` plus `mdshelf` (`daemonStateDir` in `daemon_registry.go`). There is no mdshelf-specific flag or environment variable to override it. On Linux, `os.UserConfigDir` honors `XDG_CONFIG_HOME`, and the daemon inherits the environment of the `mdshelf add` that spawns it, so pointing `XDG_CONFIG_HOME` at a temp dir isolates daemon state for a test run. On macOS and Windows there is no such override; the daemon uses the real user config dir.

When using the real state dir:

- A personal daemon may already run on port 7332. Check with `mdshelf status`.
- Exercise the flow with `mdshelf add --json <file>`, comment in the browser, then `mdshelf review show --json <file>` and `mdshelf review address --message <text> <comment-id>`.
- Clean up with `mdshelf remove <file>` and `mdshelf stop`. Comment state stays in `reviews.json` until you delete it.
- Stop the daemon before changing its `config.json`.

Go tests never touch real daemon state; they use `t.TempDir()`.
