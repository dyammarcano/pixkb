package epoch

import (
	"context"
	"fmt"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Committer commits the canonical OKF bundle after each epoch, returning the
// commit SHA so it can be recorded in the epoch row.
type Committer interface {
	Commit(ctx context.Context, msg string) (sha string, err error)
}

type gitCommitter struct{ dir string }

// NewGitCommitter returns a Committer that stages everything under dir and
// commits it with go-git, initializing the repository on first use. Empty
// commits are allowed so an epoch with zero changes still records a marker.
func NewGitCommitter(dir string) Committer { return &gitCommitter{dir: dir} }

func (g *gitCommitter) Commit(_ context.Context, msg string) (string, error) {
	repo, err := git.PlainOpen(g.dir)
	if err != nil {
		repo, err = git.PlainInit(g.dir, false)
		if err != nil {
			return "", fmt.Errorf("git init %s: %w", g.dir, err)
		}
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("git worktree: %w", err)
	}
	if err := wt.AddGlob("."); err != nil {
		return "", fmt.Errorf("git add: %w", err)
	}
	sig := &object.Signature{Name: "pixkb", Email: "pixkb@local", When: time.Now()}
	h, err := wt.Commit(msg, &git.CommitOptions{Author: sig, AllowEmptyCommits: true})
	if err != nil {
		return "", fmt.Errorf("git commit: %w", err)
	}
	return h.String(), nil
}
