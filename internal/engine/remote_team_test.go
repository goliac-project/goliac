package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/goliac-project/goliac/internal/observability"
	"github.com/goliac-project/goliac/internal/utils"
	"github.com/stretchr/testify/assert"
)

// CreateTeamMockClient is a dedicated mock client for CreateTeam tests
type CreateTeamMockClient struct {
	// Track REST API calls
	lastEndpoint string
	lastMethod   string
	lastBody     map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *CreateTeamMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *CreateTeamMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.lastEndpoint = endpoint
	m.lastMethod = method
	m.lastBody = body

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}

	// Default successful response
	return []byte(`{
		"id": 123,
		"node_id": "T_123",
		"name": "test-team",
		"slug": "test-team",
		"description": "Test team description"
	}`), nil
}

func (m *CreateTeamMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *CreateTeamMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *CreateTeamMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestCreateTeam(t *testing.T) {
	t.Run("happy path: create team", func(t *testing.T) {
		// Setup mock client
		mockClient := &CreateTeamMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call CreateTeam with all required arguments
		remoteImpl.CreateTeam(ctx, errorCollector, false, "test-team", "Test team description", nil, []string{})

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the REST API call was made correctly
		assert.Equal(t, "/orgs/myorg/teams", mockClient.lastEndpoint)
		assert.Equal(t, "POST", mockClient.lastMethod)

		// Verify the request body
		expectedBody := map[string]interface{}{
			"name":        "test-team",
			"description": "Test team description",
			"privacy":     "closed",
		}
		assert.True(t, utils.DeepEqualUnordered(expectedBody, mockClient.lastBody))

		// Verify the team was added to the cache
		assert.Contains(t, remoteImpl.teams, "test-team")
		assert.Equal(t, 123, remoteImpl.teams["test-team"].Id)
		assert.Equal(t, "test-team", remoteImpl.teams["test-team"].Slug)
	})

	t.Run("happy path: create team with members", func(t *testing.T) {
		// Setup mock client
		mockClient := &CreateTeamMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()
		parentTeamId := 456

		// Call CreateTeam with all required arguments
		remoteImpl.CreateTeam(ctx, errorCollector, false, "test-team", "Test team description", &parentTeamId, []string{"member1", "member2"})

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the last REST API call was made correctly
		assert.Equal(t, "orgs/myorg/teams/test-team/memberships/member2", mockClient.lastEndpoint)
		assert.Equal(t, "PUT", mockClient.lastMethod)

		// Verify the request body
		expectedBody := map[string]interface{}{
			"role": "member",
		}
		assert.True(t, utils.DeepEqualUnordered(expectedBody, mockClient.lastBody))

		// Verify the team was added to the cache
		assert.Contains(t, remoteImpl.teams, "test-team")
		assert.Equal(t, 123, remoteImpl.teams["test-team"].Id)
		assert.Equal(t, "test-team", remoteImpl.teams["test-team"].Slug)
	})

	t.Run("error path: team already exists", func(t *testing.T) {
		// Setup mock client
		mockClient := &CreateTeamMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add existing team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
			},
		}

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""

		errorCollector := observability.NewErrorCollection()

		// Call CreateTeam with all required arguments
		remoteImpl.CreateTeam(ctx, errorCollector, false, "test-team", "Test team description", nil, nil)

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "team test-team already exists")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &CreateTeamMockClient{
			shouldError:  true,
			errorMessage: "failed to create team",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call CreateTeam with all required arguments
		remoteImpl.CreateTeam(ctx, errorCollector, false, "test-team", "Test team description", nil, []string{"member1"})

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to create team")

		// Verify the team was not added to the cache
		assert.NotContains(t, remoteImpl.teams, "test-team")
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &CreateTeamMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()
		parentTeamId := 456

		// Call CreateTeam in dry run mode with all required arguments
		remoteImpl.CreateTeam(ctx, errorCollector, true, "test-team", "Test team description", &parentTeamId, []string{"member1"})

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)

		// Verify the team was added to the cache
		assert.Contains(t, remoteImpl.teams, "test-team")
	})

	t.Run("error path: parent team not found", func(t *testing.T) {
		// Setup mock client with parent team error
		mockClient := &CreateTeamMockClient{
			responseBody: `{"message": "Parent team does not exist"}`,
			shouldError:  true,
			errorMessage: "Parent team does not exist",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()
		invalidParentId := 999

		// Call CreateTeam with non-existent parent team
		remoteImpl.CreateTeam(ctx, errorCollector, false, "test-team", "Test team description", &invalidParentId, []string{"member1"})

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "Parent team does not exist")

		// Verify the team was not added to the cache
		assert.NotContains(t, remoteImpl.teams, "test-team")
	})

	t.Run("error path: invalid team name", func(t *testing.T) {
		// Setup mock client with validation error
		mockClient := &CreateTeamMockClient{
			responseBody: `{"message": "Invalid team name"}`,
			shouldError:  true,
			errorMessage: "Invalid team name",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call CreateTeam with invalid team name
		remoteImpl.CreateTeam(ctx, errorCollector, false, "", "Test team description", nil, []string{"member1"})

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "Invalid team name")

		// Verify the team was not added to the cache
		assert.NotContains(t, remoteImpl.teams, "")
	})
}

