package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const (
	// perPage is GitHub's max page size for list endpoints. Using the
	// max minimises round-trips for repos with many comments.
	perPage = 100

	// maxPagesGitHub caps pagination at 5000 comments. Real PRs almost
	// never exceed a few dozen; the cap exists so a buggy server or
	// pagination loop cannot spin forever. Hitting the cap is logged
	// as a warning so it is visible in the Action output.
	maxPagesGitHub = 50

	// maxBodyChars is a defensive cap below GitHub's documented
	// ~65,536-char comment body limit. CloudOracle's rendered Markdown
	// is typically <5KB, but the truncation guard prevents a 422
	// response if a future change makes the comment unexpectedly
	// large. Truncation is best-effort: it may strip the trailing
	// HTML marker, in which case the next push posts a fresh comment
	// rather than updating the truncated one.
	maxBodyChars = 60000

	// truncationSuffix is appended to a body that was cropped to fit
	// maxBodyChars. The visible "[truncated]" tells the reviewer the
	// comment was incomplete; engineering can investigate via the
	// Action logs.
	truncationSuffix = "...\n[truncated]"
)

// PostOrUpdateComment finds the most recent comment in the given PR
// whose body contains marker, and updates it; if no such comment
// exists, posts a new one. Returns the resulting comment ID, a
// "created" flag (true for a new comment, false for an update), and
// the first error encountered.
//
// The marker is a substring guaranteed to appear in CloudOracle-
// generated comments — typically an HTML comment like
// "<!-- cloudoracle-pr-v1 -->" placed at the end of the rendered
// Markdown. The marker contract is symmetric: every CloudOracle
// comment must include it, and any comment containing it is
// considered "ours" for the purpose of update-vs-post.
//
// If multiple comments match the marker (rare; possible if a previous
// integration left duplicates or a manual paste happened), this
// function picks the one with the most recent UpdatedAt and emits a
// slog.Warn — it does not delete the others, since deletion is
// destructive and out of scope.
//
// Body length is capped at 60,000 characters; oversize bodies are
// truncated with a "...[truncated]" suffix and a slog.Warn. Note
// that truncation can remove the trailing marker, in which case the
// next pr-check posts a fresh comment instead of updating the
// truncated one.
//
// Errors are wrapped with stable prefixes the caller can match on:
// "github: authentication failed", "github: ... not found",
// "github: validation failed", "github: server error", and
// "github: request failed". No retries are performed — the caller
// (Action wrapper in Hito 16.3) owns retry policy.
//
// PRs and issues share the same numbering on GitHub; the "issues"
// comments endpoint serves both, which is why the parameter is named
// prNumber even though we hit /issues/{n}/comments.
func (c *Client) PostOrUpdateComment(ctx context.Context, repo Repo, prNumber int, body, marker string) (int64, bool, error) {
	body = capBody(body)

	existing, err := c.listComments(ctx, repo, prNumber)
	if err != nil {
		return 0, false, err
	}

	if match, ok := pickMostRecentMatch(existing, marker); ok {
		if err := c.updateComment(ctx, repo, match.ID, body); err != nil {
			return 0, false, err
		}
		return match.ID, false, nil
	}

	id, err := c.postComment(ctx, repo, prNumber, body)
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// pickMostRecentMatch scans comments for ones whose body contains the
// marker and returns the one with the latest UpdatedAt. If more than
// one matches, a warning is logged: a single match is the steady
// state, multiple matches indicate either a manual duplication or
// drift in the marker convention.
func pickMostRecentMatch(comments []Comment, marker string) (Comment, bool) {
	var matches []Comment
	for _, c := range comments {
		if strings.Contains(c.Body, marker) {
			matches = append(matches, c)
		}
	}
	if len(matches) == 0 {
		return Comment{}, false
	}
	winner := matches[0]
	for _, m := range matches[1:] {
		if m.UpdatedAt.After(winner.UpdatedAt) {
			winner = m
		}
	}
	if len(matches) > 1 {
		slog.Warn("github: multiple comments match marker, updating most recent",
			"matches", len(matches),
			"winner_id", winner.ID,
			"marker", marker)
	}
	return winner, true
}

// capBody truncates a comment body that exceeds maxBodyChars,
// appending truncationSuffix so the reader knows the comment is
// incomplete. The combined length is exactly maxBodyChars (suffix
// included) — i.e. we crop the original to maxBodyChars - len(suffix)
// before appending. A warning is logged so operators can investigate.
func capBody(body string) string {
	if len(body) <= maxBodyChars {
		return body
	}
	keep := max(maxBodyChars-len(truncationSuffix), 0)
	slog.Warn("github: comment body exceeds size cap, truncating",
		"original_len", len(body),
		"max", maxBodyChars,
		"kept", keep)
	return body[:keep] + truncationSuffix
}

// listComments fetches every comment on the given issue/PR. GitHub
// uses cursor-style pagination with Link headers, but a per_page=100
// request also reports the count via the JSON array length: a short
// final page (or an empty one) signals end-of-results. Parsing only
// the array length keeps the implementation simple and avoids a Link
// header parser.
//
// The loop is hard-capped at maxPagesGitHub iterations (5000 comments)
// so a buggy server cannot spin forever. Hitting the cap is logged
// and the comments collected so far are returned — the caller still
// gets useful data, and the warning is visible in the Action output.
func (c *Client) listComments(ctx context.Context, repo Repo, prNumber int) ([]Comment, error) {
	var all []Comment
	subject := fmt.Sprintf("repo %s/%s or PR #%d", repo.Owner, repo.Name, prNumber)

	for page := 1; page <= maxPagesGitHub; page++ {
		batch, err := c.fetchCommentsPage(ctx, repo, prNumber, page, subject)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < perPage {
			return all, nil
		}
	}

	slog.Warn("github: pagination cap hit, returning collected comments",
		"max_pages", maxPagesGitHub,
		"comments", len(all))
	return all, nil
}

// fetchCommentsPage pulls a single page of issue comments. Split out
// of listComments so the request body close happens at function exit
// instead of being deferred inside a loop (which would leak HTTP
// connections until listComments itself returned).
func (c *Client) fetchCommentsPage(ctx context.Context, repo Repo, prNumber, page int, subject string) ([]Comment, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments?per_page=%d&page=%d",
		c.baseURL, repo.Owner, repo.Name, prNumber, perPage, page)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: request failed: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("github: request failed: reading body: %w", err)
	}
	if err := mapHTTPError(resp.StatusCode, body, subject); err != nil {
		return nil, err
	}

	var batch []Comment
	if err := json.Unmarshal(body, &batch); err != nil {
		return nil, fmt.Errorf("github: parsing list response: %w", err)
	}
	return batch, nil
}

