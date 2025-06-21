package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/goliac-project/goliac/internal/observability"
	"github.com/stretchr/testify/assert"
)

// CreateRepositoryMockClient is a dedicated mock client for CreateRepository tests
type CreateRepositoryMockClient struct {
	// Track calls to verify test behavior
	lastEndpoint string
	lastMethod   string
	lastBody     map[string]interface{}

	// Configure mock responses
	shouldError    bool
	errorMessage   string
	responseID     int
	responseNodeID string
	responseName   string
}

func (m *CreateRepositoryMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *CreateRepositoryMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	// Store the call details for verification
	m.lastEndpoint = endpoint
	m.lastMethod = method
	m.lastBody = body

	if m.shouldError {
		return nil, errors.New(m.errorMessage)
	}

	// Return a successful response
	response := map[string]interface{}{
		"id":      m.responseID,
		"node_id": m.responseNodeID,
		"name":    m.responseName,
	}

	jsonResponse, _ := json.Marshal(response)
	return jsonResponse, nil
}

func (m *CreateRepositoryMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *CreateRepositoryMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *CreateRepositoryMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestCreateRepository(t *testing.T) {
	t.Run("happy path: create repository with fork", func(t *testing.T) {
		// Setup mock client
		mockClient := &CreateRepositoryMockClient{
			responseID:     123,
			responseNodeID: "R_123",
			responseName:   "forked-repo",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)
		ctx := context.TODO()
		// initiate the cache
		remoteImpl.Load(ctx, false)
		errorCollector := observability.NewErrorCollection()

		// Test creating a forked repository
		remoteImpl.CreateRepository(
			ctx,
			errorCollector,
			false,
			"forked-repo",
			"A forked repository",
			"private",
			[]string{},
			[]string{},
			map[string]bool{
				"delete_branch_on_merge": false,
				"allow_update_branch":    false,
				"archived":               false,
				"allow_auto_merge":       false,
			},
			"main",
			nil,
			"original-org/source-repo",
		)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the API call was made correctly
		assert.Equal(t, "/repos/original-org/source-repo/forks", mockClient.lastEndpoint)
		assert.Equal(t, "POST", mockClient.lastMethod)
		assert.Equal(t, map[string]interface{}{
			"organization": "myorg",
			"name":         "forked-repo",
		}, mockClient.lastBody)

		// Verify the repository was created in our cache
		repos := remoteImpl.Repositories(ctx)
		assert.Contains(t, repos, "forked-repo")

		// Verify the repository properties
		repo := repos["forked-repo"]
		assert.Equal(t, 123, repo.Id)
		assert.Equal(t, "R_123", repo.RefId)
		assert.Equal(t, "private", repo.Visibility)
		assert.Equal(t, "main", repo.DefaultBranchName)
		assert.True(t, repo.IsFork)
		assert.Equal(t, map[string]bool{
			"delete_branch_on_merge": false,
			"allow_update_branch":    false,
			"archived":               false,
			"allow_auto_merge":       false,
		}, repo.BoolProperties)
	})

	t.Run("happy path: create repository with fork and team", func(t *testing.T) {
		// Setup mock client
		mockClient := &CreateRepositoryMockClient{
			responseID:     123,
			responseNodeID: "R_123",
			responseName:   "forked-repo",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)
		ctx := context.TODO()
		// initiate the cache
		remoteImpl.Load(ctx, false)
		errorCollector := observability.NewErrorCollection()

		// Test creating a forked repository
		remoteImpl.CreateRepository(
			ctx,
			errorCollector,
			false,
			"forked-repo",
			"A forked repository",
			"private",
			[]string{"team1"},
			[]string{},
			map[string]bool{
				"delete_branch_on_merge": false,
				"allow_update_branch":    false,
				"archived":               false,
				"allow_auto_merge":       false,
			},
			"main",
			nil,
			"original-org/source-repo",
		)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the API call was made correctly
		assert.Equal(t, "orgs/myorg/teams/team1/repos/myorg/forked-repo", mockClient.lastEndpoint)
		assert.Equal(t, "PUT", mockClient.lastMethod)
		assert.Equal(t, map[string]interface{}{
			"permission": "push",
		}, mockClient.lastBody)

		// Verify the repository was created in our cache
		repos := remoteImpl.Repositories(ctx)
		assert.Contains(t, repos, "forked-repo")

		// Verify the repository properties
		repo := repos["forked-repo"]
		assert.Equal(t, 123, repo.Id)
		assert.Equal(t, "R_123", repo.RefId)
		assert.Equal(t, "private", repo.Visibility)
		assert.Equal(t, "main", repo.DefaultBranchName)
		assert.True(t, repo.IsFork)
		assert.Equal(t, map[string]bool{
			"delete_branch_on_merge": false,
			"allow_update_branch":    false,
			"archived":               false,
			"allow_auto_merge":       false,
		}, repo.BoolProperties)
	})

	t.Run("error path: invalid fork format", func(t *testing.T) {
		// Setup mock client
		mockClient := &CreateRepositoryMockClient{
			shouldError:  true,
			errorMessage: "invalid fork format",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)
		ctx := context.TODO()
		// initiate the cache
		remoteImpl.Load(ctx, false)
		errorCollector := observability.NewErrorCollection()

		// Test with invalid fork format
		remoteImpl.CreateRepository(
			ctx,
			errorCollector,
			false,
			"forked-repo",
			"A forked repository",
			"private",
			[]string{},
			[]string{},
			map[string]bool{},
			"main",
			nil,
			"invalid-fork-format", // Invalid format - should be "org/repo"
		)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "invalid fork format")
	})

	t.Run("error path: fork API error", func(t *testing.T) {
		// Setup mock client
		mockClient := &CreateRepositoryMockClient{
			shouldError:  true,
			errorMessage: "repository not found",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)
		ctx := context.TODO()
		// initiate the cache
		remoteImpl.Load(ctx, false)
		errorCollector := observability.NewErrorCollection()

		// Test forking a non-existent repository
		remoteImpl.CreateRepository(
			ctx,
			errorCollector,
			false,
			"forked-repo",
			"A forked repository",
			"private",
			[]string{},
			[]string{},
			map[string]bool{},
			"main",
			nil,
			"original-org/non-existent-repo",
		)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to fork repository")

		// Verify the API call was attempted
		assert.Equal(t, "/repos/original-org/non-existent-repo/forks", mockClient.lastEndpoint)
		assert.Equal(t, "POST", mockClient.lastMethod)
	})

	t.Run("happy path: create regular repository", func(t *testing.T) {
		// Setup mock client
		mockClient := &CreateRepositoryMockClient{
			responseID:     456,
			responseNodeID: "R_456",
			responseName:   "new-repo",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)
		ctx := context.TODO()
		// initiate the cache
		remoteImpl.Load(ctx, false)
		errorCollector := observability.NewErrorCollection()

		// Test creating a regular repository
		remoteImpl.CreateRepository(
			ctx,
			errorCollector,
			false,
			"new-repo",
			"A new repository",
			"private",
			[]string{},
			[]string{},
			map[string]bool{
				"delete_branch_on_merge": true,
				"allow_update_branch":    false,
				"archived":               false,
				"allow_auto_merge":       true,
			},
			"main",
			nil,
			"", // No fork
		)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the API call was made correctly
		assert.Equal(t, "/orgs/myorg/repos", mockClient.lastEndpoint)
		assert.Equal(t, "POST", mockClient.lastMethod)
		assert.Equal(t, map[string]interface{}{
			"name":                   "new-repo",
			"description":            "A new repository",
			"visibility":             "private",
			"default_branch":         "main",
			"delete_branch_on_merge": true,
			"allow_update_branch":    false,
			"archived":               false,
			"allow_auto_merge":       true,
		}, mockClient.lastBody)

		// Verify the repository was created in our cache
		repos := remoteImpl.Repositories(ctx)
		assert.Contains(t, repos, "new-repo")

		// Verify the repository properties
		repo := repos["new-repo"]
		assert.Equal(t, 456, repo.Id)
		assert.Equal(t, "R_456", repo.RefId)
		assert.Equal(t, "private", repo.Visibility)
		assert.Equal(t, "main", repo.DefaultBranchName)
		assert.False(t, repo.IsFork)
		assert.Equal(t, map[string]bool{
			"delete_branch_on_merge": true,
			"allow_update_branch":    false,
			"archived":               false,
			"allow_auto_merge":       true,
		}, repo.BoolProperties)
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &CreateRepositoryMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)
		ctx := context.TODO()
		// initiate the cache
		remoteImpl.Load(ctx, false)
		mockClient.lastEndpoint = ""
		mockClient.lastMethod = ""

		errorCollector := observability.NewErrorCollection()

		// Test creating a repository in dry run mode
		remoteImpl.CreateRepository(
			ctx,
			errorCollector,
			true, // dryrun
			"dry-run-repo",
			"A dry run repository",
			"private",
			[]string{},
			[]string{},
			map[string]bool{},
			"main",
			nil,
			"",
		)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
		assert.Empty(t, mockClient.lastMethod)

		// Verify the repository was still added to our cache
		repos := remoteImpl.Repositories(ctx)
		assert.Contains(t, repos, "dry-run-repo")
	})
}