// DeleteTeamMockClient is a dedicated mock client for DeleteTeam tests
type DeleteTeamMockClient struct {
	// Track REST API calls
	lastEndpoint string
	lastMethod   string
	lastBody     map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *DeleteTeamMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *DeleteTeamMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.lastEndpoint = endpoint
	m.lastMethod = method
	m.lastBody = body

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}

	return []byte(`{}`), nil
}

func (m *DeleteTeamMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *DeleteTeamMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *DeleteTeamMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestDeleteTeam(t *testing.T) {
	t.Run("happy path: delete existing team", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteTeamMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteTeam
		remoteImpl.DeleteTeam(ctx, errorCollector, false, "test-team")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the REST API call was made correctly
		assert.Equal(t, "/orgs/myorg/teams/test-team", mockClient.lastEndpoint)
		assert.Equal(t, "DELETE", mockClient.lastMethod)

		// Verify the team was removed from the cache
		assert.NotContains(t, remoteImpl.teams, "test-team")
	})

	t.Run("error path: team not found in cache", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteTeamMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Call DeleteTeam with non-existent team
		remoteImpl.DeleteTeam(ctx, errorCollector, false, "non-existent-team")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "team non-existent-team not found")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &DeleteTeamMockClient{
			shouldError:  true,
			errorMessage: "failed to delete team",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteTeam
		remoteImpl.DeleteTeam(ctx, errorCollector, false, "test-team")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to delete team")

		// Verify the team remains in the cache
		assert.Contains(t, remoteImpl.teams, "test-team")
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &DeleteTeamMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Call DeleteTeam in dry run mode
		remoteImpl.DeleteTeam(ctx, errorCollector, true, "test-team")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)

		// Verify the team was removed from the cache
		assert.NotContains(t, remoteImpl.teams, "test-team")
	})

	t.Run("error path: API not found response", func(t *testing.T) {
		// Setup mock client with 404 response
		mockClient := &DeleteTeamMockClient{
			responseBody: `{"message": "Not Found"}`,
			shouldError:  true,
			errorMessage: "Not Found",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteTeam
		remoteImpl.DeleteTeam(ctx, errorCollector, false, "test-team")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "Not Found")

		// Verify the team remains in the cache since deletion failed
		assert.Contains(t, remoteImpl.teams, "test-team")
	})

	t.Run("error path: team with repositories", func(t *testing.T) {
		// Setup mock client with error indicating team has repositories
		mockClient := &DeleteTeamMockClient{
			responseBody: `{"message": "Cannot delete team with repositories"}`,
			shouldError:  true,
			errorMessage: "Cannot delete team with repositories",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call DeleteTeam
		remoteImpl.DeleteTeam(ctx, errorCollector, false, "test-team")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "Cannot delete team with repositories")

		// Verify the team remains in the cache
		assert.Contains(t, remoteImpl.teams, "test-team")
	})
}

// UpdateTeamAddMemberMockClient is a dedicated mock client for UpdateTeamAddMember tests
type UpdateTeamAddMemberMockClient struct {
	// Track REST API calls
	lastEndpoint string
	lastMethod   string
	lastBody     map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *UpdateTeamAddMemberMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *UpdateTeamAddMemberMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.lastEndpoint = endpoint
	m.lastMethod = method
	m.lastBody = body

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}

	// Default successful response
	return []byte(`{
		"url": "https://api.github.com/teams/123/memberships/testuser",
		"role": "member",
		"state": "active"
	}`), nil
}

func (m *UpdateTeamAddMemberMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *UpdateTeamAddMemberMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *UpdateTeamAddMemberMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestUpdateTeamAddMember(t *testing.T) {
	t.Run("happy path: add member to team", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateTeamAddMemberMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamAddMember
		remoteImpl.UpdateTeamAddMember(ctx, errorCollector, false, "test-team", "testuser", "member")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the REST API call was made correctly
		assert.Equal(t, "/orgs/myorg/teams/test-team/memberships/testuser", mockClient.lastEndpoint)
		assert.Equal(t, "PUT", mockClient.lastMethod)

		// Verify the request body
		expectedBody := map[string]interface{}{
			"role": "member",
		}
		assert.True(t, utils.DeepEqualUnordered(expectedBody, mockClient.lastBody))
	})

	t.Run("happy path: add maintainer to team", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateTeamAddMemberMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamAddMember with maintainer role
		remoteImpl.UpdateTeamAddMember(ctx, errorCollector, false, "test-team", "testuser", "maintainer")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the REST API call was made correctly
		assert.Equal(t, "/orgs/myorg/teams/test-team/memberships/testuser", mockClient.lastEndpoint)
		assert.Equal(t, "PUT", mockClient.lastMethod)

		// Verify the request body
		expectedBody := map[string]interface{}{
			"role": "maintainer",
		}
		assert.True(t, utils.DeepEqualUnordered(expectedBody, mockClient.lastBody))
	})

	t.Run("error path: team not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateTeamAddMemberMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamAddMember with non-existent team
		remoteImpl.UpdateTeamAddMember(ctx, errorCollector, false, "non-existent-team", "testuser", "member")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "team non-existent-team not found")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	// t.Run("error path: invalid role", func(t *testing.T) {
	// 	// Setup mock client
	// 	mockClient := &UpdateTeamAddMemberMockClient{}
	// 	remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg",true)

	// 	// Add team to the cache
	// 	remoteImpl.teams = map[string]*GithubTeam{
	// 		"test-team": {
	// 			Name: "test-team",
	// 			Id:   123,
	// 			Slug: "test-team",
	// 		},
	// 	}

	// 	ctx := context.TODO()
	// 	errorCollector := observability.NewErrorCollection()

	// 	// Call UpdateTeamAddMember with invalid role
	// 	remoteImpl.UpdateTeamAddMember(ctx, errorCollector, false, "test-team", "testuser", "invalid-role")

	// 	// Verify error was collected
	// 	assert.True(t, errorCollector.HasErrors())
	// 	assert.Contains(t, errorCollector.Errors[0].Error(), "invalid role")

	// 	// Verify no API call was made
	// 	assert.Empty(t, mockClient.lastEndpoint)
	// })

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &UpdateTeamAddMemberMockClient{
			shouldError:  true,
			errorMessage: "failed to add member to team",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamAddMember
		remoteImpl.UpdateTeamAddMember(ctx, errorCollector, false, "test-team", "testuser", "member")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to add member to team")
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateTeamAddMemberMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamAddMember in dry run mode
		remoteImpl.UpdateTeamAddMember(ctx, errorCollector, true, "test-team", "testuser", "member")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: empty username", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateTeamAddMemberMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamAddMember with empty username
		remoteImpl.UpdateTeamAddMember(ctx, errorCollector, false, "test-team", "", "member")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "invalid username")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})
}

// UpdateTeamRemoveMemberMockClient is a dedicated mock client for UpdateTeamRemoveMember tests
type UpdateTeamRemoveMemberMockClient struct {
	// Track REST API calls
	lastEndpoint string
	lastMethod   string
	lastBody     map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *UpdateTeamRemoveMemberMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *UpdateTeamRemoveMemberMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.lastEndpoint = endpoint
	m.lastMethod = method
	m.lastBody = body

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}

	return []byte(`{}`), nil
}

func (m *UpdateTeamRemoveMemberMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *UpdateTeamRemoveMemberMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *UpdateTeamRemoveMemberMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestUpdateTeamRemoveMember(t *testing.T) {
	t.Run("happy path: remove member from team", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateTeamRemoveMemberMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamRemoveMember
		remoteImpl.UpdateTeamRemoveMember(ctx, errorCollector, false, "test-team", "testuser")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the REST API call was made correctly
		assert.Equal(t, "orgs/myorg/teams/test-team/memberships/testuser", mockClient.lastEndpoint)
		assert.Equal(t, "DELETE", mockClient.lastMethod)
	})

	t.Run("error path: team not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateTeamRemoveMemberMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamRemoveMember with non-existent team
		remoteImpl.UpdateTeamRemoveMember(ctx, errorCollector, false, "non-existent-team", "testuser")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "team non-existent-team not found")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &UpdateTeamRemoveMemberMockClient{
			shouldError:  true,
			errorMessage: "failed to remove member from team",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamRemoveMember
		remoteImpl.UpdateTeamRemoveMember(ctx, errorCollector, false, "test-team", "testuser")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to remove member from team")
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateTeamRemoveMemberMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamRemoveMember in dry run mode
		remoteImpl.UpdateTeamRemoveMember(ctx, errorCollector, true, "test-team", "testuser")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: empty username", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateTeamRemoveMemberMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamRemoveMember with empty username
		remoteImpl.UpdateTeamRemoveMember(ctx, errorCollector, false, "test-team", "")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "invalid username")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: member not found", func(t *testing.T) {
		// Setup mock client with not found error
		mockClient := &UpdateTeamRemoveMemberMockClient{
			responseBody: `{"message": "Not Found"}`,
			shouldError:  true,
			errorMessage: "Not Found",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamRemoveMember with non-existent member
		remoteImpl.UpdateTeamRemoveMember(ctx, errorCollector, false, "test-team", "non-existent-user")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "Not Found")
	})
}

// UpdateTeamUpdateMemberMockClient is a dedicated mock client for UpdateTeamUpdateMember tests
type UpdateTeamUpdateMemberMockClient struct {
	// Track REST API calls
	lastEndpoint string
	lastMethod   string
	lastBody     map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *UpdateTeamUpdateMemberMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *UpdateTeamUpdateMemberMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.lastEndpoint = endpoint
	m.lastMethod = method
	m.lastBody = body

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}

	// Default successful response
	return []byte(`{
		"url": "https://api.github.com/teams/123/memberships/testuser",
		"role": "maintainer",
		"state": "active"
	}`), nil
}

func (m *UpdateTeamUpdateMemberMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *UpdateTeamUpdateMemberMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *UpdateTeamUpdateMemberMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestUpdateTeamUpdateMember(t *testing.T) {
	t.Run("happy path: update member role to maintainer", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateTeamUpdateMemberMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamUpdateMember
		remoteImpl.UpdateTeamUpdateMember(ctx, errorCollector, false, "test-team", "testuser", "maintainer")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the REST API call was made correctly
		assert.Equal(t, "/orgs/myorg/teams/test-team/memberships/testuser", mockClient.lastEndpoint)
		assert.Equal(t, "PUT", mockClient.lastMethod)

		// Verify the request body
		expectedBody := map[string]interface{}{
			"role": "maintainer",
		}
		assert.True(t, utils.DeepEqualUnordered(expectedBody, mockClient.lastBody))
	})

	t.Run("happy path: update member role to member", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateTeamUpdateMemberMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamUpdateMember
		remoteImpl.UpdateTeamUpdateMember(ctx, errorCollector, false, "test-team", "testuser", "member")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the REST API call was made correctly
		assert.Equal(t, "/orgs/myorg/teams/test-team/memberships/testuser", mockClient.lastEndpoint)
		assert.Equal(t, "PUT", mockClient.lastMethod)

		// Verify the request body
		expectedBody := map[string]interface{}{
			"role": "member",
		}
		assert.True(t, utils.DeepEqualUnordered(expectedBody, mockClient.lastBody))
	})

	t.Run("error path: team not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateTeamUpdateMemberMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamUpdateMember with non-existent team
		remoteImpl.UpdateTeamUpdateMember(ctx, errorCollector, false, "non-existent-team", "testuser", "member")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "team non-existent-team not found")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	// t.Run("error path: invalid role", func(t *testing.T) {
	// 	// Setup mock client
	// 	mockClient := &UpdateTeamUpdateMemberMockClient{}
	// 	remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg",true)

	// 	// Add team to the cache
	// 	remoteImpl.teams = map[string]*GithubTeam{
	// 		"test-team": {
	// 			Name: "test-team",
	// 			Id:   123,
	// 			Slug: "test-team",
	// 		},
	// 	}

	// 	ctx := context.TODO()
	// 	errorCollector := observability.NewErrorCollection()

	// 	// Call UpdateTeamUpdateMember with invalid role
	// 	remoteImpl.UpdateTeamUpdateMember(ctx, errorCollector, false, "test-team", "testuser", "invalid-role")

	// 	// Verify error was collected
	// 	assert.True(t, errorCollector.HasErrors())
	// 	assert.Contains(t, errorCollector.Errors[0].Error(), "invalid role")

	// 	// Verify no API call was made
	// 	assert.Empty(t, mockClient.lastEndpoint)
	// })

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &UpdateTeamUpdateMemberMockClient{
			shouldError:  true,
			errorMessage: "failed to update team member",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamUpdateMember
		remoteImpl.UpdateTeamUpdateMember(ctx, errorCollector, false, "test-team", "testuser", "maintainer")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to update team member")
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateTeamUpdateMemberMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamUpdateMember in dry run mode
		remoteImpl.UpdateTeamUpdateMember(ctx, errorCollector, true, "test-team", "testuser", "maintainer")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: empty username", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateTeamUpdateMemberMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamUpdateMember with empty username
		remoteImpl.UpdateTeamUpdateMember(ctx, errorCollector, false, "test-team", "", "maintainer")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "invalid username")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: member not found", func(t *testing.T) {
		// Setup mock client with not found error
		mockClient := &UpdateTeamUpdateMemberMockClient{
			responseBody: `{"message": "Not Found"}`,
			shouldError:  true,
			errorMessage: "Not Found",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   123,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateTeamUpdateMember with non-existent member
		remoteImpl.UpdateTeamUpdateMember(ctx, errorCollector, false, "test-team", "non-existent-user", "maintainer")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "Not Found")
	})
}

