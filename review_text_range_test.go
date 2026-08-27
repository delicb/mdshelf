package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReviewStoreMigratesVersionOne(t *testing.T) {
	store := newTestReviewStore(t)
	context := newTestReviewContext(t, "migration.md", "# Migration\n")
	if _, _, err := store.addComment(context, 0, context.SourceHash, "Keep this.", ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"version": 2`), []byte(`"version": 1`), 1)
	if err := os.WriteFile(store.path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	migrated, err := loadReviewStoreFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.Version != reviewStoreVersion || migrated.Documents[0].Comments[0].TextRange != nil {
		t.Fatalf("migrated store = %#v", migrated)
	}
}

func TestReviewStoreVersionTwoStrictDecode(t *testing.T) {
	for name, data := range map[string]string{
		"unknown field":     `{"version":2,"documents":[],"unknown":true}`,
		"version one range": `{"version":1,"documents":[{"documentId":"000000000000000000000000","path":"/tmp/a.md","revision":0,"comments":[{"textRange":{}}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "reviews.json")
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadReviewStoreFile(path); err == nil {
				t.Fatal("invalid store loaded")
			}
		})
	}
}

func TestReviewTextRangeCloneIsDeep(t *testing.T) {
	context := newTestReviewContext(t, "clone.md", "# Clone\n\nText.\n")
	selection := testSelection(context.Blocks, 0, 1)
	_, textRange, err := commentRangeForRequest(context.Blocks, "", selection)
	if err != nil {
		t.Fatal(err)
	}
	comment := reviewComment{Anchor: anchorForBlock(context.Blocks[0]), TextRange: textRange}
	clone := cloneReviewComment(comment)
	clone.Anchor.HeadingPath = append(clone.Anchor.HeadingPath, "changed")
	clone.TextRange.Anchors[0].HeadingPath = append(clone.TextRange.Anchors[0].HeadingPath, "changed")
	clone.TextRange.Anchors[0].Quote = "changed"
	if reflect.DeepEqual(comment, clone) || comment.TextRange.Anchors[0].Quote == "changed" {
		t.Fatalf("clone shares range state: original=%#v clone=%#v", comment, clone)
	}
}

func TestReviewCommentAddModes(t *testing.T) {
	context := newTestReviewContext(t, "modes.md", "# Modes\n\nText.\n")
	store := newTestReviewStore(t)
	document, wholeDocument, err := store.addCommentWithSelection(context, 0, context.SourceHash, "Document", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	document, wholeBlock, err := store.addCommentWithSelection(
		context, document.Revision, context.SourceHash, "Block", context.Blocks[0].Key, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	document, selected, err := store.addCommentWithSelection(
		context, document.Revision, context.SourceHash, "Selection", "", testSelection(context.Blocks, 0, 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	if wholeDocument.Anchor != nil || wholeDocument.TextRange != nil {
		t.Fatalf("whole document comment = %#v", wholeDocument)
	}
	if wholeBlock.Anchor == nil || wholeBlock.TextRange != nil {
		t.Fatalf("whole block comment = %#v", wholeBlock)
	}
	if selected.Anchor == nil || selected.TextRange == nil || len(selected.TextRange.Anchors) != 2 {
		t.Fatalf("selected comment = %#v", selected)
	}
	if !blockAnchorsEqual(*selected.Anchor, selected.TextRange.Anchors[0]) {
		t.Fatal("compatibility anchor differs from first range anchor")
	}
	if document.Revision != 3 {
		t.Fatalf("revision = %d", document.Revision)
	}

	_, _, err = store.addCommentWithSelection(
		context, document.Revision, context.SourceHash, "Both", context.Blocks[0].Key, testSelection(context.Blocks, 0, 0),
	)
	if err == nil {
		t.Fatal("block key and selection succeeded")
	}
}

func TestReviewSelectionOffsetValidation(t *testing.T) {
	tests := []struct {
		name        string
		start, end  int64
		anchorCount int
		wantError   bool
	}{
		{name: "zero start", start: 0, end: 1, anchorCount: 1},
		{name: "zero end", start: 0, end: 0, anchorCount: 1, wantError: true},
		{name: "negative start", start: -1, end: 1, anchorCount: 1, wantError: true},
		{name: "negative end", start: 0, end: -1, anchorCount: 1, wantError: true},
		{name: "excessive start", start: maxReviewTextOffset + 1, end: 1, anchorCount: 1, wantError: true},
		{name: "excessive end", start: 0, end: maxReviewTextOffset + 1, anchorCount: 1, wantError: true},
		{name: "equal same block", start: 2, end: 2, anchorCount: 1, wantError: true},
		{name: "reversed same block", start: 3, end: 2, anchorCount: 1, wantError: true},
		{name: "cross block offsets", start: 20, end: 2, anchorCount: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateReviewRangeOffsets(test.start, test.end, test.anchorCount)
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, wantError=%v", err, test.wantError)
			}
		})
	}
}

func TestReviewSelectionBlockValidation(t *testing.T) {
	context := newTestReviewContext(t, "keys.md", testParagraphs(17))
	valid16 := testSelection(context.Blocks, 0, 15)
	if _, _, err := commentRangeForRequest(context.Blocks, "", valid16); err != nil {
		t.Fatalf("16 blocks: %v", err)
	}

	tests := map[string]*reviewSelectionRequest{
		"zero keys":      {Version: 1, BlockKeys: []string{}, StartOffset: 0, EndOffset: 1, Quote: "text"},
		"seventeen keys": testSelection(context.Blocks, 0, 16),
		"duplicate keys": selectionWithKeys(context.Blocks[0].Key, context.Blocks[0].Key),
		"reversed keys":  selectionWithKeys(context.Blocks[1].Key, context.Blocks[0].Key),
		"unknown key":    selectionWithKeys(strings.Repeat("f", 24)),
		"nonconsecutive": selectionWithKeys(context.Blocks[0].Key, context.Blocks[2].Key),
	}
	for name, selection := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := commentRangeForRequest(context.Blocks, "", selection); err == nil {
				t.Fatal("invalid selection succeeded")
			}
		})
	}

	if _, _, err := commentRangeForRequest(context.Blocks, "", testSelection(context.Blocks, 0, 0)); err != nil {
		t.Fatalf("one block: %v", err)
	}
}

