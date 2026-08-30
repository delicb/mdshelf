package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type reviewStore struct {
	mu sync.Mutex

	path   string
	file   reviewStoreFile
	now    func() time.Time
	random io.Reader
	save   func(string, reviewStoreFile) error
}

type reviewDocumentContext struct {
	DocumentID string
	Path       string
	SourceHash string
	Blocks     []markdownBlock
}

func newReviewStore(path string) (*reviewStore, error) {
	file, err := loadReviewStoreFile(path)
	if err != nil {
		return nil, err
	}
	return &reviewStore{
		path: path,
		file: file,
		now: func() time.Time {
			return time.Now().UTC()
		},
		random: rand.Reader,
		save:   saveReviewStoreFile,
	}, nil
}

func loadReviewStoreFile(path string) (reviewStoreFile, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return reviewStoreFile{Version: reviewStoreVersion, Documents: []documentReview{}}, nil
	}
	if err != nil {
		return reviewStoreFile{}, fmt.Errorf("open reviews: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return reviewStoreFile{}, fmt.Errorf("inspect reviews: %w", err)
	}
	if info.Size() > maxReviewStoreBytes {
		return reviewStoreFile{}, fmt.Errorf("decode reviews: file exceeds %d bytes", maxReviewStoreBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxReviewStoreBytes+1))
	if err != nil {
		return reviewStoreFile{}, fmt.Errorf("read reviews: %w", err)
	}
	if len(data) > maxReviewStoreBytes {
		return reviewStoreFile{}, fmt.Errorf("decode reviews: file exceeds %d bytes", maxReviewStoreBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var stored reviewStoreFile
	if err := decoder.Decode(&stored); err != nil {
		return reviewStoreFile{}, fmt.Errorf("decode reviews: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return reviewStoreFile{}, fmt.Errorf("decode reviews: %w", err)
	}
	if err := migrateReviewStoreFile(&stored); err != nil {
		return reviewStoreFile{}, fmt.Errorf("migrate reviews: %w", err)
	}
	if err := validateReviewStoreFile(stored); err != nil {
		return reviewStoreFile{}, fmt.Errorf("validate reviews: %w", err)
	}
	return stored, nil
}

func migrateReviewStoreFile(stored *reviewStoreFile) error {
	switch stored.Version {
	case 1:
		for _, document := range stored.Documents {
			for _, comment := range document.Comments {
				if comment.TextRange != nil {
					return errors.New("version 1 review state contains a text range")
				}
			}
		}
		stored.Version = reviewStoreVersion
		return nil
	case reviewStoreVersion:
		return nil
	default:
		return fmt.Errorf("unsupported review store version %d", stored.Version)
	}
}

func saveReviewStoreFile(path string, stored reviewStoreFile) error {
	stored.Documents = append([]documentReview(nil), stored.Documents...)
	sort.Slice(stored.Documents, func(i, j int) bool {
		return strings.Compare(stored.Documents[i].Path, stored.Documents[j].Path) < 0
	})
	if err := validateReviewStoreFile(stored); err != nil {
		return err
	}
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(stored); err != nil {
		return fmt.Errorf("encode reviews: %w", err)
	}
	if data.Len() > maxReviewStoreBytes {
		return fmt.Errorf("reviews file exceeds %d bytes", maxReviewStoreBytes)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".reviews-*.tmp")
	if err != nil {
		return fmt.Errorf("create reviews temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	remove := true
	defer func() {
		_ = temporary.Close()
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("set reviews permissions: %w", err)
	}
	if _, err := temporary.Write(data.Bytes()); err != nil {
		return fmt.Errorf("encode reviews: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("flush reviews: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close reviews: %w", err)
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("replace reviews: %w", err)
	}
	remove = false
	if directory, err := os.Open(filepath.Dir(path)); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func (s *reviewStore) snapshot(documentID, path string) (documentReview, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := reviewDocumentIndex(s.file.Documents, documentID, path)
	if index < 0 {
		return emptyDocumentReview(documentID, path), false
	}
	return cloneDocumentReview(s.file.Documents[index]), true
}

func (s *reviewStore) documentForComment(commentID string) (string, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, document := range s.file.Documents {
		if findReviewComment(&document, commentID) != nil {
			return document.DocumentID, document.Path, true
		}
	}
	return "", "", false
}

func (s *reviewStore) summary(documentID, path, currentHash string, removed bool) (documentReviewStatus, int) {
	document, _ := s.snapshot(documentID, path)
	openComments := 0
	for _, comment := range document.Comments {
		if comment.Status == commentStatusOpen || comment.Status == commentStatusAddressed {
			openComments++
		}
	}
	return reviewStatusForDocument(document, currentHash, removed), openComments
}

func reviewStatusForDocument(stored documentReview, currentHash string, removed bool) documentReviewStatus {
	if removed {
		return documentReviewRemoved
	}
	if len(stored.Comments) == 0 {
		return documentReviewNeedsReview
	}
	if stored.Comments[len(stored.Comments)-1].BaseHash != currentHash {
		return documentReviewUpdated
	}
	return documentReviewComments
}

func (s *reviewStore) addCommentWithSelection(document reviewDocumentContext, expectedRevision uint64, expectedSourceHash, body, blockKey string, selection *reviewSelectionRequest) (documentReview, reviewComment, error) {
	if err := validateReviewText(body, maxReviewTextBytes, false, "comment body"); err != nil {
		return documentReview{}, reviewComment{}, reviewInputTextError(body, maxReviewTextBytes, err)
	}
	anchor, textRange, err := commentRangeForRequest(document.Blocks, blockKey, selection)
	if err != nil {
		return documentReview{}, reviewComment{}, reviewInputTextError(selectionQuote(selection), maxReviewTextBytes, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	proposed := cloneReviewStoreFile(s.file)
	stored := ensureDocumentReview(&proposed, document.DocumentID, document.Path)
	if err := checkReviewExpectation(stored, expectedRevision, expectedSourceHash, document.SourceHash); err != nil {
		return documentReview{}, reviewComment{}, err
	}
	if len(stored.Comments) >= maxCommentsPerDocument {
		return documentReview{}, reviewComment{}, reviewLimitError(fmt.Errorf("comment limit is %d", maxCommentsPerDocument))
	}
	now := s.now().UTC()
	id, err := s.newID("comment")
	if err != nil {
		return documentReview{}, reviewComment{}, wrapReviewError(http.StatusInternalServerError, reviewCodeStateInvalid, "could not create comment id", err)
	}
	comment := reviewComment{
		ID: id, Body: body, Status: commentStatusOpen, BaseHash: document.SourceHash,
		Anchor: anchor, TextRange: textRange, Replies: []reviewReply{}, CreatedAt: now, UpdatedAt: now,
	}
	stored.Comments = append(stored.Comments, comment)
	stored.Revision++
	if err := s.persistLocked(proposed); err != nil {
		return documentReview{}, reviewComment{}, err
	}
	return cloneDocumentReview(*stored), cloneReviewComment(comment), nil
}

func (s *reviewStore) replyToComment(document reviewDocumentContext, expectedRevision uint64, expectedSourceHash, commentID, body string) (documentReview, reviewComment, error) {
	if err := validateReviewText(body, maxReviewTextBytes, false, "reply body"); err != nil {
		return documentReview{}, reviewComment{}, reviewInputTextError(body, maxReviewTextBytes, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	proposed := cloneReviewStoreFile(s.file)
	stored, err := requiredDocumentReview(&proposed, document.DocumentID, document.Path)
	if err != nil {
		return documentReview{}, reviewComment{}, err
	}
	if err := checkReviewExpectation(stored, expectedRevision, expectedSourceHash, document.SourceHash); err != nil {
		return documentReview{}, reviewComment{}, err
	}
	comment := findReviewComment(stored, commentID)
	if comment == nil {
		return documentReview{}, reviewComment{}, commentNotFoundError()
	}
	if comment.Status == commentStatusResolved {
		return documentReview{}, reviewComment{}, invalidTransitionError("reopen the comment before you reply")
	}
	now := s.now().UTC()
	if _, err := s.appendReply(comment, replyAuthorReviewer, body, now); err != nil {
		return documentReview{}, reviewComment{}, err
	}
	comment.Status = commentStatusOpen
	comment.UpdatedAt = now
	stored.Revision++
	result := cloneReviewComment(*comment)
	if err := s.persistLocked(proposed); err != nil {
		return documentReview{}, reviewComment{}, err
	}
	return cloneDocumentReview(*stored), result, nil
}

func (s *reviewStore) addressComment(commentID, message string) (documentReview, reviewComment, error) {
	if err := validateReviewText(message, maxReviewTextBytes, false, "agent reply"); err != nil {
		return documentReview{}, reviewComment{}, reviewInputTextError(message, maxReviewTextBytes, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	proposed := cloneReviewStoreFile(s.file)
	var stored *documentReview
	var comment *reviewComment
	for index := range proposed.Documents {
		if found := findReviewComment(&proposed.Documents[index], commentID); found != nil {
			stored = &proposed.Documents[index]
			comment = found
			break
		}
	}
	if comment == nil {
		return documentReview{}, reviewComment{}, commentNotFoundError()
	}
	if comment.Status != commentStatusOpen && comment.Status != commentStatusAddressed {
		return documentReview{}, reviewComment{}, invalidTransitionError("only open or addressed comments can be addressed")
	}
	now := s.now().UTC()
	if _, err := s.appendReply(comment, replyAuthorAgent, message, now); err != nil {
		return documentReview{}, reviewComment{}, err
	}
	comment.Status = commentStatusAddressed
	comment.AddressedAt = timePointer(now)
	comment.UpdatedAt = now
	stored.Revision++
	result := cloneReviewComment(*comment)
	if err := s.persistLocked(proposed); err != nil {
		return documentReview{}, reviewComment{}, err
	}
	return cloneDocumentReview(*stored), result, nil
}

func (s *reviewStore) appendReply(comment *reviewComment, author replyAuthor, body string, now time.Time) (reviewReply, error) {
	if len(comment.Replies) >= maxRepliesPerComment {
		return reviewReply{}, reviewLimitError(fmt.Errorf("reply limit is %d", maxRepliesPerComment))
	}
	id, err := s.newID("reply")
	if err != nil {
		return reviewReply{}, wrapReviewError(http.StatusInternalServerError, reviewCodeStateInvalid, "could not create reply id", err)
	}
	reply := reviewReply{ID: id, Body: body, Author: author, CreatedAt: now}
	comment.Replies = append(comment.Replies, reply)
	return reply, nil
}

func (s *reviewStore) resolveComment(document reviewDocumentContext, expectedRevision uint64, expectedSourceHash, commentID string) (documentReview, reviewComment, error) {
	return s.setReviewerCommentState(document, expectedRevision, expectedSourceHash, commentID, commentStatusResolved)
}

func (s *reviewStore) reopenComment(document reviewDocumentContext, expectedRevision uint64, expectedSourceHash, commentID string) (documentReview, reviewComment, error) {
	return s.setReviewerCommentState(document, expectedRevision, expectedSourceHash, commentID, commentStatusOpen)
}

func (s *reviewStore) setReviewerCommentState(document reviewDocumentContext, expectedRevision uint64, expectedSourceHash, commentID string, target commentStatus) (documentReview, reviewComment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	proposed := cloneReviewStoreFile(s.file)
	stored, err := requiredDocumentReview(&proposed, document.DocumentID, document.Path)
	if err != nil {
		return documentReview{}, reviewComment{}, err
	}
	if err := checkReviewExpectation(stored, expectedRevision, expectedSourceHash, document.SourceHash); err != nil {
		return documentReview{}, reviewComment{}, err
	}
	comment := findReviewComment(stored, commentID)
	if comment == nil {
		return documentReview{}, reviewComment{}, commentNotFoundError()
	}
	now := s.now().UTC()
	switch target {
	case commentStatusResolved:
		if comment.Status != commentStatusOpen && comment.Status != commentStatusAddressed {
			return documentReview{}, reviewComment{}, invalidTransitionError("only open or addressed comments can be resolved")
		}
		comment.Status = commentStatusResolved
		comment.ResolvedAt = timePointer(now)
	case commentStatusOpen:
		if comment.Status != commentStatusAddressed && comment.Status != commentStatusResolved {
			return documentReview{}, reviewComment{}, invalidTransitionError("only addressed or resolved comments can be reopened")
		}
		comment.Status = commentStatusOpen
		comment.ResolvedAt = nil
	default:
		return documentReview{}, reviewComment{}, invalidTransitionError("unsupported reviewer transition")
	}
	comment.UpdatedAt = now
	stored.Revision++
	result := cloneReviewComment(*comment)
	if err := s.persistLocked(proposed); err != nil {
		return documentReview{}, reviewComment{}, err
	}
	return cloneDocumentReview(*stored), result, nil
}

func (s *reviewStore) persistLocked(proposed reviewStoreFile) error {
	stored := cloneReviewStoreFile(proposed)
	sort.Slice(stored.Documents, func(i, j int) bool {
		return strings.Compare(stored.Documents[i].Path, stored.Documents[j].Path) < 0
	})
	if err := validateReviewStoreFile(stored); err != nil {
		return wrapReviewError(http.StatusInternalServerError, reviewCodeStateInvalid, "review state is invalid", err)
	}
	if err := s.save(s.path, stored); err != nil {
		return wrapReviewError(http.StatusInternalServerError, reviewCodeStateInvalid, "could not save review state", err)
	}
	s.file = stored
	return nil
}

func (s *reviewStore) newID(prefix string) (string, error) {
	value := make([]byte, 12)
	if _, err := io.ReadFull(s.random, value); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(value), nil
}

func checkReviewExpectation(document *documentReview, expectedRevision uint64, expectedSourceHash, currentSourceHash string) error {
	if expectedSourceHash != currentSourceHash {
		return staleDocumentError()
	}
	if document.Revision != expectedRevision {
		return newReviewError(http.StatusConflict, reviewCodeStaleReview, "The comments changed. Refresh them and retry.")
	}
	return nil
}

func commentRangeForRequest(blocks []markdownBlock, blockKey string, selection *reviewSelectionRequest) (*blockAnchor, *reviewTextRange, error) {
	if blockKey != "" && selection != nil {
		return nil, nil, errors.New("block key and selection cannot both be set")
	}
	if selection == nil {
		anchor, err := blockAnchorByKey(blocks, blockKey)
		return anchor, nil, err
	}
	if err := validateReviewSelection(selection); err != nil {
		return nil, nil, err
	}
	indexes := make(map[string]int, len(blocks))
	for index, block := range blocks {
		indexes[block.Key] = index
	}
	anchors := make([]blockAnchor, len(selection.BlockKeys))
	seen := make(map[string]struct{}, len(selection.BlockKeys))
	previous := -1
	for index, key := range selection.BlockKeys {
		if !blockKeyPattern.MatchString(key) {
			return nil, nil, errors.New("selection has an invalid block key")
		}
		if _, exists := seen[key]; exists {
			return nil, nil, errors.New("selection has duplicate block keys")
		}
		seen[key] = struct{}{}
		blockIndex, exists := indexes[key]
		if !exists {
			return nil, nil, errors.New("selection block key does not match the current document")
		}
		if previous >= 0 && blockIndex != previous+1 {
			return nil, nil, errors.New("selection blocks must be consecutive and in document order")
		}
		previous = blockIndex
		anchors[index] = *anchorForBlock(blocks[blockIndex])
	}
	compatibilityAnchor := cloneBlockAnchor(anchors[0])
	return &compatibilityAnchor, &reviewTextRange{
		Version: reviewTextRangeVersion, Anchors: anchors,
		StartOffset: selection.StartOffset, EndOffset: selection.EndOffset, Quote: selection.Quote,
	}, nil
}

func selectionQuote(selection *reviewSelectionRequest) string {
	if selection == nil {
		return ""
	}
	return selection.Quote
}

func blockAnchorByKey(blocks []markdownBlock, blockKey string) (*blockAnchor, error) {
	if blockKey == "" {
		return nil, nil
	}
	for _, block := range blocks {
		if block.Key == blockKey {
			return anchorForBlock(block), nil
		}
	}
	return nil, errors.New("block key does not match the current document")
}

func staleDocumentError() error {
	return newReviewError(http.StatusConflict, reviewCodeStaleDocument, "The document changed while saving the comment.")
}

func commentNotFoundError() error {
	return newReviewError(http.StatusNotFound, reviewCodeCommentMissing, "Comment not found.")
}

func invalidTransitionError(message string) error {
	return newReviewError(http.StatusConflict, reviewCodeTransition, message)
}

func reviewInputTextError(value string, limit int, err error) error {
	if len(value) > limit {
		return reviewLimitError(err)
	}
	return err
}

func reviewLimitError(err error) error {
	if err == nil {
		return nil
	}
	return wrapReviewError(http.StatusRequestEntityTooLarge, reviewCodeLimit, err.Error(), err)
}

func emptyDocumentReview(documentID, path string) documentReview {
	return documentReview{DocumentID: documentID, Path: path, Comments: []reviewComment{}}
}

func ensureDocumentReview(file *reviewStoreFile, documentID, path string) *documentReview {
	if index := reviewDocumentIndex(file.Documents, documentID, path); index >= 0 {
		return &file.Documents[index]
	}
	file.Documents = append(file.Documents, emptyDocumentReview(documentID, path))
	return &file.Documents[len(file.Documents)-1]
}

func requiredDocumentReview(file *reviewStoreFile, documentID, path string) (*documentReview, error) {
	index := reviewDocumentIndex(file.Documents, documentID, path)
	if index < 0 {
		return nil, newReviewError(http.StatusNotFound, reviewCodeStateInvalid, "Comment state not found.")
	}
	return &file.Documents[index], nil
}

func reviewDocumentIndex(documents []documentReview, documentID, path string) int {
	for index := range documents {
		if documents[index].DocumentID == documentID && documents[index].Path == path {
			return index
		}
	}
	return -1
}

func findReviewComment(document *documentReview, commentID string) *reviewComment {
	for index := range document.Comments {
		if document.Comments[index].ID == commentID {
			return &document.Comments[index]
		}
	}
	return nil
}

func cloneReviewStoreFile(file reviewStoreFile) reviewStoreFile {
	clone := reviewStoreFile{Version: file.Version, Documents: make([]documentReview, len(file.Documents))}
	for index, document := range file.Documents {
		clone.Documents[index] = cloneDocumentReview(document)
	}
	return clone
}

func cloneDocumentReview(document documentReview) documentReview {
	clone := document
	clone.Comments = make([]reviewComment, len(document.Comments))
	for index, comment := range document.Comments {
		clone.Comments[index] = cloneReviewComment(comment)
	}
	return clone
}

func cloneReviewComment(comment reviewComment) reviewComment {
	clone := comment
	clone.Replies = make([]reviewReply, len(comment.Replies))
	copy(clone.Replies, comment.Replies)
	if comment.Anchor != nil {
		anchor := cloneBlockAnchor(*comment.Anchor)
		clone.Anchor = &anchor
	}
	if comment.TextRange != nil {
		textRange := *comment.TextRange
		textRange.Anchors = make([]blockAnchor, len(comment.TextRange.Anchors))
		for index, anchor := range comment.TextRange.Anchors {
			textRange.Anchors[index] = cloneBlockAnchor(anchor)
		}
		clone.TextRange = &textRange
	}
	if comment.AddressedAt != nil {
		value := *comment.AddressedAt
		clone.AddressedAt = &value
	}
	if comment.ResolvedAt != nil {
		value := *comment.ResolvedAt
		clone.ResolvedAt = &value
	}
	return clone
}

func cloneBlockAnchor(anchor blockAnchor) blockAnchor {
	anchor.HeadingPath = append([]string(nil), anchor.HeadingPath...)
	return anchor
}

func timePointer(value time.Time) *time.Time {
	return &value
}
