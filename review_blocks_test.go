package main

import (
	"strings"
	"testing"
)

func TestReviewBlocksTrackSourceLinesAndWrappers(t *testing.T) {
	source := []byte("# Storage\n\nKeep state.\n\n- first\n- second\n")
	rendered, err := renderMarkdown(newMarkdownRenderer(), source, "plan.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rendered.sourceHash != sourceHash(source) {
		t.Fatalf("source hash = %q", rendered.sourceHash)
	}
	if len(rendered.blocks) != 3 {
		t.Fatalf("blocks = %#v", rendered.blocks)
	}
	want := []struct {
		kind       string
		start, end int
	}{
		{kind: "heading", start: 1, end: 1},
		{kind: "paragraph", start: 3, end: 3},
		{kind: "list", start: 5, end: 6},
	}
	for index, expected := range want {
		block := rendered.blocks[index]
		if block.Kind != expected.kind || block.StartLine != expected.start || block.EndLine != expected.end {
			t.Errorf("block %d = %#v, want kind %q lines %d-%d", index, block, expected.kind, expected.start, expected.end)
		}
		if !strings.Contains(rendered.html, `data-md-block="`+block.Key+`"`) {
			t.Errorf("HTML has no wrapper for block %q: %s", block.Key, rendered.html)
		}
	}
	if got := rendered.blocks[1].HeadingPath; len(got) != 1 || got[0] != "Storage" {
		t.Fatalf("paragraph heading path = %#v", got)
	}
}

func TestReviewBlocksWrapSupportedBlockKinds(t *testing.T) {
	source := []byte(`# Blocks

Paragraph.

- one
- two

> quote

~~~go
fmt.Println("code")
~~~

~~~mermaid
graph TD
~~~

$$
x + y
$$

| A | B |
| --- | --- |
| 1 | 2 |

> [!NOTE]
> Keep this.

---
`)
	rendered, err := renderMarkdown(newMarkdownRenderer(), source, "blocks.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	kinds := make(map[string]bool)
	for _, block := range rendered.blocks {
		kinds[block.Kind] = true
	}
	for _, kind := range []string{"heading", "paragraph", "list", "block_quote", "code", "mermaid", "math", "table", "callout", "horizontal_rule"} {
		if !kinds[kind] {
			t.Errorf("missing wrapped kind %q in %#v", kind, rendered.blocks)
		}
	}
	lineRanges := map[string][2]int{
		"heading": {1, 1}, "paragraph": {3, 3}, "list": {5, 6}, "block_quote": {8, 8},
		"code": {10, 12}, "mermaid": {14, 16}, "math": {18, 20}, "table": {22, 24},
		"callout": {26, 27}, "horizontal_rule": {29, 29},
	}
	for _, block := range rendered.blocks {
		want := lineRanges[block.Kind]
		if block.StartLine != want[0] || block.EndLine != want[1] {
			t.Errorf("%s lines = %d-%d, want %d-%d", block.Kind, block.StartLine, block.EndLine, want[0], want[1])
		}
	}
	if got := strings.Count(rendered.html, `class="md-block"`); got != len(rendered.blocks) {
		t.Fatalf("wrapper count = %d, blocks = %d", got, len(rendered.blocks))
	}
}

func TestReviewBlocksDoNotWrapGeneratedBibliography(t *testing.T) {
	rendered, err := renderDemo(newMarkdownRenderer())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.html, `"><section class="bibliography"`) {
		t.Fatalf("generated bibliography got a review wrapper: %s", rendered.html)
	}
	for _, block := range rendered.blocks {
		if block.Kind == strings.ToLower(kindBibliography.String()) {
			t.Fatalf("generated bibliography block = %#v", block)
		}
	}

	withoutBibliography, err := renderMarkdown(newMarkdownRenderer(), demoMarkdown, "demo.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutBibliography.blocks) != len(rendered.blocks) {
		t.Fatalf("block counts with and without bibliography = %d and %d", len(rendered.blocks), len(withoutBibliography.blocks))
	}
	for index := range rendered.blocks {
		if rendered.blocks[index].Key != withoutBibliography.blocks[index].Key {
			t.Fatalf("block %d key with and without bibliography = %q and %q", index, rendered.blocks[index].Key, withoutBibliography.blocks[index].Key)
		}
	}
}

func TestReviewBlocksDoNotWrapRemovedRawHTML(t *testing.T) {
	rendered, err := renderMarkdown(newMarkdownRenderer(), []byte("<script>hostile()</script>\n"), "raw.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered.blocks) != 0 || strings.Contains(rendered.html, "md-block") {
		t.Fatalf("raw HTML got a review wrapper: blocks=%#v HTML=%s", rendered.blocks, rendered.html)
	}
	if !strings.Contains(rendered.html, "raw HTML omitted") {
		t.Fatalf("safe renderer did not remove raw HTML: %s", rendered.html)
	}
}

func TestReviewBlockLinesHandleUnicodeCRLFAndFrontMatter(t *testing.T) {
	source := []byte("---\r\ntitle: Café\r\n---\r\n# Héading\r\n\r\n日本語 text.\r\n")
	rendered, err := renderMarkdown(newMarkdownRenderer(), source, "unicode.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered.blocks) != 2 {
		t.Fatalf("blocks = %#v", rendered.blocks)
	}
	if rendered.blocks[0].StartLine != 4 || rendered.blocks[0].EndLine != 4 {
		t.Errorf("heading lines = %d-%d", rendered.blocks[0].StartLine, rendered.blocks[0].EndLine)
	}
	if rendered.blocks[1].StartLine != 6 || rendered.blocks[1].EndLine != 6 {
		t.Errorf("paragraph lines = %d-%d", rendered.blocks[1].StartLine, rendered.blocks[1].EndLine)
	}
}

