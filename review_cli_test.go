package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDaemonAddJSONReturnsOneObject(t *testing.T) {
	path := mustWriteFile(t, t.TempDir(), "json.md", "# JSON\n")
	canonical, err := canonicalDocumentPath(path)
	if err != nil {
		t.Fatal(err)
	}
	deps := daemonCommandDeps{
		health: func() error { return nil },
		start:  func() error { return errors.New("must not start") },
		control: func(endpoint string, request, response any) error {
			if endpoint != "add" || reflect.ValueOf(request).FieldByName("Path").String() != canonical {
				t.Fatalf("endpoint=%q request=%#v", endpoint, request)
			}
			value := reflect.ValueOf(response).Elem()
			value.FieldByName("Added").SetBool(true)
			value.FieldByName("Document").Set(reflect.ValueOf(daemonDocumentResponse{
				ID: documentID(canonical), Path: canonical, Title: "JSON", URL: daemonBaseURL(defaultDaemonPort) + "/#/" + documentID(canonical),
			}))
			return nil
		},
		sleep: func(time.Duration) {}, now: time.Now,
	}
	var output bytes.Buffer
	if err := runDaemonCommandWithDeps("add", []string{"--json", path}, &output, deps); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		SchemaVersion int                    `json:"schemaVersion"`
		Added         bool                   `json:"added"`
		Document      daemonDocumentResponse `json:"document"`
	}
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	if err := decoder.Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		t.Fatalf("add output contains more than one JSON value: %v", err)
	}
	if payload.SchemaVersion != 1 || !payload.Added || payload.Document.Path != canonical || payload.Document.Removed {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestReviewShowDoesNotStartDaemonAndRequiresFeature(t *testing.T) {
	path := mustWriteFile(t, t.TempDir(), "show.md", "# Show\n")
	controlCalled := false
	deps := reviewCommandDeps{
		health: func() error { return errDaemonReviewsMissing },
		control: func(string, any, any) error {
			controlCalled = true
			return nil
		},
	}
	err := runReviewCommandWithDeps([]string{"show", "--json", path}, &bytes.Buffer{}, &bytes.Buffer{}, deps)
	if !errors.Is(err, errDaemonReviewsMissing) || controlCalled {
		t.Fatalf("error=%v controlCalled=%v", err, controlCalled)
	}
}

func TestReviewShowCanonicalizesAliasesAndFiltersResolved(t *testing.T) {
	root := t.TempDir()
	path := mustWriteFile(t, root, "notes/review.md", "# Review\n")
	alias := filepath.Join(root, "notes", "..", "notes", "review.md")
	canonical, err := canonicalDocumentPath(path)
	if err != nil {
		t.Fatal(err)
	}
	var gotRequest showReviewRequest
	deps := reviewCommandDeps{
		health: func() error { return nil },
		control: func(endpoint string, request, response any) error {
			if endpoint != "review/show" {
				t.Fatalf("endpoint = %q", endpoint)
			}
			gotRequest = request.(showReviewRequest)
			*response.(*reviewShowResponse) = testReviewShowResponse(canonical)
			return nil
		},
	}
	var output bytes.Buffer
	if err := runReviewCommandWithDeps([]string{"show", "--json", "--include-resolved", alias}, &output, &bytes.Buffer{}, deps); err != nil {
		t.Fatal(err)
	}
	if gotRequest.Path != canonical || !gotRequest.IncludeResolved {
		t.Fatalf("show request = %#v", gotRequest)
	}
	var payload reviewShowResponse
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SchemaVersion != 1 || len(payload.Comments) != 1 || payload.Comments[0].CurrentBlockKey == nil {
		t.Fatalf("show payload = %#v", payload)
	}
}

func TestReviewShowDefaultOutputUsesSharedStableMarkdown(t *testing.T) {
	path := mustWriteFile(t, t.TempDir(), "plain.md", "# Plain\n")
	canonical, err := canonicalDocumentPath(path)
	if err != nil {
		t.Fatal(err)
	}
	response := testReviewShowResponse(canonical)
	deps := reviewCommandDeps{
		health: func() error { return nil },
		control: func(_ string, _ any, target any) error {
			*target.(*reviewShowResponse) = response
			return nil
		},
	}
	var first, second bytes.Buffer
	if err := runReviewCommandWithDeps([]string{"show", path}, &first, &bytes.Buffer{}, deps); err != nil {
		t.Fatal(err)
	}
	if err := runReviewCommandWithDeps([]string{"show", path}, &second, &bytes.Buffer{}, deps); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatal("plain Markdown output is not stable")
	}
	for _, field := range []string{
		canonical, response.Document.URL, "Current source hash",
		response.Comments[0].ID, "Comment status: open", "Heading path: Storage",
		"Original line range: 4-5", "Current line range: 7-8", "Original text", "Comment text", "Agent response",
	} {
		if !strings.Contains(first.String(), field) {
			t.Errorf("Markdown does not contain %q:\n%s", field, first.String())
		}
	}
}

