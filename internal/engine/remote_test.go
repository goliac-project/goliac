package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/goliac-project/goliac/internal/github"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

func GetGithubGraphqlSchema() (string, error) {
	response, err := http.Get("https://docs.github.com/public/schema.docs.graphql")
	if err != nil {
		return "", err
	} else {
		defer response.Body.Close()
		content, err := io.ReadAll(response.Body)
		return string(content), err
	}
}

type MockGithubClient struct {
	cursorValue    string
	cursorPosition int
}

type GraphQLResult struct {
	Data map[string]interface{} `mapstructure:"data" json:"data"`
}

func extractVariable(name string, args ast.ArgumentList, variables map[string]interface{}) string {
	value := ""
	for _, a := range args {
		if a.Name == name {
			value = a.Value.String()
			if len(value) > 0 && value[0] == '$' {
				if variables[value[1:]] == nil {
					value = ""
				} else {
					value = variables[value[1:]].(string)
				}
			}
			return value
		}
	}
	return value
}

func hasChild(childname string, children ast.SelectionSet) (bool, *ast.Field) {
	for _, c := range children {
		switch s := c.(type) {
		case *ast.Field:
			if s.Name == childname {
				return true, s
			}
			//		case *ast.InlineFragment:
			//		case *ast.FragmentSpread:
		}
	}
	return false, nil
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

/*
 * Returns:
 * - nodes
 * - hasNextPage
 * - endCursor
 * - totalCount
 */
func (m *MockGithubClient) reposNodes(first, after string, args ast.ArgumentList, children ast.SelectionSet, variables map[string]interface{}, maxToFake int) ([]map[string]interface{}, bool, *string, int) {
	data := make([]map[string]interface{}, 0)

	iFirst, err := strconv.Atoi(first)
	if err != nil {
		iFirst = 0
	}
	iAfter := 0
	if after == m.cursorValue {
		iAfter = m.cursorPosition
	}

	searchName, _ := hasChild("name", children)
	searchArchived, _ := hasChild("isArchived", children)
	searchPrivate, _ := hasChild("visibility", children)

	index := iAfter
	totalCount := 0
	hasNext := true
	var endCursor *string
	r := (randStringRunes(12))
	endCursor = &r
	for totalCount = 0; totalCount < iFirst; totalCount++ {
		block := make(map[string]interface{})
		if searchName {
			block["name"] = fmt.Sprintf("repo_%d", index)
		}
		if searchArchived {
			block["isArchived"] = index%3 == 0 // let's pretend each 3 repo is an archive repo
		}
		if searchPrivate {
			if index%10 == 0 { // let's pretend each 10 repo is a private repo
				block["visibility"] = "private"
			} else {
				block["visibility"] = "public"
			}
		}
		index++
		if index > maxToFake { // let's pretend we have maxToFake repos
			hasNext = false
			endCursor = nil
			break
		}
		data = append(data, block)
	}

	m.cursorValue = r
	m.cursorPosition = index

	return data, hasNext, endCursor, totalCount
}

/*
 * Returns:
 * - nodes
 * - hasNextPage
 * - endCursor
 * - totalCount
 */
func (m *MockGithubClient) reposEdges(first, after string, args ast.ArgumentList, children ast.SelectionSet, variables map[string]interface{}, maxToFake int) ([]map[string]interface{}, bool, *string, int) {
	data := make([]map[string]interface{}, 0)

	iFirst, err := strconv.Atoi(first)
	if err != nil {
		iFirst = 0
	}
	iAfter := 0
	if after == m.cursorValue {
		iAfter = m.cursorPosition
	}

	searchPermission, _ := hasChild("permission", children)
	searchNode, nodeField := hasChild("node", children)

	index := iAfter
	totalCount := 0
	hasNext := true
	var endCursor *string
	r := (randStringRunes(12))
	endCursor = &r
	for totalCount = 0; totalCount < iFirst; totalCount++ {
		block := make(map[string]interface{})
		if searchPermission {
			block["permission"] = "push"
		}
		if searchNode {
			node := make(map[string]interface{})
			if c, _ := hasChild("name", nodeField.SelectionSet); c {
				node["name"] = fmt.Sprintf("repo_%d", index)
			}
			if c, _ := hasChild("id", nodeField.SelectionSet); c {
				node["id"] = fmt.Sprintf("id_%d", index)
			}
			block["node"] = node
		}
		index++
		if index > maxToFake { // let's pretend we have maxToFake repos
			hasNext = false
			endCursor = nil
			break
		}
		data = append(data, block)
	}

	m.cursorValue = r
	m.cursorPosition = index

	return data, hasNext, endCursor, totalCount
}

func (m *MockGithubClient) repositories(args ast.ArgumentList, children ast.SelectionSet, variables map[string]interface{}) map[string]interface{} {
	data := make(map[string]interface{})
	first := extractVariable("first", args, variables)
	after := extractVariable("after", args, variables)

	var hasNextPage bool
	var endCursor *string
	var totalCount int
	if c, s := hasChild("nodes", children); c {
		data["nodes"], hasNextPage, endCursor, totalCount = m.reposNodes(first, after, s.Arguments, s.SelectionSet, variables, 133)
	}
	if c, _ := hasChild("pageInfo", children); c {
		block := make(map[string]interface{})
		block["hasNextPage"] = hasNextPage
		if endCursor == nil {
			block["endCursor"] = nil
		} else {
			block["endCursor"] = *endCursor
		}

		data["pageInfo"] = block
	}
	if c, _ := hasChild("totalCount", children); c {
		data["totalCount"] = totalCount
	}

	return data
}

/*
 * Returns:
 * - nodes
 * - hasNextPage
 * - endCursor
 * - totalCount
 */
func (m *MockGithubClient) teamsNodes(first, after string, args ast.ArgumentList, children ast.SelectionSet, variables map[string]interface{}) ([]map[string]interface{}, bool, *string, int) {
	data := make([]map[string]interface{}, 0)

	iFirst, err := strconv.Atoi(first)
	if err != nil {
		iFirst = 0
	}
	iAfter := 0
	if after == m.cursorValue {
		iAfter = m.cursorPosition
	}

	searchName, _ := hasChild("name", children)
	searchSlug, _ := hasChild("slug", children)

	index := iAfter
	totalCount := 0
	hasNext := true
	var endCursor *string
	r := (randStringRunes(12))
	endCursor = &r
	for totalCount = 0; totalCount < iFirst; totalCount++ {
		block := make(map[string]interface{})
		if searchName {
			block["name"] = fmt.Sprintf("team_%d", index)
		}
		if searchSlug {
			block["slug"] = fmt.Sprintf("slug-%d", index)
		}
		index++
		if index > 122 { // let's pretend we have 133 teams
			hasNext = false
			endCursor = nil
			break
		}
		data = append(data, block)
	}

	m.cursorValue = r
	m.cursorPosition = index

	return data, hasNext, endCursor, totalCount
}

func (m *MockGithubClient) teams(args ast.ArgumentList, children ast.SelectionSet, variables map[string]interface{}) map[string]interface{} {
	data := make(map[string]interface{})
	first := extractVariable("first", args, variables)
	after := extractVariable("after", args, variables)

	var hasNextPage bool
	var endCursor *string
	var totalCount int
	if c, s := hasChild("nodes", children); c {
		data["nodes"], hasNextPage, endCursor, totalCount = m.teamsNodes(first, after, s.Arguments, s.SelectionSet, variables)
	}
	if c, _ := hasChild("pageInfo", children); c {
		block := make(map[string]interface{})
		block["hasNextPage"] = hasNextPage
		if endCursor == nil {
			block["endCursor"] = nil
		} else {
			block["endCursor"] = *endCursor
		}

		data["pageInfo"] = block
	}
	if c, _ := hasChild("totalCount", children); c {
		data["totalCount"] = totalCount
	}

	return data
}

func (m *MockGithubClient) teamrepositories(args ast.ArgumentList, children ast.SelectionSet, variables map[string]interface{}) map[string]interface{} {
	data := make(map[string]interface{})
	first := extractVariable("first", args, variables)
	after := extractVariable("after", args, variables)

	var hasNextPage bool
	var endCursor *string
	var totalCount int
	if c, s := hasChild("edges", children); c {
		data["edges"], hasNextPage, endCursor, totalCount = m.reposEdges(first, after, s.Arguments, s.SelectionSet, variables, 2)
	}
	if c, _ := hasChild("pageInfo", children); c {
		block := make(map[string]interface{})
		block["hasNextPage"] = hasNextPage
		if endCursor == nil {
			block["endCursor"] = nil
		} else {
			block["endCursor"] = *endCursor
		}

		data["pageInfo"] = block
	}
	if c, _ := hasChild("totalCount", children); c {
		data["totalCount"] = totalCount
	}

	return data
}

func (m *MockGithubClient) team(args ast.ArgumentList, children ast.SelectionSet, variables map[string]interface{}) map[string]interface{} {
	data := make(map[string]interface{})
	//	slug := extractVariable("slug", args, variables)

	if c, s := hasChild("repositories", children); c {
		data["repositories"] = m.teamrepositories(s.Arguments, s.SelectionSet, variables)
	}

	return data
}
func (m *MockGithubClient) organization(args ast.ArgumentList, children ast.SelectionSet, variables map[string]interface{}) map[string]interface{} {
	data := make(map[string]interface{})
	//login := extractVariable("login", args, variables)

	if c, s := hasChild("repositories", children); c {
		data["repositories"] = m.repositories(s.Arguments, s.SelectionSet, variables)
	}
	if c, s := hasChild("teams", children); c {
		data["teams"] = m.teams(s.Arguments, s.SelectionSet, variables)
	}
	if c, s := hasChild("team", children); c {
		data["team"] = m.team(s.Arguments, s.SelectionSet, variables)
	}
	return data
}

func (m *MockGithubClient) GetAppSlug() string {
	return "mock-github-client"
}

func (m *MockGithubClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {

	doc, err := parser.ParseQuery(&ast.Source{Input: query})

	// Check for parsing error.
	if err != nil {
		return nil, err
	}

	result := GraphQLResult{
		Data: make(map[string]interface{}),
	}

	// Print the parsed query document.
	for _, op := range doc.Operations {
		if op.Operation == "query" {
			if c, s := hasChild("organization", op.SelectionSet); c {
				result.Data["organization"] = m.organization(s.Arguments, s.SelectionSet, variables)
			}
		}
	}

	j, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}

	return j, nil
}

func (m *MockGithubClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	// /repos/myorg/"+repository+"/teams
	if strings.HasPrefix(endpoint, "/repos/myorg/repo_") {
		if strings.HasSuffix(endpoint, "/variables") {
			return []byte(`{"total_count": 2, "variables": [{"name": "VAR1", "value": "value1"}, {"name": "VAR2", "value": "value2"}]}`), nil
		}
		if strings.HasSuffix(endpoint, "/secrets") {
			return []byte(`{"total_count": 2, "secrets": [{"name": "VAR1", "value": "value1"}, {"name": "VAR2", "value": "value2"}]}`), nil
		}
		if strings.HasSuffix(endpoint, "/environments") {
			return []byte(`{"total_count": 2, "environments": [{"id": 1, "name": "production", "node_id": "123", "protection_rules": [{"id": 1, "type": "required_reviewers", "reviewer_teams": ["team1"]}]}, {"id": 2, "name": "staging", "node_id": "456", "protection_rules": []}]}`), nil
		}
		// we still pretend we have 133 teams, cf L263
		repoSuffix := strings.TrimPrefix(endpoint, "/repos/myorg/repo_")
		repoIdStr := strings.Split(repoSuffix, "/")[0]
		repoId, err := strconv.Atoi(repoIdStr)
		if err != nil {
			return nil, err
		}
		return []byte(fmt.Sprintf(`[{"name":"team_1","permission":"push","slug":"slug-%d"},{"name":"team_2","permission":"push","slug":"slug-2"}]`, repoId)), nil
	}
	if strings.HasSuffix(endpoint, "installations") {

		type Installation struct {
			TotalCount    int `json:"total_count"`
			Installations []struct {
				Id      int    `json:"id"`
				AppId   int    `json:"app_id"`
				Name    string `json:"name"`
				AppSlug string `json:"app_slug"`
			} `json:"installations"`
		}

		installation := Installation{
			TotalCount: 0,
		}
		body, err := json.Marshal(&installation)
		if err == nil {
			return body, nil
		}
	}
	return nil, nil
}

func (m *MockGithubClient) GetAccessToken(ctx context.Context) (string, error) {
	return "", nil
}
func (m *MockGithubClient) CreateJWT() (string, error) {
	return "", nil
}

func TestRemoteRepository(t *testing.T) {

	// happy path
	t.Run("happy path: load remote repositories", func(t *testing.T) {
		// MockGithubClient doesn't support concurrent access
		client := MockGithubClient{}

		remoteImpl := NewGoliacRemoteImpl(&client, "myorg", true, true)

		ctx := context.TODO()
		repositories, _, err := remoteImpl.loadRepositories(ctx)
		assert.Nil(t, err)
		assert.Equal(t, 133, len(repositories))
		assert.Equal(t, false, repositories["repo_1"].BoolProperties["archived"])
		assert.Equal(t, true, repositories["repo_3"].BoolProperties["archived"])
		assert.Equal(t, "public", repositories["repo_1"].Visibility)
		assert.Equal(t, "private", repositories["repo_10"].Visibility)
	})
	t.Run("happy path: load remote teams", func(t *testing.T) {
		// MockGithubClient doesn't support concurrent access
		client := MockGithubClient{}

		remoteImpl := NewGoliacRemoteImpl(&client, "myorg", true, true)

		ctx := context.TODO()
		teams, _, err := remoteImpl.loadTeams(ctx)
		assert.Nil(t, err)
		assert.Equal(t, 122, len(teams))
		assert.Equal(t, "team_1", teams["slug-1"].Name)
	})

	t.Run("happy path: load remote team's repos", func(t *testing.T) {
		// MockGithubClient doesn't support concurrent access
		client := MockGithubClient{}

		remoteImpl := NewGoliacRemoteImpl(&client, "myorg", true, true)

		ctx := context.TODO()
		repo_0 := &GithubRepository{
			Name: "repo_0",
			Id:   1,
		}

		repos, err := remoteImpl.loadTeamRepos(ctx, repo_0)
		assert.Nil(t, err)
		assert.Equal(t, 2, len(repos))
		assert.Equal(t, "WRITE", repos["slug-0"].Permission)
	})

	t.Run("happy path: load remote teams and team's repos", func(t *testing.T) {
		// MockGithubClient doesn't support concurrent access
		client := MockGithubClient{}

		remoteImpl := NewGoliacRemoteImpl(&client, "myorg", true, true)

		ctx := context.TODO()
		err := remoteImpl.Load(ctx, false)
		assert.Nil(t, err)
		assert.Equal(t, 122, len(remoteImpl.teams))
		assert.Equal(t, 1, len(remoteImpl.teamRepos["slug-1"]))
	})
}

type GitHubClientIsEnterpriseMock struct {
	results map[string][]byte
	err     error
}

func (g *GitHubClientIsEnterpriseMock) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte(""), nil
}
func (g *GitHubClientIsEnterpriseMock) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	return g.results[endpoint], g.err
}
func (g *GitHubClientIsEnterpriseMock) GetAccessToken(ctx context.Context) (string, error) {
	return "", nil
}
func (g *GitHubClientIsEnterpriseMock) CreateJWT() (string, error) {
	return "", nil
}
func (g *GitHubClientIsEnterpriseMock) GetAppSlug() string {
	return ""
}

