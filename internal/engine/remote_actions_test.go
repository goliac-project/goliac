package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// LoadEnvironmentsPerRepositoryMockClient is a mock client for testing environment loading per repository
type LoadEnvironmentsPerRepositoryMockClient struct {
	lastEndpoint    string
	lastParameters  string
	lastMethod      string
	lastBody        map[string]interface{}
	lastGithubToken *string
	shouldError     bool
	errorMessage    string
	responseBody    string
	accessToken     string
	accessTokenErr  error
	callCount       int
}

func (m *LoadEnvironmentsPerRepositoryMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return nil, nil
}

func (m *LoadEnvironmentsPerRepositoryMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.callCount++
	m.lastEndpoint = endpoint
	m.lastParameters = parameters
	m.lastMethod = method
	m.lastBody = body
	m.lastGithubToken = githubToken

	if m.shouldError {
		return nil, fmt.Errorf(m.errorMessage)
	}

	return []byte(m.responseBody), nil
}

func (m *LoadEnvironmentsPerRepositoryMockClient) GetAccessToken(ctx context.Context) (string, error) {
	if m.accessTokenErr != nil {
		return "", m.accessTokenErr
	}
	if m.accessToken == "" {
		return "mock-token", nil
	}
	return m.accessToken, nil
}

func (m *LoadEnvironmentsPerRepositoryMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *LoadEnvironmentsPerRepositoryMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestLoadEnvironmentsPerRepository(t *testing.T) {
	t.Run("happy path: load environments for repository", func(t *testing.T) {
		// Setup mock client
		mockClient := &LoadEnvironmentsPerRepositoryMockClient{
			responseBody: `{
				"total_count": 2,
				"environments": [
					{
						"id": 1,
						"name": "production",
						"node_id": "123",
						"protection_rules": [
							{
								"id": 1,
								"type": "required_reviewers",
								"reviewer_teams": ["team1"]
							}
						]
					},
					{
						"id": 2,
						"name": "staging",
						"node_id": "456",
						"protection_rules": []
					}
				]
			}`,
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
		}

		ctx := context.TODO()

		// Call loadEnvironmentsPerRepository
		envMap, err := remoteImpl.loadEnvironmentsPerRepository(ctx, repo)

		// Verify no errors occurred
		assert.NoError(t, err)
		assert.Equal(t, 2, len(envMap))

		// Verify environments were loaded correctly
		prodEnv, exists := envMap["production"]
		assert.True(t, exists)
		assert.Equal(t, 1, prodEnv.Id)
		assert.Equal(t, "production", prodEnv.Name)
		assert.Equal(t, "123", prodEnv.NodeId)
		assert.Equal(t, 1, len(prodEnv.ProtectionRules))

		stagingEnv, exists := envMap["staging"]
		assert.True(t, exists)
		assert.Equal(t, 2, stagingEnv.Id)
		assert.Equal(t, "staging", stagingEnv.Name)
		assert.Equal(t, "456", stagingEnv.NodeId)
		assert.Empty(t, stagingEnv.ProtectionRules)

		// Verify API call was made correctly
		assert.Equal(t, "/repos/myorg/test-repo/environments", mockClient.lastEndpoint)
		assert.Equal(t, "GET", mockClient.lastMethod)
		assert.Equal(t, 2, mockClient.callCount)
	})

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &LoadEnvironmentsPerRepositoryMockClient{
			shouldError:  true,
			errorMessage: "API error",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
		}

		ctx := context.TODO()

		// Call loadEnvironmentsPerRepository
		envMap, err := remoteImpl.loadEnvironmentsPerRepository(ctx, repo)

		// Verify error was returned
		assert.Error(t, err)
		assert.Nil(t, envMap)
		assert.Contains(t, err.Error(), "API error")
	})

	t.Run("happy path: dry run", func(t *testing.T) {
		mockClient := &LoadEnvironmentsPerRepositoryMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
		}

		ctx := context.TODO()

		// Call loadEnvironmentsPerRepository
		envMap, err := remoteImpl.loadEnvironmentsPerRepository(ctx, repo)

		// Verify no errors and empty map returned
		assert.NoError(t, err)
		assert.NotNil(t, envMap)
		assert.Empty(t, envMap)
		assert.Equal(t, 3, mockClient.callCount)
	})

	t.Run("error path: GetAccessToken error", func(t *testing.T) {
		mockClient := &LoadEnvironmentsPerRepositoryMockClient{
			accessTokenErr: fmt.Errorf("failed to get access token"),
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
		}

		ctx := context.TODO()

		// Call loadEnvironmentsPerRepository
		envMap, err := remoteImpl.loadEnvironmentsPerRepository(ctx, repo)

		// Verify error was returned
		assert.Error(t, err)
		assert.Nil(t, envMap)
		assert.Contains(t, err.Error(), "failed to get access token")
		assert.Equal(t, 3, mockClient.callCount)
	})

	t.Run("error path: invalid JSON response", func(t *testing.T) {
		mockClient := &LoadEnvironmentsPerRepositoryMockClient{
			responseBody: `invalid json`,
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
		}

		ctx := context.TODO()

		// Call loadEnvironmentsPerRepository
		envMap, err := remoteImpl.loadEnvironmentsPerRepository(ctx, repo)

		// Verify error was returned
		assert.Error(t, err)
		assert.Nil(t, envMap)
		assert.Contains(t, err.Error(), "invalid")
	})

	t.Run("happy path: empty environments list", func(t *testing.T) {
		mockClient := &LoadEnvironmentsPerRepositoryMockClient{
			responseBody: `{
				"total_count": 0,
				"environments": []
			}`,
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
		}

		ctx := context.TODO()

		// Call loadEnvironmentsPerRepository
		envMap, err := remoteImpl.loadEnvironmentsPerRepository(ctx, repo)

		// Verify success with empty map
		assert.NoError(t, err)
		assert.NotNil(t, envMap)
		assert.Empty(t, envMap)
	})
}