// UpdateRepositoryAddTeamAccessMockClient is a dedicated mock client for UpdateRepositoryAddTeamAccess tests
type UpdateRepositoryAddTeamAccessMockClient struct {
	// Track REST API calls
	lastEndpoint string
	lastMethod   string
	lastBody     map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *UpdateRepositoryAddTeamAccessMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *UpdateRepositoryAddTeamAccessMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.lastEndpoint = endpoint
	m.lastMethod = method
	m.lastBody = body

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}

	return []byte(`{}`), nil
}

func (m *UpdateRepositoryAddTeamAccessMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *UpdateRepositoryAddTeamAccessMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *UpdateRepositoryAddTeamAccessMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestUpdateRepositoryAddTeamAccess(t *testing.T) {
	t.Run("happy path: add team access with pull permission", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryAddTeamAccessMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository and team to the cache
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name: "test-repo",
				Id:   123,
			},
		}
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   456,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		remoteImpl.loadRepositories(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryAddTeamAccess
		remoteImpl.UpdateRepositoryAddTeamAccess(ctx, errorCollector, false, "test-repo", "test-team", "pull")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the REST API call was made correctly
		assert.Equal(t, "/orgs/myorg/teams/test-team/repos/myorg/test-repo", mockClient.lastEndpoint)
		assert.Equal(t, "PUT", mockClient.lastMethod)

		// Verify the request body
		expectedBody := map[string]interface{}{
			"permission": "pull",
		}
		assert.True(t, utils.DeepEqualUnordered(expectedBody, mockClient.lastBody))
	})

	t.Run("happy path: add team access with push permission", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryAddTeamAccessMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository and team to the cache
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name: "test-repo",
				Id:   123,
			},
		}
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   456,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryAddTeamAccess
		remoteImpl.UpdateRepositoryAddTeamAccess(ctx, errorCollector, false, "test-repo", "test-team", "push")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the REST API call was made correctly
		assert.Equal(t, "/orgs/myorg/teams/test-team/repos/myorg/test-repo", mockClient.lastEndpoint)
		assert.Equal(t, "PUT", mockClient.lastMethod)

		// Verify the request body
		expectedBody := map[string]interface{}{
			"permission": "push",
		}
		assert.True(t, utils.DeepEqualUnordered(expectedBody, mockClient.lastBody))
	})

	t.Run("error path: repository not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryAddTeamAccessMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add only team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   456,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		remoteImpl.loadRepositories(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryAddTeamAccess with non-existent repository
		remoteImpl.UpdateRepositoryAddTeamAccess(ctx, errorCollector, false, "non-existent-repo", "test-team", "pull")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository non-existent-repo not found")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: team not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryAddTeamAccessMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add only repository to the cache
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name: "test-repo",
				Id:   123,
			},
		}

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		remoteImpl.loadRepositories(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryAddTeamAccess with non-existent team
		remoteImpl.UpdateRepositoryAddTeamAccess(ctx, errorCollector, false, "test-repo", "non-existent-team", "pull")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "team non-existent-team not found")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	// t.Run("error path: invalid permission", func(t *testing.T) {
	// 	// Setup mock client
	// 	mockClient := &UpdateRepositoryAddTeamAccessMockClient{}
	// 	remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg",true)

	// 	// Add repository and team to the cache
	// 	remoteImpl.repositories = map[string]*GithubRepository{
	// 		"test-repo": {
	// 			Name: "test-repo",
	// 			Id:   123,
	// 		},
	// 	}
	// 	remoteImpl.teams = map[string]*GithubTeam{
	// 		"test-team": {
	// 			Name: "test-team",
	// 			Id:   456,
	// 			Slug: "test-team",
	// 		},
	// 	}

	// 	ctx := context.TODO()
	// 	remoteImpl.loadTeams(ctx)
	// 	remoteImpl.loadRepositories(ctx)
	// 	mockClient.lastEndpoint = ""
	// 	errorCollector := observability.NewErrorCollection()

	// 	// Call UpdateRepositoryAddTeamAccess with invalid permission
	// 	remoteImpl.UpdateRepositoryAddTeamAccess(ctx, errorCollector, false, "test-repo", "test-team", "invalid")

	// 	// Verify error was collected
	// 	assert.True(t, errorCollector.HasErrors())
	// 	assert.Contains(t, errorCollector.Errors[0].Error(), "invalid permission")

	// 	// Verify no API call was made
	// 	assert.Empty(t, mockClient.lastEndpoint)
	// })

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &UpdateRepositoryAddTeamAccessMockClient{
			shouldError:  true,
			errorMessage: "failed to add team access",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Add repository and team to the cache
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name: "test-repo",
				Id:   123,
			},
		}
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   456,
				Slug: "test-team",
			},
		}

		// Call UpdateRepositoryAddTeamAccess
		remoteImpl.UpdateRepositoryAddTeamAccess(ctx, errorCollector, false, "test-repo", "test-team", "pull")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to add team access")
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryAddTeamAccessMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Add repository and team to the cache
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name: "test-repo",
				Id:   123,
			},
		}
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   456,
				Slug: "test-team",
			},
		}

		// Call UpdateRepositoryAddTeamAccess in dry run mode
		remoteImpl.UpdateRepositoryAddTeamAccess(ctx, errorCollector, true, "test-repo", "test-team", "pull")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})
}

