package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/goliac-project/goliac/internal/entity"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/goliac-project/goliac/internal/utils"
	"github.com/stretchr/testify/assert"
)

// RulesetMockClient is a dedicated mock client for ruleset tests
type RulesetMockClient struct {
	// Track calls to verify test behavior
	lastGraphQLQuery string
	lastVariables    map[string]interface{}
	lastEndpoint     string
	lastMethod       string
	lastBody         map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
}

func (m *RulesetMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	m.lastGraphQLQuery = query
	m.lastVariables = variables

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	return []byte("{}"), nil
}

func (m *RulesetMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.lastEndpoint = endpoint
	m.lastMethod = method
	m.lastBody = body

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	return []byte("{}"), nil
}

func (m *RulesetMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *RulesetMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *RulesetMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestFromGraphQLToGithubRuleset(t *testing.T) {
	t.Run("happy path: complete ruleset conversion", func(t *testing.T) {
		// Setup mock client and remote impl
		mockClient := &RulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Setup test data - GraphQL ruleset
		graphqlRuleset := &GraphQLGithubRuleSet{
			DatabaseId: 123,
			Source: struct {
				Name string
			}{
				Name: "test-repo",
			},
			Name:        "test-ruleset",
			Target:      "BRANCH",
			Enforcement: "ACTIVE",
			BypassActors: struct {
				Actors []GithubRuleSetActor
			}{
				Actors: []GithubRuleSetActor{
					{
						Actor: struct {
							DatabaseId int
							Name       string
							TeamSlug   string
						}{
							Name: "test-app",
						},
						BypassMode: "ALWAYS",
					},
					{
						Actor: struct {
							DatabaseId int
							Name       string
							TeamSlug   string
						}{
							TeamSlug: "test-team",
						},
						BypassMode: "PULL_REQUEST",
					},
				},
			},
			Conditions: struct {
				RefName struct {
					Include []string
					Exclude []string
				}
				RepositoryName struct {
					Include   []string
					Exclude   []string
					Protected bool
				}
				RepositoryId struct {
					RepositoryIds []string
				}
			}{
				RefName: struct {
					Include []string
					Exclude []string
				}{
					Include: []string{"refs/heads/main", "~DEFAULT_BRANCH"},
					Exclude: []string{"refs/heads/test"},
				},
				RepositoryId: struct {
					RepositoryIds []string
				}{
					RepositoryIds: []string{"R_123"},
				},
			},
			Rules: struct {
				Nodes []GithubRuleSetRule
			}{
				Nodes: []GithubRuleSetRule{
					{
						Parameters: struct {
							DismissStaleReviewsOnPush        bool
							RequireCodeOwnerReview           bool
							RequiredApprovingReviewCount     int
							RequiredReviewThreadResolution   bool
							RequireLastPushApproval          bool
							RequiredStatusChecks             []GithubRuleSetRuleStatusCheck
							StrictRequiredStatusChecksPolicy bool
						}{
							DismissStaleReviewsOnPush:      true,
							RequireCodeOwnerReview:         true,
							RequiredApprovingReviewCount:   2,
							RequiredReviewThreadResolution: true,
							RequireLastPushApproval:        true,
						},
						Type: "PULL_REQUEST",
					},
					{
						Parameters: struct {
							DismissStaleReviewsOnPush        bool
							RequireCodeOwnerReview           bool
							RequiredApprovingReviewCount     int
							RequiredReviewThreadResolution   bool
							RequireLastPushApproval          bool
							RequiredStatusChecks             []GithubRuleSetRuleStatusCheck
							StrictRequiredStatusChecksPolicy bool
						}{
							RequiredStatusChecks: []GithubRuleSetRuleStatusCheck{
								{Context: "test-check"},
							},
							StrictRequiredStatusChecksPolicy: true,
						},
						Type: "REQUIRED_STATUS_CHECKS",
					},
				},
			},
		}

		// Add a repository to repositoriesByRefId for testing repository ID resolution
		remoteImpl.repositoriesByRefId = map[string]*GithubRepository{
			"R_123": {
				Name: "test-repo",
				Id:   123,
			},
		}

		// Convert the ruleset
		result := remoteImpl.fromGraphQLToGithubRuleset(graphqlRuleset)

		// Verify the conversion
		assert.Equal(t, "test-ruleset", result.Name)
		assert.Equal(t, 123, result.Id)
		assert.Equal(t, "active", result.Enforcement)

		// Verify bypass actors
		assert.Equal(t, "always", result.BypassApps["test-app"])
		assert.Equal(t, "pull_request", result.BypassTeams["test-team"])

		// Verify conditions
		assert.Contains(t, result.OnInclude, "main")
		assert.Contains(t, result.OnInclude, "~DEFAULT_BRANCH")
		assert.Contains(t, result.OnExclude, "test")

		// Verify repositories
		assert.Contains(t, result.Repositories, "test-repo")

		// Verify rules
		pullRequestRule := result.Rules["pull_request"]
		assert.True(t, pullRequestRule.DismissStaleReviewsOnPush)
		assert.True(t, pullRequestRule.RequireCodeOwnerReview)
		assert.Equal(t, 2, pullRequestRule.RequiredApprovingReviewCount)
		assert.True(t, pullRequestRule.RequiredReviewThreadResolution)
		assert.True(t, pullRequestRule.RequireLastPushApproval)

		statusChecksRule := result.Rules["required_status_checks"]
		assert.Equal(t, []string{"test-check"}, statusChecksRule.RequiredStatusChecks)
		assert.True(t, statusChecksRule.StrictRequiredStatusChecksPolicy)
	})

	t.Run("empty ruleset conversion", func(t *testing.T) {
		// Setup mock client and remote impl
		mockClient := &RulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Setup minimal test data
		graphqlRuleset := &GraphQLGithubRuleSet{
			DatabaseId:  123,
			Name:        "test-ruleset",
			Target:      "BRANCH",
			Enforcement: "DISABLED",
			BypassActors: struct {
				Actors []GithubRuleSetActor
			}{
				Actors: []GithubRuleSetActor{},
			},
			Conditions: struct {
				RefName struct {
					Include []string
					Exclude []string
				}
				RepositoryName struct {
					Include   []string
					Exclude   []string
					Protected bool
				}
				RepositoryId struct {
					RepositoryIds []string
				}
			}{},
			Rules: struct {
				Nodes []GithubRuleSetRule
			}{
				Nodes: []GithubRuleSetRule{},
			},
		}

		// Convert the ruleset
		result := remoteImpl.fromGraphQLToGithubRuleset(graphqlRuleset)

		// Verify the conversion
		assert.Equal(t, "test-ruleset", result.Name)
		assert.Equal(t, 123, result.Id)
		assert.Equal(t, "disabled", result.Enforcement)
		assert.Empty(t, result.BypassApps)
		assert.Empty(t, result.BypassTeams)
		assert.Empty(t, result.OnInclude)
		assert.Empty(t, result.OnExclude)
		assert.Empty(t, result.Rules)
		assert.Empty(t, result.Repositories)
	})
}

// AddRulesetMockClient is a dedicated mock client for AddRuleset tests
type AddRulesetMockClient struct {
	// Track calls to verify test behavior
	lastEndpoint string
	lastMethod   string
	lastBody     map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *AddRulesetMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *AddRulesetMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.lastEndpoint = endpoint
	m.lastMethod = method
	m.lastBody = body

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}
	return []byte("{}"), nil
}

