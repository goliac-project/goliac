package utils

import (
	"fmt"
	"regexp"
	"strings"
)

// Function to extract organization and repository from a GitHub URL (https or ssh)
func ExtractOrgRepo(urlstring string) (string, string, error) {
	// Parse URL

	// Regular expression to match both HTTPS and SSH GitHub URLs
	pattern := `^(https:\/\/|git@)?(github.com[:\/])([^\/]+)\/([^\/]+)([/])?`
	re := regexp.MustCompile(pattern)

	// Find matches
	matches := re.FindStringSubmatch(urlstring)
	if len(matches) == 6 {
		org := matches[3]
		repo := matches[4]
		repo = strings.TrimSuffix(repo, ".git")
		return org, repo, nil
	}

	return "", "", fmt.Errorf("invalid URL %s", urlstring)
}
