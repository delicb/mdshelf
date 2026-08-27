package main

import (
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
)

type addReviewCommentRequest struct {
	Path               string                  `json:"path"`
	ExpectedRevision   uint64                  `json:"expectedRevision"`
	ExpectedSourceHash string                  `json:"expectedSourceHash"`
	Body               string                  `json:"body"`
	BlockKey           string                  `json:"blockKey,omitempty"`
	Selection          *reviewSelectionRequest `json:"selection,omitempty"`
}

type commentReviewerRequest struct {
	Path               string `json:"path"`
	ExpectedRevision   uint64 `json:"expectedRevision"`
	ExpectedSourceHash string `json:"expectedSourceHash"`
	CommentID          string `json:"commentId"`
}

type replyReviewCommentRequest struct {
	Path               string `json:"path"`
	ExpectedRevision   uint64 `json:"expectedRevision"`
	ExpectedSourceHash string `json:"expectedSourceHash"`
	CommentID          string `json:"commentId"`
	Body               string `json:"body"`
}

type addressReviewCommentRequest struct {
	CommentID string `json:"commentId"`
	Message   string `json:"message"`
}

type showReviewRequest struct {
	Path            string `json:"path"`
	IncludeResolved bool   `json:"includeResolved"`
}

type reviewMutationResponse struct {
	Review  browserReviewResponse  `json:"review"`
	Comment *reviewCommentResponse `json:"comment,omitempty"`
}

func (d *daemonServer) handleReview(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	includeResolved, ok := reviewIncludeResolved(w, r)
	if !ok {
		return
	}
	document, err := d.registeredReviewDocumentID(r.URL.Query().Get("path"))
	if err != nil {
		writeReviewError(w, err)
		return
	}
	rendered, err := d.renderReviewDocument(document)
	if err != nil {
		log.Printf("render comments: %v", err)
		writeJSONErrorCode(w, http.StatusInternalServerError, "Could not render the document comments.", reviewCodeStateInvalid)
		return
	}
	stored, _ := d.reviews.snapshot(document.ID, document.Path)
	writeJSON(w, http.StatusOK, buildBrowserReviewResponse(stored, document, rendered, daemonBaseURL(d.config.Port), includeResolved))
}

func (d *daemonServer) handleControlReviewShow(w http.ResponseWriter, r *http.Request) {
	var request showReviewRequest
	if !d.decodeControl(w, r, &request) {
		return
	}
	canonical, err := canonicalDocumentPath(request.Path)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	document, err := d.registeredReviewDocument(canonical)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	rendered, err := d.renderReviewDocument(document)
	if err != nil {
		log.Printf("render comments for CLI: %v", err)
		writeJSONErrorCode(w, http.StatusInternalServerError, "Could not render the document comments.", reviewCodeStateInvalid)
		return
	}
	stored, _ := d.reviews.snapshot(document.ID, document.Path)
	writeJSON(w, http.StatusOK, buildReviewShowResponse(stored, document, rendered, daemonBaseURL(d.config.Port), request.IncludeResolved))
}

func (d *daemonServer) handleControlReviewCommentAdd(w http.ResponseWriter, r *http.Request) {
	var request addReviewCommentRequest
	if !d.decodeControl(w, r, &request) {
		return
	}
	document, rendered, context, ok := d.reviewerMutationContext(w, request.Path)
	if !ok {
		return
	}
	var stored documentReview
	var comment reviewComment
	current, removed, err := d.updater.withCurrentDocument(document, func() error {
		var mutationErr error
		stored, comment, mutationErr = d.reviews.addCommentWithSelection(
			context, request.ExpectedRevision, request.ExpectedSourceHash, request.Body, request.BlockKey, request.Selection,
		)
		if mutationErr == nil {
			d.publishReviewChange(document.ID)
		}
		return mutationErr
	})
	if err != nil {
		writeReviewError(w, err)
		return
	}
	if !current {
		if removed {
			writeReviewError(w, newReviewError(http.StatusNotFound, reviewCodeRemoved, "Markdown file not found."))
		} else {
			writeReviewError(w, staleDocumentError())
		}
		return
	}
	d.writeReviewMutation(w, stored, document, rendered, &comment)
}