func TestIsEnterprise(t *testing.T) {

	t.Run("test GHES", func(t *testing.T) {
		type ResultSet struct {
			mock     github.GitHubClient
			expected bool
		}

		tests := []ResultSet{
			{
				mock: &GitHubClientIsEnterpriseMock{
					results: map[string][]byte{
						"/api/v3":      []byte(`{"github_services_sha": "SOME_SHA_VALUE_HERE","installed_version": "3.12.1"}`),
						"/orgs/foobar": []byte(`{"two_factor_requirement_enabled": false,"plan": {"name":"unknown"}}`),
					},
					err: nil,
				},
				expected: true,
			},
			{
				mock: &GitHubClientIsEnterpriseMock{
					results: map[string][]byte{
						"/api/v3":      []byte(`{"github_services_sha": "SOME_SHA_VALUE_HERE","installed_version": "3.10"}`),
						"/orgs/foobar": []byte(`{"two_factor_requirement_enabled": false,"plan": {"name":"unknown"}}`),
					},
					err: nil,
				},
				expected: false,
			},
			{
				mock: &GitHubClientIsEnterpriseMock{
					results: map[string][]byte{
						"/api/v3":      []byte(`{"github_services_sha": "SOME_SHA_VALUE_HERE","installed_version": "3.11"}`),
						"/orgs/foobar": []byte(`{"two_factor_requirement_enabled": false,"plan": {"name":"unknown"}}`),
					},
					err: nil,
				},
				expected: true,
			},
			{
				mock: &GitHubClientIsEnterpriseMock{
					results: map[string][]byte{
						"/api/v3":      []byte(`{"github_services_sha": "SOME_SHA_VALUE_HERE","installed_version": "3.11"}`),
						"/orgs/foobar": []byte(`{"two_factor_requirement_enabled": false,"plan": {"name":"unknown"}}`),
					},
					err: fmt.Errorf("an error occured"),
				},
				expected: false,
			},
		}

		for _, set := range tests {
			ctx := context.TODO()
			res := isEnterprise(ctx, "foobar", set.mock)

			assert.Equal(t, set.expected, res)

		}
	})

	t.Run("test Enterprise", func(t *testing.T) {
		type ResultSet struct {
			mock     github.GitHubClient
			expected bool
		}

		tests := []ResultSet{
			{
				mock: &GitHubClientIsEnterpriseMock{
					results: map[string][]byte{
						"/api/v3":      []byte(``),
						"/orgs/foobar": []byte(`{"two_factor_requirement_enabled": false,"plan": {"name":"unknown"}}`),
					},
					err: nil,
				},
				expected: false,
			},
			{
				mock: &GitHubClientIsEnterpriseMock{
					results: map[string][]byte{
						"/api/v3":      []byte(``),
						"/orgs/foobar": []byte(`{"two_factor_requirement_enabled": false,"plan": {"name":"enterprise"}}`),
					},
					err: nil,
				},
				expected: true,
			},
			{
				mock: &GitHubClientIsEnterpriseMock{
					results: map[string][]byte{
						"/api/v3":      []byte(``),
						"/orgs/foobar": []byte(`{"two_factor_requirement_enabled": false,"plan": {"name":"unknown"}}`),
					},
					err: fmt.Errorf("an error occured"),
				},
				expected: false,
			},
		}

		for _, set := range tests {
			ctx := context.TODO()
			res := isEnterprise(ctx, "foobar", set.mock)

			assert.Equal(t, set.expected, res)

		}
	})
}

