package main

import (
	"fmt"
	"io"
	"strings"
)

func buildReviewShowResponse(stored documentReview, document *daemonDocument, rendered renderedMarkdown, baseURL string, includeResolved bool) reviewShowResponse {
	return reviewShowResponse{
		SchemaVersion: reviewAPISchemaVersion,
		Document: reviewDocumentResponse{
			ID:           document.ID,
			Path:         displayDocumentPath(document.Path),
			Title:        document.title,
			URL:          baseURL + "/#/" + document.ID,
			SourceHash:   rendered.sourceHash,
			ReviewStatus: reviewStatusForDocument(stored, rendered.sourceHash, document.removed),
		},
		Comments: reviewCommentResponses(stored.Comments, rendered.sourceHash, rendered.blocks, includeResolved),
	}
}

func buildBrowserReviewResponse(stored documentReview, document *daemonDocument, rendered renderedMarkdown, baseURL string, includeResolved bool) browserReviewResponse {
	return browserReviewResponse{
		SchemaVersion: reviewAPISchemaVersion,
		Document: reviewDocumentResponse{
			ID:           document.ID,
			Path:         document.ID,
			Title:        document.title,
			URL:          baseURL + "/#/" + document.ID,
			SourceHash:   rendered.sourceHash,
			ReviewStatus: reviewStatusForDocument(stored, rendered.sourceHash, document.removed),
		},
		Revision: stored.Revision,
		Comments: reviewCommentResponses(stored.Comments, rendered.sourceHash, rendered.blocks, includeResolved),
	}
}

func reviewCommentResponses(comments []reviewComment, currentHash string, blocks []markdownBlock, includeResolved bool) []reviewCommentResponse {
	responses := make([]reviewCommentResponse, 0, len(comments))
	for _, comment := range comments {
		if comment.Status == commentStatusResolved && !includeResolved {
			continue
		}
		location, currentBlockKey, outdated := matchBlockAnchor(comment.BaseHash, currentHash, comment.Anchor, blocks)
		replies := make([]reviewReplyResponse, len(comment.Replies))
		for index, reply := range comment.Replies {
			replies[index] = reviewReplyResponse{
				ID: reply.ID, Body: reply.Body, Author: reply.Author, CreatedAt: reply.CreatedAt,
			}
		}
		response := reviewCommentResponse{
			ID: comment.ID, Body: comment.Body, Status: comment.Status, BaseHash: comment.BaseHash,
			Outdated: outdated, CurrentLocation: location, CurrentBlockKey: currentBlockKey, Replies: replies,
		}
		if comment.Anchor != nil {
			response.Anchor = &reviewAnchorResponse{
				BlockKey: comment.Anchor.BlockKey, Kind: comment.Anchor.Kind,
				StartLine: comment.Anchor.StartLine, EndLine: comment.Anchor.EndLine,
				HeadingPath: append([]string(nil), comment.Anchor.HeadingPath...), Quote: comment.Anchor.Quote,
			}
		}
		responses = append(responses, response)
	}
	return responses
}

func formatReviewMarkdown(w io.Writer, response reviewShowResponse) error {
	if _, err := fmt.Fprintf(w, "# MDShelf comments: %s\n\n", response.Document.Title); err != nil {
		return err
	}
	for _, field := range [][2]string{
		{"Document path", response.Document.Path},
		{"Document URL", response.Document.URL},
		{"Current source hash", response.Document.SourceHash},
	} {
		if _, err := fmt.Fprintf(w, "- %s: %s\n", field[0], field[1]); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, "\n## Comments\n"); err != nil {
		return err
	}
	if len(response.Comments) == 0 {
		_, err := io.WriteString(w, "\nNo comments.\n")
		return err
	}
	for _, comment := range response.Comments {
		if _, err := fmt.Fprintf(w, "\n### %s\n\n", comment.ID); err != nil {
			return err
		}
		fields := [][2]string{
			{"Comment ID", comment.ID},
			{"Comment status", string(comment.Status)},
			{"Heading path", "Whole document"},
			{"Original line range", "Whole document"},
		}
		if comment.Anchor != nil {
			fields[2][1] = strings.Join(comment.Anchor.HeadingPath, " > ")
			if fields[2][1] == "" {
				fields[2][1] = "Document root"
			}
			fields[3][1] = formatLineRange(comment.Anchor.StartLine, comment.Anchor.EndLine)
		}
		if comment.CurrentLocation != nil {
			fields = append(fields, [2]string{"Current line range", formatLineRange(comment.CurrentLocation.StartLine, comment.CurrentLocation.EndLine)})
		}
		if comment.Outdated {
			fields = append(fields, [2]string{"Outdated", "yes"})
		}
		for _, field := range fields {
			if _, err := fmt.Fprintf(w, "- %s: %s\n", field[0], field[1]); err != nil {
				return err
			}
		}
		quote := "Whole document"
		if comment.Anchor != nil {
			quote = comment.Anchor.Quote
		}
		if _, err := fmt.Fprintf(w, "\nOriginal quote:\n\n%s\n\nComment body:\n\n%s\n", quote, comment.Body); err != nil {
			return err
		}
		for _, reply := range comment.Replies {
			if _, err := fmt.Fprintf(w, "\n%s reply (%s):\n\n%s\n", strings.ToUpper(string(reply.Author[:1]))+string(reply.Author[1:]), reply.ID, reply.Body); err != nil {
				return err
			}
		}
	}
	return nil
}

func formatLineRange(start, end int) string {
	if start == end {
		return fmt.Sprint(start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}
