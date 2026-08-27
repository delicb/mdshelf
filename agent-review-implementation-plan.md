# MDShelf comment review implementation plan

## Goal

Add a simple local comment flow for Markdown documents in daemon mode.

A reviewer can:

1. Open a document URL.
2. Add a comment to a rendered section.
3. See existing comments beside their sections.
4. Select a comment to highlight its section.
5. Read, reply to, resolve, or reopen comments in the Comments panel.

Saving a comment publishes it at once. There are no draft comments, comment types, review decisions, summaries, approvals, or review submission steps.

An agent can:

1. Add a Markdown file with `mdshelf add --json`.
2. Read comments with `mdshelf review show --json`.
3. Change the source document.
4. Reply with `mdshelf review address --message`.

## Scope

The feature works only in daemon mode on `127.0.0.1:7332`.

MDShelf stores comments in the daemon state folder. It does not write files beside the Markdown document. Removing a document from MDShelf does not remove its comments.

Ad hoc mode stays read-only and does not show comment controls.

## Stored data

Store one row per document:

```json
{
  "documentId": "24 lowercase hexadecimal characters",
  "path": "/absolute/path/to/file.md",
  "revision": 3,
  "comments": []
}
```

Each comment has:

- A stable opaque ID.
- A body.
- An `open`, `addressed`, or `resolved` status.
- The source hash from creation time.
- An optional rendered-block anchor.
- A one-level list of reviewer and agent replies.
- Creation, update, address, and resolution times.

There is one comment form. Do not store a comment kind.

Write `reviews.json` atomically with file mode `0600`. Create its parent folder with mode `0700` where the operating system supports these modes.

The current development format has no migration code. Reject unknown fields and invalid state.

## Concurrency

Each document comment row has a revision.

Browser writes include:

- The opaque document ID.
- The expected comment revision.
- The expected current source hash.

Reject a write when the revision or source hash changed. Hold the daemon document lock while the comment store commits a new comment. This prevents a file update or removal from racing a comment save.

A delayed browser request must not change the interface for a different open document.

## Anchors

The Markdown renderer returns safe metadata for each top-level rendered block:

- Opaque block key.
- Block kind.
- Start line.
- End line.

The browser never sends source text as an anchor.

The comment store records the current block metadata and a short quote. When the document changes, match the stored anchor to the current rendered blocks. Mark a comment as outdated when no safe match exists.

## HTTP API

Browser endpoints:

```text
GET  /api/review?path=<document-id>&includeResolved=<bool>
POST /api/control/review/comments/add
POST /api/control/review/comments/reply
POST /api/control/review/comments/resolve
POST /api/control/review/comments/reopen
```

Agent endpoints:

```text
POST /api/control/review/show
POST /api/control/review/comments/address
```

Saving through `comments/add` creates an `open` comment. It does not create a draft or require a second submit request.

The file-list response includes:

```json
{
  "reviewStatus": "comments",
  "openComments": 2
}
```

Status values are:

- `needs_review`: The document has no comments.
- `comments`: The document has comments on its current content.
- `updated`: The document changed after the latest comment.
- `removed`: The registered file is not available.

The daemon health response includes `reviews-v1`.

## Browser interface

Each rendered block has a small `+` control outside its content.

- Show the control on hover or keyboard focus.
- Use a two-step section tap on touch devices.
- Do not trigger it from links or other controls.
- `Cmd+Enter` saves on macOS.
- `Ctrl+Enter` saves on other systems.
- Keep Save and Cancel compact.
- Show the comment form in the same rail and style as existing comment bubbles.

A saved comment appears at once in a floating section bubble.

- Keep the count pill and comment bubbles visible.
- Dim bubbles until selected.
- Brighten the selected bubble.
- Add a light highlight to its source section.
- Clear the selection when the user selects another part of the page.
- Keep the document text width unchanged when the Comments panel opens.
- Reserve enough horizontal or vertical space so controls and bubbles do not cover document text or each other.
- Put bubbles below their sections on narrow screens.

The Comments panel lists existing comment threads.

- Do not add root comments from the panel.
- Let the reviewer reply, resolve, or reopen each comment.
- Keep replies one level deep.
- Do not show review decisions, summaries, approvals, draft controls, copy controls, or a review submit form.
- Selecting a panel item highlights and scrolls to its section.
- On a narrow screen, close the panel after selection so the highlighted section is visible.

Opening Settings closes the Comments panel.

## CLI

`mdshelf add --json <path>` returns one JSON object with the document ID, path, title, URL, and state.

`mdshelf review show [--json] [--include-resolved] <path>` returns current comments. It does not return drafts or submitted review records.

`mdshelf review address [--json] --message <text> <comment-id>` appends an agent reply and marks the comment as addressed.

## Agent skill

Use `skills/mdshelf/SKILL.md` as the only skill source.

The skill must tell an agent to:

1. Add the document with `mdshelf add --json`.
2. Give the URL to the user.
3. Wait until the user finishes commenting without polling.
4. Read comments through the CLI.
5. Complete the requested work.
6. Address each completed comment through the CLI.
7. Read comments again and report what remains.

Do not use browser automation or edit `reviews.json` directly.

## Verification

Automated checks must cover:

- Atomic store load and save.
- Invalid state rejection.
- Immediate comment publication.
- Source and revision conflicts.
- File update and removal races during Save.
- Reviewer and agent replies.
- Address, resolve, and reopen transitions.
- Anchor matching after document changes.
- Daemon file-list comment counts.
- CLI JSON and Markdown output.
- Agent skill print and install.
- Frontend helper logic.
- Go race tests, vet, JavaScript tests, and cross-platform builds.

Browser checks must cover desktop and phone widths:

- The `+` does not cover content.
- `Cmd+Enter` and `Ctrl+Enter` publish.
- Saving does not open the Comments panel.
- Count pills and bubbles stay visible.
- Bubbles are dim until selected.
- Selecting a bubble highlights its section.
- The Comments panel contains no root-comment or review-submit controls.
- Reply, Resolve, and Reopen work in section bubbles and the Comments panel.
- Selecting a panel item highlights its section.
- Settings opens above the document after it closes Comments.
- No browser errors occur.
