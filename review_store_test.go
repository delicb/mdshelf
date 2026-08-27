package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReviewStoreMissingFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "reviews.json")
	store, err := newReviewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.file.Version != reviewStoreVersion || store.file.Documents == nil || len(store.file.Documents) != 0 {
		t.Fatalf("store = %#v", store.file)
	}
}

func TestReviewStoreLoadsCommentsWithoutReplies(t *testing.T) {
	store := newTestReviewStore(t)
	context := newTestReviewContext(t, "plain.md", "# Plain\n")
	if _, _, err := store.addComment(context, 0, context.SourceHash, "No replies yet.", ""); err != nil {
		t.Fatal(err)
	}
	reloaded, err := newReviewStore(store.path)
	if err != nil {
		t.Fatal(err)
	}
	document, found := reloaded.snapshot(context.DocumentID, context.Path)
	if !found || len(document.Comments) != 1 || document.Comments[0].Replies == nil || len(document.Comments[0].Replies) != 0 {
		t.Fatalf("reloaded = %#v, found=%v", document, found)
	}
}

func TestReviewStoreSaveLoadPreservesComments(t *testing.T) {
	store := newTestReviewStore(t)
	context := newTestReviewContext(t, "plan.md", "# Plan\n\nPersist this.\n")
	document, comment, err := store.addComment(context, 0, context.SourceHash, "Keep this state.", context.Blocks[1].Key)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err = store.addressComment(comment.ID, "Stored the state.")
	if err != nil {
		t.Fatal(err)
	}
	document, _, err = store.resolveComment(context, document.Revision, context.SourceHash, comment.ID)
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := newReviewStore(store.path)
	if err != nil {
		t.Fatal(err)
	}
	got, found := reloaded.snapshot(context.DocumentID, context.Path)
	if !found || !reflect.DeepEqual(got, document) {
		t.Fatalf("reloaded = %#v, found=%v, want %#v", got, found, document)
	}
}

