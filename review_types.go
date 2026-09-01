package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	reviewStoreVersion       = 2
	reviewAPISchemaVersion   = 1
	reviewTextRangeVersion   = 1
	maxReviewTextBytes       = 16 << 10
	maxReviewRangeAnchors    = 16
	maxReviewTextOffset      = 1 << 30
	maxCommentsPerDocument   = 500
	maxRepliesPerComment     = 100
	maxReviewStoreBytes      = 32 << 20
	maxReviewWriteRequest    = 64 << 10
	reviewFeature            = "reviews-v1"
	reviewCodeNotRegistered  = "document_not_registered"
	reviewCodeRemoved        = "document_removed"
	reviewCodeCommentMissing = "comment_not_found"
	reviewCodeTransition     = "invalid_transition"
	reviewCodeStaleDocument  = "stale_document"
	reviewCodeStaleReview    = "stale_review"
	reviewCodeLimit          = "review_limit"
	reviewCodeStateInvalid   = "review_state_invalid"
)

var (
	commentIDPattern  = regexp.MustCompile(`^comment_[0-9a-f]{24}$`)
	replyIDPattern    = regexp.MustCompile(`^reply_[0-9a-f]{24}$`)
	blockKeyPattern   = regexp.MustCompile(`^[0-9a-f]{24}$`)
	sourceHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type commentStatus string

const (
	commentStatusOpen      commentStatus = "open"
	commentStatusAddressed commentStatus = "addressed"
	commentStatusResolved  commentStatus = "resolved"
)

type replyAuthor string

const (
	replyAuthorReviewer replyAuthor = "reviewer"
	replyAuthorAgent    replyAuthor = "agent"
)

type documentReviewStatus string

const (
	documentReviewNeedsReview documentReviewStatus = "needs_review"
	documentReviewComments    documentReviewStatus = "comments"
	documentReviewUpdated     documentReviewStatus = "updated"
	documentReviewRemoved     documentReviewStatus = "removed"
)

type reviewStoreFile struct {
	Version   int              `json:"version"`
	Documents []documentReview `json:"documents"`
}

type documentReview struct {
	DocumentID string          `json:"documentId"`
	Path       string          `json:"path"`
	Revision   uint64          `json:"revision"`
	Comments   []reviewComment `json:"comments"`
}

type reviewComment struct {
	ID          string           `json:"id"`
	Body        string           `json:"body"`
	Status      commentStatus    `json:"status"`
	BaseHash    string           `json:"baseHash"`
	Anchor      *blockAnchor     `json:"anchor,omitempty"`
	TextRange   *reviewTextRange `json:"textRange,omitempty"`
	Replies     []reviewReply    `json:"replies,omitempty"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
	AddressedAt *time.Time       `json:"addressedAt,omitempty"`
	ResolvedAt  *time.Time       `json:"resolvedAt,omitempty"`
}

type reviewTextRange struct {
	Version     int           `json:"version"`
	Anchors     []blockAnchor `json:"anchors"`
	StartOffset int64         `json:"startOffset"`
	EndOffset   int64         `json:"endOffset"`
	Quote       string        `json:"quote"`
}

type reviewSelectionRequest struct {
	Version     int      `json:"version"`
	BlockKeys   []string `json:"blockKeys"`
	StartOffset int64    `json:"startOffset"`
	EndOffset   int64    `json:"endOffset"`
	Quote       string   `json:"quote"`
}

type reviewReply struct {
	ID        string      `json:"id"`
	Body      string      `json:"body"`
	Author    replyAuthor `json:"author"`
	CreatedAt time.Time   `json:"createdAt"`
}

type reviewDocumentResponse struct {
	ID           string               `json:"id"`
	Path         string               `json:"path"`
	Title        string               `json:"title"`
	URL          string               `json:"url"`
	SourceHash   string               `json:"sourceHash"`
	ReviewStatus documentReviewStatus `json:"reviewStatus"`
}

type reviewAnchorResponse struct {
	BlockKey    string   `json:"blockKey"`
	Kind        string   `json:"kind"`
	StartLine   int      `json:"startLine"`
	EndLine     int      `json:"endLine"`
	HeadingPath []string `json:"headingPath"`
	Quote       string   `json:"quote"`
}

type reviewReplyResponse struct {
	ID        string      `json:"id"`
	Body      string      `json:"body"`
	Author    replyAuthor `json:"author"`
	CreatedAt time.Time   `json:"createdAt"`
}

type reviewTextRangeResponse struct {
	Version          int                    `json:"version"`
	Anchors          []reviewAnchorResponse `json:"anchors"`
	StartOffset      int64                  `json:"startOffset"`
	EndOffset        int64                  `json:"endOffset"`
	Quote            string                 `json:"quote"`
	CurrentBlockKeys []string               `json:"currentBlockKeys,omitempty"`
}

type reviewCommentResponse struct {
	ID              string                   `json:"id"`
	Body            string                   `json:"body"`
	Status          commentStatus            `json:"status"`
	BaseHash        string                   `json:"baseHash"`
	Outdated        bool                     `json:"outdated"`
	Anchor          *reviewAnchorResponse    `json:"anchor"`
	TextRange       *reviewTextRangeResponse `json:"textRange,omitempty"`
	CurrentLocation *sourceLocation          `json:"currentLocation"`
	CurrentBlockKey *string                  `json:"currentBlockKey"`
	Replies         []reviewReplyResponse    `json:"replies"`
}

type reviewShowResponse struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Document      reviewDocumentResponse  `json:"document"`
	Comments      []reviewCommentResponse `json:"comments"`
}

type browserReviewResponse struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Document      reviewDocumentResponse  `json:"document"`
	Revision      uint64                  `json:"revision"`
	Comments      []reviewCommentResponse `json:"comments"`
}

type reviewError struct {
	code    string
	message string
	status  int
	err     error
}

func (e *reviewError) Error() string {
	if e.message != "" {
		return e.message
	}
	return e.err.Error()
}

func (e *reviewError) Unwrap() error { return e.err }

func newReviewError(status int, code, message string) error {
	return &reviewError{status: status, code: code, message: message}
}

func wrapReviewError(status int, code, message string, err error) error {
	return &reviewError{status: status, code: code, message: message, err: err}
}

func validCommentStatus(value commentStatus) bool {
	return value == commentStatusOpen || value == commentStatusAddressed || value == commentStatusResolved
}

func validateReviewStoreFile(store reviewStoreFile) error {
	if store.Version != reviewStoreVersion {
		return fmt.Errorf("unsupported review store version %d", store.Version)
	}
	if store.Documents == nil {
		return errors.New("review documents must be a JSON array")
	}
	documentIDs := make(map[string]struct{}, len(store.Documents))
	paths := make(map[string]struct{}, len(store.Documents))
	commentIDs := make(map[string]struct{})
	replyIDs := make(map[string]struct{})
	for index := range store.Documents {
		document := &store.Documents[index]
		if !documentIDPattern.MatchString(document.DocumentID) {
			return fmt.Errorf("invalid review document id %q", document.DocumentID)
		}
		if !filepath.IsAbs(document.Path) || filepath.Clean(document.Path) != document.Path || !isMarkdownPath(document.Path) {
			return fmt.Errorf("invalid review document path %q", document.Path)
		}
		if document.DocumentID != documentID(document.Path) {
			return fmt.Errorf("review document id does not match path %q", document.Path)
		}
		if _, exists := documentIDs[document.DocumentID]; exists {
			return fmt.Errorf("duplicate review document id %q", document.DocumentID)
		}
		if _, exists := paths[document.Path]; exists {
			return fmt.Errorf("duplicate review document path %q", document.Path)
		}
		documentIDs[document.DocumentID] = struct{}{}
		paths[document.Path] = struct{}{}
		if document.Comments == nil {
			return fmt.Errorf("document %q: comments must be a JSON array", document.Path)
		}
		if len(document.Comments) > maxCommentsPerDocument {
			return fmt.Errorf("document %q: comment limit exceeds %d", document.Path, maxCommentsPerDocument)
		}
		for commentIndex := range document.Comments {
			comment := &document.Comments[commentIndex]
			if _, exists := commentIDs[comment.ID]; exists {
				return fmt.Errorf("duplicate comment id %q", comment.ID)
			}
			commentIDs[comment.ID] = struct{}{}
			if err := validateReviewComment(comment); err != nil {
				return fmt.Errorf("comment %q: %w", comment.ID, err)
			}
			for _, reply := range comment.Replies {
				if _, exists := replyIDs[reply.ID]; exists {
					return fmt.Errorf("duplicate reply id %q", reply.ID)
				}
				replyIDs[reply.ID] = struct{}{}
			}
		}
	}
	return nil
}

func validateReviewComment(comment *reviewComment) error {
	if !commentIDPattern.MatchString(comment.ID) {
		return errors.New("invalid comment id")
	}
	if !validCommentStatus(comment.Status) {
		return fmt.Errorf("invalid status %q", comment.Status)
	}
	if err := validateReviewText(comment.Body, maxReviewTextBytes, false, "comment body"); err != nil {
		return err
	}
	if len(comment.Replies) > maxRepliesPerComment {
		return fmt.Errorf("reply limit exceeds %d", maxRepliesPerComment)
	}
	for index := range comment.Replies {
		if err := validateReviewReply(&comment.Replies[index]); err != nil {
			return fmt.Errorf("reply %q: %w", comment.Replies[index].ID, err)
		}
	}
	if !sourceHashPattern.MatchString(comment.BaseHash) {
		return errors.New("invalid base hash")
	}
	if comment.Anchor != nil {
		if err := validateBlockAnchor(comment.Anchor); err != nil {
			return err
		}
	}
	if comment.TextRange != nil {
		if err := validateReviewTextRange(comment.TextRange, comment.Anchor); err != nil {
			return err
		}
	}
	if err := validateUTCTime(comment.CreatedAt, "creation time"); err != nil {
		return err
	}
	if err := validateUTCTime(comment.UpdatedAt, "update time"); err != nil {
		return err
	}
	if comment.UpdatedAt.Before(comment.CreatedAt) {
		return errors.New("update time precedes creation time")
	}
	for _, reply := range comment.Replies {
		if reply.CreatedAt.Before(comment.CreatedAt) || reply.CreatedAt.After(comment.UpdatedAt) {
			return fmt.Errorf("reply %q has an invalid creation time", reply.ID)
		}
	}
	if comment.AddressedAt != nil {
		if err := validateUTCTime(*comment.AddressedAt, "address time"); err != nil {
			return err
		}
	}
	if comment.ResolvedAt != nil {
		if err := validateUTCTime(*comment.ResolvedAt, "resolution time"); err != nil {
			return err
		}
	}
	switch comment.Status {
	case commentStatusOpen:
		if comment.ResolvedAt != nil {
			return errors.New("open comment has a resolution time")
		}
	case commentStatusAddressed:
		if !commentHasAgentReply(comment) || comment.AddressedAt == nil || comment.ResolvedAt != nil {
			return errors.New("addressed comment has invalid response state")
		}
	case commentStatusResolved:
		if comment.ResolvedAt == nil {
			return errors.New("resolved comment has no resolution time")
		}
	}
	return nil
}

func validateReviewTextRange(textRange *reviewTextRange, compatibilityAnchor *blockAnchor) error {
	if textRange.Version != reviewTextRangeVersion {
		return fmt.Errorf("unsupported text range version %d", textRange.Version)
	}
	if len(textRange.Anchors) < 1 || len(textRange.Anchors) > maxReviewRangeAnchors {
		return fmt.Errorf("text range anchor count must be from 1 through %d", maxReviewRangeAnchors)
	}
	if compatibilityAnchor == nil || !blockAnchorsEqual(*compatibilityAnchor, textRange.Anchors[0]) {
		return errors.New("text range compatibility anchor does not match its first anchor")
	}
	seenKeys := make(map[string]struct{}, len(textRange.Anchors))
	for index := range textRange.Anchors {
		anchor := &textRange.Anchors[index]
		if err := validateBlockAnchor(anchor); err != nil {
			return fmt.Errorf("text range anchor %d: %w", index, err)
		}
		if _, exists := seenKeys[anchor.BlockKey]; exists {
			return errors.New("text range has duplicate block keys")
		}
		seenKeys[anchor.BlockKey] = struct{}{}
		if index > 0 && anchor.StartLine <= textRange.Anchors[index-1].EndLine {
			return errors.New("text range anchors are not in source order")
		}
	}
	if err := validateReviewRangeOffsets(textRange.StartOffset, textRange.EndOffset, len(textRange.Anchors)); err != nil {
		return err
	}
	return validateReviewText(textRange.Quote, maxReviewTextBytes, false, "selected quote")
}

func validateReviewSelection(selection *reviewSelectionRequest) error {
	if selection.Version != reviewTextRangeVersion {
		return fmt.Errorf("unsupported selection version %d", selection.Version)
	}
	if len(selection.BlockKeys) < 1 || len(selection.BlockKeys) > maxReviewRangeAnchors {
		return fmt.Errorf("selection block count must be from 1 through %d", maxReviewRangeAnchors)
	}
	if err := validateReviewRangeOffsets(selection.StartOffset, selection.EndOffset, len(selection.BlockKeys)); err != nil {
		return err
	}
	return validateReviewText(selection.Quote, maxReviewTextBytes, false, "selected quote")
}

func validateReviewRangeOffsets(startOffset, endOffset int64, anchorCount int) error {
	if startOffset < 0 || startOffset > maxReviewTextOffset {
		return errors.New("selection start offset is out of range")
	}
	if endOffset <= 0 || endOffset > maxReviewTextOffset {
		return errors.New("selection end offset is out of range")
	}
	if anchorCount == 1 && startOffset >= endOffset {
		return errors.New("selection offsets are not in order")
	}
	return nil
}

func validateReviewReply(reply *reviewReply) error {
	if !replyIDPattern.MatchString(reply.ID) {
		return errors.New("invalid reply id")
	}
	if reply.Author != replyAuthorReviewer && reply.Author != replyAuthorAgent {
		return fmt.Errorf("invalid author %q", reply.Author)
	}
	if err := validateReviewText(reply.Body, maxReviewTextBytes, false, "reply body"); err != nil {
		return err
	}
	return validateUTCTime(reply.CreatedAt, "creation time")
}

func commentHasAgentReply(comment *reviewComment) bool {
	for _, reply := range comment.Replies {
		if reply.Author == replyAuthorAgent {
			return true
		}
	}
	return false
}

func blockAnchorsEqual(left, right blockAnchor) bool {
	return left.BlockKey == right.BlockKey &&
		left.BlockHash == right.BlockHash &&
		left.Kind == right.Kind &&
		left.StartLine == right.StartLine &&
		left.EndLine == right.EndLine &&
		left.Quote == right.Quote &&
		slices.Equal(left.HeadingPath, right.HeadingPath)
}

func validateBlockAnchor(anchor *blockAnchor) error {
	if !blockKeyPattern.MatchString(anchor.BlockKey) {
		return errors.New("anchor has an invalid block key")
	}
	if !sourceHashPattern.MatchString(anchor.BlockHash) {
		return errors.New("anchor has an invalid block hash")
	}
	if anchor.Kind == "" || len(anchor.Kind) > 64 || strings.ContainsRune(anchor.Kind, '\x00') {
		return errors.New("anchor has an invalid kind")
	}
	if anchor.StartLine < 1 || anchor.EndLine < anchor.StartLine {
		return errors.New("anchor has an invalid line range")
	}
	if err := validateReviewText(anchor.Quote, maxAnchorQuoteBytes, false, "anchor quote"); err != nil {
		return err
	}
	for _, heading := range anchor.HeadingPath {
		if strings.ContainsRune(heading, '\x00') || !utf8.ValidString(heading) {
			return errors.New("anchor has an invalid heading path")
		}
	}
	return nil
}

func validateReviewText(value string, limit int, allowEmpty bool, name string) error {
	if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s contains invalid text", name)
	}
	if len(value) > limit {
		return fmt.Errorf("%s exceeds %d bytes", name, limit)
	}
	if !allowEmpty && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

func validateUTCTime(value time.Time, name string) error {
	if value.IsZero() || value.Location() != time.UTC {
		return fmt.Errorf("%s must be a UTC timestamp", name)
	}
	return nil
}