func TestReviewBlockKeysDistinguishEqualBlocks(t *testing.T) {
	rendered, err := renderMarkdown(newMarkdownRenderer(), []byte("# Same\n\nRepeat.\n\nRepeat.\n"), "same.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered.blocks) != 3 {
		t.Fatalf("blocks = %#v", rendered.blocks)
	}
	first, second := rendered.blocks[1], rendered.blocks[2]
	if first.BlockHash != second.BlockHash || first.Key == second.Key {
		t.Fatalf("equal blocks have hash/key %#v and %#v", first, second)
	}
}

func TestReviewAnchorRematchesOneMovedBlockAndReturnsCurrentKey(t *testing.T) {
	before := mustRenderReviewBlocks(t, "# First\n\nMove me.\n\n# Second\n")
	after := mustRenderReviewBlocks(t, "# First\n\n# Second\n\nMove me.\n")
	anchor := anchorForBlock(before.blocks[1])
	location, currentKey, outdated := matchBlockAnchor(before.sourceHash, after.sourceHash, anchor, after.blocks)
	if outdated || location == nil || location.StartLine != 5 || location.EndLine != 5 {
		t.Fatalf("match = %#v, key=%v, outdated=%v", location, currentKey, outdated)
	}
	if currentKey == nil || *currentKey != after.blocks[2].Key || *currentKey == anchor.BlockKey {
		t.Fatalf("current block key = %v, old = %q, new = %q", currentKey, anchor.BlockKey, after.blocks[2].Key)
	}
}

func TestReviewAnchorMarksChangedAndDuplicateBlocksOutdated(t *testing.T) {
	before := mustRenderReviewBlocks(t, "# Heading\n\nOriginal.\n")
	anchor := anchorForBlock(before.blocks[1])
	for _, source := range []string{
		"# Heading\n\nChanged.\n",
		"# One\n\nOriginal.\n\n# Two\n\nOriginal.\n",
	} {
		after := mustRenderReviewBlocks(t, source)
		location, currentKey, outdated := matchBlockAnchor(before.sourceHash, after.sourceHash, anchor, after.blocks)
		if !outdated || location != nil || currentKey != nil {
			t.Fatalf("source %q match = %#v, key=%v, outdated=%v", source, location, currentKey, outdated)
		}
	}
}

func TestWholeDocumentCommentNeverBecomesOutdated(t *testing.T) {
	location, currentKey, outdated := matchBlockAnchor(strings.Repeat("a", 64), strings.Repeat("b", 64), nil, nil)
	if location != nil || currentKey != nil || outdated {
		t.Fatalf("whole document match = %#v, key=%v, outdated=%v", location, currentKey, outdated)
	}
}

func TestReviewWrappersKeepHeadingIDsLinksAndBlockChanges(t *testing.T) {
	before := mustRenderReviewBlocks(t, "# Linked heading\n\n[Link](https://example.com)\n")
	after := mustRenderReviewBlocks(t, "# Linked heading\n\nChanged [link](https://example.com).\n")
	for _, fragment := range []string{`<h1 id="linked-heading">`, `href="https://example.com"`} {
		if !strings.Contains(before.html, fragment) {
			t.Errorf("HTML does not contain %q: %s", fragment, before.html)
		}
	}
	if before.blocks[0].Key != after.blocks[0].Key {
		t.Error("unchanged heading block key changed")
	}
	if before.blocks[1].Key == after.blocks[1].Key {
		t.Error("changed paragraph block key did not change")
	}
}

func TestReviewTextRangeRelocatesOnlyAsOneConsecutiveGroup(t *testing.T) {
	before := mustRenderReviewBlocks(t, "# Group\n\nFirst.\n\nSecond.\n\n# Tail\n")
	anchors := []blockAnchor{*anchorForBlock(before.blocks[1]), *anchorForBlock(before.blocks[2])}

	after := mustRenderReviewBlocks(t, "# Group\n\nBefore.\n\nFirst.\n\nSecond.\n\n# Tail\n")
	location, currentKey, currentKeys, outdated := matchTextRangeAnchors(before.sourceHash, after.sourceHash, anchors, after.blocks)
	if outdated || location == nil || currentKey == nil || len(currentKeys) != 2 {
		t.Fatalf("group match = %#v, key=%v, keys=%v, outdated=%v", location, currentKey, currentKeys, outdated)
	}
	if location.StartLine != after.blocks[2].StartLine || location.EndLine != after.blocks[3].EndLine {
		t.Fatalf("group location = %#v", location)
	}

	for name, source := range map[string]string{
		"changed":   "# Group\n\nFirst changed.\n\nSecond.\n\n# Tail\n",
		"missing":   "# Group\n\nFirst.\n\n# Tail\n",
		"duplicate": "# Group\n\nFirst.\n\nSecond.\n\nFirst.\n\n# Tail\n",
		"reordered": "# Group\n\nSecond.\n\nFirst.\n\n# Tail\n",
		"inserted":  "# Group\n\nFirst.\n\nBetween.\n\nSecond.\n\n# Tail\n",
	} {
		t.Run(name, func(t *testing.T) {
			candidate := mustRenderReviewBlocks(t, source)
			location, currentKey, currentKeys, outdated := matchTextRangeAnchors(
				before.sourceHash, candidate.sourceHash, anchors, candidate.blocks,
			)
			if !outdated || location != nil || currentKey != nil || currentKeys != nil {
				t.Fatalf("match = %#v, key=%v, keys=%v, outdated=%v", location, currentKey, currentKeys, outdated)
			}
		})
	}
}

func mustRenderReviewBlocks(t *testing.T, source string) renderedMarkdown {
	t.Helper()
	rendered, err := renderMarkdown(newMarkdownRenderer(), []byte(source), "review.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	return rendered
}
