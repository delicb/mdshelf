package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDaemonReviewUsesOpaqueDocumentID(t *testing.T) {
	d, document, rendered := newReviewHTTPFixture(t, "# Opaque\n\nBody.\n")
	response := daemonRequest(t, d.handler, http.MethodGet, reviewAPIPath("/api/review", document.ID, false), nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var payload browserReviewResponse
	decodeJSON(t, response, &payload)
	if payload.Document.Path != document.ID || payload.Document.SourceHash != rendered.sourceHash {
		t.Fatalf("document = %#v", payload.Document)
	}

	byPath := daemonRequest(t, d.handler, http.MethodGet, reviewAPIPath("/api/review", document.Path, false), nil)
	assertReviewHTTPError(t, byPath, http.StatusNotFound, reviewCodeNotRegistered)
	_ = byPath.Body.Close()
}

func TestDaemonReviewRejectsUnknownRemovedAndConflictingDocuments(t *testing.T) {
	d, document, rendered := newReviewHTTPFixture(t, "# Conflict\n")
	unknown := daemonRequest(t, d.handler, http.MethodGet, reviewAPIPath("/api/review", strings.Repeat("a", 24), false), nil)
	assertReviewHTTPError(t, unknown, http.StatusNotFound, reviewCodeNotRegistered)
	_ = unknown.Body.Close()

	staleSource := postControl(t, d.handler, "/api/control/review/comments/add", addReviewCommentRequest{
		Path: document.ID, ExpectedRevision: 0, ExpectedSourceHash: strings.Repeat("f", 64), Body: "Old source",
	})
	assertReviewHTTPError(t, staleSource, http.StatusConflict, reviewCodeStaleDocument)
	_ = staleSource.Body.Close()

	added := postControl(t, d.handler, "/api/control/review/comments/add", addReviewCommentRequest{
		Path: document.ID, ExpectedRevision: 0, ExpectedSourceHash: rendered.sourceHash, Body: "First",
	})
	_ = added.Body.Close()
	staleRevision := postControl(t, d.handler, "/api/control/review/comments/add", addReviewCommentRequest{
		Path: document.ID, ExpectedRevision: 0, ExpectedSourceHash: rendered.sourceHash, Body: "Old revision",
	})
	assertReviewHTTPError(t, staleRevision, http.StatusConflict, reviewCodeStaleReview)
	_ = staleRevision.Body.Close()

	if err := os.Remove(document.Path); err != nil {
		t.Fatal(err)
	}
	d.updater.reconcile(document.ID)
	removed := daemonRequest(t, d.handler, http.MethodGet, reviewAPIPath("/api/review", document.ID, false), nil)
	assertReviewHTTPError(t, removed, http.StatusNotFound, reviewCodeRemoved)
	_ = removed.Body.Close()
}

func TestCommentWriteGuardRejectsChangedAndRemovedSnapshots(t *testing.T) {
	d, document, _ := newReviewHTTPFixture(t, "# Guard\n")
	called := false
	if err := os.WriteFile(document.Path, []byte("# Changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.updater.reconcile(document.ID)
	current, removed, err := d.updater.withCurrentDocument(document, func() error {
		called = true
		return nil
	})
	if err != nil || current || removed || called {
		t.Fatalf("changed guard: current=%v removed=%v called=%v err=%v", current, removed, called, err)
	}

	currentSnapshot := d.updater.cloneDocument(document.ID)
	if _, err := d.updater.remove(document.Path); err != nil {
		t.Fatal(err)
	}
	current, removed, err = d.updater.withCurrentDocument(currentSnapshot, func() error {
		called = true
		return nil
	})
	if err != nil || current || !removed || called {
		t.Fatalf("removed guard: current=%v removed=%v called=%v err=%v", current, removed, called, err)
	}
}

func TestCommentSaveSerializesDocumentUpdateAndRemoval(t *testing.T) {
	for _, operation := range []string{"update", "remove"} {
		t.Run(operation, func(t *testing.T) {
			d, document, rendered := newReviewHTTPFixture(t, "# Serialize\n")
			originalSave := d.reviews.save
			saveStarted := make(chan struct{})
			releaseSave := make(chan struct{})
			d.reviews.save = func(path string, stored reviewStoreFile) error {
				close(saveStarted)
				<-releaseSave
				return originalSave(path, stored)
			}

			encoded, err := json.Marshal(addReviewCommentRequest{
				Path: document.ID, ExpectedRevision: 0, ExpectedSourceHash: rendered.sourceHash, Body: "Publish me.",
			})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/control/review/comments/add", bytes.NewReader(encoded))
			request.Host = "localhost:7332"
			request.RemoteAddr = "127.0.0.1:41000"
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			requestDone := make(chan struct{})
			go func() {
				d.handler.ServeHTTP(recorder, request)
				close(requestDone)
			}()
			<-saveStarted

			operationStarted := make(chan struct{})
			operationDone := make(chan error, 1)
			switch operation {
			case "update":
				if err := os.WriteFile(document.Path, []byte("# Updated\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				go func() {
					close(operationStarted)
					d.updater.reconcile(document.ID)
					operationDone <- nil
				}()
			case "remove":
				go func() {
					close(operationStarted)
					_, err := d.updater.remove(document.Path)
					operationDone <- err
				}()
			}
			<-operationStarted

			select {
			case err := <-operationDone:
				t.Fatalf("%s completed during comment save: %v", operation, err)
			case <-time.After(50 * time.Millisecond):
			}
			close(releaseSave)
			<-requestDone
			if response := recorder.Result(); response.StatusCode != http.StatusOK {
				t.Fatalf("save status = %d, body = %s", response.StatusCode, readBody(t, response))
			} else {
				_ = response.Body.Close()
			}
			if err := <-operationDone; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAddressSerializesDocumentRemoval(t *testing.T) {
	d, document, rendered := newReviewHTTPFixture(t, "# Address\n")
	_, comment, err := d.reviews.addComment(
		reviewDocumentContext{DocumentID: document.ID, Path: document.Path, SourceHash: rendered.sourceHash, Blocks: rendered.blocks},
		0, rendered.sourceHash, "Root", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	originalSave := d.reviews.save
	saveStarted := make(chan struct{})
	releaseSave := make(chan struct{})
	d.reviews.save = func(path string, stored reviewStoreFile) error {
		close(saveStarted)
		<-releaseSave
		return originalSave(path, stored)
	}

	encoded, err := json.Marshal(addressReviewCommentRequest{CommentID: comment.ID, Message: "Done"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/control/review/comments/address", bytes.NewReader(encoded))
	request.Host = "localhost:7332"
	request.RemoteAddr = "127.0.0.1:41000"
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	requestDone := make(chan struct{})
	go func() {
		d.handler.ServeHTTP(recorder, request)
		close(requestDone)
	}()
	<-saveStarted

	removeDone := make(chan error, 1)
	go func() {
		_, err := d.updater.remove(document.Path)
		removeDone <- err
	}()
	select {
	case err := <-removeDone:
		t.Fatalf("remove completed during address save: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseSave)
	<-requestDone
	if response := recorder.Result(); response.StatusCode != http.StatusOK {
		t.Fatalf("address status = %d, body = %s", response.StatusCode, readBody(t, response))
	} else {
		_ = response.Body.Close()
	}
	if err := <-removeDone; err != nil {
		t.Fatal(err)
	}
}

func TestDaemonCommentPublishesAddressResolvesAndReopens(t *testing.T) {
	d, document, rendered := newReviewHTTPFixture(t, "# Flow\n\nChange this.\n")
	startRevision := changeFeedRevision(d.updater.feed)
	add := postControl(t, d.handler, "/api/control/review/comments/add", addReviewCommentRequest{
		Path: document.ID, ExpectedRevision: 0, ExpectedSourceHash: rendered.sourceHash,
		Body: "Change this text.", BlockKey: rendered.blocks[1].Key,
	})
	var added reviewMutationResponse
	decodeReviewMutation(t, add, &added)
	if added.Review.Revision != 1 || added.Comment == nil || added.Comment.Status != commentStatusOpen {
		t.Fatalf("add response = %#v", added)
	}
	commentID := added.Comment.ID
	assertOneReviewEvent(t, d.updater.feed, startRevision, document.ID)

	show := postControl(t, d.handler, "/api/control/review/show", showReviewRequest{Path: document.Path})
	var shown reviewShowResponse
	decodeJSON(t, show, &shown)
	_ = show.Body.Close()
	if len(shown.Comments) != 1 || shown.Comments[0].ID != commentID {
		t.Fatalf("published comments = %#v", shown.Comments)
	}

	eventRevision := changeFeedRevision(d.updater.feed)
	address := postControl(t, d.handler, "/api/control/review/comments/address", addressReviewCommentRequest{CommentID: commentID, Message: "Updated it."})
	var addressed reviewMutationResponse
	decodeReviewMutation(t, address, &addressed)
	if addressed.Comment == nil || addressed.Comment.Status != commentStatusAddressed || len(addressed.Comment.Replies) != 1 {
		t.Fatalf("address response = %#v", addressed)
	}
	if reply := addressed.Comment.Replies[0]; reply.Author != replyAuthorAgent || reply.Body != "Updated it." {
		t.Fatalf("address reply = %#v", reply)
	}
	assertOneReviewEvent(t, d.updater.feed, eventRevision, document.ID)

	eventRevision = changeFeedRevision(d.updater.feed)
	resolve := postControl(t, d.handler, "/api/control/review/comments/resolve", commentReviewerRequest{
		Path: document.ID, ExpectedRevision: 2, ExpectedSourceHash: rendered.sourceHash, CommentID: commentID,
	})
	var resolved reviewMutationResponse
	decodeReviewMutation(t, resolve, &resolved)
	if resolved.Comment == nil || resolved.Comment.Status != commentStatusResolved {
		t.Fatalf("resolve response = %#v", resolved)
	}
	assertOneReviewEvent(t, d.updater.feed, eventRevision, document.ID)

	reopen := postControl(t, d.handler, "/api/control/review/comments/reopen", commentReviewerRequest{
		Path: document.ID, ExpectedRevision: 3, ExpectedSourceHash: rendered.sourceHash, CommentID: commentID,
	})
	var reopened reviewMutationResponse
	decodeReviewMutation(t, reopen, &reopened)
	if reopened.Comment == nil || reopened.Comment.Status != commentStatusOpen {
		t.Fatalf("reopen response = %#v", reopened)
	}
}

func TestDaemonReviewerReplyCreatesThread(t *testing.T) {
	d, document, rendered := newReviewHTTPFixture(t, "# Thread\n")
	add := postControl(t, d.handler, "/api/control/review/comments/add", addReviewCommentRequest{
		Path: document.ID, ExpectedRevision: 0, ExpectedSourceHash: rendered.sourceHash, Body: "Root comment.",
	})
	var added reviewMutationResponse
	decodeReviewMutation(t, add, &added)
	if added.Comment == nil {
		t.Fatal("add response has no comment")
	}

	reply := postControl(t, d.handler, "/api/control/review/comments/reply", replyReviewCommentRequest{
		Path: document.ID, ExpectedRevision: 1, ExpectedSourceHash: rendered.sourceHash,
		CommentID: added.Comment.ID, Body: "Reviewer reply.",
	})
	var replied reviewMutationResponse
	decodeReviewMutation(t, reply, &replied)
	if replied.Comment == nil || replied.Comment.Status != commentStatusOpen || len(replied.Comment.Replies) != 1 {
		t.Fatalf("reply response = %#v", replied)
	}
	if got := replied.Comment.Replies[0]; got.Author != replyAuthorReviewer || got.Body != "Reviewer reply." {
		t.Fatalf("reply = %#v", got)
	}
}

func TestDaemonReviewAnchorMovesWithDocument(t *testing.T) {
	d, document, rendered := newReviewHTTPFixture(t, "# One\n\nMove me.\n")
	add := postControl(t, d.handler, "/api/control/review/comments/add", addReviewCommentRequest{
		Path: document.ID, ExpectedRevision: 0, ExpectedSourceHash: rendered.sourceHash,
		Body: "Track this.", BlockKey: rendered.blocks[1].Key,
	})
	var added reviewMutationResponse
	decodeReviewMutation(t, add, &added)
	originalKey := added.Comment.CurrentBlockKey

	newSource := "# One\n\n# Two\n\nMove me.\n"
	if err := os.WriteFile(document.Path, []byte(newSource), 0o644); err != nil {
		t.Fatal(err)
	}
	d.updater.reconcile(document.ID)
	show := postControl(t, d.handler, "/api/control/review/show", showReviewRequest{Path: document.Path, IncludeResolved: true})
	var moved reviewShowResponse
	decodeJSON(t, show, &moved)
	_ = show.Body.Close()
	if len(moved.Comments) != 1 || moved.Comments[0].Outdated || moved.Comments[0].CurrentLocation.StartLine != 5 || moved.Comments[0].CurrentBlockKey == nil || *moved.Comments[0].CurrentBlockKey == *originalKey {
		t.Fatalf("moved comment = %#v", moved.Comments)
	}
}

func TestDaemonRenderReturnsPublicBlockMetadata(t *testing.T) {
	d, document, rendered := newReviewHTTPFixture(t, "# Metadata\n\nBody.\n")
	response := daemonRequest(t, d.handler, http.MethodGet, apiPath("/api/render", document.ID), nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("render status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var payload map[string]any
	decodeJSON(t, response, &payload)
	if payload["sourceHash"] != rendered.sourceHash {
		t.Fatalf("sourceHash = %#v", payload["sourceHash"])
	}
	blocks, ok := payload["blocks"].([]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("blocks = %#v", payload["blocks"])
	}
	for _, raw := range blocks {
		block := raw.(map[string]any)
		if len(block) != 4 || block["key"] == nil || block["kind"] == nil || block["startLine"] == nil || block["endLine"] == nil {
			t.Fatalf("public block shape = %#v", block)
		}
	}
}

func TestDaemonFilesIncludesCommentSummary(t *testing.T) {
	d, document, rendered := newReviewHTTPFixture(t, "# Summary\n")
	add := postControl(t, d.handler, "/api/control/review/comments/add", addReviewCommentRequest{
		Path: document.ID, ExpectedRevision: 0, ExpectedSourceHash: rendered.sourceHash, Body: "Change it.",
	})
	_ = add.Body.Close()
	response := daemonRequest(t, d.handler, http.MethodGet, "/api/files", nil)
	defer response.Body.Close()
	var payload struct {
		Files []struct {
			ReviewStatus documentReviewStatus `json:"reviewStatus"`
			OpenComments int                  `json:"openComments"`
		} `json:"files"`
	}
	decodeJSON(t, response, &payload)
	if len(payload.Files) != 1 || payload.Files[0].ReviewStatus != documentReviewComments || payload.Files[0].OpenComments != 1 {
		t.Fatalf("file summary = %#v", payload.Files)
	}
}

func TestDaemonCommentSaveFailureDoesNotPublishEvent(t *testing.T) {
	d, document, rendered := newReviewHTTPFixture(t, "# Failure\n")
	d.reviews.save = func(string, reviewStoreFile) error { return errors.New("disk full") }
	revision := changeFeedRevision(d.updater.feed)
	response := postControl(t, d.handler, "/api/control/review/comments/add", addReviewCommentRequest{
		Path: document.ID, ExpectedRevision: 0, ExpectedSourceHash: rendered.sourceHash, Body: "Must not save.",
	})
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	_ = response.Body.Close()
	if changeFeedRevision(d.updater.feed) != revision {
		t.Fatal("failed save published a review event")
	}
}

func newReviewHTTPFixture(t *testing.T, source string) (*daemonServer, *daemonDocument, renderedMarkdown) {
	t.Helper()
	stateDir := t.TempDir()
	path := mustWriteFile(t, t.TempDir(), "review.md", source)
	d, err := newDaemonServer(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.close)
	configureTestReviewStore(d.reviews)
	document, _, err := d.updater.add(path)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := d.renderReviewDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	return d, document, rendered
}

func reviewAPIPath(endpoint, documentID string, includeResolved bool) string {
	query := url.Values{"path": {documentID}}
	query.Set("includeResolved", map[bool]string{true: "true", false: "false"}[includeResolved])
	return endpoint + "?" + query.Encode()
}

func decodeReviewMutation(t *testing.T, response *http.Response, target *reviewMutationResponse) {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	decodeJSON(t, response, target)
}

func assertReviewHTTPError(t *testing.T, response *http.Response, status int, code string) {
	t.Helper()
	if response.StatusCode != status {
		t.Fatalf("status = %d, want %d, body=%s", response.StatusCode, status, readBody(t, response))
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != code {
		t.Fatalf("code = %q, want %q", payload.Code, code)
	}
}

func changeFeedRevision(feed *changeFeed) uint64 {
	feed.mu.Lock()
	defer feed.mu.Unlock()
	return feed.revision
}

func assertOneReviewEvent(t *testing.T, feed *changeFeed, since uint64, documentID string) {
	t.Helper()
	feed.mu.Lock()
	batch, _ := feed.batchAfter(since)
	feed.mu.Unlock()
	if len(batch.Changes) != 1 || batch.Changes[0].Path != documentID || batch.Changes[0].Kind != "review" {
		t.Fatalf("events = %#v", batch.Changes)
	}
}