// UpdateRepositoryUpdateTeamAccessMockClient is a dedicated mock client for UpdateRepositoryUpdateTeamAccess tests
type UpdateRepositoryUpdateTeamAccessMockClient struct {
	// Track REST API calls
	lastEndpoint string
	lastMethod   string
	lastBody     map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *UpdateRepositoryUpdateTeamAccessMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *UpdateRepositoryUpdateTeamAccessMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.lastEndpoint = endpoint
	m.lastMethod = method
	m.lastBody = body

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}

	return []byte(`{}`), nil
}

func (m *UpdateRepositoryUpdateTeamAccessMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *UpdateRepositoryUpdateTeamAccessMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *UpdateRepositoryUpdateTeamAccessMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestUpdateRepositoryUpdateTeamAccess(t *testing.T) {
	t.Run("happy path: update team access to pull permission", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryUpdateTeamAccessMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		remoteImpl.loadRepositories(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Add repository and team to the cache
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name: "test-repo",
				Id:   123,
			},
		}
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   456,
				Slug: "test-team",
			},
		}

		// Call UpdateRepositoryUpdateTeamAccess
		remoteImpl.UpdateRepositoryUpdateTeamAccess(ctx, errorCollector, false, "test-repo", "test-team", "pull")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the REST API call was made correctly
		assert.Equal(t, "/orgs/myorg/teams/test-team/repos/myorg/test-repo", mockClient.lastEndpoint)
		assert.Equal(t, "PUT", mockClient.lastMethod)

		// Verify the request body
		expectedBody := map[string]interface{}{
			"permission": "pull",
		}
		assert.True(t, utils.DeepEqualUnordered(expectedBody, mockClient.lastBody))
	})

	t.Run("happy path: update team access to push permission", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryUpdateTeamAccessMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository and team to the cache
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name: "test-repo",
				Id:   123,
			},
		}
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   456,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryUpdateTeamAccess
		remoteImpl.UpdateRepositoryUpdateTeamAccess(ctx, errorCollector, false, "test-repo", "test-team", "push")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the REST API call was made correctly
		assert.Equal(t, "/orgs/myorg/teams/test-team/repos/myorg/test-repo", mockClient.lastEndpoint)
		assert.Equal(t, "PUT", mockClient.lastMethod)

		// Verify the request body
		expectedBody := map[string]interface{}{
			"permission": "push",
		}
		assert.True(t, utils.DeepEqualUnordered(expectedBody, mockClient.lastBody))
	})

	t.Run("happy path: update team access to admin permission", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryUpdateTeamAccessMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		// Add repository and team to the cache
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name: "test-repo",
				Id:   123,
			},
		}
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   456,
				Slug: "test-team",
			},
		}

		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Call UpdateRepositoryUpdateTeamAccess
		remoteImpl.UpdateRepositoryUpdateTeamAccess(ctx, errorCollector, false, "test-repo", "test-team", "admin")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the REST API call was made correctly
		assert.Equal(t, "/orgs/myorg/teams/test-team/repos/myorg/test-repo", mockClient.lastEndpoint)
		assert.Equal(t, "PUT", mockClient.lastMethod)

		// Verify the request body
		expectedBody := map[string]interface{}{
			"permission": "admin",
		}
		assert.True(t, utils.DeepEqualUnordered(expectedBody, mockClient.lastBody))
	})

	t.Run("error path: repository not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryUpdateTeamAccessMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		remoteImpl.loadRepositories(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Add only team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   456,
				Slug: "test-team",
			},
		}

		// Call UpdateRepositoryUpdateTeamAccess with non-existent repository
		remoteImpl.UpdateRepositoryUpdateTeamAccess(ctx, errorCollector, false, "non-existent-repo", "test-team", "pull")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository non-existent-repo not found")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: team not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryUpdateTeamAccessMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Add only repository to the cache
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name: "test-repo",
				Id:   123,
			},
		}

		// Call UpdateRepositoryUpdateTeamAccess with non-existent team
		remoteImpl.UpdateRepositoryUpdateTeamAccess(ctx, errorCollector, false, "test-repo", "non-existent-team", "pull")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "team non-existent-team not found")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	// t.Run("error path: invalid permission", func(t *testing.T) {
	// 	// Setup mock client
	// 	mockClient := &UpdateRepositoryUpdateTeamAccessMockClient{}
	// 	remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg",true)

	// 	// Add repository and team to the cache
	// 	remoteImpl.repositories = map[string]*GithubRepository{
	// 		"test-repo": {
	// 			Name: "test-repo",
	// 			Id:   123,
	// 		},
	// 	}
	// 	remoteImpl.teams = map[string]*GithubTeam{
	// 		"test-team": {
	// 			Name: "test-team",
	// 			Id:   456,
	// 			Slug: "test-team",
	// 		},
	// 	}

	// 	ctx := context.TODO()
	// 	errorCollector := observability.NewErrorCollection()

	// 	// Call UpdateRepositoryUpdateTeamAccess with invalid permission
	// 	remoteImpl.UpdateRepositoryUpdateTeamAccess(ctx, errorCollector, false, "test-repo", "test-team", "invalid")

	// 	// Verify error was collected
	// 	assert.True(t, errorCollector.HasErrors())
	// 	assert.Contains(t, errorCollector.Errors[0].Error(), "invalid permission")

	// 	// Verify no API call was made
	// 	assert.Empty(t, mockClient.lastEndpoint)
	// })

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &UpdateRepositoryUpdateTeamAccessMockClient{
			shouldError:  true,
			errorMessage: "failed to update team access",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Add repository and team to the cache
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name: "test-repo",
				Id:   123,
			},
		}
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   456,
				Slug: "test-team",
			},
		}

		// Call UpdateRepositoryUpdateTeamAccess
		remoteImpl.UpdateRepositoryUpdateTeamAccess(ctx, errorCollector, false, "test-repo", "test-team", "pull")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to update team access")
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryUpdateTeamAccessMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Add repository and team to the cache
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name: "test-repo",
				Id:   123,
			},
		}
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   456,
				Slug: "test-team",
			},
		}

		// Call UpdateRepositoryUpdateTeamAccess in dry run mode
		remoteImpl.UpdateRepositoryUpdateTeamAccess(ctx, errorCollector, true, "test-repo", "test-team", "pull")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})
}