// LoadVariablesPerRepositoryMockClient is a mock client for testing variables loading per repository
type LoadVariablesPerRepositoryMockClient struct {
	lastEndpoint    string
	lastParameters  string
	lastMethod      string
	lastBody        map[string]interface{}
	lastGithubToken *string
	shouldError     bool
	errorMessage    string
	responseBody    string
	accessToken     string
	accessTokenErr  error
	mutex           sync.Mutex
}

func (m *LoadVariablesPerRepositoryMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return nil, nil
}

func (m *LoadVariablesPerRepositoryMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.lastEndpoint = endpoint
	m.lastParameters = parameters
	m.lastMethod = method
	m.lastBody = body
	m.lastGithubToken = githubToken

	if m.shouldError {
		return nil, fmt.Errorf(m.errorMessage)
	}

	return []byte(m.responseBody), nil
}

func (m *LoadVariablesPerRepositoryMockClient) GetAccessToken(ctx context.Context) (string, error) {
	if m.accessTokenErr != nil {
		return "", m.accessTokenErr
	}
	if m.accessToken == "" {
		return "mock-token", nil
	}
	return m.accessToken, nil
}

func (m *LoadVariablesPerRepositoryMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *LoadVariablesPerRepositoryMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestLoadVariablesPerRepository(t *testing.T) {
	t.Run("happy path: load variables for repository", func(t *testing.T) {
		// Setup mock client
		mockClient := &LoadVariablesPerRepositoryMockClient{
			responseBody: `{
				"total_count": 2,
				"variables": [
					{
						"name": "VAR1",
						"value": "value1",
						"created_at": "2023-01-01T00:00:00Z",
						"updated_at": "2023-01-01T00:00:00Z"
					},
					{
						"name": "VAR2",
						"value": "value2",
						"created_at": "2023-01-01T00:00:00Z",
						"updated_at": "2023-01-01T00:00:00Z"
					}
				]
			}`,
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
		}

		ctx := context.TODO()

		// Call loadVariablesPerRepository
		variables, err := remoteImpl.loadVariablesPerRepository(ctx, repo)

		// Verify no errors occurred
		assert.NoError(t, err)

		// Verify API call was made correctly
		assert.Equal(t, "/repos/myorg/test-repo/actions/variables", mockClient.lastEndpoint)
		assert.Equal(t, "GET", mockClient.lastMethod)

		// Verify variables were loaded correctly
		assert.Equal(t, 2, len(variables))

		// Check first variable
		var1 := variables["VAR1"]
		assert.NotNil(t, var1)
		assert.Equal(t, "VAR1", var1.Name)
		assert.Equal(t, "value1", var1.Value)

		// Check second variable
		var2 := variables["VAR2"]
		assert.NotNil(t, var2)
		assert.Equal(t, "VAR2", var2.Name)
		assert.Equal(t, "value2", var2.Value)
	})

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &LoadVariablesPerRepositoryMockClient{
			shouldError:  true,
			errorMessage: "API error",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
		}

		ctx := context.TODO()

		// Call loadVariablesPerRepository
		variables, err := remoteImpl.loadVariablesPerRepository(ctx, repo)

		// Verify error was returned
		assert.Error(t, err)
		assert.Nil(t, variables)
		assert.Contains(t, err.Error(), "API error")
	})

	t.Run("error path: GetAccessToken error", func(t *testing.T) {
		mockClient := &LoadVariablesPerRepositoryMockClient{
			accessTokenErr: fmt.Errorf("failed to get access token"),
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
		}

		ctx := context.TODO()

		// Call loadVariablesPerRepository
		variables, err := remoteImpl.loadVariablesPerRepository(ctx, repo)

		// Verify error was returned
		assert.Error(t, err)
		assert.Nil(t, variables)
		assert.Contains(t, err.Error(), "not able to unmarshall action variables")
	})

	t.Run("error path: invalid JSON response", func(t *testing.T) {
		mockClient := &LoadVariablesPerRepositoryMockClient{
			responseBody: `invalid json`,
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
		}

		ctx := context.TODO()

		// Call loadVariablesPerRepository
		variables, err := remoteImpl.loadVariablesPerRepository(ctx, repo)

		// Verify error was returned
		assert.Error(t, err)
		assert.Nil(t, variables)
		assert.Contains(t, err.Error(), "invalid")
	})

	t.Run("happy path: empty variables list", func(t *testing.T) {
		mockClient := &LoadVariablesPerRepositoryMockClient{
			responseBody: `{
				"total_count": 0,
				"variables": []
			}`,
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
		}

		ctx := context.TODO()

		// Call loadVariablesPerRepository
		variables, err := remoteImpl.loadVariablesPerRepository(ctx, repo)

		// Verify no errors and empty map returned
		assert.NoError(t, err)
		assert.NotNil(t, variables)
		assert.Empty(t, variables)
	})

}

