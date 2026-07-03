package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	git "github.com/go-git/go-git/v5"

	"pixkb/internal/okf"
)

type gitSource struct {
	repos     []RepoSpec
	mirrorDir string
}

// NewGitSource builds a Source that reads pre-staged local repository mirrors
// under mirrorDir/<name> and emits one Repo overview concept per repo (README +
// HEAD commit). Air-gap friendly: it never clones — mirrors are staged ahead of
// time; a repo with no local mirror is skipped.
func NewGitSource(repos []RepoSpec, mirrorDir string) Source {
	return &gitSource{repos: repos, mirrorDir: mirrorDir}
}

func (s *gitSource) Name() string { return "git" }

func (s *gitSource) Fetch(_ context.Context) ([]okf.Concept, error) {
	var out []okf.Concept
	for _, rs := range s.repos {
		dir := filepath.Join(s.mirrorDir, rs.Name)
		if _, err := os.Stat(dir); err != nil {
			continue // mirror not staged; skip
		}
		head := ""
		if repo, err := git.PlainOpen(dir); err == nil {
			if ref, err := repo.Head(); err == nil {
				head = ref.Hash().String()
			}
		}
		readme := readReadme(dir)
		body := fmt.Sprintf("# %s/%s\n\nRepository overview (staged mirror).\n\n%s", rs.Owner, rs.Name, readme)
		out = append(out, okf.Concept{
			ID:          "repos/" + rs.Name + ".md",
			Type:        "Repo",
			Title:       rs.Owner + "/" + rs.Name,
			Description: firstLine(readme),
			Resource:    "https://github.com/" + rs.Owner + "/" + rs.Name,
			Tags:        []string{"repo", rs.Name},
			Language:    "en",
			SourceURI:   fmt.Sprintf("git:%s/%s@%s", rs.Owner, rs.Name, head),
			Body:        body,
			ContentSHA:  okf.ComputeSHA(body),
		})
	}
	return out, nil
}

func readReadme(dir string) string {
	for _, name := range []string{"README.md", "README", "readme.md", "Readme.md"} {
		if data, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			s := string(data)
			if len(s) > 4000 {
				s = s[:4000]
			}
			return s
		}
	}
	return ""
}
