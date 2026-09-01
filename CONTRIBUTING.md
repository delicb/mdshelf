# Contributing

## Set up

Install Go and Node.js 22 or newer. CI and release binaries use Go 1.27; the source builds with Go 1.25 or newer. Then build:

```sh
go build -o mdshelf .
```

The binary embeds the `web/` frontend, so there is no separate frontend build step.

## Checks

CI runs these checks. Run them before you push:

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

## Commit style

Use conventional commits, as in the history: `feat: ...`, `fix: ...`, `docs: ...`, `ci: ...`, with an optional scope such as `feat(review): ...`.

## Where tests live

Go tests sit beside the code as `*_test.go` files in the repository root. Frontend tests are `web/app.test.cjs` and `web/text-selection.test.cjs`.

## Agent-assisted development

[CLAUDE.md](CLAUDE.md) holds the build commands and repository gotchas for coding agents. The repository also ships agent skills in `.claude/skills/` and an embedded review skill in `skills/mdshelf/SKILL.md`.