func TestReviewStoreRepliesStayOneLevelDeep(t *testing.T) {
	store := newTestReviewStore(t)
	context := newTestReviewContext(t, "thread.md", "# Thread\n")
	document, comment, err := store.addComment(context, 0, context.SourceHash, "Root comment.", "")
	if err != nil {
		t.Fatal(err)
	}
	document, _, err = store.addressComment(comment.ID, "Agent reply.")
	if err != nil {
		t.Fatal(err)
	}
	document, comment, err = store.replyToComment(
		context, document.Revision, context.SourceHash, comment.ID, "Reviewer reply.",
	)
	if err != nil {
		t.Fatal(err)
	}
	if comment.Status != commentStatusOpen || len(comment.Replies) != 2 {
		t.Fatalf("thread = %#v", comment)
	}
	if comment.Replies[0].Author != replyAuthorAgent || comment.Replies[1].Author != replyAuthorReviewer {
		t.Fatalf("replies = %#v", comment.Replies)
	}
	_, _, err = store.replyToComment(
		context, document.Revision, context.SourceHash, comment.Replies[1].ID, "Nested reply.",
	)
	assertReviewErrorStatus(t, err, http.StatusNotFound, reviewCodeCommentMissing)

	document, _, err = store.resolveComment(context, document.Revision, context.SourceHash, comment.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.replyToComment(context, document.Revision, context.SourceHash, comment.ID, "Reply to resolved.")
	assertReviewErrorStatus(t, err, http.StatusConflict, reviewCodeTransition)
}

func TestReviewStoreSaveOrderIsStable(t *testing.T) {
	store := newTestReviewStore(t)
	root := t.TempDir()
	z := testReviewContextForPath(t, mustWriteFile(t, root, "z.md", "# Z\n"), "# Z\n")
	a := testReviewContextForPath(t, mustWriteFile(t, root, "a.md", "# A\n"), "# A\n")
	if _, _, err := store.addComment(z, 0, z.SourceHash, "Z", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.addComment(a, 0, a.SourceHash, "A", ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Index(data, []byte(a.Path)) > bytes.Index(data, []byte(z.Path)) {
		t.Fatalf("documents are not path-sorted:\n%s", data)
	}
	first := bytes.Clone(data)
	if err := saveReviewStoreFile(store.path, store.file); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("stable save changed bytes:\nfirst=%s\nsecond=%s", first, second)
	}
}

func TestReviewStoreRejectsCorruptData(t *testing.T) {
	for name, data := range map[string]string{
		"version":         `{"version":2,"documents":[]}`,
		"top-level field": `{"version":1,"documents":[],"extra":true}`,
		"nested field":    `{"version":1,"documents":[{"documentId":"000000000000000000000000","path":"/tmp/a.md","revision":0,"comments":[],"extra":true}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "reviews.json")
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := newReviewStore(path); err == nil {
				t.Fatal("corrupt comment store loaded")
			}
		})
	}
}

func TestReviewStoreValidationRejectsInvalidState(t *testing.T) {
	store := newTestReviewStore(t)
	context := newTestReviewContext(t, "state.md", "# State\n\nBody.\n")
	document, comment, err := store.addComment(context, 0, context.SourceHash, "Why?", "")
	if err != nil {
		t.Fatal(err)
	}
	valid := reviewStoreFile{Version: reviewStoreVersion, Documents: []documentReview{document}}

	tests := map[string]func(*reviewStoreFile){
		"duplicate document": func(file *reviewStoreFile) {
			file.Documents = append(file.Documents, cloneDocumentReview(file.Documents[0]))
		},
		"duplicate comment": func(file *reviewStoreFile) {
			file.Documents[0].Comments = append(file.Documents[0].Comments, cloneReviewComment(file.Documents[0].Comments[0]))
		},
		"invalid path":   func(file *reviewStoreFile) { file.Documents[0].Path = "relative.md" },
		"invalid hash":   func(file *reviewStoreFile) { file.Documents[0].Comments[0].BaseHash = "bad" },
		"invalid status": func(file *reviewStoreFile) { file.Documents[0].Comments[0].Status = "draft" },
		"invalid reply": func(file *reviewStoreFile) {
			file.Documents[0].Comments[0].Replies = []reviewReply{{
				ID: "bad", Body: "Reply", Author: replyAuthorReviewer, CreatedAt: time.Now().UTC(),
			}}
		},
		"invalid resolved state": func(file *reviewStoreFile) {
			file.Documents[0].Comments[0].Status = commentStatusResolved
			file.Documents[0].Comments[0].ResolvedAt = nil
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := cloneReviewStoreFile(valid)
			mutate(&candidate)
			if err := validateReviewStoreFile(candidate); err == nil {
				t.Fatalf("invalid state passed validation: %#v, comment=%s", candidate, comment.ID)
			}
		})
	}
}

func TestReviewStorePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions do not apply")
	}
	stateDir := filepath.Join(t.TempDir(), "state")
	store, err := newReviewStore(filepath.Join(stateDir, "reviews.json"))
	if err != nil {
		t.Fatal(err)
	}
	configureTestReviewStore(store)
	context := newTestReviewContext(t, "permissions.md", "# Permissions\n")
	if _, _, err := store.addComment(context, 0, context.SourceHash, "Check modes.", ""); err != nil {
		t.Fatal(err)
	}
	directoryInfo, err := os.Stat(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("state directory mode = %o", got)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Errorf("comments file mode = %o", got)
	}
}

func TestReviewStoreFailedSaveKeepsValidState(t *testing.T) {
	store := newTestReviewStore(t)
	context := newTestReviewContext(t, "failure.md", "# Failure\n")
	document, _, err := store.addComment(context, 0, context.SourceHash, "Before", "")
	if err != nil {
		t.Fatal(err)
	}
	store.save = func(string, reviewStoreFile) error { return errors.New("disk full") }
	if _, _, err := store.addComment(context, document.Revision, context.SourceHash, "After", ""); err == nil {
		t.Fatal("save failure did not fail the mutation")
	}
	memory, _ := store.snapshot(context.DocumentID, context.Path)
	if len(memory.Comments) != 1 || memory.Comments[0].Body != "Before" || memory.Revision != document.Revision {
		t.Fatalf("memory changed after failed save: %#v", memory)
	}
	reloaded, err := loadReviewStoreFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Documents[0].Comments) != 1 || reloaded.Documents[0].Comments[0].Body != "Before" {
		t.Fatalf("disk state changed after failed save: %#v", reloaded)
	}
}

func TestReviewStoreSurvivesDocumentRemoveAndReadd(t *testing.T) {
	stateDir := t.TempDir()
	path := mustWriteFile(t, t.TempDir(), "return.md", "# Return\n")
	d, err := newDaemonServer(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.close)
	document, _, err := d.updater.add(path)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := d.renderReviewDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	context := reviewDocumentContext{DocumentID: document.ID, Path: document.Path, SourceHash: rendered.sourceHash, Blocks: rendered.blocks}
	stored, _, err := d.reviews.addComment(context, 0, context.SourceHash, "Keep me.", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.updater.remove(path); err != nil {
		t.Fatal(err)
	}
	readded, added, err := d.updater.add(path)
	if err != nil || !added || readded.ID != document.ID {
		t.Fatalf("re-add = %#v, added=%v, err=%v", readded, added, err)
	}
	got, found := d.reviews.snapshot(readded.ID, readded.Path)
	if !found || got.Revision != stored.Revision || len(got.Comments) != 1 {
		t.Fatalf("comments after re-add = %#v, found=%v", got, found)
	}
}

func TestReviewStatusAndCounts(t *testing.T) {
	hash := strings.Repeat("a", 64)
	document := emptyDocumentReview(strings.Repeat("1", 24), "/tmp/status.md")
	if got := reviewStatusForDocument(document, hash, false); got != documentReviewNeedsReview {
		t.Fatalf("empty status = %q", got)
	}
	if got := reviewStatusForDocument(document, hash, true); got != documentReviewRemoved {
		t.Fatalf("removed status = %q", got)
	}
	document.Comments = []reviewComment{{BaseHash: hash}}
	if got := reviewStatusForDocument(document, hash, false); got != documentReviewComments {
		t.Fatalf("comment status = %q", got)
	}
	if got := reviewStatusForDocument(document, strings.Repeat("b", 64), false); got != documentReviewUpdated {
		t.Fatalf("updated status = %q", got)
	}

	store := newTestReviewStore(t)
	context := newTestReviewContext(t, "counts.md", "# Counts\n")
	store.file.Documents = []documentReview{{
		DocumentID: context.DocumentID, Path: context.Path,
		Comments: []reviewComment{{Status: commentStatusOpen}, {Status: commentStatusAddressed}, {Status: commentStatusResolved}},
	}}
	_, open := store.summary(context.DocumentID, context.Path, context.SourceHash, false)
	if open != 2 {
		t.Fatalf("open=%d", open)
	}
}

func TestReviewStoreCommentLimit(t *testing.T) {
	context := newTestReviewContext(t, "limits.md", "# Limits\n")
	store := newTestReviewStore(t)
	limited := emptyDocumentReview(context.DocumentID, context.Path)
	limited.Comments = make([]reviewComment, maxCommentsPerDocument)
	store.file.Documents = []documentReview{limited}
	_, _, err := store.addComment(context, 0, context.SourceHash, "One more", "")
	assertReviewErrorStatus(t, err, http.StatusRequestEntityTooLarge, reviewCodeLimit)
}

func TestReviewStoreReplyLimit(t *testing.T) {
	store := newTestReviewStore(t)
	context := newTestReviewContext(t, "reply-limit.md", "# Limit\n")
	_, comment, err := store.addComment(context, 0, context.SourceHash, "Root", "")
	if err != nil {
		t.Fatal(err)
	}
	store.file.Documents[0].Comments[0].Replies = make([]reviewReply, maxRepliesPerComment)
	_, _, err = store.addressComment(comment.ID, "One more")
	assertReviewErrorStatus(t, err, http.StatusRequestEntityTooLarge, reviewCodeLimit)
}

func TestReviewStoreConcurrentAddressMutations(t *testing.T) {
	store := newTestReviewStore(t)
	context := newTestReviewContext(t, "race.md", "# Race\n")
	_, comment, err := store.addComment(context, 0, context.SourceHash, "Change it.", "")
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errorsFound := make(chan error, 32)
	for index := range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _, err := store.addressComment(comment.ID, fmt.Sprintf("Reply %d", index))
			errorsFound <- err
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Error(err)
		}
	}
	stored, _ := store.snapshot(context.DocumentID, context.Path)
	comment = stored.Comments[0]
	if stored.Revision != 33 || comment.Status != commentStatusAddressed || len(comment.Replies) != 32 {
		t.Fatalf("concurrent result = %#v", stored)
	}
	for _, reply := range comment.Replies {
		if reply.Author != replyAuthorAgent || !strings.HasPrefix(reply.Body, "Reply ") {
			t.Fatalf("concurrent reply = %#v", reply)
		}
	}
}

func newTestReviewStore(t *testing.T) *reviewStore {
	t.Helper()
	store, err := newReviewStore(filepath.Join(t.TempDir(), "reviews.json"))
	if err != nil {
		t.Fatal(err)
	}
	configureTestReviewStore(store)
	return store
}

func configureTestReviewStore(store *reviewStore) {
	data := make([]byte, 12*(maxCommentsPerDocument+64))
	for index := range data {
		data[index] = byte(index)
	}
	store.random = bytes.NewReader(data)
	now := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	store.now = func() time.Time {
		result := now
		now = now.Add(time.Second)
		return result
	}
}

func newTestReviewContext(t *testing.T, name, source string) reviewDocumentContext {
	t.Helper()
	path := mustWriteFile(t, t.TempDir(), name, source)
	return testReviewContextForPath(t, path, source)
}

func testReviewContextForPath(t *testing.T, path, source string) reviewDocumentContext {
	t.Helper()
	canonical, err := canonicalDocumentPath(path)
	if err != nil {
		t.Fatal(err)
	}
	rendered := mustRenderReviewBlocks(t, source)
	return reviewDocumentContext{
		DocumentID: documentID(canonical), Path: canonical, SourceHash: rendered.sourceHash, Blocks: rendered.blocks,
	}
}

func assertReviewErrorStatus(t *testing.T, err error, status int, code string) {
	t.Helper()
	var typed *reviewError
	if !errors.As(err, &typed) || typed.status != status || typed.code != code {
		t.Fatalf("error = %#v, want status=%d code=%q", err, status, code)
	}
}