func (d *daemonServer) handleControlReviewCommentReply(w http.ResponseWriter, r *http.Request) {
	var request replyReviewCommentRequest
	if !d.decodeControl(w, r, &request) {
		return
	}
	document, rendered, context, ok := d.reviewerMutationContext(w, request.Path)
	if !ok {
		return
	}
	var stored documentReview
	var comment reviewComment
	current, removed, err := d.updater.withCurrentDocument(document, func() error {
		var mutationErr error
		stored, comment, mutationErr = d.reviews.replyToComment(
			context, request.ExpectedRevision, request.ExpectedSourceHash, request.CommentID, request.Body,
		)
		if mutationErr == nil {
			d.publishReviewChange(document.ID)
		}
		return mutationErr
	})
	if err != nil {
		writeReviewError(w, err)
		return
	}
	if !current {
		if removed {
			writeReviewError(w, newReviewError(http.StatusNotFound, reviewCodeRemoved, "Markdown file not found."))
		} else {
			writeReviewError(w, staleDocumentError())
		}
		return
	}
	d.writeReviewMutation(w, stored, document, rendered, &comment)
}

func (d *daemonServer) handleControlReviewCommentAddress(w http.ResponseWriter, r *http.Request) {
	var request addressReviewCommentRequest
	if !d.decodeControl(w, r, &request) {
		return
	}
	documents := d.updater.documentSnapshot()
	documentID, path, found := d.reviews.documentForComment(request.CommentID)
	if !found {
		writeReviewError(w, commentNotFoundError())
		return
	}
	var document *daemonDocument
	for _, candidate := range documents {
		if candidate.ID == documentID && candidate.Path == path {
			document = candidate
			break
		}
	}
	if document == nil {
		writeReviewError(w, newReviewError(http.StatusNotFound, reviewCodeNotRegistered, "Document not registered."))
		return
	}
	if document.removed {
		writeReviewError(w, newReviewError(http.StatusNotFound, reviewCodeRemoved, "Markdown file not found."))
		return
	}
	rendered, err := d.renderReviewDocument(document)
	if err != nil {
		writeJSONErrorCode(w, http.StatusInternalServerError, "Could not render the document comments.", reviewCodeStateInvalid)
		return
	}
	var stored documentReview
	var comment reviewComment
	current, removed, err := d.updater.withCurrentDocument(document, func() error {
		var mutationErr error
		stored, comment, mutationErr = d.reviews.addressComment(request.CommentID, request.Message)
		if mutationErr == nil {
			d.publishReviewChange(document.ID)
		}
		return mutationErr
	})
	if err != nil {
		writeReviewError(w, err)
		return
	}
	if !current {
		if removed {
			writeReviewError(w, newReviewError(http.StatusNotFound, reviewCodeRemoved, "Markdown file not found."))
		} else {
			writeReviewError(w, staleDocumentError())
		}
		return
	}
	d.writeReviewMutation(w, stored, document, rendered, &comment)
}

func (d *daemonServer) handleControlReviewCommentResolve(w http.ResponseWriter, r *http.Request) {
	d.handleReviewerCommentState(w, r, commentStatusResolved)
}

func (d *daemonServer) handleControlReviewCommentReopen(w http.ResponseWriter, r *http.Request) {
	d.handleReviewerCommentState(w, r, commentStatusOpen)
}

