---
name: mdshelf
description: Publish Markdown files for local review, give users MDShelf links, read comments, and address MDShelf feedback.
---

# MDShelf review flow

Use MDShelf when a user wants to review a Markdown file in the local MDShelf interface.

## Publish a document

1. Save the Markdown file in its expected project path.
2. Use an absolute path for each MDShelf command.
3. Run `mdshelf add --json <absolute-path>`.
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

## Safety rules

- Do not edit `reviews.json`.
- Do not create review sidecar files in the project.
- Do not use browser automation to read comments.
- If the daemon does not support `reviews-v1`, ask the user to restart MDShelf.