func TestReviewAddressValidatesArgumentsAndReturnsUpdatedComment(t *testing.T) {
	for name, args := range map[string][]string{
		"missing id":      {"address", "--message", "Done"},
		"missing message": {"address", "comment_00112233445566778899aabb"},
		"empty message":   {"address", "--message", "  ", "comment_00112233445566778899aabb"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := runReviewCommandWithDeps(args, &bytes.Buffer{}, &bytes.Buffer{}, reviewCommandDeps{}); err == nil {
				t.Fatal("invalid address arguments succeeded")
			}
		})
	}

	comment := testReviewShowResponse("/tmp/review.md").Comments[0]
	comment.Status = commentStatusAddressed
	comment.Replies = append(comment.Replies, reviewReplyResponse{
		ID: "reply_aabbccddeeff001122334455", Body: "Updated storage.", Author: replyAuthorAgent, CreatedAt: time.Now().UTC(),
	})
	deps := reviewCommandDeps{
		health: func() error { return nil },
		control: func(endpoint string, request, response any) error {
			if endpoint != "review/comments/address" || request.(addressReviewCommentRequest).Message != "Updated storage." {
				t.Fatalf("endpoint=%q request=%#v", endpoint, request)
			}
			response.(*reviewMutationResponse).Comment = &comment
			return nil
		},
	}
	var output bytes.Buffer
	if err := runReviewCommandWithDeps([]string{"address", "--json", "--message", "Updated storage.", comment.ID}, &output, &bytes.Buffer{}, deps); err != nil {
		t.Fatal(err)
	}
	var got reviewCommentResponse
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != commentStatusAddressed || len(got.Replies) != 2 || got.Replies[1].Body != "Updated storage." {
		t.Fatalf("comment = %#v", got)
	}

	deps.control = func(string, any, any) error { return errors.New("only open or addressed comments can be addressed") }
	if err := runReviewCommandWithDeps([]string{"address", "--message", "Again", comment.ID}, &bytes.Buffer{}, &bytes.Buffer{}, deps); err == nil || !strings.Contains(err.Error(), "open or addressed") {
		t.Fatalf("resolved rejection = %v", err)
	}
}

func TestReviewAndAddHelpFormsSucceed(t *testing.T) {
	forms := [][]string{
		{"add", "--help"},
		{"review", "--help"},
		{"review", "show", "--help"},
		{"review", "address", "--help"},
	}
	for _, args := range forms {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run(args, &stdout, &stderr); err != nil {
				t.Fatalf("run(%q): %v", args, err)
			}
			if !strings.Contains(stdout.String(), "Usage:") || stderr.Len() != 0 {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestDaemonHealthFeatureDetectionKeepsOldHealthCompatible(t *testing.T) {
	original := daemonHTTPClient
	t.Cleanup(func() { daemonHTTPClient = original })
	features := []string(nil)
	daemonHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		body, err := json.Marshal(daemonHealthResponse{Service: "mdshelf-daemon", Protocol: daemonProtocol, PID: 42, Features: features})
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})}
	if err := checkDaemonHealth(); err != nil {
		t.Fatalf("old health check = %v", err)
	}
	if err := checkDaemonReviewHealth(); !errors.Is(err, errDaemonReviewsMissing) {
		t.Fatalf("review health without feature = %v", err)
	}
	features = []string{reviewFeature}
	if err := checkDaemonReviewHealth(); err != nil {
		t.Fatalf("review health with feature = %v", err)
	}
}

func testReviewShowResponse(path string) reviewShowResponse {
	currentKey := "ffeeddccbbaa998877665544"
	return reviewShowResponse{
		SchemaVersion: 1,
		Document: reviewDocumentResponse{
			ID: "00112233445566778899aabb", Path: path, Title: "Review",
			URL: daemonBaseURL(defaultDaemonPort) + "/#/00112233445566778899aabb", SourceHash: strings.Repeat("b", 64),
			ReviewStatus: documentReviewComments,
		},
		Comments: []reviewCommentResponse{{
			ID: "comment_00112233445566778899aabb", Body: "Comment text", Status: commentStatusOpen,
			BaseHash:        strings.Repeat("a", 64),
			Anchor:          &reviewAnchorResponse{StartLine: 4, EndLine: 5, HeadingPath: []string{"Storage"}, Quote: "Original text"},
			CurrentLocation: &sourceLocation{StartLine: 7, EndLine: 8}, CurrentBlockKey: &currentKey,
			Replies: []reviewReplyResponse{{
				ID: "reply_00112233445566778899aabb", Body: "Agent response", Author: replyAuthorAgent, CreatedAt: time.Now().UTC(),
			}},
		}},
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