func (m *AddRulesetMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *AddRulesetMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *AddRulesetMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestAddRuleset(t *testing.T) {
	t.Run("happy path: add ruleset", func(t *testing.T) {
		// Setup mock client
		mockClient := &AddRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Setup test data
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Id:          123,
			Enforcement: "active",
			BypassApps: map[string]string{
				"test-app": "always",
			},
			BypassTeams: map[string]string{
				"test-team": "pull_request",
			},
			OnInclude: []string{"main", "~DEFAULT_BRANCH"},
			OnExclude: []string{"test"},
			Rules: map[string]entity.RuleSetParameters{
				"pull_request": {
					DismissStaleReviewsOnPush:      true,
					RequireCodeOwnerReview:         true,
					RequiredApprovingReviewCount:   2,
					RequiredReviewThreadResolution: true,
					RequireLastPushApproval:        true,
				},
				"required_status_checks": {
					RequiredStatusChecks:             []string{"test-check"},
					StrictRequiredStatusChecksPolicy: true,
				},
			},
			Repositories: []string{"test-repo"},
		}

		// Add app ID to the remote impl for bypass actor resolution
		remoteImpl.appIds = map[string]int{
			"test-app": 456,
		}

		// Add team to the remote impl for bypass actor resolution
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Id:   789,
				Slug: "test-team",
			},
		}

		// Add repository to the remote impl for repository ID resolution
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Id:   321,
				Name: "test-repo",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call AddRuleset
		remoteImpl.AddRuleset(ctx, errorCollector, false, ruleset)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the API call was made correctly
		assert.Equal(t, "/orgs/myorg/rulesets", mockClient.lastEndpoint)
		assert.Equal(t, "POST", mockClient.lastMethod)

		// Verify the request body
		expectedBody := map[string]interface{}{
			"name":        "test-ruleset",
			"target":      "branch",
			"enforcement": "active",
			"bypass_actors": []map[string]interface{}{
				{
					"actor_id":    456,
					"actor_type":  "Integration",
					"bypass_mode": "always",
				},
				{
					"actor_id":    789,
					"actor_type":  "Team",
					"bypass_mode": "pull_request",
				},
			},
			"conditions": map[string]interface{}{
				"ref_name": map[string]interface{}{
					"include": []string{"refs/heads/main", "~DEFAULT_BRANCH"},
					"exclude": []string{"refs/heads/test"},
				},
				"repository_id": map[string]interface{}{
					"repository_ids": []int{321},
				},
			},
			"rules": []map[string]interface{}{
				{
					"type": "pull_request",
					"parameters": map[string]interface{}{
						"dismiss_stale_reviews_on_push":     true,
						"require_code_owner_review":         true,
						"required_approving_review_count":   2,
						"required_review_thread_resolution": true,
						"require_last_push_approval":        true,
					},
				},
				{
					"type": "required_status_checks",
					"parameters": map[string]interface{}{
						"strict_required_status_checks_policy": true,
						"required_status_checks": []map[string]interface{}{
							{"context": "test-check"},
						},
					},
				},
			},
		}
		// test recursive comparison
		assert.True(t, utils.DeepEqualUnordered(expectedBody, mockClient.lastBody))

		// Verify the ruleset was added to the cache
		assert.Contains(t, remoteImpl.rulesets, "test-ruleset")
		assert.Equal(t, ruleset, remoteImpl.rulesets["test-ruleset"])
	})

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &AddRulesetMockClient{
			shouldError:  true,
			errorMessage: "failed to create ruleset",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Setup minimal test data
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Enforcement: "active",
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call AddRuleset
		remoteImpl.AddRuleset(ctx, errorCollector, false, ruleset)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to add ruleset to org")
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to create ruleset")

		// Verify the ruleset was not added to the cache
		assert.NotContains(t, remoteImpl.rulesets, "test-ruleset")
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &AddRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Setup test data
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Enforcement: "active",
		}

		ctx := context.TODO()
		remoteImpl.loadRulesets(ctx)
		mockClient.lastEndpoint = ""
		mockClient.lastMethod = ""
		errorCollector := observability.NewErrorCollection()

		// Call AddRuleset in dry run mode
		remoteImpl.AddRuleset(ctx, errorCollector, true, ruleset)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
		assert.Empty(t, mockClient.lastMethod)

		// Verify the ruleset was added to the cache
		assert.Contains(t, remoteImpl.rulesets, "test-ruleset")
		assert.Equal(t, ruleset, remoteImpl.rulesets["test-ruleset"])
	})

	t.Run("error path: invalid ruleset", func(t *testing.T) {
		// Setup mock client
		mockClient := &AddRulesetMockClient{
			responseBody: `{"message": "Invalid ruleset configuration"}`,
			shouldError:  true,
			errorMessage: "Invalid ruleset configuration",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Setup test data with invalid configuration
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Enforcement: "invalid", // Invalid enforcement value
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call AddRuleset
		remoteImpl.AddRuleset(ctx, errorCollector, false, ruleset)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "Invalid ruleset configuration")

		// Verify the ruleset was not added to the cache
		assert.NotContains(t, remoteImpl.rulesets, "test-ruleset")
	})
}