// postComment creates a new comment under the given issue/PR and
// returns its ID. GitHub responds with 201 Created and the full
// comment object on success; we only need the ID for downstream
// referencing.
func (c *Client) postComment(ctx context.Context, repo Repo, prNumber int, body string) (int64, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments",
		c.baseURL, repo.Owner, repo.Name, prNumber)
	subject := fmt.Sprintf("repo %s/%s or PR #%d", repo.Owner, repo.Name, prNumber)

	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return 0, fmt.Errorf("github: marshalling comment: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("github: request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("github: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("github: request failed: reading body: %w", err)
	}
	if err := mapHTTPError(resp.StatusCode, respBody, subject); err != nil {
		return 0, err
	}

	var created Comment
	if err := json.Unmarshal(respBody, &created); err != nil {
		return 0, fmt.Errorf("github: parsing post response: %w", err)
	}
	return created.ID, nil
}

// updateComment replaces the body of an existing comment. The endpoint
// is /repos/{owner}/{repo}/issues/comments/{commentID} — note that
// the PR number is not part of the path: GitHub identifies comments
// globally by ID once you have one. We still pass repo so a 404 can
// be reported with the same shape as listComments and postComment.
func (c *Client) updateComment(ctx context.Context, repo Repo, commentID int64, body string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/comments/%d",
		c.baseURL, repo.Owner, repo.Name, commentID)
	subject := fmt.Sprintf("comment #%d on repo %s/%s", commentID, repo.Owner, repo.Name)

	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return fmt.Errorf("github: marshalling comment update: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("github: request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("github: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("github: request failed: reading body: %w", err)
	}
	return mapHTTPError(resp.StatusCode, respBody, subject)
}

// mapHTTPError translates a GitHub response status into a wrapped
// error with a stable prefix the caller can match on. 2xx returns
// nil. The mapping is deliberately coarse — callers that need the
// raw status or body can wrap a transport that captures them; for
// CloudOracle's purposes the prefix is what matters because the
// Action wrapper renders it to stderr.
func mapHTTPError(status int, body []byte, subject string) error {
	if status >= 200 && status < 300 {
		return nil
	}
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return fmt.Errorf("github: authentication failed (check GITHUB_TOKEN): %d %s", status, string(body))
	case status == http.StatusNotFound:
		return fmt.Errorf("github: %s not found", subject)
	case status == http.StatusUnprocessableEntity:
		return fmt.Errorf("github: validation failed: %s", string(body))
	case status >= 500:
		return fmt.Errorf("github: server error: %d %s", status, string(body))
	default:
		return fmt.Errorf("github: unexpected status %d: %s", status, string(body))
	}
}
