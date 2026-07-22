package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"pixkb/internal/okf"
)

// issuesSource ingests OPEN GitHub issues from operator-declared official repos
// as concepts. It hits the live GitHub REST API, so — unlike the pre-staged
// mirror sources — it is TOLERANT of failure: a repo that cannot be fetched
// (offline, rate-limited, 404) is logged and skipped, never returned as an error,
// so a scheduled or air-gapped gather still succeeds. Set GITHUB_TOKEN for a
// higher rate limit.
type issuesSource struct {
	repos   []string
	baseURL string
	client  *http.Client
}

// NewIssuesSource builds a Source over the given "owner/repo" list (from
// official_sources.issues). baseURL defaults to the public GitHub API.
func NewIssuesSource(repos []string) Source {
	return &issuesSource{
		repos:   repos,
		baseURL: "https://api.github.com",
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *issuesSource) Name() string { return "github-issues" }

// ghIssue is the subset of the GitHub issue object we ingest. PullRequest is
// non-nil for pull requests (the issues endpoint returns them too) — we skip
// those so only real issues become concepts.
type ghIssue struct {
	Number      int              `json:"number"`
	Title       string           `json:"title"`
	Body        string           `json:"body"`
	HTMLURL     string           `json:"html_url"`
	State       string           `json:"state"`
	PullRequest *json.RawMessage `json:"pull_request"`
}

func (s *issuesSource) Fetch(ctx context.Context) ([]okf.Concept, error) {
	var out []okf.Concept
	for _, repo := range s.repos {
		cs, err := s.fetchRepo(ctx, strings.TrimSpace(repo))
		if err != nil {
			slog.Warn("github-issues: skipped a repo", "repo", repo, "err", err)
			continue
		}
		out = append(out, cs...)
	}
	return out, nil
}

func (s *issuesSource) fetchRepo(ctx context.Context, repo string) ([]okf.Concept, error) {
	if repo == "" || !strings.Contains(repo, "/") {
		return nil, fmt.Errorf("issues repo %q must be owner/name", repo)
	}
	url := fmt.Sprintf("%s/repos/%s/issues?state=open&per_page=100", s.baseURL, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	var issues []ghIssue
	if err := json.Unmarshal(body, &issues); err != nil {
		return nil, fmt.Errorf("parse issues for %s: %w", repo, err)
	}
	slug := slugify(repo)
	var out []okf.Concept
	for _, is := range issues {
		if is.PullRequest != nil {
			continue // the issues endpoint also returns PRs; skip them
		}
		title := fmt.Sprintf("#%d %s", is.Number, is.Title)
		md := strings.ToValidUTF8(fmt.Sprintf("# %s\n\nSource: %s\n\n%s\n", title, is.HTMLURL, is.Body), "")
		out = append(out, okf.Concept{
			ID:          fmt.Sprintf("issues/%s-%d.md", slug, is.Number),
			Type:        "Issue",
			Title:       title,
			Description: firstLine(is.Body),
			Tags:        []string{"issue"},
			Language:    "en",
			SourceURI:   is.HTMLURL,
			Body:        md,
			ContentSHA:  okf.ComputeSHA(md),
		})
	}
	return out, nil
}