func TestReviewSelectionQuoteValidation(t *testing.T) {
	context := newTestReviewContext(t, "quotes.md", "Text.\n")
	exact := strings.Repeat("é", maxReviewTextBytes/2)
	selection := testSelection(context.Blocks, 0, 0)
	selection.Quote = exact
	if _, _, err := commentRangeForRequest(context.Blocks, "", selection); err != nil {
		t.Fatalf("exact byte limit: %v", err)
	}

	for name, quote := range map[string]string{
		"empty":         "",
		"white space":   "  \n",
		"NUL":           "a\x00b",
		"invalid UTF-8": string([]byte{0xff}),
		"over limit":    exact + "é",
	} {
		t.Run(name, func(t *testing.T) {
			candidate := *selection
			candidate.Quote = quote
			if _, _, err := commentRangeForRequest(context.Blocks, "", &candidate); err == nil {
				t.Fatal("invalid quote succeeded")
			}
		})
	}
}

func TestReviewTextRangePersistence(t *testing.T) {
	context := newTestReviewContext(t, "persist-range.md", "First.\n\nSecond.\n")
	store := newTestReviewStore(t)
	_, comment, err := store.addCommentWithSelection(
		context, 0, context.SourceHash, "Keep range", "", testSelection(context.Blocks, 0, 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := newReviewStore(store.path)
	if err != nil {
		t.Fatal(err)
	}
	document, found := reloaded.snapshot(context.DocumentID, context.Path)
	if !found || len(document.Comments) != 1 || !reflect.DeepEqual(document.Comments[0], comment) {
		t.Fatalf("reloaded range = %#v, found=%v", document, found)
	}
}

func TestReviewStoreBudgetRejectsLargeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reviews.json")
	if err := os.WriteFile(path, make([]byte, maxReviewStoreBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadReviewStoreFile(path); err == nil {
		t.Fatal("oversize review store loaded")
	}
}

func TestReviewTextRangeValidationRequiresCompatibilityCopy(t *testing.T) {
	context := newTestReviewContext(t, "validation.md", "Text.\n")
	anchor, textRange, err := commentRangeForRequest(context.Blocks, "", testSelection(context.Blocks, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateReviewTextRange(textRange, anchor); err != nil {
		t.Fatal(err)
	}
	anchor.Quote = "different"
	if err := validateReviewTextRange(textRange, anchor); err == nil {
		t.Fatal("mismatched compatibility anchor passed validation")
	}
}

func TestReviewTextRangeValidationRejectsOverlappingAnchors(t *testing.T) {
	context := newTestReviewContext(t, "overlap.md", "First.\n\nSecond.\n")
	anchor, textRange, err := commentRangeForRequest(context.Blocks, "", testSelection(context.Blocks, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	textRange.Anchors[0].EndLine = textRange.Anchors[1].StartLine
	*anchor = cloneBlockAnchor(textRange.Anchors[0])
	if err := validateReviewTextRange(textRange, anchor); err == nil {
		t.Fatal("overlapping text range anchors passed validation")
	}
}

func TestReviewTextRangeJSONRoundTrip(t *testing.T) {
	context := newTestReviewContext(t, "roundtrip.md", "First.\n\nSecond.\n")
	_, textRange, err := commentRangeForRequest(context.Blocks, "", testSelection(context.Blocks, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(textRange)
	if err != nil {
		t.Fatal(err)
	}
	var decoded reviewTextRange
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Version != textRange.Version || decoded.StartOffset != textRange.StartOffset ||
		decoded.EndOffset != textRange.EndOffset || decoded.Quote != textRange.Quote ||
		len(decoded.Anchors) != len(textRange.Anchors) {
		t.Fatalf("decoded = %#v, want %#v", decoded, *textRange)
	}
	for index := range decoded.Anchors {
		if !blockAnchorsEqual(decoded.Anchors[index], textRange.Anchors[index]) {
			t.Fatalf("decoded anchor %d = %#v, want %#v", index, decoded.Anchors[index], textRange.Anchors[index])
		}
	}
}

func testSelection(blocks []markdownBlock, first, last int) *reviewSelectionRequest {
	keys := make([]string, last-first+1)
	for index := range keys {
		keys[index] = blocks[first+index].Key
	}
	return &reviewSelectionRequest{
		Version: reviewTextRangeVersion, BlockKeys: keys,
		StartOffset: 0, EndOffset: 1, Quote: "selected text",
	}
}

func selectionWithKeys(keys ...string) *reviewSelectionRequest {
	return &reviewSelectionRequest{
		Version: reviewTextRangeVersion, BlockKeys: keys,
		StartOffset: 0, EndOffset: 1, Quote: "selected text",
	}
}

func testParagraphs(count int) string {
	var source strings.Builder
	for index := range count {
		if index > 0 {
			source.WriteString("\n")
		}
		source.WriteString("Text.\n")
	}
	return source.String()
}