func (d *daemonServer) handleReviewerCommentState(w http.ResponseWriter, r *http.Request, target commentStatus) {
	var request commentReviewerRequest
	if !d.decodeControl(w, r, &request) {
		return
	}
	document, rendered, context, ok := d.reviewerMutationContext(w, request.Path)
	if !ok {
		return
	}
	var stored documentReview
	var comment reviewComment
	current, removed, err := d.updater.withCurrentDocument(document, func() error {
		var mutationErr error
		if target == commentStatusResolved {
			stored, comment, mutationErr = d.reviews.resolveComment(
				context, request.ExpectedRevision, request.ExpectedSourceHash, request.CommentID,
			)
		} else {
			stored, comment, mutationErr = d.reviews.reopenComment(
				context, request.ExpectedRevision, request.ExpectedSourceHash, request.CommentID,
			)
		}
		if mutationErr == nil {
			d.publishReviewChange(document.ID)
		}
		return mutationErr
	})
	if err != nil {
		writeReviewError(w, err)
		return
	}
	if !current {
		if removed {
			writeReviewError(w, newReviewError(http.StatusNotFound, reviewCodeRemoved, "Markdown file not found."))
		} else {
			writeReviewError(w, staleDocumentError())
		}
		return
	}
	d.writeReviewMutation(w, stored, document, rendered, &comment)
}

func (d *daemonServer) reviewerMutationContext(w http.ResponseWriter, id string) (*daemonDocument, renderedMarkdown, reviewDocumentContext, bool) {
	document, err := d.registeredReviewDocumentID(id)
	if err != nil {
		writeReviewError(w, err)
		return nil, renderedMarkdown{}, reviewDocumentContext{}, false
	}
	rendered, err := d.renderReviewDocument(document)
	if err != nil {
		log.Printf("render comment document: %v", err)
		writeJSONErrorCode(w, http.StatusInternalServerError, "Could not render the document comments.", reviewCodeStateInvalid)
		return nil, renderedMarkdown{}, reviewDocumentContext{}, false
	}
	context := reviewDocumentContext{
		DocumentID: document.ID, Path: document.Path, SourceHash: rendered.sourceHash, Blocks: rendered.blocks,
	}
	return document, rendered, context, true
}

func (d *daemonServer) registeredReviewDocumentID(identifier string) (*daemonDocument, error) {
	if !documentIDPattern.MatchString(identifier) {
		return nil, newReviewError(http.StatusNotFound, reviewCodeNotRegistered, "Document not registered.")
	}
	return d.registeredReviewDocument(identifier)
}

func (d *daemonServer) registeredReviewDocument(identifier string) (*daemonDocument, error) {
	if identifier == "" {
		return nil, newReviewError(http.StatusNotFound, reviewCodeNotRegistered, "Document not registered.")
	}
	document := d.updater.cloneDocument(identifier)
	if document == nil {
		return nil, newReviewError(http.StatusNotFound, reviewCodeNotRegistered, "Document not registered.")
	}
	if document.removed {
		return document, newReviewError(http.StatusNotFound, reviewCodeRemoved, "Markdown file not found.")
	}
	return document, nil
}

func (d *daemonServer) renderReviewDocument(document *daemonDocument) (renderedMarkdown, error) {
	return renderMarkdown(d.markdown, document.source, filepath.Base(document.Path), nil)
}

func (d *daemonServer) writeReviewMutation(w http.ResponseWriter, stored documentReview, document *daemonDocument, rendered renderedMarkdown, comment *reviewComment) {
	response := reviewMutationResponse{Review: buildBrowserReviewResponse(stored, document, rendered, daemonBaseURL(d.config.Port), true)}
	if comment != nil {
		comments := reviewCommentResponses([]reviewComment{*comment}, rendered.sourceHash, rendered.blocks, true)
		if len(comments) == 1 {
			response.Comment = &comments[0]
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (d *daemonServer) publishReviewChange(documentID string) {
	d.updater.feed.publish(markdownChange{Path: documentID, Kind: "review", Diff: ""})
}

func reviewIncludeResolved(w http.ResponseWriter, r *http.Request) (bool, bool) {
	value := r.URL.Query().Get("includeResolved")
	if value == "" {
		return false, true
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "includeResolved must be true or false")
		return false, false
	}
	return parsed, true
}

func writeReviewError(w http.ResponseWriter, err error) {
	var typed *reviewError
	if errors.As(err, &typed) {
		writeJSONErrorCode(w, typed.status, typed.Error(), typed.code)
		return
	}
	writeJSONError(w, http.StatusBadRequest, err.Error())
}