// UpdateRepositoryRemoveTeamAccessMockClient is a dedicated mock client for UpdateRepositoryRemoveTeamAccess tests
type UpdateRepositoryRemoveTeamAccessMockClient struct {
	// Track REST API calls
	lastEndpoint string
	lastMethod   string
	lastBody     map[string]interface{}

	// Configure mock responses
	shouldError  bool
	errorMessage string
	responseBody string
}

func (m *UpdateRepositoryRemoveTeamAccessMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *UpdateRepositoryRemoveTeamAccessMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.lastEndpoint = endpoint
	m.lastMethod = method
	m.lastBody = body

	if m.shouldError {
		return []byte(m.errorMessage), errors.New(m.errorMessage)
	}

	if m.responseBody != "" {
		return []byte(m.responseBody), nil
	}

	return []byte(`{}`), nil
}

func (m *UpdateRepositoryRemoveTeamAccessMockClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock-token", nil
}

func (m *UpdateRepositoryRemoveTeamAccessMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *UpdateRepositoryRemoveTeamAccessMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestUpdateRepositoryRemoveTeamAccess(t *testing.T) {
	t.Run("happy path: remove team access", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryRemoveTeamAccessMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Add repository and team to the cache
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name: "test-repo",
				Id:   123,
			},
		}
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   456,
				Slug: "test-team",
			},
		}

		// Call UpdateRepositoryRemoveTeamAccess
		remoteImpl.UpdateRepositoryRemoveTeamAccess(ctx, errorCollector, false, "test-repo", "test-team")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify the REST API call was made correctly
		assert.Equal(t, "orgs/myorg/teams/test-team/repos/myorg/test-repo", mockClient.lastEndpoint)
		assert.Equal(t, "DELETE", mockClient.lastMethod)
	})

	t.Run("error path: repository not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryRemoveTeamAccessMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Add only team to the cache
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   456,
				Slug: "test-team",
			},
		}

		// Call UpdateRepositoryRemoveTeamAccess with non-existent repository
		remoteImpl.UpdateRepositoryRemoveTeamAccess(ctx, errorCollector, false, "non-existent-repo", "test-team")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository non-existent-repo not found")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: team not found", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryRemoveTeamAccessMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Add only repository to the cache
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name: "test-repo",
				Id:   123,
			},
		}

		// Call UpdateRepositoryRemoveTeamAccess with non-existent team
		remoteImpl.UpdateRepositoryRemoveTeamAccess(ctx, errorCollector, false, "test-repo", "non-existent-team")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "team non-existent-team not found")

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &UpdateRepositoryRemoveTeamAccessMockClient{
			shouldError:  true,
			errorMessage: "failed to remove team access",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Add repository and team to the cache
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name: "test-repo",
				Id:   123,
			},
		}
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   456,
				Slug: "test-team",
			},
		}

		// Call UpdateRepositoryRemoveTeamAccess
		remoteImpl.UpdateRepositoryRemoveTeamAccess(ctx, errorCollector, false, "test-repo", "test-team")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "failed to remove team access")
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		// Setup mock client
		mockClient := &UpdateRepositoryRemoveTeamAccessMockClient{}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Add repository and team to the cache
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name: "test-repo",
				Id:   123,
			},
		}
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   456,
				Slug: "test-team",
			},
		}

		// Call UpdateRepositoryRemoveTeamAccess in dry run mode
		remoteImpl.UpdateRepositoryRemoveTeamAccess(ctx, errorCollector, true, "test-repo", "test-team")

		// Verify no errors occurred
		assert.False(t, errorCollector.HasErrors())

		// Verify no API call was made
		assert.Empty(t, mockClient.lastEndpoint)
	})

	t.Run("error path: team access not found", func(t *testing.T) {
		// Setup mock client with not found error
		mockClient := &UpdateRepositoryRemoveTeamAccessMockClient{
			responseBody: `{"message": "Not Found"}`,
			shouldError:  true,
			errorMessage: "Not Found",
		}
		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true, true)

		ctx := context.TODO()
		remoteImpl.loadRepositories(ctx)
		remoteImpl.loadTeams(ctx)
		mockClient.lastEndpoint = ""
		errorCollector := observability.NewErrorCollection()

		// Add repository and team to the cache
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": {
				Name: "test-repo",
				Id:   123,
			},
		}
		remoteImpl.teams = map[string]*GithubTeam{
			"test-team": {
				Name: "test-team",
				Id:   456,
				Slug: "test-team",
			},
		}

		// Call UpdateRepositoryRemoveTeamAccess
		remoteImpl.UpdateRepositoryRemoveTeamAccess(ctx, errorCollector, false, "test-repo", "test-team")

		// Verify error was collected
		assert.True(t, errorCollector.HasErrors())
		assert.Contains(t, errorCollector.Errors[0].Error(), "Not Found")
	})
}
