---
name: mdshelf
description: Use when asked to publish a Markdown file for review in MDShelf, to give a user an MDShelf link, or to read and address MDShelf review comments.
---

# MDShelf review flow

Use MDShelf when a user wants to review a Markdown file in the local MDShelf interface.

## Publish a document

1. Save the Markdown file in its expected project path.
2. Use an absolute path for each MDShelf command.
3. Run `mdshelf add --json <absolute-path>`. The command starts the daemon automatically when it is not running.
4. Give the returned document URL to the user.
5. Wait until the user says that they finished commenting.

Do not poll the daemon while the user reviews the document.

## Read feedback

1. Run `mdshelf review show --json <absolute-path>`.
2. Read only comments with the `open` or `addressed` status.
3. For each outdated comment, compare the original quote with the current file.
4. If the intent of an outdated comment is not clear, ask the user before you change the file.
5. Update the Markdown file or answer the reviewer question.
6. Complete the work before you mark the comment as addressed.
7. Run `mdshelf review address --message <summary> <comment-id>` to append an agent reply.
8. Run `mdshelf review show --json <absolute-path>` again.
9. Report the changed sections and all comments that remain open.

Do not resolve comments. That action belongs to the reviewer.

`mdshelf review show --json` writes one object with this shape:

```json
{
  "schemaVersion": 1,
  "document": {
    "id": "af64912fc0c447f60b3788d1",
    "path": "/path/to/notes/plan.md",
    "title": "Storage plan",
    "url": "http://localhost:7332/#/af64912fc0c447f60b3788d1",
    "sourceHash": "d35e98e3d521e9131953412e3bc8733d66670962c2fe1a1c5ba1303c376d098b",
    "reviewStatus": "comments"
  },
  "comments": [
    {
      "id": "comment_39f841d48fa9934402c1108f",
      "body": "Please expand the introduction.",
      "status": "open",
      "baseHash": "d35e98e3d521e9131953412e3bc8733d66670962c2fe1a1c5ba1303c376d098b",
      "outdated": false,
      "anchor": {
        "blockKey": "6628bd9a6d07dde340e5b218",
        "kind": "heading",
        "startLine": 12,
        "endLine": 12,
        "headingPath": ["Storage plan"],
        "quote": "# Storage plan"
      },
      "currentLocation": { "startLine": 12, "endLine": 12 },
      "currentBlockKey": "6628bd9a6d07dde340e5b218",
      "replies": [
        {
          "id": "reply_2ea2fbc79e7d9df817eae748",
          "body": "Added the missing sentence.",
          "author": "agent",
          "createdAt": "2026-08-30T22:05:52Z"
        }
      ]
    }
  ]
}
```

A comment on selected text also carries a `textRange` object with the selected `quote`. `outdated` is `true` when the anchored block no longer matches the current file content; `currentLocation` and `currentBlockKey` are then `null`.

## Safety rules

- Do not edit `reviews.json`.
- Do not create review sidecar files in the project.
- Do not use browser automation to read comments.
- If the daemon does not support `reviews-v1`, ask the user to restart MDShelf.
