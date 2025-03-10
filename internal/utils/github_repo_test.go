package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGithubRepoParsing(t *testing.T) {
	t.Run("happy path: SSH format", func(t *testing.T) {
		url := "git@github.com:org/repo.git"
		org, repo, err := ExtractOrgRepo(url)
		assert.Nil(t, err)
		assert.Equal(t, "org", org)
		assert.Equal(t, "repo", repo)
	})
	t.Run("happy path: HTTPS format", func(t *testing.T) {
		url := "https://github.com/org/repo.git"
		org, repo, err := ExtractOrgRepo(url)
		assert.Nil(t, err)
		assert.Equal(t, "org", org)
		assert.Equal(t, "repo", repo)
	})
	t.Run("happy path: HTTPS format without .git", func(t *testing.T) {
		url := "https://github.com/org/repo"
		org, repo, err := ExtractOrgRepo(url)
		assert.Nil(t, err)
		assert.Equal(t, "org", org)
		assert.Equal(t, "repo", repo)
	})
	t.Run("happy path: HTTPS format without .git but with /", func(t *testing.T) {
		url := "https://github.com/org/repo/"
		org, repo, err := ExtractOrgRepo(url)
		assert.Nil(t, err)
		assert.Equal(t, "org", org)
		assert.Equal(t, "repo", repo)
	})
	t.Run("not happy path: invalid URL", func(t *testing.T) {
		url := "http://github.com"
		_, _, err := ExtractOrgRepo(url)
		assert.NotNil(t, err)
	})
}