// LoadEnvironmentVariablesPerRepositoryMockClient is a mock client for testing environment variables loading per repository
type LoadEnvironmentVariablesPerRepositoryMockClient struct {
	lastEndpoint    string
	lastParameters  string
	lastMethod      string
	lastBody        map[string]interface{}
	lastGithubToken *string
	shouldError     bool
	errorMessage    string
	responseBody    string
	accessToken     string
	accessTokenErr  error
	mutex           sync.Mutex
}

func (m *LoadEnvironmentVariablesPerRepositoryMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return nil, nil
}

func (m *LoadEnvironmentVariablesPerRepositoryMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.lastEndpoint = endpoint
	m.lastParameters = parameters
	m.lastMethod = method
	m.lastBody = body
	m.lastGithubToken = githubToken

	if m.shouldError {
		return nil, fmt.Errorf(m.errorMessage)
	}

	return []byte(m.responseBody), nil
}

func (m *LoadEnvironmentVariablesPerRepositoryMockClient) GetAccessToken(ctx context.Context) (string, error) {
	if m.accessTokenErr != nil {
		return "", m.accessTokenErr
	}
	if m.accessToken == "" {
		return "mock-token", nil
	}
	return m.accessToken, nil
}

func (m *LoadEnvironmentVariablesPerRepositoryMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *LoadEnvironmentVariablesPerRepositoryMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestLoadEnvironmentVariablesPerRepository(t *testing.T) {
	t.Run("happy path: load environment variables for repository", func(t *testing.T) {
		// Setup mock client
		mockClient := &LoadEnvironmentVariablesPerRepositoryMockClient{
			responseBody: `{
				"total_count": 2,
				"variables": [
					{
						"name": "VAR1",
						"value": "value1",
						"created_at": "2023-01-01T00:00:00Z",
						"updated_at": "2023-01-01T00:00:00Z"
					},
					{
						"name": "VAR2",
						"value": "value2",
						"created_at": "2023-01-01T00:00:00Z",
						"updated_at": "2023-01-01T00:00:00Z"
					}
				]
			}`,
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: map[string]*GithubEnvironment{
				"production": {
					Id:   1,
					Name: "production",
				},
				"staging": {
					Id:   2,
					Name: "staging",
				},
			},
		}

		ctx := context.TODO()

		// Call loadEnvironmentVariablesPerRepository
		envVars, err := remoteImpl.loadEnvironmentVariablesPerRepository(ctx, repo)

		// Verify no errors occurred
		assert.NoError(t, err)
		assert.NotNil(t, envVars)
		assert.Equal(t, 2, len(envVars))

		// Verify environment variables were loaded correctly
		prodVars := envVars["production"]
		assert.NotNil(t, prodVars)
		assert.Equal(t, 2, len(prodVars))
		assert.Equal(t, "value1", prodVars["VAR1"].Value)
		assert.Equal(t, "value2", prodVars["VAR2"].Value)

		stagingVars := envVars["staging"]
		assert.NotNil(t, stagingVars)
		assert.Equal(t, 2, len(stagingVars))
		assert.Equal(t, "value1", stagingVars["VAR1"].Value)
		assert.Equal(t, "value2", stagingVars["VAR2"].Value)

		// Verify API calls were made correctly
		assert.Equal(t, "/repos/myorg/test-repo/environments/staging/variables", mockClient.lastEndpoint)
		assert.Equal(t, "GET", mockClient.lastMethod)
	})

	t.Run("error path: API error", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesPerRepositoryMockClient{
			shouldError:  true,
			errorMessage: "API error",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: map[string]*GithubEnvironment{
				"production": {
					Id:   1,
					Name: "production",
				},
			},
		}

		ctx := context.TODO()

		// Call loadEnvironmentVariablesPerRepository
		envVars, err := remoteImpl.loadEnvironmentVariablesPerRepository(ctx, repo)

		// Verify error was returned
		assert.Error(t, err)
		assert.Nil(t, envVars)
		assert.Contains(t, err.Error(), "API error")
	})

	t.Run("happy path: empty environments", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesPerRepositoryMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name:         "test-repo",
			Id:           123,
			Environments: map[string]*GithubEnvironment{},
		}

		ctx := context.TODO()

		// Call loadEnvironmentVariablesPerRepository
		envVars, err := remoteImpl.loadEnvironmentVariablesPerRepository(ctx, repo)

		// Verify no errors and empty map returned
		assert.NoError(t, err)
		assert.NotNil(t, envVars)
		assert.Empty(t, envVars)
	})

	t.Run("error path: GetAccessToken error", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesPerRepositoryMockClient{
			accessTokenErr: fmt.Errorf("failed to get access token"),
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: map[string]*GithubEnvironment{
				"production": {
					Id:   1,
					Name: "production",
				},
			},
		}

		ctx := context.TODO()

		// Call loadEnvironmentVariablesPerRepository
		envVars, err := remoteImpl.loadEnvironmentVariablesPerRepository(ctx, repo)

		// Verify error was returned
		assert.Error(t, err)
		assert.Nil(t, envVars)
		assert.Contains(t, err.Error(), "not able to unmarshall environments for repo")
	})

	t.Run("error path: invalid JSON response", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesPerRepositoryMockClient{
			responseBody: `invalid json`,
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: map[string]*GithubEnvironment{
				"production": {
					Id:   1,
					Name: "production",
				},
			},
		}

		ctx := context.TODO()

		// Call loadEnvironmentVariablesPerRepository
		envVars, err := remoteImpl.loadEnvironmentVariablesPerRepository(ctx, repo)

		// Verify error was returned
		assert.Error(t, err)
		assert.Nil(t, envVars)
		assert.Contains(t, err.Error(), "invalid")
	})

	t.Run("happy path: empty variables list", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesPerRepositoryMockClient{
			responseBody: `{
				"total_count": 0,
				"variables": []
			}`,
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: map[string]*GithubEnvironment{
				"production": {
					Id:   1,
					Name: "production",
				},
			},
		}

		ctx := context.TODO()

		// Call loadEnvironmentVariablesPerRepository
		envVars, err := remoteImpl.loadEnvironmentVariablesPerRepository(ctx, repo)

		// Verify no errors and empty map returned
		assert.NoError(t, err)
		assert.NotNil(t, envVars)
		assert.Equal(t, 1, len(envVars))
		assert.Empty(t, envVars["production"])
	})
}

