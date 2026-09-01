# Field Guide

This guide exercises navigation: headings, paragraphs, and code blocks.

## Getting started

Install the binary and point it at a folder of Markdown files.

```sh
mdshelf -port 7331 ~/notes
```

The server prints the local address when it is ready.

## Configuration

MDShelf stores reading preferences in the browser, so the server needs no
configuration file at all.

Design, appearance, and syntax theme are all chosen from the settings popup.

## Troubleshooting

If the document list is empty, check that the folder actually contains
Markdown files with an `.md` or `.markdown` extension.

Restart the server after moving the folder somewhere else.
