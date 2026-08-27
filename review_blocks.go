package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	extensionast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

const maxAnchorQuoteBytes = 2 << 10

var kindReviewBlock = ast.NewNodeKind("ReviewBlock")

type reviewBlock struct {
	ast.BaseBlock
	key string
}

func (n *reviewBlock) Kind() ast.NodeKind { return kindReviewBlock }

func (n *reviewBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Key": n.key}, nil)
}

type markdownBlock struct {
	Key         string   `json:"key"`
	Kind        string   `json:"kind"`
	StartLine   int      `json:"startLine"`
	EndLine     int      `json:"endLine"`
	HeadingPath []string `json:"headingPath"`
	Quote       string   `json:"-"`
	BlockHash   string   `json:"-"`
}

type markdownBlockResponse struct {
	Key       string `json:"key"`
	Kind      string `json:"kind"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
}

type blockAnchor struct {
	BlockKey    string   `json:"blockKey"`
	BlockHash   string   `json:"blockHash"`
	Kind        string   `json:"kind"`
	StartLine   int      `json:"startLine"`
	EndLine     int      `json:"endLine"`
	HeadingPath []string `json:"headingPath,omitempty"`
	Quote       string   `json:"quote"`
}

type sourceLocation struct {
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
}

type sourceRangedBlock interface {
	reviewSourceRange() (int, int)
}

type reviewBlockRenderer struct{}

func (reviewBlockRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(kindReviewBlock, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		block := node.(*reviewBlock)
		if entering {
			_, _ = fmt.Fprintf(w, `<div class="md-block" data-md-block="%s">`, block.key)
			return ast.WalkContinue, nil
		}
		_, _ = w.WriteString("</div>\n")
		return ast.WalkContinue, nil
	})
}

func wrapReviewBlocks(document *ast.Document, source []byte, sourceLineOffset int) []markdownBlock {
	lineStarts := sourceLineStarts(source)
	occurrences := make(map[string]int)
	headingPath := make([]string, 0, 6)
	blocks := make([]markdownBlock, 0, document.ChildCount())

	for node := document.FirstChild(); node != nil; {
		next := node.NextSibling()
		if node.Kind() == ast.KindHTMLBlock {
			node = next
			continue
		}
		start, stop, ok := blockSourceBounds(node, source)
		if !ok {
			node = next
			continue
		}
		kind := markdownBlockKind(node)
		if heading, ok := node.(*ast.Heading); ok {
			level := heading.Level
			if level < 1 {
				level = 1
			}
			if len(headingPath) >= level {
				headingPath = headingPath[:level-1]
			}
			for len(headingPath) < level-1 {
				headingPath = append(headingPath, "")
			}
			headingPath = append(headingPath, inlineText(heading, source))
		}
		normalized := normalizeBlockSource(source[start:stop])
		blockHash := hashBytes(normalized)
		pathCopy := make([]string, len(headingPath))
		copy(pathCopy, headingPath)
		occurrenceID := kind + "\x00" + blockHash + "\x00" + strings.Join(pathCopy, "\x1f")
		occurrence := occurrences[occurrenceID]
		occurrences[occurrenceID] = occurrence + 1
		key := reviewBlockKey(kind, blockHash, pathCopy, occurrence)
		block := markdownBlock{
			Key:         key,
			Kind:        kind,
			StartLine:   sourceLineOffset + sourceLineNumber(lineStarts, start),
			EndLine:     sourceLineOffset + sourceLineNumber(lineStarts, max(start, stop-1)),
			HeadingPath: pathCopy,
			Quote:       shortenAnchorQuote(normalized),
			BlockHash:   blockHash,
		}
		blocks = append(blocks, block)

		wrapper := &reviewBlock{key: key}
		document.ReplaceChild(document, node, wrapper)
		wrapper.AppendChild(wrapper, node)
		node = next
	}
	return blocks
}

func blockSourceBounds(node ast.Node, source []byte) (int, int, bool) {
	if ranged, ok := node.(sourceRangedBlock); ok {
		start, stop := ranged.reviewSourceRange()
		if start >= 0 && stop > start && stop <= len(source) {
			return trimBlockBounds(source, start, stop)
		}
	}

	start := nodeSourceStart(node)
	if start < 0 || start >= len(source) {
		return 0, 0, false
	}
	stop := len(source)
	for sibling := node.NextSibling(); sibling != nil; sibling = sibling.NextSibling() {
		candidate := nodeSourceStart(sibling)
		if candidate >= 0 {
			stop = candidate
			break
		}
	}
	return trimBlockBounds(source, start, stop)
}

func nodeSourceStart(node ast.Node) int {
	if ranged, ok := node.(sourceRangedBlock); ok {
		start, _ := ranged.reviewSourceRange()
		if start >= 0 {
			return start
		}
	}
	if start := node.Pos(); start >= 0 {
		return start
	}
	return firstNodeSourceOffset(node)
}

func firstNodeSourceOffset(node ast.Node) int {
	first := -1
	_ = ast.Walk(node, func(current ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if current.Type() != ast.TypeInline {
			lines := current.Lines()
			for index := range lines.Len() {
				segment := lines.At(index)
				if segment.Start >= 0 && (first < 0 || segment.Start < first) {
					first = segment.Start
				}
			}
		}
		switch current := current.(type) {
		case *ast.Text:
			if current.Segment.Start >= 0 && (first < 0 || current.Segment.Start < first) {
				first = current.Segment.Start
			}
		case *ast.String:
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return first
}

func trimBlockBounds(source []byte, start, stop int) (int, int, bool) {
	if start < 0 {
		start = 0
	}
	if stop > len(source) {
		stop = len(source)
	}
	for start > 0 && source[start-1] != '\n' {
		start--
	}
	for stop > start {
		lineEnd := stop
		if source[lineEnd-1] == '\n' {
			lineEnd--
		}
		if lineEnd > start && source[lineEnd-1] == '\r' {
			lineEnd--
		}
		lineStart := bytes.LastIndexByte(source[start:lineEnd], '\n')
		if lineStart < 0 {
			lineStart = start
		} else {
			lineStart += start + 1
		}
		if len(bytes.Trim(source[lineStart:lineEnd], " \t\r")) != 0 {
			stop = lineEnd
			break
		}
		stop = lineStart
	}
	if stop <= start {
		return 0, 0, false
	}
	return start, stop, true
}

func sourceLineStarts(source []byte) []int {
	starts := []int{0}
	for index, value := range source {
		if value == '\n' {
			starts = append(starts, index+1)
		}
	}
	return starts
}

func sourceLineNumber(starts []int, offset int) int {
	return sort.Search(len(starts), func(index int) bool { return starts[index] > offset })
}

func normalizeBlockSource(source []byte) []byte {
	normalized := bytes.ReplaceAll(source, []byte("\r\n"), []byte("\n"))
	lines := bytes.Split(normalized, []byte("\n"))
	first := 0
	for first < len(lines) && len(bytes.Trim(lines[first], " \t\r")) == 0 {
		first++
	}
	last := len(lines)
	for last > first && len(bytes.Trim(lines[last-1], " \t\r")) == 0 {
		last--
	}
	return bytes.Join(lines[first:last], []byte("\n"))
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func sourceHash(source []byte) string { return hashBytes(source) }

func reviewBlockKey(kind, blockHash string, headingPath []string, occurrence int) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(kind))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(blockHash))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(strings.Join(headingPath, "\x1f")))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(strconv.Itoa(occurrence)))
	return hex.EncodeToString(hash.Sum(nil))[:24]
}

func shortenAnchorQuote(source []byte) string {
	if len(source) <= maxAnchorQuoteBytes {
		return string(source)
	}
	const ellipsis = "…"
	stop := maxAnchorQuoteBytes - len(ellipsis)
	for stop > 0 && !utf8.RuneStart(source[stop]) {
		stop--
	}
	return string(source[:stop]) + ellipsis
}

func markdownBlockKind(node ast.Node) string {
	switch node.Kind() {
	case ast.KindHeading:
		return "heading"
	case ast.KindParagraph, ast.KindTextBlock:
		return "paragraph"
	case ast.KindList:
		return "list"
	case ast.KindBlockquote:
		return "block_quote"
	case ast.KindFencedCodeBlock, ast.KindCodeBlock:
		return "code"
	case ast.KindThematicBreak:
		return "horizontal_rule"
	case extensionast.KindTable:
		return "table"
	case kindMermaidBlock:
		return "mermaid"
	case kindMathBlock:
		return "math"
	case kindCallout:
		return "callout"
	default:
		return strings.ToLower(node.Kind().String())
	}
}

func markdownBlockResponses(blocks []markdownBlock) []markdownBlockResponse {
	responses := make([]markdownBlockResponse, 0, len(blocks))
	for _, block := range blocks {
		responses = append(responses, markdownBlockResponse{
			Key: block.Key, Kind: block.Kind, StartLine: block.StartLine, EndLine: block.EndLine,
		})
	}
	return responses
}

func anchorForBlock(block markdownBlock) *blockAnchor {
	headingPath := make([]string, len(block.HeadingPath))
	copy(headingPath, block.HeadingPath)
	return &blockAnchor{
		BlockKey: block.Key, BlockHash: block.BlockHash, Kind: block.Kind,
		StartLine: block.StartLine, EndLine: block.EndLine, HeadingPath: headingPath, Quote: block.Quote,
	}
}

func matchBlockAnchor(baseHash, currentHash string, anchor *blockAnchor, blocks []markdownBlock) (*sourceLocation, *string, bool) {
	if anchor == nil {
		return nil, nil, false
	}
	if baseHash == currentHash {
		for _, block := range blocks {
			if block.Key == anchor.BlockKey {
				return &sourceLocation{StartLine: block.StartLine, EndLine: block.EndLine}, stringPointer(block.Key), false
			}
		}
		return nil, nil, true
	}

	matchingHeading := make([]markdownBlock, 0, 1)
	for _, block := range blocks {
		if block.BlockHash == anchor.BlockHash && slicesEqual(block.HeadingPath, anchor.HeadingPath) {
			matchingHeading = append(matchingHeading, block)
		}
	}
	if len(matchingHeading) == 1 {
		block := matchingHeading[0]
		return &sourceLocation{StartLine: block.StartLine, EndLine: block.EndLine}, stringPointer(block.Key), false
	}
	if len(matchingHeading) > 1 {
		return nil, nil, true
	}

	matchingDocument := make([]markdownBlock, 0, 1)
	for _, block := range blocks {
		if block.BlockHash == anchor.BlockHash {
			matchingDocument = append(matchingDocument, block)
		}
	}
	if len(matchingDocument) == 1 {
		block := matchingDocument[0]
		return &sourceLocation{StartLine: block.StartLine, EndLine: block.EndLine}, stringPointer(block.Key), false
	}
	return nil, nil, true
}

func stringPointer(value string) *string { return &value }

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
