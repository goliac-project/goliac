package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/goliac-project/goliac/internal/observability"
	"github.com/stretchr/testify/assert"
)

// AddBranchProtectionMockClient is a dedicated mock client for AddRepositoryBranchProtection tests
type AddBranchProtectionMockClient struct {
	// Track calls to verify test behavior
	lastGraphQLQuery string
	lastVariables    map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *AddBranchProtectionMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	m.lastGraphQLQuery = query
	m.lastVariables = variables

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}

	// Default successful response
	return []byte(`{
		"data": {
			"createBranchProtectionRule": {
				"branchProtectionRule": {
					"id": "BP_123"
				}
			}
		}
	}`), nil
}

func (m *AddBranchProtectionMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *AddBranchProtectionMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *AddBranchProtectionMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *AddBranchProtectionMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestAddRepositoryBranchProtection(t *testing.T) {
	t.Run("happy path: add branch protection", func(t *testing.T) {
		// Setup mock client
		mockClient := &AddBranchProtectionMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository to the remote impl
		repo := &GithubRepository{
			Name:              "test-repo",
			Id:                123,
			RefId:             "R_123",
			BranchProtections: make(map[string]*GithubBranchProtection),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Setup test data
		branchProtection := &GithubBranchProtection{
			Pattern:                        "main",
			RequiresApprovingReviews:       true,
			RequiredApprovingReviewCount:   2,
			DismissesStaleReviews:          true,
			RequiresCodeOwnerReviews:       true,
			RequireLastPushApproval:        true,
			RequiresStatusChecks:           true,
			RequiresStrictStatusChecks:     true,
			RequiredStatusCheckContexts:    []string{"test-check"},
			RequiresConversationResolution: true,
			RequiresCommitSignatures:       true,
			RequiresLinearHistory:          true,
			AllowsForcePushes:              false,
			AllowsDeletions:                false,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call AddRepositoryBranchProtection
		remoteImpl.AddRepositoryBranchProtection(ctx, errorCollector, false, "test-repo", branchProtection)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the GraphQL query was made with correct variables
		expectedVariables := map[string]interface{}{
			"repositoryId":                   "R_123",
			"pattern":                        "main",
			"requiresApprovingReviews":       true,
			"requiredApprovingReviewCount":   2,
			"dismissesStaleReviews":          true,
			"requiresCodeOwnerReviews":       true,
			"requireLastPushApproval":        true,
			"requiresStatusChecks":           true,
			"requiresStrictStatusChecks":     true,
			"requiredStatusCheckContexts":    []string{"test-check"},
			"requiresConversationResolution": true,
			"requiresCommitSignatures":       true,
			"requiresLinearHistory":          true,
			"allowsForcePushes":              false,
			"allowsDeletions":                false,
		}
		assert.Equal(t, expectedVariables, mockClient.lastVariables)

		// Verify the branch protection was added to the cache
		assert.Contains(t, repo.BranchProtections, "main")
		assert.Equal(t, "BP_123", repo.BranchProtections["main"].Id)
	})

	t.Run("error path: repository not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &AddBranchProtectionMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Setup test data
		branchProtection := &GithubBranchProtection{
			Pattern: "main",
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call AddRepositoryBranchProtection with non-existent repository
		remoteImpl.AddRepositoryBranchProtection(ctx, errorCollector, false, "non-existent-repo", branchProtection)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository non-existent-repo not found")

		// Verify no GraphQL query was made
		assert.Empty(t, mockClient.lastGraphQLQuery)
	})

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &AddBranchProtectionMockClient{
			shouldError:  true,
			errorMessage: "failed to create branch protection",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository to the remote impl
		repo := &GithubRepository{
			Name:              "test-repo",
			Id:                123,
			RefId:             "R_123",
			BranchProtections: make(map[string]*GithubBranchProtection),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Setup test data
		branchProtection := &GithubBranchProtection{
			Pattern: "main",
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call AddRepositoryBranchProtection
		remoteImpl.AddRepositoryBranchProtection(ctx, errorCollector, false, "test-repo", branchProtection)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to add branch protection to repository")

		// Verify the branch protection was not added to the cache
		assert.NotContains(t, repo.BranchProtections, "main")
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &AddBranchProtectionMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository to the remote impl
		repo := &GithubRepository{
			Name:              "test-repo",
			Id:                123,
			RefId:             "R_123",
			BranchProtections: make(map[string]*GithubBranchProtection),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Setup test data
		branchProtection := &GithubBranchProtection{
			Pattern: "main",
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call AddRepositoryBranchProtection in dry run mode
		remoteImpl.AddRepositoryBranchProtection(ctx, errorCollector, true, "test-repo", branchProtection)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no GraphQL query was made
		assert.Empty(t, mockClient.lastGraphQLQuery)

		// Verify the branch protection was added to the cache
		assert.Contains(t, repo.BranchProtections, "main")
	})

	t.Run("error path: GraphQL response error", func(t *testing.T) {
		// Setup mock client with error response
		mockClient := &AddBranchProtectionMockClient{
			responseBody: `{
				"errors": [
					{
						"message": "Invalid branch protection configuration",
						"path": ["createBranchProtectionRule"]
					}
				]
			}`,
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository to the remote impl
		repo := &GithubRepository{
			Name:              "test-repo",
			Id:                123,
			RefId:             "R_123",
			BranchProtections: make(map[string]*GithubBranchProtection),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Setup test data
		branchProtection := &GithubBranchProtection{
			Pattern: "main",
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call AddRepositoryBranchProtection
		remoteImpl.AddRepositoryBranchProtection(ctx, errorCollector, false, "test-repo", branchProtection)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "graphql error on AddRepositoryBranchProtection")

		// Verify the branch protection was not added to the cache
		assert.NotContains(t, repo.BranchProtections, "main")
	})
}

// DeleteBranchProtectionMockClient is a dedicated mock client for DeleteRepositoryBranchProtection tests
type DeleteBranchProtectionMockClient struct {
	// Track calls to verify test behavior
	lastGraphQLQuery string
	lastVariables    map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *DeleteBranchProtectionMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	m.lastGraphQLQuery = query
	m.lastVariables = variables

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}

	// Default successful response
	return []byte(`{
		"data": {
			"deleteBranchProtectionRule": {
				"clientMutationId": "test-mutation"
			}
		}
	}`), nil
}

func (m *DeleteBranchProtectionMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *DeleteBranchProtectionMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *DeleteBranchProtectionMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *DeleteBranchProtectionMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestDeleteRepositoryBranchProtection(t *testing.T) {
	t.Run("happy path: delete branch protection", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteBranchProtectionMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository with branch protection to the remote impl
		branchProtection := &GithubBranchProtection{
			Id:      "BP_123",
			Pattern: "main",
		}
		repo := &GithubRepository{
			Name:  "test-repo",
			Id:    123,
			RefId: "R_123",
			BranchProtections: map[string]*GithubBranchProtection{
				"main": branchProtection,
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteRepositoryBranchProtection
		remoteImpl.DeleteRepositoryBranchProtection(ctx, errorCollector, false, "test-repo", branchProtection)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the GraphQL query was made with correct variables
		expectedVariables := map[string]interface{}{
			"branchProtectionRuleId": "BP_123",
		}
		assert.Equal(t, expectedVariables, mockClient.lastVariables)
		assert.Contains(t, mockClient.lastGraphQLQuery, "deleteBranchProtectionRule")

		// Verify the branch protection was removed from the cache
		assert.NotContains(t, repo.BranchProtections, "main")
	})

	t.Run("error path: repository not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteBranchProtectionMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Setup test data
		branchProtection := &GithubBranchProtection{
			Id:      "BP_123",
			Pattern: "main",
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteRepositoryBranchProtection with non-existent repository
		remoteImpl.DeleteRepositoryBranchProtection(ctx, errorCollector, false, "non-existent-repo", branchProtection)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository non-existent-repo not found")

		// Verify no GraphQL query was made
		assert.Empty(t, mockClient.lastGraphQLQuery)
	})

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &DeleteBranchProtectionMockClient{
			shouldError:  true,
			errorMessage: "failed to delete branch protection",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository with branch protection to the remote impl
		branchProtection := &GithubBranchProtection{
			Id:      "BP_123",
			Pattern: "main",
		}
		repo := &GithubRepository{
			Name:  "test-repo",
			Id:    123,
			RefId: "R_123",
			BranchProtections: map[string]*GithubBranchProtection{
				"main": branchProtection,
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteRepositoryBranchProtection
		remoteImpl.DeleteRepositoryBranchProtection(ctx, errorCollector, false, "test-repo", branchProtection)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to delete branch protection for repository")

		// Verify the branch protection remains in the cache
		assert.Contains(t, repo.BranchProtections, "main")
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteBranchProtectionMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository with branch protection to the remote impl
		branchProtection := &GithubBranchProtection{
			Id:      "BP_123",
			Pattern: "main",
		}
		repo := &GithubRepository{
			Name:  "test-repo",
			Id:    123,
			RefId: "R_123",
			BranchProtections: map[string]*GithubBranchProtection{
				"main": branchProtection,
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteRepositoryBranchProtection in dry run mode
		remoteImpl.DeleteRepositoryBranchProtection(ctx, errorCollector, true, "test-repo", branchProtection)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no GraphQL query was made
		assert.Empty(t, mockClient.lastGraphQLQuery)

		// Verify the branch protection was removed from the cache
		assert.NotContains(t, repo.BranchProtections, "main")
	})

	t.Run("error path: GraphQL response error", func(t *testing.T) {
		// Setup mock client with error response
		mockClient := &DeleteBranchProtectionMockClient{
			responseBody: `{
				"errors": [
					{
						"message": "Branch protection rule not found",
						"path": ["deleteBranchProtectionRule"]
					}
				]
			}`,
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository with branch protection to the remote impl
		branchProtection := &GithubBranchProtection{
			Id:      "BP_123",
			Pattern: "main",
		}
		repo := &GithubRepository{
			Name:  "test-repo",
			Id:    123,
			RefId: "R_123",
			BranchProtections: map[string]*GithubBranchProtection{
				"main": branchProtection,
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteRepositoryBranchProtection
		remoteImpl.DeleteRepositoryBranchProtection(ctx, errorCollector, false, "test-repo", branchProtection)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "graphql error on DeleteRepositoryBranchProtection")

		// Verify the branch protection remains in the cache
		assert.Contains(t, repo.BranchProtections, "main")
	})

	t.Run("happy path: delete non-existent branch protection", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteBranchProtectionMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository without branch protection to the remote impl
		repo := &GithubRepository{
			Name:              "test-repo",
			Id:                123,
			RefId:             "R_123",
			BranchProtections: map[string]*GithubBranchProtection{},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Setup test data for non-existent branch protection
		branchProtection := &GithubBranchProtection{
			Id:      "BP_123",
			Pattern: "main",
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteRepositoryBranchProtection
		remoteImpl.DeleteRepositoryBranchProtection(ctx, errorCollector, false, "test-repo", branchProtection)

		// Verify no errors occurred
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "branch protection for repository test-repo not found")

		// Verify the cache remains empty
		assert.Empty(t, repo.BranchProtections)
	})
}

// UpdateBranchProtectionMockClient is a dedicated mock client for UpdateRepositoryBranchProtection tests
type UpdateBranchProtectionMockClient struct {
	// Track calls to verify test behavior
	lastGraphQLQuery string
	lastVariables    map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *UpdateBranchProtectionMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	m.lastGraphQLQuery = query
	m.lastVariables = variables

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}

	// Default successful response
	return []byte(`{
		"data": {
			"updateBranchProtectionRule": {
				"branchProtectionRule": {
					"id": "BP_123"
				}
			}
		}
	}`), nil
}

func (m *UpdateBranchProtectionMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *UpdateBranchProtectionMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *UpdateBranchProtectionMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *UpdateBranchProtectionMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestUpdateRepositoryBranchProtection(t *testing.T) {
	t.Run("happy path: update branch protection", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateBranchProtectionMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository with existing branch protection to the remote impl
		existingBranchProtection := &GithubBranchProtection{
			Id:                             "BP_123",
			Pattern:                        "main",
			RequiresApprovingReviews:       false,
			RequiredApprovingReviewCount:   1,
			DismissesStaleReviews:          false,
			RequiresCodeOwnerReviews:       false,
			RequireLastPushApproval:        false,
			RequiresStatusChecks:           false,
			RequiresStrictStatusChecks:     false,
			RequiredStatusCheckContexts:    []string{},
			RequiresConversationResolution: false,
			RequiresCommitSignatures:       false,
			RequiresLinearHistory:          false,
			AllowsForcePushes:              true,
			AllowsDeletions:                true,
		}
		repo := &GithubRepository{
			Name:  "test-repo",
			Id:    123,
			RefId: "R_123",
			BranchProtections: map[string]*GithubBranchProtection{
				"main": existingBranchProtection,
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Setup updated branch protection
		updatedBranchProtection := &GithubBranchProtection{
			Id:                             "BP_123",
			Pattern:                        "main",
			RequiresApprovingReviews:       true,
			RequiredApprovingReviewCount:   2,
			DismissesStaleReviews:          true,
			RequiresCodeOwnerReviews:       true,
			RequireLastPushApproval:        true,
			RequiresStatusChecks:           true,
			RequiresStrictStatusChecks:     true,
			RequiredStatusCheckContexts:    []string{"test-check"},
			RequiresConversationResolution: true,
			RequiresCommitSignatures:       true,
			RequiresLinearHistory:          true,
			AllowsForcePushes:              false,
			AllowsDeletions:                false,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryBranchProtection
		remoteImpl.UpdateRepositoryBranchProtection(ctx, errorCollector, false, "test-repo", updatedBranchProtection)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the GraphQL query was made with correct variables
		expectedVariables := map[string]interface{}{
			"branchProtectionRuleId":         "BP_123",
			"pattern":                        "main",
			"requiresApprovingReviews":       true,
			"requiredApprovingReviewCount":   2,
			"dismissesStaleReviews":          true,
			"requiresCodeOwnerReviews":       true,
			"requireLastPushApproval":        true,
			"requiresStatusChecks":           true,
			"requiresStrictStatusChecks":     true,
			"requiredStatusCheckContexts":    []string{"test-check"},
			"requiresConversationResolution": true,
			"requiresCommitSignatures":       true,
			"requiresLinearHistory":          true,
			"allowsForcePushes":              false,
			"allowsDeletions":                false,
		}
		assert.Equal(t, expectedVariables, mockClient.lastVariables)
		assert.Contains(t, mockClient.lastGraphQLQuery, "updateBranchProtectionRule")

		// Verify the branch protection was updated in the cache
		assert.Equal(t, updatedBranchProtection, repo.BranchProtections["main"])
	})

	t.Run("error path: repository not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateBranchProtectionMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Setup test data
		branchProtection := &GithubBranchProtection{
			Id:      "BP_123",
			Pattern: "main",
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryBranchProtection with non-existent repository
		remoteImpl.UpdateRepositoryBranchProtection(ctx, errorCollector, false, "non-existent-repo", branchProtection)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository non-existent-repo not found")

		// Verify no GraphQL query was made
		assert.Empty(t, mockClient.lastGraphQLQuery)
	})

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &UpdateBranchProtectionMockClient{
			shouldError:  true,
			errorMessage: "failed to update branch protection",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository with branch protection to the remote impl
		originalBranchProtection := &GithubBranchProtection{
			Id:      "BP_123",
			Pattern: "main",
		}
		repo := &GithubRepository{
			Name:  "test-repo",
			Id:    123,
			RefId: "R_123",
			BranchProtections: map[string]*GithubBranchProtection{
				"main": originalBranchProtection,
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Setup updated branch protection
		updatedBranchProtection := &GithubBranchProtection{
			Id:                       "BP_123",
			Pattern:                  "main",
			RequiresApprovingReviews: true,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryBranchProtection
		remoteImpl.UpdateRepositoryBranchProtection(ctx, errorCollector, false, "test-repo", updatedBranchProtection)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to update branch protection for repository")

		// Verify the branch protection remains unchanged in the cache
		assert.Equal(t, originalBranchProtection, repo.BranchProtections["main"])
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateBranchProtectionMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository with branch protection to the remote impl
		originalBranchProtection := &GithubBranchProtection{
			Id:      "BP_123",
			Pattern: "main",
		}
		repo := &GithubRepository{
			Name:  "test-repo",
			Id:    123,
			RefId: "R_123",
			BranchProtections: map[string]*GithubBranchProtection{
				"main": originalBranchProtection,
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Setup updated branch protection
		updatedBranchProtection := &GithubBranchProtection{
			Id:                       "BP_123",
			Pattern:                  "main",
			RequiresApprovingReviews: true,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryBranchProtection in dry run mode
		remoteImpl.UpdateRepositoryBranchProtection(ctx, errorCollector, true, "test-repo", updatedBranchProtection)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no GraphQL query was made
		assert.Empty(t, mockClient.lastGraphQLQuery)

		// Verify the branch protection was updated in the cache
		assert.Equal(t, updatedBranchProtection, repo.BranchProtections["main"])
	})

	t.Run("error path: GraphQL response error", func(t *testing.T) {
		// Setup mock client with error response
		mockClient := &UpdateBranchProtectionMockClient{
			responseBody: `{
				"errors": [
					{
						"message": "Invalid branch protection configuration",
						"path": ["updateBranchProtectionRule"]
					}
				]
			}`,
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository with branch protection to the remote impl
		originalBranchProtection := &GithubBranchProtection{
			Id:      "BP_123",
			Pattern: "main",
		}
		repo := &GithubRepository{
			Name:  "test-repo",
			Id:    123,
			RefId: "R_123",
			BranchProtections: map[string]*GithubBranchProtection{
				"main": originalBranchProtection,
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Setup updated branch protection
		updatedBranchProtection := &GithubBranchProtection{
			Id:                       "BP_123",
			Pattern:                  "main",
			RequiresApprovingReviews: true,
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryBranchProtection
		remoteImpl.UpdateRepositoryBranchProtection(ctx, errorCollector, false, "test-repo", updatedBranchProtection)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "graphql error on UpdateRepositoryBranchProtection")

		// Verify the branch protection remains unchanged in the cache
		assert.Equal(t, originalBranchProtection, repo.BranchProtections["main"])
	})
}
