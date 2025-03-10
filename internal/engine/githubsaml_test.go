package engine

import (
	"context"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

type GithubSamlGitHubClient struct {
}

func NewGithubSamlGitHubClient() *GithubSamlGitHubClient {
	return &GithubSamlGitHubClient{}
}

func extractQueryName(query string) string {
	queryRegex := regexp.MustCompile(`query\s+(\w+)\(.*`)
	matches := queryRegex.FindStringSubmatch(query)
	if len(matches) == 2 {
		return matches[1]
	}
	return ""
}

func (c *GithubSamlGitHubClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	// extract query name
	queryName := extractQueryName(query)

	if queryName == "listSamlUsers" {
		return []byte(`{
			"data": {
				"organization": {
					"samlIdentityProvider": {
						"externalIdentities": {
							"edges": [
								{
									"node": {
										"guid": "guid1",
										"samlIdentity": {
											"nameId": "username1"
										},
										"user": {
											"login": "githubid1"
										}
									}
								},
								{
									"node": {
										"guid": "guid2",
										"samlIdentity": {
											"nameId": "username2"
										},
										"user": {
											"login": "githubid2"
										}
									}
								},
								{
									"node": {
										"guid": "guid3",
										"samlIdentity": {
											"nameId": "username3"
										},
										"user": {
											"login": "githubid3"
										}
									}
								},
								{
									"node": {
										"guid": "guid4",
										"samlIdentity": {
											"nameId": "username4"
										},
										"user": {
											"login": "githubid4"
										}
									}
								}
							],
							"pageInfo": {
								"hasNextPage": false,
								"endCursor": null
							},
							"totalCount": 4
						}
					}
				}
			}
		}`), nil
	}
	return nil, nil
}

func (c *GithubSamlGitHubClient) CallRestAPI(context.Context, string, string, string, map[string]interface{}, *string) ([]byte, error) {
	return nil, nil
}
func (c *GithubSamlGitHubClient) GetAccessToken(context.Context) (string, error) {
	return "accesstoken", nil
}
func (c *GithubSamlGitHubClient) GetAppSlug() string {
	return "foobar"
}

func TestLoadUsersFromGithubOrgSaml(t *testing.T) {

	// happy path
	t.Run("happy path: load users from Enterprise Github", func(t *testing.T) {
		client := NewGithubSamlGitHubClient()
		ctx := context.TODO()
		users, err := LoadUsersFromGithubOrgSaml(ctx, client, nil)
		assert.Nil(t, err)
		assert.Equal(t, 4, len(users))
	})
}