func TestPrepareRuleset(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		ghClient := MockGithubClient{}
		g := NewGoliacRemoteImpl(&ghClient, "myorg", true, true)
		g.appIds["goliac-project-app"] = 1
		g.repositories["repo1"] = &GithubRepository{
			Name: "repo1",
			Id:   123,
		}
		g.repositories["repo2"] = &GithubRepository{
			Name: "repo2",
			Id:   456,
		}

		rulesetData := []byte(`
name: ruleset1
id: 1
enforcement: evaluate
bypassapps:
  goliac-project-app: always
oninclude:
  - "~DEFAULT_BRANCH"
rules:
  required_status_checks:
    requiredStatusChecks:
      - circleCI check
      - jenkins check
repositories:
  - repo1
  - repo2
`)
		var ruleset GithubRuleSet
		err := yaml.Unmarshal(rulesetData, &ruleset)
		assert.Nil(t, err)

		payload := g.prepareRuleset(&ruleset)

		assert.Equal(t, "ruleset1", payload["name"].(string))
		assert.Equal(t, "evaluate", payload["enforcement"].(string))
		assert.Equal(t, 1, len(payload["bypass_actors"].([]map[string]interface{})))
		assert.Equal(t, "~DEFAULT_BRANCH", payload["conditions"].(map[string]interface{})["ref_name"].(map[string]interface{})["include"].([]string)[0])
		assert.Equal(t, 2, len(payload["conditions"].(map[string]interface{})["repository_id"].(map[string]interface{})["repository_ids"].([]int)))
		assert.Equal(t, "circleCI check", payload["rules"].([]map[string]interface{})[0]["parameters"].(map[string]interface{})["required_status_checks"].([]map[string]interface{})[0]["context"].(string))
	})

	t.Run("happy path: non default branch name", func(t *testing.T) {
		ghClient := MockGithubClient{}
		g := NewGoliacRemoteImpl(&ghClient, "myorg", true, true)
		g.appIds["goliac-project-app"] = 1
		g.repositories["repo1"] = &GithubRepository{
			Name: "repo1",
			Id:   123,
		}
		g.repositories["repo2"] = &GithubRepository{
			Name: "repo2",
			Id:   456,
		}

		rulesetData := []byte(`
name: ruleset1
id: 1
enforcement: evaluate
bypassapps:
  goliac-project-app: always
oninclude:
  - main
rules:
  required_status_checks:
    requiredStatusChecks:
      - circleCI check
      - jenkins check
repositories:
  - repo1
  - repo2
`)
		var ruleset GithubRuleSet
		err := yaml.Unmarshal(rulesetData, &ruleset)
		assert.Nil(t, err)

		payload := g.prepareRuleset(&ruleset)

		assert.Equal(t, "ruleset1", payload["name"].(string))
		assert.Equal(t, "evaluate", payload["enforcement"].(string))
		assert.Equal(t, 1, len(payload["bypass_actors"].([]map[string]interface{})))
		// Github API uses refs/heads/ as prefix
		assert.Equal(t, "refs/heads/main", payload["conditions"].(map[string]interface{})["ref_name"].(map[string]interface{})["include"].([]string)[0])
	})
}