// DeleteRulesetMockClient is a dedicated mock client for DeleteRuleset tests
type DeleteRulesetMockClient struct {
	// Track calls to verify test behavior
	lastEndpoint string
	lastMethod   string
	lastBody     map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *DeleteRulesetMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *DeleteRulesetMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.lastEndpoint = endpoint
	m.lastMethod = method
	m.lastBody = body

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}
	return []byte("{}"), nil
}

func (m *DeleteRulesetMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *DeleteRulesetMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *DeleteRulesetMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestDeleteRuleset(t *testing.T) {
	t.Run("happy path: delete existing ruleset", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add a ruleset to the cache
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Id:          123,
			Enforcement: "active",
		}
		remoteImpl.rulesets = map[string]*GithubRuleSet{
			"test-ruleset": ruleset,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteRuleset
		remoteImpl.DeleteRuleset(ctx, errorCollector, false, 123)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the API call was made correctly
		assert.Equal(t, "/orgs/myorg/rulesets/123", mockClient.lastEndpoint)
		assert.Equal(t, "DELETE", mockClient.lastMethod)
		assert.Nil(t, mockClient.lastBody)

		// Verify the ruleset was removed from the cache
		assert.NotContains(t, remoteImpl.rulesets, "test-ruleset")
	})

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &DeleteRulesetMockClient{
			shouldError:  true,
			errorMessage: "ruleset not found",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add a ruleset to the cache
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Id:          123,
			Enforcement: "active",
		}
		remoteImpl.rulesets = map[string]*GithubRuleSet{
			"test-ruleset": ruleset,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteRuleset
		remoteImpl.DeleteRuleset(ctx, errorCollector, false, 123)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to remove ruleset from org")
		assert.Contains(t, errorCollector.Errors[0].Error(), "ruleset not found")

		// Verify the ruleset remains in the cache due to error
		assert.Contains(t, remoteImpl.rulesets, "test-ruleset")
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add a ruleset to the cache
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Id:          123,
			Enforcement: "active",
		}
		remoteImpl.rulesets = map[string]*GithubRuleSet{
			"test-ruleset": ruleset,
		}

		ctx := context.TODO()
		remoteImpl.loadRulesets(ctx)
		mockClient.lastEndpoint = ""
		mockClient.lastMethod = ""
		errorCollector := observability.NewErrorCollection()

		// Call DeleteRuleset in dry run mode
		remoteImpl.DeleteRuleset(ctx, errorCollector, true, 123)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
		assert.Empty(t, mockClient.lastMethod)

		// Verify the ruleset was removed from the cache
		assert.NotContains(t, remoteImpl.rulesets, "test-ruleset")
	})

	t.Run("happy path: delete non-existent ruleset ID", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add a ruleset to the cache with different ID
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Id:          456, // Different ID than what we'll try to delete
			Enforcement: "active",
		}
		remoteImpl.rulesets = map[string]*GithubRuleSet{
			"test-ruleset": ruleset,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteRuleset with non-existent ID
		remoteImpl.DeleteRuleset(ctx, errorCollector, false, 123)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the API call was made
		assert.Equal(t, "/orgs/myorg/rulesets/123", mockClient.lastEndpoint)
		assert.Equal(t, "DELETE", mockClient.lastMethod)

		// Verify the existing ruleset remains in the cache
		assert.Contains(t, remoteImpl.rulesets, "test-ruleset")
	})

	t.Run("error path: delete with multiple rulesets in cache", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add multiple rulesets to the cache
		ruleset1 := &GithubRuleSet{
			Name:        "test-ruleset-1",
			Id:          123,
			Enforcement: "active",
		}
		ruleset2 := &GithubRuleSet{
			Name:        "test-ruleset-2",
			Id:          456,
			Enforcement: "active",
		}
		remoteImpl.rulesets = map[string]*GithubRuleSet{
			"test-ruleset-1": ruleset1,
			"test-ruleset-2": ruleset2,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteRuleset
		remoteImpl.DeleteRuleset(ctx, errorCollector, false, 123)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the API call was made correctly
		assert.Equal(t, "/orgs/myorg/rulesets/123", mockClient.lastEndpoint)
		assert.Equal(t, "DELETE", mockClient.lastMethod)

		// Verify only the target ruleset was removed from the cache
		assert.NotContains(t, remoteImpl.rulesets, "test-ruleset-1")
		assert.Contains(t, remoteImpl.rulesets, "test-ruleset-2")
	})
}

