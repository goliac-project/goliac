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

// CreateRepositoryRulesetMockClient is a dedicated mock client for CreateRepositoryRuleset tests
type CreateRepositoryRulesetMockClient struct {
	// Track calls to verify test behavior
	lastEndpoint string
	lastMethod   string
	lastBody     map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *CreateRepositoryRulesetMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *CreateRepositoryRulesetMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.lastEndpoint = endpoint
	m.lastMethod = method
	m.lastBody = body

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}
	return []byte(`{"id": 123}`), nil
}

func (m *CreateRepositoryRulesetMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *CreateRepositoryRulesetMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *CreateRepositoryRulesetMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestCreateRepositoryRuleset(t *testing.T) {
	t.Run("happy path: create repository ruleset", func(t *testing.T) {
		// Setup mock client
		mockClient := &CreateRepositoryRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository to the remote impl
		repo := &GithubRepository{
			Name:     "test-repo",
			Id:       123,
			RefId:    "R_123",
			RuleSets: make(map[string]*GithubRuleSet),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
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

		// Setup test data
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
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
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call CreateRepositoryRuleset
		remoteImpl.AddRepositoryRuleset(ctx, errorCollector, false, "test-repo", ruleset)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the API call was made correctly
		assert.Equal(t, "/repos/myorg/test-repo/rulesets", mockClient.lastEndpoint)
		assert.Equal(t, "POST", mockClient.lastMethod)

		// Verify the request body using deep comparison
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

		// Verify the ruleset was added to the repository's cache
		assert.Contains(t, repo.RuleSets, "test-ruleset")
		assert.Equal(t, 123, repo.RuleSets["test-ruleset"].Id)
	})

	t.Run("error path: repository not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &CreateRepositoryRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Setup test data
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Enforcement: "active",
		}

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		mockClient.lastEndpoint = ""
		mockClient.lastMethod = ""
		mockClient.lastBody = nil

		errorCollector := observability.NewErrorCollection()

		// Call CreateRepositoryRuleset with non-existent repository
		remoteImpl.AddRepositoryRuleset(ctx, errorCollector, false, "non-existent-repo", ruleset)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository non-existent-repo not found")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &CreateRepositoryRulesetMockClient{
			shouldError:  true,
			errorMessage: "failed to create ruleset",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository to the remote impl
		repo := &GithubRepository{
			Name:     "test-repo",
			Id:       123,
			RefId:    "R_123",
			RuleSets: make(map[string]*GithubRuleSet),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Setup test data
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Enforcement: "active",
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call CreateRepositoryRuleset
		remoteImpl.AddRepositoryRuleset(ctx, errorCollector, false, "test-repo", ruleset)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to add ruleset to repository")

		// Verify the ruleset was not added to the cache
		assert.NotContains(t, repo.RuleSets, "test-ruleset")
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &CreateRepositoryRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository to the remote impl
		repo := &GithubRepository{
			Name:     "test-repo",
			Id:       123,
			RefId:    "R_123",
			RuleSets: make(map[string]*GithubRuleSet),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Setup test data
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Enforcement: "active",
		}

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		mockClient.lastEndpoint = ""
		mockClient.lastMethod = ""
		mockClient.lastBody = nil

		errorCollector := observability.NewErrorCollection()

		// Call CreateRepositoryRuleset in dry run mode
		remoteImpl.AddRepositoryRuleset(ctx, errorCollector, true, "test-repo", ruleset)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)

		// Verify the ruleset was added to the cache
		assert.Contains(t, repo.RuleSets, "test-ruleset")
	})

	t.Run("error path: invalid ruleset configuration", func(t *testing.T) {
		// Setup mock client with error response
		mockClient := &CreateRepositoryRulesetMockClient{
			responseBody: `{"message": "Invalid ruleset configuration"}`,
			shouldError:  true,
			errorMessage: "Invalid ruleset configuration",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository to the remote impl
		repo := &GithubRepository{
			Name:     "test-repo",
			Id:       123,
			RefId:    "R_123",
			RuleSets: make(map[string]*GithubRuleSet),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Setup test data with invalid configuration
		ruleset := &GithubRuleSet{
			Name:        "test-ruleset",
			Enforcement: "invalid", // Invalid enforcement value
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call CreateRepositoryRuleset
		remoteImpl.AddRepositoryRuleset(ctx, errorCollector, false, "test-repo", ruleset)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "Invalid ruleset configuration")

		// Verify the ruleset was not added to the cache
		assert.NotContains(t, repo.RuleSets, "test-ruleset")
	})
}

// DeleteRepositoryRulesetMockClient is a dedicated mock client for DeleteRepositoryRuleset tests
type DeleteRepositoryRulesetMockClient struct {
	// Track calls to verify test behavior
	lastEndpoint string
	lastMethod   string

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *DeleteRepositoryRulesetMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *DeleteRepositoryRulesetMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.lastEndpoint = endpoint
	m.lastMethod = method

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}
	return []byte(`{}`), nil
}

func (m *DeleteRepositoryRulesetMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *DeleteRepositoryRulesetMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *DeleteRepositoryRulesetMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestDeleteRepositoryRuleset(t *testing.T) {
	t.Run("happy path: delete existing ruleset", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteRepositoryRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository with ruleset to the remote impl
		repo := &GithubRepository{
			Name:  "test-repo",
			Id:    123,
			RefId: "R_123",
			RuleSets: map[string]*GithubRuleSet{
				"test-ruleset": {
					Id:          456,
					Name:        "test-ruleset",
					Enforcement: "active",
				},
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteRepositoryRuleset
		remoteImpl.DeleteRepositoryRuleset(ctx, errorCollector, false, "test-repo", 456)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the API call was made correctly
		assert.Equal(t, "/repos/myorg/test-repo/rulesets/456", mockClient.lastEndpoint)
		assert.Equal(t, "DELETE", mockClient.lastMethod)

		// Verify the ruleset was removed from the cache
		assert.NotContains(t, repo.RuleSets, "test-ruleset")
	})

	t.Run("error path: repository not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteRepositoryRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		mockClient.lastEndpoint = ""
		mockClient.lastMethod = ""

		errorCollector := observability.NewErrorCollection()

		// Call DeleteRepositoryRuleset with non-existent repository
		remoteImpl.DeleteRepositoryRuleset(ctx, errorCollector, false, "non-existent-repo", 456)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository non-existent-repo not found")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: ruleset not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteRepositoryRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository without the target ruleset
		repo := &GithubRepository{
			Name:     "test-repo",
			Id:       123,
			RefId:    "R_123",
			RuleSets: make(map[string]*GithubRuleSet),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		mockClient.lastEndpoint = ""
		mockClient.lastMethod = ""

		errorCollector := observability.NewErrorCollection()

		// Call DeleteRepositoryRuleset with non-existent ruleset
		remoteImpl.DeleteRepositoryRuleset(ctx, errorCollector, false, "test-repo", 456)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "ruleset 456 not found")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &DeleteRepositoryRulesetMockClient{
			shouldError:  true,
			errorMessage: "failed to delete ruleset",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository with ruleset to the remote impl
		repo := &GithubRepository{
			Name:  "test-repo",
			Id:    123,
			RefId: "R_123",
			RuleSets: map[string]*GithubRuleSet{
				"test-ruleset": {
					Id:          456,
					Name:        "test-ruleset",
					Enforcement: "active",
				},
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteRepositoryRuleset
		remoteImpl.DeleteRepositoryRuleset(ctx, errorCollector, false, "test-repo", 456)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to delete ruleset")

		// Verify the ruleset remains in the cache
		assert.Contains(t, repo.RuleSets, "test-ruleset")
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteRepositoryRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository with ruleset to the remote impl
		repo := &GithubRepository{
			Name:  "test-repo",
			Id:    123,
			RefId: "R_123",
			RuleSets: map[string]*GithubRuleSet{
				"test-ruleset": {
					Id:          456,
					Name:        "test-ruleset",
					Enforcement: "active",
				},
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		mockClient.lastEndpoint = ""
		mockClient.lastMethod = ""

		errorCollector := observability.NewErrorCollection()

		// Call DeleteRepositoryRuleset in dry run mode
		remoteImpl.DeleteRepositoryRuleset(ctx, errorCollector, true, "test-repo", 456)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)

		// Verify the ruleset was removed from the cache
		assert.NotContains(t, repo.RuleSets, "test-ruleset")
	})

	t.Run("error path: API not found response", func(t *testing.T) {
		// Setup mock client with 404 response
		mockClient := &DeleteRepositoryRulesetMockClient{
			responseBody: `{"message": "Not Found"}`,
			shouldError:  true,
			errorMessage: "Not Found",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository with ruleset to the remote impl
		repo := &GithubRepository{
			Name:  "test-repo",
			Id:    123,
			RefId: "R_123",
			RuleSets: map[string]*GithubRuleSet{
				"test-ruleset": {
					Id:          456,
					Name:        "test-ruleset",
					Enforcement: "active",
				},
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteRepositoryRuleset
		remoteImpl.DeleteRepositoryRuleset(ctx, errorCollector, false, "test-repo", 456)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "Not Found")

		// Verify the ruleset remains in the cache since the deletion failed
		assert.Contains(t, repo.RuleSets, "test-ruleset")
	})
}

// UpdateRepositoryRulesetMockClient is a dedicated mock client for UpdateRepositoryRuleset tests
type UpdateRepositoryRulesetMockClient struct {
	// Track calls to verify test behavior
	lastEndpoint string
	lastMethod   string
	lastBody     map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *UpdateRepositoryRulesetMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *UpdateRepositoryRulesetMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.lastEndpoint = endpoint
	m.lastMethod = method
	m.lastBody = body

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}
	return []byte(`{"id": 456}`), nil
}

func (m *UpdateRepositoryRulesetMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *UpdateRepositoryRulesetMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *UpdateRepositoryRulesetMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestUpdateRepositoryRuleset(t *testing.T) {
	t.Run("happy path: update existing ruleset", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository with existing ruleset to the remote impl
		repo := &GithubRepository{
			Name:  "test-repo",
			Id:    123,
			RefId: "R_123",
			RuleSets: map[string]*GithubRuleSet{
				"test-ruleset": {
					Id:          456,
					Name:        "test-ruleset",
					Enforcement: "active",
				},
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Add app ID to the remote impl for bypass actor resolution
		remoteImpl.appIds = map[string]int{
			"test-app": 789,
		}

		// Add team to the remote impl for bypass actor resolution
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Id:   101,
				Slug: "test-team",
			},
		}

		// Setup updated ruleset
		updatedRuleset := &GithubRuleSet{
			Id:          456,
			Name:        "test-ruleset",
			Enforcement: "evaluate",
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
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryRuleset
		remoteImpl.UpdateRepositoryRuleset(ctx, errorCollector, false, "test-repo", updatedRuleset)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the API call was made correctly
		assert.Equal(t, "/repos/myorg/test-repo/rulesets/456", mockClient.lastEndpoint)
		assert.Equal(t, "PUT", mockClient.lastMethod)

		// Verify the request body
		expectedBody := map[string]interface{}{
			"name":        "test-ruleset",
			"target":      "branch",
			"enforcement": "evaluate",
			"bypass_actors": []map[string]interface{}{
				{
					"actor_id":    789,
					"actor_type":  "Integration",
					"bypass_mode": "always",
				},
				{
					"actor_id":    101,
					"actor_type":  "Team",
					"bypass_mode": "pull_request",
				},
			},
			"conditions": map[string]interface{}{
				"ref_name": map[string]interface{}{
					"include": []string{"refs/heads/main", "~DEFAULT_BRANCH"},
					"exclude": []string{"refs/heads/test"},
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
		assert.Equal(t, "evaluate", repo.RuleSets["test-ruleset"].Enforcement)
	})

	t.Run("error path: repository not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Setup ruleset for update
		ruleset := &GithubRuleSet{
			Id:          456,
			Name:        "test-ruleset",
			Enforcement: "evaluate",
		}

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		mockClient.lastEndpoint = ""
		mockClient.lastMethod = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryRuleset with non-existent repository
		remoteImpl.UpdateRepositoryRuleset(ctx, errorCollector, false, "non-existent-repo", ruleset)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository non-existent-repo not found")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: ruleset not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository without the target ruleset
		repo := &GithubRepository{
			Name:     "test-repo",
			Id:       123,
			RefId:    "R_123",
			RuleSets: make(map[string]*GithubRuleSet),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Setup ruleset for update
		ruleset := &GithubRuleSet{
			Id:          456,
			Name:        "non-existent-ruleset",
			Enforcement: "evaluate",
		}

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		mockClient.lastEndpoint = ""
		mockClient.lastMethod = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryRuleset with non-existent ruleset
		remoteImpl.UpdateRepositoryRuleset(ctx, errorCollector, false, "test-repo", ruleset)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "ruleset 456 not found")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &UpdateRepositoryRulesetMockClient{
			shouldError:  true,
			errorMessage: "failed to update ruleset",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository with existing ruleset
		repo := &GithubRepository{
			Name:  "test-repo",
			Id:    123,
			RefId: "R_123",
			RuleSets: map[string]*GithubRuleSet{
				"test-ruleset": {
					Id:          456,
					Name:        "test-ruleset",
					Enforcement: "active",
				},
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Setup ruleset for update
		updatedRuleset := &GithubRuleSet{
			Id:          456,
			Name:        "test-ruleset",
			Enforcement: "evaluate",
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryRuleset
		remoteImpl.UpdateRepositoryRuleset(ctx, errorCollector, false, "test-repo", updatedRuleset)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to update ruleset")

		// Verify the ruleset in cache remains unchanged
		assert.Equal(t, "active", repo.RuleSets["test-ruleset"].Enforcement)
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryRulesetMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository with existing ruleset
		repo := &GithubRepository{
			Name:  "test-repo",
			Id:    123,
			RefId: "R_123",
			RuleSets: map[string]*GithubRuleSet{
				"test-ruleset": {
					Id:          456,
					Name:        "test-ruleset",
					Enforcement: "active",
				},
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Setup ruleset for update
		updatedRuleset := &GithubRuleSet{
			Id:          456,
			Name:        "test-ruleset",
			Enforcement: "evaluate",
		}

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		mockClient.lastEndpoint = ""
		mockClient.lastMethod = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryRuleset in dry run mode
		remoteImpl.UpdateRepositoryRuleset(ctx, errorCollector, true, "test-repo", updatedRuleset)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)

		// Verify the ruleset was updated in the cache
		assert.Equal(t, "evaluate", repo.RuleSets["test-ruleset"].Enforcement)
	})

	t.Run("error path: invalid ruleset configuration", func(t *testing.T) {
		// Setup mock client with validation error response
		mockClient := &UpdateRepositoryRulesetMockClient{
			responseBody: `{"message": "Invalid ruleset configuration"}`,
			shouldError:  true,
			errorMessage: "Invalid ruleset configuration",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository with existing ruleset
		repo := &GithubRepository{
			Name:  "test-repo",
			Id:    123,
			RefId: "R_123",
			RuleSets: map[string]*GithubRuleSet{
				"test-ruleset": {
					Id:          456,
					Name:        "test-ruleset",
					Enforcement: "active",
				},
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Setup ruleset with invalid configuration
		updatedRuleset := &GithubRuleSet{
			Id:          456,
			Name:        "test-ruleset",
			Enforcement: "invalid", // Invalid enforcement value
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryRuleset
		remoteImpl.UpdateRepositoryRuleset(ctx, errorCollector, false, "test-repo", updatedRuleset)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "Invalid ruleset configuration")

		// Verify the ruleset in cache remains unchanged
		assert.Equal(t, "active", repo.RuleSets["test-ruleset"].Enforcement)
	})
}
