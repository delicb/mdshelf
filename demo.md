# MDShelf feature demo

This document is part of the MDShelf binary. It works in ad hoc mode and daemon mode.

Use the table of contents to inspect each feature:

- [Markdown primer](#markdown-primer)
- [Text and headings](#text-and-headings)
- [Lists and tasks](#lists-and-tasks)
- [Definition lists](#definition-lists)
- [Quotes and rules](#quotes-and-rules)
- [Alerts and callouts](#alerts-and-callouts)
- [Tables](#tables)
- [Footnotes](#footnotes)
- [Links and images](#links-and-images)
- [Syntax highlighting](#syntax-highlighting)
- [Math notation](#math-notation)
- [Mermaid diagrams](#mermaid-diagrams)

---

## Markdown primer

MDShelf renders CommonMark and GitHub Flavored Markdown through Goldmark. It adds syntax highlighting, Mermaid diagrams, local navigation, and live updates.

| Feature | Markdown form | Result |
| :--- | :--- | :--- |
| Heading | `## Heading` | A heading with a route fragment |
| Emphasis | `*text*` or `_text_` | *Emphasized text* |
| Strong text | `**text**` | **Strong text** |
| Strikethrough | `~~text~~` | ~~Removed text~~ |
| Inline code | `` `value` `` | `value` |
| Link | `[label](https://example.com)` | [Example](https://example.com) |
| Automatic link | A plain web address | https://example.com/docs |
| Quote | `> text` | A block quote |
| Callout | `> [!NOTE]` | A labeled alert or note |
| Lists | `- item` or `1. item` | Ordered and unordered lists |
| Definition | `Term` followed by `: Meaning` | A term and its meaning |
| Task | `- [x] item` | A task list item |
| Table | Pipes and a header row | A scrollable table |
| Footnote | `text[^name]` and `[^name]: note` | A linked note at the end |
| Code block | A fenced block with a language | Highlighted source code |
| Math | `$...$` or `$$...$$` | Inline or display notation |
| Diagram | A fenced `mermaid` block | A rendered Mermaid diagram |
| Image | `![alt](image.png)` | A local or remote raster image |
| Rule | `---` | A thematic break |

Local images can use PNG, JPEG, GIF, or WebP files. Relative Markdown links open inside MDShelf.

## Text and headings

### Third-level heading

#### Fourth-level heading

##### Fifth-level heading

###### Sixth-level heading

A paragraph can contain *emphasis*, **strong text**, ***both styles***, ~~strikethrough~~, and `inline code`.

Reserved Markdown characters can be escaped: \*not emphasized\*, \# not a heading, and \[not a link\].

Unicode text works without extra settings: café, Ελληνικά, 日本語, العربية, and 🚀.

This line ends with two spaces.  
This text starts on the next line.

## Lists and tasks

- A top-level item
  - A nested item
    - A third-level item
  - Another nested item
- A final top-level item

1. Inspect the document.
2. Open the appearance settings.
3. Select a color theme.
   1. Select a syntax theme.
   2. Reload the page.
4. Confirm that both choices remain selected.

- [x] Render CommonMark
- [x] Render GitHub Flavored Markdown
- [x] Highlight source code
- [x] Render Mermaid diagrams
- [ ] Add more Markdown extensions

## Definition lists

MDShelf
: A local reader for Markdown documents.
: A daemon that can watch selected files.

Goldmark
: The Go parser that converts Markdown into HTML.

Live update
: A browser update that appears after a watched file changes.

Definition text can include **emphasis**, `inline code`, and [links](https://commonmark.org/).

## Quotes and rules

> MDShelf keeps Markdown reading local and direct.
>
> > Nested quotes also work.
>
> A quote can include **formatted text**, `code`, and lists.
>
> - First point
> - Second point

---

The rule above separates sections without extra HTML.

## Alerts and callouts

Use a supported marker on the first line of a block quote.

> [!NOTE]
> Notes add useful context without interrupting the main steps.

> [!TIP]
> Tips show an optional way to get a better result.

> [!IMPORTANT]
> Important details can affect whether a procedure succeeds.

> [!WARNING]
> Warnings identify a condition that can cause a problem.

> [!CAUTION]
> Cautions identify a condition that can cause data loss or another serious result.

Normal block quotes keep their standard appearance.

## Tables

| Component | Input | Output | Status |
| :--- | :---: | ---: | :---: |
| Goldmark | Markdown | HTML | Ready |
| Chroma | Source code | Token classes | Ready |
| Mermaid | Diagram text | SVG | Ready |
| File watcher | File events | Live update | Ready |

Long tables use a horizontal scroll area on narrow screens.

## Footnotes

A footnote keeps supporting details out of the main sentence.[^footnote-source] The same note can have more than one reference.[^footnote-source]

Footnotes can also contain more than one paragraph.[^footnote-detail]

[^footnote-source]: MDShelf uses Goldmark footnotes. Select the number to move between the reference and this note.

[^footnote-detail]: The first paragraph can contain **formatting**, links, and `inline code`.

    Indent the next paragraph to keep it in the same footnote.

## Links and images

MDShelf supports [normal web links](https://commonmark.org/), automatic links such as https://github.com, and heading links such as [Tables](#tables).

A regular document can use relative links and images:

```markdown
[Open another document](../guides/setup.md#getting-started)
![Architecture overview](images/architecture.png "Architecture overview")
```

MDShelf rewrites these paths so navigation and local raster images stay inside the server rules.

## Syntax highlighting

Select a syntax theme from the settings popup. MDShelf uses the language name after the opening fence.

### Go

```go
package main

import "fmt"

type Document struct {
    Title string
    Ready bool
}

func main() {
    document := Document{Title: "MDShelf", Ready: true}
    fmt.Printf("%s ready: %t\n", document.Title, document.Ready)
}
```

### TypeScript

```typescript
type Theme = "system" | "light" | "dark";

const settings: Readonly<{ theme: Theme; syntax: string }> = {
  theme: "system",
  syntax: "github-auto",
};

console.log(`${settings.theme}: ${settings.syntax}`);
```

### JSON

```json
{
  "name": "mdshelf",
  "features": ["markdown", "highlighting", "mermaid", "live updates"],
  "offline": true
}
```

### Shell

```bash
mdshelf add ./README.md
mdshelf list
mdshelf status
```

An indented block renders as plain code:

    no language tag
    no syntax colors

## Math notation

Inline math can use dollar delimiters, such as $E = mc^2$, or slash delimiters, such as \(a^2 + b^2 = c^2\).

Display math can use double-dollar delimiters:

$$
\int_0^1 x^2\,dx = \frac{1}{3}
$$

Slash delimiters also work for display math:

\[
\sum_{k=1}^{n} k = \frac{n(n+1)}{2}
\]

MDShelf bundles KaTeX, so math rendering works without a network connection.

## Mermaid diagrams

Mermaid uses the selected light or dark color theme.

### Flowchart

```mermaid
flowchart LR
    Markdown[Markdown file] --> Watcher[File watcher]
    Watcher --> Renderer[Goldmark renderer]
    Renderer --> Browser[MDShelf reader]
    Browser --> Settings{Theme setting}
    Settings -->|Light| Light[Light diagram]
    Settings -->|Dark| Dark[Dark diagram]
```

### Sequence diagram

```mermaid
sequenceDiagram
    participant U as User
    participant B as Browser
    participant S as MDShelf
    U->>B: Open document
    B->>S: GET /api/render
    S-->>B: Rendered Markdown
    S-->>B: File update
    B-->>U: Updated document
```

### State diagram

```mermaid
stateDiagram-v2
    [*] --> Loading
    Loading --> Ready: render succeeds
    Loading --> Error: render fails
    Ready --> Loading: file changes
    Error --> Loading: retry
```

---

## End of demo

Open the settings popup to compare color themes and syntax themes. Return to a document from the sidebar when finished.