// UpdateRulesetMockClient is a dedicated mock client for UpdateRuleset tests
type UpdateRulesetMockClient struct {
	// Track calls to verify test behavior
	lastEndpoint string
	lastMethod   string
	lastBody     map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *UpdateRulesetMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *UpdateRulesetMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.lastEndpoint = endpoint
	m.lastMethod = method
	m.lastBody = body

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}
	return []byte("{}"), nil
}

func (m *UpdateRulesetMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *UpdateRulesetMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *UpdateRulesetMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestUpdateRuleset(t *testing.T) {
	t.Run("happy path: update existing ruleset", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Setup test data
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Id:          123,
			Enforcement: "active",
			BypassApps: map[string]string{
				"test-app": "always",
			},
			BypassTeams: map[string]string{
				"test-team": "pull_request",
			},
			OnInclude: []string{"main", "~DEFAULT_BRANCH"},
			OnExclude: []string{"test"},
			Rules: map[string]entity.RuleSetParameters{
				"pull_request": {
					DismissStaleReviewsOnPush:      true,
					RequireCodeOwnerReview:         true,
					RequiredApprovingReviewCount:   2,
					RequiredReviewThreadResolution: true,
					RequireLastPushApproval:        true,
				},
				"required_status_checks": {
					RequiredStatusChecks:             []string{"test-check"},
					StrictRequiredStatusChecksPolicy: true,
				},
			},
			Repositories: []string{"test-repo"},
		}

		// Add app ID to the remote impl for bypass actor resolution
		remoteImpl.appIds = map[string]int{
			"test-app": 456,
		}

		// Add team to the remote impl for bypass actor resolution
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Id:   789,
				Slug: "test-team",
			},
		}

		// Add repository to the remote impl for repository ID resolution
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Id:   321,
				Name: "test-repo",
			},
		}

		// Add existing ruleset to the cache
		remoteImpl.rulesets = map[string]*GithubRuleSet{
			"test-ruleset": {
				Name:        "test-ruleset",
				Id:          123,
				Enforcement: "evaluate",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRuleset
		remoteImpl.UpdateRuleset(ctx, errorCollector, false, ruleset)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the API call was made correctly
		assert.Equal(t, "/orgs/myorg/rulesets/123", mockClient.lastEndpoint)
		assert.Equal(t, "PUT", mockClient.lastMethod)

		// Verify the request body
		expectedBody := map[string]interface{}{
			"name":        "test-ruleset",
			"target":      "branch",
			"enforcement": "active",
			"bypass_actors": []map[string]interface{}{
				{
					"actor_id":    456,
					"actor_type":  "Integration",
					"bypass_mode": "always",
				},
				{
					"actor_id":    789,
					"actor_type":  "Team",
					"bypass_mode": "pull_request",
				},
			},
			"conditions": map[string]interface{}{
				"ref_name": map[string]interface{}{
					"include": []string{"refs/heads/main", "~DEFAULT_BRANCH"},
					"exclude": []string{"refs/heads/test"},
				},
				"repository_id": map[string]interface{}{
					"repository_ids": []int{321},
				},
			},
			"rules": []map[string]interface{}{
				{
					"type": "pull_request",
					"parameters": map[string]interface{}{
						"dismiss_stale_reviews_on_push":     true,
						"require_code_owner_review":         true,
						"required_approving_review_count":   2,
						"required_review_thread_resolution": true,
						"require_last_push_approval":        true,
					},
				},
				{
					"type": "required_status_checks",
					"parameters": map[string]interface{}{
						"strict_required_status_checks_policy": true,
						"required_status_checks": []map[string]interface{}{
							{"context": "test-check"},
						},
					},
				},
			},
		}
		assert.True(t, utils.DeepEqualUnordered(expectedBody, mockClient.lastBody))

		// Verify the ruleset was updated in the cache
		assert.Equal(t, ruleset, remoteImpl.rulesets["test-ruleset"])
	})

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &UpdateRulesetMockClient{
			shouldError:  true,
			errorMessage: "failed to update ruleset",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Setup test data
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Id:          123,
			Enforcement: "active",
		}

		// Add existing ruleset to the cache
		originalRuleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Id:          123,
			Enforcement: "evaluate",
		}
		remoteImpl.rulesets = map[string]*GithubRuleSet{
			"test-ruleset": originalRuleset,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRuleset
		remoteImpl.UpdateRuleset(ctx, errorCollector, false, ruleset)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to update ruleset")

		// Verify the cache remains unchanged
		assert.Equal(t, originalRuleset, remoteImpl.rulesets["test-ruleset"])
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Setup test data
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Id:          123,
			Enforcement: "active",
		}

		// Add existing ruleset to the cache
		remoteImpl.rulesets = map[string]*GithubRuleSet{
			"test-ruleset": {
				Name:        "test-ruleset",
				Id:          123,
				Enforcement: "evaluate",
			},
		}

		ctx := context.TODO()
		remoteImpl.loadRulesets(ctx)
		mockClient.lastEndpoint = ""
		mockClient.lastMethod = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRuleset in dry run mode
		remoteImpl.UpdateRuleset(ctx, errorCollector, true, ruleset)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
		assert.Empty(t, mockClient.lastMethod)

		// Verify the ruleset was updated in the cache
		assert.Equal(t, ruleset, remoteImpl.rulesets["test-ruleset"])
	})

	t.Run("error path: invalid ruleset configuration", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRulesetMockClient{
			responseBody: `{"message": "Invalid ruleset configuration"}`,
			shouldError:  true,
			errorMessage: "Invalid ruleset configuration",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Setup test data with invalid configuration
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Id:          123,
			Enforcement: "invalid", // Invalid enforcement value
		}

		// Add existing ruleset to the cache
		originalRuleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Id:          123,
			Enforcement: "evaluate",
		}
		remoteImpl.rulesets = map[string]*GithubRuleSet{
			"test-ruleset": originalRuleset,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRuleset
		remoteImpl.UpdateRuleset(ctx, errorCollector, false, ruleset)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "Invalid ruleset configuration")

		// Verify the cache remains unchanged
		assert.Equal(t, originalRuleset, remoteImpl.rulesets["test-ruleset"])
	})

	t.Run("error path: non-existent ruleset", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Setup test data for a ruleset that doesn't exist in cache
		ruleset := &GithubRuleSet{
			Name:        "non-existent-ruleset",
			Id:          123,
			Enforcement: "active",
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRuleset
		remoteImpl.UpdateRuleset(ctx, errorCollector, false, ruleset)

		// Verify the API call was still made
		assert.Equal(t, "/orgs/myorg/rulesets/123", mockClient.lastEndpoint)
		assert.Equal(t, "PUT", mockClient.lastMethod)

		// Verify the ruleset was added to the cache
		assert.Equal(t, ruleset, remoteImpl.rulesets["non-existent-ruleset"])
	})
}
