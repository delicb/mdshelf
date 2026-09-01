# CLAUDE.md

MDShelf is a Go web server that serves the Markdown files in a folder as a small, phone-friendly website, with a daemon mode that publishes single files and carries a review-comment flow for agents. The frontend lives in `web/` and is embedded in the binary.

## Build and test

Run the same checks as CI:

```sh
gofmt -l .                     # must print nothing
go vet ./...
go test -race -count=1 ./...
go mod tidy && git diff --exit-code -- go.mod go.sum
node --check web/text-selection.js
node --check web/app.js
node --test web/app.test.cjs web/text-selection.test.cjs
```

Build the binary with `go build -o mdshelf .`.

## Gotchas

- CI and release binaries use Go 1.27. The go.mod language requirement is 1.25, so a local sandbox with Go 1.25 or newer can build and test the source.
- Pass the frontend test files to `node --test` by name. A bare `node --test web/` does not find the `.cjs` tests on Node 22.
- macOS builds need cgo, which Go enables by default, for FSEvents.
- `add`, `list`, `remove`, `status`, and `stop` are reserved as the first CLI argument. Use `./add` to serve a folder named `add` in ad hoc mode.
- `web/` assets are embedded with `go:embed`. There is no separate frontend build step; rebuilding the Go binary picks up frontend changes.
- Review and comment state lives in `reviews.json` in the `mdshelf` folder under the user config dir: `~/Library/Application Support/mdshelf` on macOS, `~/.config/mdshelf` on Linux. The daemon also keeps `registry.json`, `config.json`, and `daemon.log` there.
- Stop the daemon with `mdshelf stop` before changing its `config.json`.