// DeleteRepositoryMockClient is a dedicated mock client for DeleteRepository tests
type DeleteRepositoryMockClient struct {
	// Track calls to verify test behavior
	lastEndpoint string
	lastMethod   string

	// Configure mock responses
	shouldError  bool
	errorMessage string
}

func (m *DeleteRepositoryMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *DeleteRepositoryMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	// Store the call details for verification
	m.lastEndpoint = endpoint
	m.lastMethod = method

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	return []byte("{}"), nil
}

func (m *DeleteRepositoryMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *DeleteRepositoryMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *DeleteRepositoryMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestDeleteRepository(t *testing.T) {
	t.Run("happy path: delete existing repository", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteRepositoryMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Add a repository to the cache first
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name:   "test-repo",
				Id:     123,
				RefId:  "R_123",
				IsFork: false,
			},
		}
		remoteImpl.repositoriesByRefId = map[string]*GithubRepository{
			"R_123": remoteImpl.repositories["test-repo"],
		}

		// Delete the repository
		remoteImpl.DeleteRepository(
			ctx,
			errorCollector,
			false, // dryrun
			"test-repo",
		)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the API call was made correctly
		assert.Equal(t, "/repos/myorg/test-repo", mockClient.lastEndpoint)
		assert.Equal(t, "DELETE", mockClient.lastMethod)

		// Verify the repository was removed from the cache
		assert.NotContains(t, remoteImpl.Repositories(ctx), "test-repo")
		assert.NotContains(t, remoteImpl.repositoriesByRefId, "R_123")
	})

	t.Run("error path: delete non-existent repository", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteRepositoryMockClient{
			shouldError:  true,
			errorMessage: "repository not found",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Try to delete a non-existent repository
		remoteImpl.DeleteRepository(
			ctx,
			errorCollector,
			false,
			"non-existent-repo",
		)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to delete repository")
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository not found")

		// Verify the API call was attempted
		assert.Equal(t, "/repos/myorg/non-existent-repo", mockClient.lastEndpoint)
		assert.Equal(t, "DELETE", mockClient.lastMethod)
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteRepositoryMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)
		ctx := context.TODO()
		remoteImpl.Load(ctx, false)
		mockClient.lastEndpoint = ""
		mockClient.lastMethod = ""
		errorCollector := observability.NewErrorCollection()

		// Add a repository to the cache first
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name:   "test-repo",
				Id:     123,
				RefId:  "R_123",
				IsFork: false,
			},
		}
		remoteImpl.repositoriesByRefId = map[string]*GithubRepository{
			"R_123": remoteImpl.repositories["test-repo"],
		}

		// Delete the repository in dry run mode
		remoteImpl.DeleteRepository(
			ctx,
			errorCollector,
			true, // dryrun
			"test-repo",
		)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
		assert.Empty(t, mockClient.lastMethod)
	})

	t.Run("happy path: delete repository with team references", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteRepositoryMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Add a repository to the cache with team references
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name:   "test-repo",
				Id:     123,
				RefId:  "R_123",
				IsFork: false,
			},
		}
		remoteImpl.repositoriesByRefId = map[string]*GithubRepository{
			"R_123": remoteImpl.repositories["test-repo"],
		}
		remoteImpl.teamRepos = map[string]map[string]*GithubTeamRepo{
			"team1": {
				"test-repo": &GithubTeamRepo{
					Name:       "test-repo",
					Permission: "WRITE",
				},
			},
			"team2": {
				"test-repo": &GithubTeamRepo{
					Name:       "test-repo",
					Permission: "READ",
				},
			},
		}

		// Delete the repository
		remoteImpl.DeleteRepository(
			ctx,
			errorCollector,
			false,
			"test-repo",
		)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the API call was made correctly
		assert.Equal(t, "/repos/myorg/test-repo", mockClient.lastEndpoint)
		assert.Equal(t, "DELETE", mockClient.lastMethod)

		// Verify the repository was removed from the cache
		assert.NotContains(t, remoteImpl.Repositories(ctx), "test-repo")
		assert.NotContains(t, remoteImpl.repositoriesByRefId, "R_123")

		// Verify the repository was removed from team references
		assert.NotContains(t, remoteImpl.teamRepos["team1"], "test-repo")
		assert.NotContains(t, remoteImpl.teamRepos["team2"], "test-repo")
	})
}