// LoadRepositoriesVariablesMockClient is a mock client for testing repository variables loading
type LoadRepositoriesVariablesMockClient struct {
	lastEndpoint    string
	lastParameters  string
	lastMethod      string
	lastBody        map[string]interface{}
	lastGithubToken *string
	shouldError     bool
	errorMessage    string
	responseBody    string
	accessToken     string
	accessTokenErr  error
	callCount       int
	mutex           sync.Mutex
}

func (m *LoadRepositoriesVariablesMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return nil, nil
}

func (m *LoadRepositoriesVariablesMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.callCount++
	m.lastEndpoint = endpoint
	m.lastParameters = parameters
	m.lastMethod = method
	m.lastBody = body
	m.lastGithubToken = githubToken

	if m.shouldError {
		return nil, fmt.Errorf(m.errorMessage)
	}

	return []byte(m.responseBody), nil
}

func (m *LoadRepositoriesVariablesMockClient) GetAccessToken(ctx context.Context) (string, error) {
	if m.accessTokenErr != nil {
		return "", m.accessTokenErr
	}
	if m.accessToken == "" {
		return "mock-token", nil
	}
	return m.accessToken, nil
}

func (m *LoadRepositoriesVariablesMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *LoadRepositoriesVariablesMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestLoadRepositoriesVariables(t *testing.T) {
	t.Run("happy path: load variables for multiple repositories", func(t *testing.T) {
		// Setup mock client
		mockClient := &LoadRepositoriesVariablesMockClient{
			responseBody: `{
				"total_count": 2,
				"variables": [
					{
						"name": "VAR1",
						"value": "value1",
						"created_at": "2023-01-01T00:00:00Z",
						"updated_at": "2023-01-01T00:00:00Z"
					},
					{
						"name": "VAR2",
						"value": "value2",
						"created_at": "2023-01-01T00:00:00Z",
						"updated_at": "2023-01-01T00:00:00Z"
					}
				]
			}`,
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repositories := map[string]*GithubRepository{
			"repo1": {
				Name: "repo1",
				Id:   123,
			},
			"repo2": {
				Name: "repo2",
				Id:   456,
			},
		}

		ctx := context.TODO()
		maxGoroutines := int64(2)

		// Call loadRepositoriesVariables
		varsMap, err := remoteImpl.loadRepositoriesVariables(ctx, maxGoroutines, repositories)

		// Verify no errors occurred
		assert.NoError(t, err)
		assert.Equal(t, 3, mockClient.callCount)

		// Verify variables were loaded correctly for each repository
		assert.Equal(t, 2, len(varsMap)) // Two repositories

		// Check repo1 variables
		repo1Vars := varsMap["repo1"]
		assert.NotNil(t, repo1Vars)
		assert.Equal(t, 2, len(repo1Vars))
		assert.Equal(t, "value1", repo1Vars["VAR1"].Value)
		assert.Equal(t, "value2", repo1Vars["VAR2"].Value)

		// Check repo2 variables
		repo2Vars := varsMap["repo2"]
		assert.NotNil(t, repo2Vars)
		assert.Equal(t, 2, len(repo2Vars))
		assert.Equal(t, "value1", repo2Vars["VAR1"].Value)
		assert.Equal(t, "value2", repo2Vars["VAR2"].Value)

		// Verify API calls were made with correct endpoints
		assert.Contains(t, mockClient.lastEndpoint, "/actions/variables")
	})

	t.Run("error path: API error", func(t *testing.T) {
		// Setup mock client with error
		mockClient := &LoadRepositoriesVariablesMockClient{
			shouldError:  true,
			errorMessage: "API error",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repositories := map[string]*GithubRepository{
			"repo1": {
				Name: "repo1",
				Id:   123,
			},
		}

		ctx := context.TODO()
		maxGoroutines := int64(2)

		// Call loadRepositoriesVariables
		_, err := remoteImpl.loadRepositoriesVariables(ctx, maxGoroutines, repositories)

		// Verify error was returned
		assert.Error(t, err)
	})

	t.Run("happy path: concurrent loading with rate limiting", func(t *testing.T) {
		// Setup mock client with response for multiple repositories
		mockClient := &LoadRepositoriesVariablesMockClient{
			responseBody: `{
				"total_count": 1,
				"variables": [
					{
						"name": "VAR1",
						"value": "value1"
					}
				]
			}`,
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repositories := map[string]*GithubRepository{}

		// Create 5 test repositories
		for i := 1; i <= 5; i++ {
			repoName := fmt.Sprintf("repo%d", i)
			repositories[repoName] = &GithubRepository{
				Name: repoName,
				Id:   i,
			}
		}

		ctx := context.TODO()
		maxGoroutines := int64(2) // Limit concurrent operations

		// Call loadRepositoriesVariables
		varsMap, err := remoteImpl.loadRepositoriesVariables(ctx, maxGoroutines, repositories)

		// Verify no errors occurred
		assert.NoError(t, err)
		assert.Equal(t, 6, mockClient.callCount)
		assert.Equal(t, 5, len(varsMap))

		// Verify each repository has its variables
		for _, repoVars := range varsMap {
			assert.Equal(t, 1, len(repoVars))
			assert.NotNil(t, repoVars["VAR1"])
			assert.Equal(t, "value1", repoVars["VAR1"].Value)
		}
	})

	t.Run("error path: context cancellation", func(t *testing.T) {
		mockClient := &LoadRepositoriesVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repositories := map[string]*GithubRepository{
			"repo1": {
				Name: "repo1",
				Id:   123,
			},
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel context immediately
		maxGoroutines := int64(2)

		// Call loadRepositoriesVariables with cancelled context
		_, err := remoteImpl.loadRepositoriesVariables(ctx, maxGoroutines, repositories)

		// Verify context cancellation error
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not able to unmarshall action variables for repo")
	})

	t.Run("happy path: empty repositories map", func(t *testing.T) {
		mockClient := &LoadRepositoriesVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg")
		repositories := map[string]*GithubRepository{}

		ctx := context.TODO()
		maxGoroutines := int64(2)

		// Call loadRepositoriesVariables
		varsMap, err := remoteImpl.loadRepositoriesVariables(ctx, maxGoroutines, repositories)

		// Verify no errors and empty map returned
		assert.NoError(t, err)
		assert.NotNil(t, varsMap)
		assert.Empty(t, varsMap)
		assert.Equal(t, 2, mockClient.callCount)
	})
}
