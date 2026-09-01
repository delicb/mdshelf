package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveUpdatesRememberMarkdownDiffsAndIgnoreOtherFiles(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "note.md", "# Note\n\nold\n")
	mustWriteFile(t, root, "ignore.txt", "old\n")
	app := mustNewTestApp(t, root)

	mustWriteFile(t, root, "note.md", "# Note\n\nnew\n")
	app.updates.recordAllChanges()
	batch := waitForChange(t, app.updates, 0)
	if batch.Revision != 1 || len(batch.Changes) != 1 {
		t.Fatalf("first batch = %#v, want one change at revision 1", batch)
	}
	change := batch.Changes[0]
	if change.Path != "note.md" || change.Kind != "updated" {
		t.Errorf("change = %#v, want updated note.md", change)
	}
	for _, fragment := range []string{"--- a/note.md", "+++ b/note.md", "-old", "+new"} {
		if !strings.Contains(change.Diff, fragment) {
			t.Errorf("diff does not contain %q:\n%s", fragment, change.Diff)
		}
	}

	mustWriteFile(t, root, "ignore.txt", "new\n")
	mustWriteFile(t, root, "note.md", "# Note\n\nnewer\n")
	app.updates.recordAllChanges()
	batch = waitForChange(t, app.updates, batch.Revision)
	if batch.Revision != 2 || len(batch.Changes) != 1 {
		t.Fatalf("second batch = %#v, want one change at revision 2", batch)
	}
	if batch.Changes[0].Path != "note.md" {
		t.Errorf("tracked non-Markdown change: %#v", batch.Changes[0])
	}
}

func TestLiveUpdatesTrackAddedAndRemovedMarkdown(t *testing.T) {
	root := t.TempDir()
	app := mustNewTestApp(t, root)

	mustWriteFile(t, root, "guides/new.MarkDown", "# New\n")
	app.updates.recordAllChanges()
	added := waitForChange(t, app.updates, 0)
	if len(added.Changes) != 1 || added.Changes[0].Kind != "added" || added.Changes[0].Path != "guides/new.MarkDown" {
		t.Fatalf("added batch = %#v", added)
	}
	if !strings.Contains(added.Changes[0].Diff, "+# New") {
		t.Errorf("added diff does not contain new content:\n%s", added.Changes[0].Diff)
	}

	if err := os.Remove(filepath.Join(root, "guides", "new.MarkDown")); err != nil {
		t.Fatalf("remove Markdown file: %v", err)
	}
	app.updates.recordAllChanges()
	removed := waitForChange(t, app.updates, added.Revision)
	if len(removed.Changes) != 1 || removed.Changes[0].Kind != "removed" || removed.Changes[0].Path != "guides/new.MarkDown" {
		t.Fatalf("removed batch = %#v", removed)
	}
	if !strings.Contains(removed.Changes[0].Diff, "-# New") {
		t.Errorf("removed diff does not contain old content:\n%s", removed.Changes[0].Diff)
	}
}

func TestWatchEndpointReturnsRememberedChanges(t *testing.T) {
	root := t.TempDir()
	app := mustNewTestApp(t, root)
	app.updates.feed.publish(markdownChange{Path: "note.md", Kind: "updated", Diff: "saved diff"})

	response := request(t, app.Handler(), http.MethodGet, "/api/watch?since=0", nil)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("watch status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	var batch changeBatch
	decodeJSON(t, response, &batch)
	if batch.Revision != 1 || len(batch.Changes) != 1 || batch.Changes[0].Diff != "saved diff" {
		t.Fatalf("watch batch = %#v", batch)
	}
}

func TestWatchEndpointRejectsInvalidRevision(t *testing.T) {
	handler := mustNewHandler(t, t.TempDir())
	for _, target := range []string{"/api/watch", "/api/watch?since=-1", "/api/watch?since=nope"} {
		response := request(t, handler, http.MethodGet, target, nil)
		body := readBody(t, response)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("GET %s status = %d, want %d; body = %s", target, response.StatusCode, http.StatusBadRequest, body)
		}
	}
}

func TestWatchEndpointStopsWhenClientLeaves(t *testing.T) {
	app := mustNewTestApp(t, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/watch?since=0", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()

	app.Handler().ServeHTTP(recorder, request)
	if recorder.Body.Len() != 0 {
		t.Errorf("canceled watch response body = %q, want empty", recorder.Body.String())
	}
}

func waitForChange(t *testing.T, updates *liveUpdates, since uint64) changeBatch {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	batch := updates.feed.wait(ctx, since)
	if ctx.Err() != nil {
		t.Fatal("timed out waiting for Markdown change")
	}
	return batch
}

func mustNewTestApp(t *testing.T, root string) *app {
	t.Helper()
	app, err := newAppWithWatcher(root, false)
	if err != nil {
		t.Fatalf("newAppWithWatcher(%q, false): %v", root, err)
	}
	t.Cleanup(app.Close)
	return app
}