// RenameRepositoryMockClient is a dedicated mock client for RenameRepository tests
type RenameRepositoryMockClient struct {
	// Track calls to verify test behavior
	lastEndpoint string
	lastMethod   string
	lastBody     map[string]interface{}

	// Configure mock responses
	shouldError    bool
	errorMessage   string
	responseID     int
	responseNodeID string
	responseName   string
}

func (m *RenameRepositoryMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *RenameRepositoryMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	// Store the call details for verification
	m.lastEndpoint = endpoint
	m.lastMethod = method
	m.lastBody = body

	if m.shouldError {
		return nil, errors.New(m.errorMessage)
	}

	// Return a successful response
	response := map[string]interface{}{
		"id":      m.responseID,
		"node_id": m.responseNodeID,
		"name":    m.responseName,
	}

	jsonResponse, _ := json.Marshal(response)
	return jsonResponse, nil
}

func (m *RenameRepositoryMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *RenameRepositoryMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *RenameRepositoryMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestRenameRepository(t *testing.T) {
	t.Run("happy path: rename existing repository", func(t *testing.T) {
		// Setup mock client
		mockClient := &RenameRepositoryMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)
		ctx := context.TODO()
		remoteImpl.Load(ctx, false)
		errorCollector := observability.NewErrorCollection()

		// Add a repository to the cache first
		repo := &GithubRepository{
			Name:   "old-name",
			Id:     123,
			RefId:  "R_123",
			IsFork: false,
			BoolProperties: map[string]bool{
				"archived": false,
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"old-name": repo,
		}
		remoteImpl.repositoriesByRefId = map[string]*GithubRepository{
			"R_123": repo,
		}

		// Add team references
		remoteImpl.teamRepos = map[string]map[string]*GithubTeamRepo{
			"team1": {
				"old-name": &GithubTeamRepo{
					Name:       "old-name",
					Permission: "WRITE",
				},
			},
			"team2": {
				"old-name": &GithubTeamRepo{
					Name:       "old-name",
					Permission: "READ",
				},
			},
		}

		// Rename the repository
		remoteImpl.RenameRepository(
			ctx,
			errorCollector,
			false, // dryrun
			"old-name",
			"new-name",
		)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the API call was made correctly
		assert.Equal(t, "/repos/myorg/old-name", mockClient.lastEndpoint)
		assert.Equal(t, "PATCH", mockClient.lastMethod)
		assert.Equal(t, map[string]interface{}{
			"name": "new-name",
		}, mockClient.lastBody)

		// Verify the repository was renamed in the cache
		assert.NotContains(t, remoteImpl.Repositories(ctx), "old-name")
		assert.Contains(t, remoteImpl.Repositories(ctx), "new-name")

		// Verify RefId mapping is updated
		assert.Equal(t, "new-name", remoteImpl.repositoriesByRefId["R_123"].Name)

		// Verify team references are updated
		assert.NotContains(t, remoteImpl.teamRepos["team1"], "old-name")
		assert.Contains(t, remoteImpl.teamRepos["team1"], "new-name")
		assert.Equal(t, "WRITE", remoteImpl.teamRepos["team1"]["new-name"].Permission)

		assert.NotContains(t, remoteImpl.teamRepos["team2"], "old-name")
		assert.Contains(t, remoteImpl.teamRepos["team2"], "new-name")
		assert.Equal(t, "READ", remoteImpl.teamRepos["team2"]["new-name"].Permission)
	})

	t.Run("error path: rename non-existent repository", func(t *testing.T) {
		// Setup mock client
		mockClient := &RenameRepositoryMockClient{
			shouldError:  true,
			errorMessage: "repository not found",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Try to rename a non-existent repository
		remoteImpl.RenameRepository(
			ctx,
			errorCollector,
			false,
			"non-existent-repo",
			"new-name",
		)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to rename the repository")
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository not found")

		// Verify the API call was attempted
		assert.Equal(t, "/repos/myorg/non-existent-repo", mockClient.lastEndpoint)
		assert.Equal(t, "PATCH", mockClient.lastMethod)
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &RenameRepositoryMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)
		ctx := context.TODO()
		remoteImpl.Load(ctx, false)
		mockClient.lastEndpoint = ""
		mockClient.lastMethod = ""
		errorCollector := observability.NewErrorCollection()

		// Add a repository to the cache first
		repo := &GithubRepository{
			Name:   "old-name",
			Id:     123,
			RefId:  "R_123",
			IsFork: false,
			BoolProperties: map[string]bool{
				"archived": false,
			},
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"old-name": repo,
		}
		remoteImpl.repositoriesByRefId = map[string]*GithubRepository{
			"R_123": repo,
		}

		// Add team references
		remoteImpl.teamRepos = map[string]map[string]*GithubTeamRepo{
			"team1": {
				"old-name": &GithubTeamRepo{
					Name:       "old-name",
					Permission: "WRITE",
				},
			},
		}

		// Rename the repository in dry run mode
		remoteImpl.RenameRepository(
			ctx,
			errorCollector,
			true, // dryrun
			"old-name",
			"new-name",
		)

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
		assert.Empty(t, mockClient.lastMethod)

		// Verify the repository name remains unchanged in the cache
		assert.Contains(t, remoteImpl.Repositories(ctx), "old-name")
		assert.NotContains(t, remoteImpl.Repositories(ctx), "new-name")

		// Verify team references remain unchanged
		assert.Contains(t, remoteImpl.teamRepos["team1"], "old-name")
		assert.NotContains(t, remoteImpl.teamRepos["team1"], "new-name")
	})

	t.Run("error path: rename to existing repository name", func(t *testing.T) {
		// Setup mock client
		mockClient := &RenameRepositoryMockClient{
			shouldError:  true,
			errorMessage: "repository with this name already exists",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)
		ctx := context.TODO()
		remoteImpl.Load(ctx, false)
		mockClient.lastEndpoint = ""
		mockClient.lastMethod = ""
		errorCollector := observability.NewErrorCollection()

		// Add two repositories to the cache
		repo1 := &GithubRepository{
			Name:   "old-name",
			Id:     123,
			RefId:  "R_123",
			IsFork: false,
		}
		repo2 := &GithubRepository{
			Name:   "existing-name",
			Id:     456,
			RefId:  "R_456",
			IsFork: false,
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"old-name":      repo1,
			"existing-name": repo2,
		}
		remoteImpl.repositoriesByRefId = map[string]*GithubRepository{
			"R_123": repo1,
			"R_456": repo2,
		}

		// Try to rename to an existing name
		remoteImpl.RenameRepository(
			ctx,
			errorCollector,
			false,
			"old-name",
			"existing-name",
		)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to rename the repository")
		assert.Contains(t, errorCollector.Errors[0].Error(), "the new name is already used")

	})
}
