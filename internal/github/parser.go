// insternal/github/parser.go

package github

import (
	"errors"
	"net/url"
	"strings"
)

func ParseRepoURL(repoURL string) (string, string, error) {
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return "", "", errors.New("invalid URL")
	}

	// Example path: /golang/go
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")

	if len(parts) < 2 {
		return "", "", errors.New("invalid GitHub repo URL")
	}

	owner := parts[0]
	repo := parts[1]

	return owner, repo, nil
}