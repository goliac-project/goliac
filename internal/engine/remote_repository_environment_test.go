package engine

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/goliac-project/goliac/internal/observability"
	"github.com/stretchr/testify/assert"
)

// LoadEnvironmentVariablesMockClient is a mock client for testing environment variables loading
type LoadEnvironmentVariablesMockClient struct {
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
	callCount       int
}

func (m *LoadEnvironmentVariablesMockClient) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return nil, nil
}

func (m *LoadEnvironmentVariablesMockClient) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.callCount++
	m.lastEndpoint = endpoint
	m.lastParameters = parameters
	m.lastMethod = method
	m.lastBody = body
	m.lastGithubToken = githubToken

	if m.shouldError {
		return nil, errors.New(m.errorMessage)
	}

	return []byte(m.responseBody), nil
}

func (m *LoadEnvironmentVariablesMockClient) GetAccessToken(ctx context.Context) (string, error) {
	if m.accessTokenErr != nil {
		return "", m.accessTokenErr
	}
	if m.accessToken == "" {
		return "mock-token", nil
	}
	return m.accessToken, nil
}

func (m *LoadEnvironmentVariablesMockClient) CreateJWT() (string, error) {
	return "mock-jwt", nil
}

func (m *LoadEnvironmentVariablesMockClient) GetAppSlug() string {
	return "mock-app"
}

func TestLoadEnvironmentVariablesForEnvironmentRepository(t *testing.T) {
	t.Run("happy path: load environment variables for a repository environment", func(t *testing.T) {
		// Setup mock client
		mockClient := &LoadEnvironmentVariablesMockClient{
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

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()

		// Call loadEnvironmentVariablesForEnvironmentRepository
		envVars, err := remoteImpl.loadEnvironmentVariablesForEnvironmentRepository(ctx, "test-repo", "production")

		// Verify no errors occurred
		assert.NoError(t, err)
		assert.NotNil(t, envVars)
		assert.Equal(t, 2, len(envVars))
		assert.Equal(t, "value1", envVars["VAR1"])
		assert.Equal(t, "value2", envVars["VAR2"])

		// Verify API call was made correctly
		assert.Equal(t, 2, mockClient.callCount)
		assert.Equal(t, "/repos/myorg/test-repo/environments/production/variables", mockClient.lastEndpoint)
		assert.Equal(t, "page=1&per_page=30", mockClient.lastParameters)
		assert.Equal(t, "GET", mockClient.lastMethod)
	})

	t.Run("error path: API error", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{
			shouldError:  true,
			errorMessage: "API error",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()

		// Call loadEnvironmentVariablesForEnvironmentRepository
		envVars, err := remoteImpl.loadEnvironmentVariablesForEnvironmentRepository(ctx, "test-repo", "production")

		// Verify error was returned
		assert.Error(t, err)
		assert.Nil(t, envVars)
		assert.Contains(t, err.Error(), "API error")
	})

	t.Run("error path: invalid JSON response", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{
			responseBody: `invalid json`,
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()

		// Call loadEnvironmentVariablesForEnvironmentRepository
		envVars, err := remoteImpl.loadEnvironmentVariablesForEnvironmentRepository(ctx, "test-repo", "production")

		// Verify error was returned
		assert.Error(t, err)
		assert.Nil(t, envVars)
		assert.Contains(t, err.Error(), "invalid")
	})

	t.Run("happy path: empty variables list", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{
			responseBody: `{
				"total_count": 0,
				"variables": []
			}`,
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()

		// Call loadEnvironmentVariablesForEnvironmentRepository
		envVars, err := remoteImpl.loadEnvironmentVariablesForEnvironmentRepository(ctx, "test-repo", "production")

		// Verify no errors and empty map returned
		assert.NoError(t, err)
		assert.NotNil(t, envVars)
		assert.Empty(t, envVars)
	})

	t.Run("happy path: pagination", func(t *testing.T) {
		// Setup mock client with pagination responses
		mockClient := &LoadEnvironmentVariablesMockClient{
			responseBody: `{
				"total_count": 2,
				"variables": [
					{
						"name": "VAR1",
						"value": "value1",
						"created_at": "2023-01-01T00:00:00Z",
						"updated_at": "2023-01-01T00:00:00Z"
					}
				]
			}`,
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()

		// Call loadEnvironmentVariablesForEnvironmentRepository
		envVars, err := remoteImpl.loadEnvironmentVariablesForEnvironmentRepository(ctx, "test-repo", "production")

		// Verify no errors occurred
		assert.NoError(t, err)
		assert.NotNil(t, envVars)
		assert.Equal(t, 1, len(envVars))
		assert.Equal(t, "value1", envVars["VAR1"])

		// Verify API call was made correctly
		assert.Equal(t, 2, mockClient.callCount)
		assert.Equal(t, "/repos/myorg/test-repo/environments/production/variables", mockClient.lastEndpoint)
		assert.Equal(t, "page=1&per_page=30", mockClient.lastParameters)
		assert.Equal(t, "GET", mockClient.lastMethod)

	})

	t.Run("happy path: lazy loading of environment variables", func(t *testing.T) {
		// Setup mock client
		mockClient := &LoadEnvironmentVariablesMockClient{
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

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()

		// Create a repository with an environment
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name:      "production",
					Variables: map[string]string{},
				}
				return envs
			}),
		}

		// Initially, only 1 call should have been made (getGHESVersion)
		assert.Equal(t, 1, mockClient.callCount)

		// Get the environment
		envs := repo.Environments.GetEntity()
		assert.NotNil(t, envs)
		assert.Equal(t, 1, len(envs))
		assert.NotNil(t, envs["production"])

		// Still only 1 call should have been made (getGHESVersion)
		assert.Equal(t, 1, mockClient.callCount)

		// Now load the environment variables
		envVars, err := remoteImpl.loadEnvironmentVariablesForEnvironmentRepository(ctx, repo.Name, "production")
		assert.NoError(t, err)
		assert.NotNil(t, envVars)
		assert.Equal(t, 2, len(envVars))
		assert.Equal(t, "value1", envVars["VAR1"])
		assert.Equal(t, "value2", envVars["VAR2"])

		// Now API calls should have been made
		assert.Equal(t, 2, mockClient.callCount)
		assert.Equal(t, "/repos/myorg/test-repo/environments/production/variables", mockClient.lastEndpoint)
		assert.Equal(t, "page=1&per_page=30", mockClient.lastParameters)
		assert.Equal(t, "GET", mockClient.lastMethod)

		// Update the environment variables in the repository
		envs["production"].Variables = envVars

		// Verify the variables are now accessible through the repository
		assert.Equal(t, "value1", envs["production"].Variables["VAR1"])
		assert.Equal(t, "value2", envs["production"].Variables["VAR2"])
	})
}

func TestAddRepositoryEnvironment(t *testing.T) {
	t.Run("happy path: add new environment to repository", func(t *testing.T) {
		// Setup mock client
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				return make(map[string]*GithubEnvironment)
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Add environment
		remoteImpl.AddRepositoryEnvironment(ctx, errorCollector, false, "test-repo", "production")

		// Verify no errors occurred
		assert.Empty(t, errorCollector.Errors)

		// Verify API call was made correctly
		assert.Equal(t, 3, mockClient.callCount) // 1 for getGHESVersion, 1 for adding environment
		assert.Equal(t, "/repos/myorg/test-repo/environments/production", mockClient.lastEndpoint)
		assert.Equal(t, "", mockClient.lastParameters)
		assert.Equal(t, "PUT", mockClient.lastMethod)
		assert.Nil(t, mockClient.lastBody)

		// Verify environment was added to repository
		envs := repo.Environments.GetEntity()
		assert.NotNil(t, envs)
		assert.Equal(t, 1, len(envs))
		assert.NotNil(t, envs["production"])
		assert.Equal(t, "production", envs["production"].Name)
		assert.Empty(t, envs["production"].Variables)
	})

	t.Run("error path: repository not found", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Try to add environment to non-existent repository
		remoteImpl.AddRepositoryEnvironment(ctx, errorCollector, false, "non-existent", "production")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository non-existent not found")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("error path: environment already exists", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with existing environment
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name:      "production",
					Variables: map[string]string{},
				}
				return envs
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to add existing environment
		remoteImpl.AddRepositoryEnvironment(ctx, errorCollector, false, "test-repo", "production")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "environment production already exists")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("happy path: dry run mode", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				return make(map[string]*GithubEnvironment)
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Add environment in dry run mode
		remoteImpl.AddRepositoryEnvironment(ctx, errorCollector, true, "test-repo", "production")

		// Verify no errors occurred
		assert.Empty(t, errorCollector.Errors)

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion

		// Verify environment was added to repository
		envs := repo.Environments.GetEntity()
		assert.NotNil(t, envs)
		assert.Equal(t, 1, len(envs))
		assert.NotNil(t, envs["production"])
		assert.Equal(t, "production", envs["production"].Name)
		assert.Empty(t, envs["production"].Variables)
	})

	t.Run("error path: API error", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{
			shouldError:  true,
			errorMessage: "API error",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				return make(map[string]*GithubEnvironment)
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to add environment
		remoteImpl.AddRepositoryEnvironment(ctx, errorCollector, false, "test-repo", "production")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "API error")

		// Verify API call was made
		assert.Equal(t, 3, mockClient.callCount) // 1 for getGHESVersion, 1 for adding environment
		assert.Equal(t, "/repos/myorg/test-repo/environments/production", mockClient.lastEndpoint)
		assert.Equal(t, "", mockClient.lastParameters)
		assert.Equal(t, "PUT", mockClient.lastMethod)
		assert.Nil(t, mockClient.lastBody)

		// Verify environment was not added to repository
		envs := repo.Environments.GetEntity()
		assert.NotNil(t, envs)
		assert.Empty(t, envs)
	})
}

func TestDeleteRepositoryEnvironment(t *testing.T) {
	t.Run("happy path: delete existing environment from repository", func(t *testing.T) {
		// Setup mock client
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an environment
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name:      "production",
					Variables: map[string]string{},
				}
				return envs
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Delete environment
		remoteImpl.DeleteRepositoryEnvironment(ctx, errorCollector, false, "test-repo", "production")

		// Verify no errors occurred
		assert.Empty(t, errorCollector.Errors)

		// Verify API call was made correctly
		assert.Equal(t, 3, mockClient.callCount) // 1 for getGHESVersion, 1 for deleting environment
		assert.Equal(t, "/repos/myorg/test-repo/environments/production", mockClient.lastEndpoint)
		assert.Equal(t, "", mockClient.lastParameters)
		assert.Equal(t, "DELETE", mockClient.lastMethod)
		assert.Nil(t, mockClient.lastBody)

		// Verify environment was removed from repository
		envs := repo.Environments.GetEntity()
		assert.NotNil(t, envs)
		assert.Empty(t, envs)
	})

	t.Run("error path: repository not found", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Try to delete environment from non-existent repository
		remoteImpl.DeleteRepositoryEnvironment(ctx, errorCollector, false, "non-existent", "production")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository non-existent not found")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("error path: environment not found", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository without the environment
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				return make(map[string]*GithubEnvironment)
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to delete non-existent environment
		remoteImpl.DeleteRepositoryEnvironment(ctx, errorCollector, false, "test-repo", "production")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "environment production not found")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("happy path: dry run mode", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an environment
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name:      "production",
					Variables: map[string]string{},
				}
				return envs
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Delete environment in dry run mode
		remoteImpl.DeleteRepositoryEnvironment(ctx, errorCollector, true, "test-repo", "production")

		// Verify no errors occurred
		assert.Empty(t, errorCollector.Errors)

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion

		// Verify environment was removed from repository
		envs := repo.Environments.GetEntity()
		assert.NotNil(t, envs)
		assert.Empty(t, envs)
	})

	t.Run("error path: API error", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{
			shouldError:  true,
			errorMessage: "API error",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an environment
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name:      "production",
					Variables: map[string]string{},
				}
				return envs
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to delete environment
		remoteImpl.DeleteRepositoryEnvironment(ctx, errorCollector, false, "test-repo", "production")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "API error")

		// Verify API call was made
		assert.Equal(t, 3, mockClient.callCount) // 1 for getGHESVersion, 1 for deleting environment
		assert.Equal(t, "/repos/myorg/test-repo/environments/production", mockClient.lastEndpoint)
		assert.Equal(t, "", mockClient.lastParameters)
		assert.Equal(t, "DELETE", mockClient.lastMethod)
		assert.Nil(t, mockClient.lastBody)

		// Verify environment was not removed from repository
		envs := repo.Environments.GetEntity()
		assert.NotNil(t, envs)
		assert.Equal(t, 1, len(envs))
		assert.NotNil(t, envs["production"])
	})
}

func TestAddRepositoryEnvironmentVariable(t *testing.T) {
	t.Run("happy path: add new variable to repository environment", func(t *testing.T) {
		// Setup mock client
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an environment
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name:      "production",
					Variables: map[string]string{},
				}
				return envs
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Add variable
		remoteImpl.AddRepositoryEnvironmentVariable(ctx, errorCollector, false, "test-repo", "production", "TEST_VAR", "test-value")

		// Verify no errors occurred
		assert.Empty(t, errorCollector.Errors)

		// Verify API call was made correctly
		assert.Equal(t, 3, mockClient.callCount) // 1 for getGHESVersion, 1 for adding variable
		assert.Equal(t, "/repos/myorg/test-repo/environments/production/variables", mockClient.lastEndpoint)
		assert.Equal(t, "", mockClient.lastParameters)
		assert.Equal(t, "POST", mockClient.lastMethod)
		assert.Equal(t, map[string]interface{}{
			"name":  "TEST_VAR",
			"value": "test-value",
		}, mockClient.lastBody)

		// Verify variable was added to repository environment
		envs := repo.Environments.GetEntity()
		assert.NotNil(t, envs)
		assert.Equal(t, 1, len(envs))
		assert.NotNil(t, envs["production"])
		assert.Equal(t, "test-value", envs["production"].Variables["TEST_VAR"])
	})

	t.Run("error path: repository not found", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Try to add variable to non-existent repository
		remoteImpl.AddRepositoryEnvironmentVariable(ctx, errorCollector, false, "non-existent", "production", "TEST_VAR", "test-value")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository non-existent not found")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("error path: environment not found", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository without the environment
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				return make(map[string]*GithubEnvironment)
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to add variable to non-existent environment
		remoteImpl.AddRepositoryEnvironmentVariable(ctx, errorCollector, false, "test-repo", "production", "TEST_VAR", "test-value")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "environment production not found")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("error path: variable already exists", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an environment and existing variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name: "production",
					Variables: map[string]string{
						"TEST_VAR": "existing-value",
					},
				}
				return envs
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to add existing variable
		remoteImpl.AddRepositoryEnvironmentVariable(ctx, errorCollector, false, "test-repo", "production", "TEST_VAR", "new-value")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "variable TEST_VAR already exists")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("happy path: dry run mode", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an environment
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name:      "production",
					Variables: map[string]string{},
				}
				return envs
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Add variable in dry run mode
		remoteImpl.AddRepositoryEnvironmentVariable(ctx, errorCollector, true, "test-repo", "production", "TEST_VAR", "test-value")

		// Verify no errors occurred
		assert.Empty(t, errorCollector.Errors)

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion

		// Verify variable was added to repository environment
		envs := repo.Environments.GetEntity()
		assert.NotNil(t, envs)
		assert.Equal(t, 1, len(envs))
		assert.NotNil(t, envs["production"])
		assert.Equal(t, "test-value", envs["production"].Variables["TEST_VAR"])
	})

	t.Run("error path: API error", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{
			shouldError:  true,
			errorMessage: "API error",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an environment
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name:      "production",
					Variables: map[string]string{},
				}
				return envs
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to add variable
		remoteImpl.AddRepositoryEnvironmentVariable(ctx, errorCollector, false, "test-repo", "production", "TEST_VAR", "test-value")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "API error")

		// Verify API call was made
		assert.Equal(t, 3, mockClient.callCount) // 1 for getGHESVersion, 1 for adding variable
		assert.Equal(t, "/repos/myorg/test-repo/environments/production/variables", mockClient.lastEndpoint)
		assert.Equal(t, "", mockClient.lastParameters)
		assert.Equal(t, "POST", mockClient.lastMethod)
		assert.Equal(t, map[string]interface{}{
			"name":  "TEST_VAR",
			"value": "test-value",
		}, mockClient.lastBody)

		// Verify variable was not added to repository environment
		envs := repo.Environments.GetEntity()
		assert.NotNil(t, envs)
		assert.Equal(t, 1, len(envs))
		assert.NotNil(t, envs["production"])
		assert.Empty(t, envs["production"].Variables)
	})
}

func TestUpdateRepositoryVariable(t *testing.T) {
	t.Run("happy path: update existing variable in repository", func(t *testing.T) {
		// Setup mock client
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an existing variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			RepositoryVariables: NewRemoteLazyLoader[string](func() map[string]string {
				vars := make(map[string]string)
				vars["TEST_VAR"] = "old-value"
				return vars
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Update variable
		remoteImpl.UpdateRepositoryVariable(ctx, errorCollector, false, "test-repo", "TEST_VAR", "new-value")

		// Verify no errors occurred
		assert.Empty(t, errorCollector.Errors)

		// Verify API call was made correctly
		assert.Equal(t, 3, mockClient.callCount) // 1 for getGHESVersion, 1 for updating variable
		assert.Equal(t, "/repos/myorg/test-repo/actions/variables/TEST_VAR", mockClient.lastEndpoint)
		assert.Equal(t, "", mockClient.lastParameters)
		assert.Equal(t, "PATCH", mockClient.lastMethod)
		assert.Equal(t, map[string]interface{}{
			"name":  "TEST_VAR",
			"value": "new-value",
		}, mockClient.lastBody)

		// Verify variable was updated in repository
		vars := repo.RepositoryVariables.GetEntity()
		assert.NotNil(t, vars)
		assert.Equal(t, "new-value", vars["TEST_VAR"])
	})

	t.Run("error path: repository not found", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Try to update variable in non-existent repository
		remoteImpl.UpdateRepositoryVariable(ctx, errorCollector, false, "non-existent", "TEST_VAR", "new-value")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository non-existent not found")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("error path: variable not found", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository without the variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			RepositoryVariables: NewRemoteLazyLoader[string](func() map[string]string {
				return make(map[string]string)
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to update non-existent variable
		remoteImpl.UpdateRepositoryVariable(ctx, errorCollector, false, "test-repo", "TEST_VAR", "new-value")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "variable TEST_VAR not found")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("happy path: dry run mode", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an existing variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			RepositoryVariables: NewRemoteLazyLoader[string](func() map[string]string {
				vars := make(map[string]string)
				vars["TEST_VAR"] = "old-value"
				return vars
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Update variable in dry run mode
		remoteImpl.UpdateRepositoryVariable(ctx, errorCollector, true, "test-repo", "TEST_VAR", "new-value")

		// Verify no errors occurred
		assert.Empty(t, errorCollector.Errors)

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion

		// Verify variable was updated in repository
		vars := repo.RepositoryVariables.GetEntity()
		assert.NotNil(t, vars)
		assert.Equal(t, "new-value", vars["TEST_VAR"])
	})

	t.Run("error path: API error", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{
			shouldError:  true,
			errorMessage: "API error",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an existing variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			RepositoryVariables: NewRemoteLazyLoader[string](func() map[string]string {
				vars := make(map[string]string)
				vars["TEST_VAR"] = "old-value"
				return vars
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to update variable
		remoteImpl.UpdateRepositoryVariable(ctx, errorCollector, false, "test-repo", "TEST_VAR", "new-value")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "API error")

		// Verify API call was made
		assert.Equal(t, 3, mockClient.callCount) // 1 for getGHESVersion, 1 for updating variable
		assert.Equal(t, "/repos/myorg/test-repo/actions/variables/TEST_VAR", mockClient.lastEndpoint)
		assert.Equal(t, "", mockClient.lastParameters)
		assert.Equal(t, "PATCH", mockClient.lastMethod)
		assert.Equal(t, map[string]interface{}{
			"name":  "TEST_VAR",
			"value": "new-value",
		}, mockClient.lastBody)

		// Verify variable was not updated in repository
		vars := repo.RepositoryVariables.GetEntity()
		assert.NotNil(t, vars)
		assert.Equal(t, "old-value", vars["TEST_VAR"])
	})
}

func TestDeleteRepositoryVariable(t *testing.T) {
	t.Run("happy path: delete existing variable from repository", func(t *testing.T) {
		// Setup mock client
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an existing variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			RepositoryVariables: NewRemoteLazyLoader[string](func() map[string]string {
				vars := make(map[string]string)
				vars["TEST_VAR"] = "value"
				return vars
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Delete variable
		remoteImpl.DeleteRepositoryVariable(ctx, errorCollector, false, "test-repo", "TEST_VAR")

		// Verify no errors occurred
		assert.Empty(t, errorCollector.Errors)

		// Verify API call was made correctly
		assert.Equal(t, 3, mockClient.callCount) // 1 for getGHESVersion, 1 for deleting variable
		assert.Equal(t, "/repos/myorg/test-repo/actions/variables/TEST_VAR", mockClient.lastEndpoint)
		assert.Equal(t, "", mockClient.lastParameters)
		assert.Equal(t, "DELETE", mockClient.lastMethod)
		assert.Nil(t, mockClient.lastBody)

		// Verify variable was removed from repository
		vars := repo.RepositoryVariables.GetEntity()
		assert.NotNil(t, vars)
		assert.Empty(t, vars)
	})

	t.Run("error path: repository not found", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Try to delete variable from non-existent repository
		remoteImpl.DeleteRepositoryVariable(ctx, errorCollector, false, "non-existent", "TEST_VAR")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository non-existent not found")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("error path: variable not found", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository without the variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			RepositoryVariables: NewRemoteLazyLoader[string](func() map[string]string {
				return make(map[string]string)
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to delete non-existent variable
		remoteImpl.DeleteRepositoryVariable(ctx, errorCollector, false, "test-repo", "TEST_VAR")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "variable TEST_VAR not found")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("happy path: dry run mode", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an existing variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			RepositoryVariables: NewRemoteLazyLoader[string](func() map[string]string {
				vars := make(map[string]string)
				vars["TEST_VAR"] = "value"
				return vars
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Delete variable in dry run mode
		remoteImpl.DeleteRepositoryVariable(ctx, errorCollector, true, "test-repo", "TEST_VAR")

		// Verify no errors occurred
		assert.Empty(t, errorCollector.Errors)

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion

		// Verify variable was removed from repository
		vars := repo.RepositoryVariables.GetEntity()
		assert.NotNil(t, vars)
		assert.Empty(t, vars)
	})

	t.Run("error path: API error", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{
			shouldError:  true,
			errorMessage: "API error",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an existing variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			RepositoryVariables: NewRemoteLazyLoader[string](func() map[string]string {
				vars := make(map[string]string)
				vars["TEST_VAR"] = "value"
				return vars
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to delete variable
		remoteImpl.DeleteRepositoryVariable(ctx, errorCollector, false, "test-repo", "TEST_VAR")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "API error")

		// Verify API call was made
		assert.Equal(t, 3, mockClient.callCount) // 1 for getGHESVersion, 1 for deleting variable
		assert.Equal(t, "/repos/myorg/test-repo/actions/variables/TEST_VAR", mockClient.lastEndpoint)
		assert.Equal(t, "", mockClient.lastParameters)
		assert.Equal(t, "DELETE", mockClient.lastMethod)
		assert.Nil(t, mockClient.lastBody)

		// Verify variable was not removed from repository
		vars := repo.RepositoryVariables.GetEntity()
		assert.NotNil(t, vars)
		assert.Equal(t, "value", vars["TEST_VAR"])
	})
}

func TestUpdateRepositoryEnvironmentVariable(t *testing.T) {
	t.Run("happy path: update existing variable in repository environment", func(t *testing.T) {
		// Setup mock client
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an environment and existing variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name: "production",
					Variables: map[string]string{
						"TEST_VAR": "old-value",
					},
				}
				return envs
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Update variable
		remoteImpl.UpdateRepositoryEnvironmentVariable(ctx, errorCollector, false, "test-repo", "production", "TEST_VAR", "new-value")

		// Verify no errors occurred
		assert.Empty(t, errorCollector.Errors)

		// Verify API call was made correctly
		assert.Equal(t, 3, mockClient.callCount) // 1 for getGHESVersion, 1 for updating variable
		assert.Equal(t, "/repos/myorg/test-repo/environments/production/variables/TEST_VAR", mockClient.lastEndpoint)
		assert.Equal(t, "", mockClient.lastParameters)
		assert.Equal(t, "PATCH", mockClient.lastMethod)
		assert.Equal(t, map[string]interface{}{
			"name":  "TEST_VAR",
			"value": "new-value",
		}, mockClient.lastBody)

		// Verify variable was updated in repository environment
		envs := repo.Environments.GetEntity()
		assert.NotNil(t, envs)
		assert.Equal(t, 1, len(envs))
		assert.NotNil(t, envs["production"])
		assert.Equal(t, "new-value", envs["production"].Variables["TEST_VAR"])
	})

	t.Run("error path: repository not found", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Try to update variable in non-existent repository
		remoteImpl.UpdateRepositoryEnvironmentVariable(ctx, errorCollector, false, "non-existent", "production", "TEST_VAR", "new-value")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository non-existent not found")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("error path: environment not found", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository without the environment
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				return make(map[string]*GithubEnvironment)
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to update variable in non-existent environment
		remoteImpl.UpdateRepositoryEnvironmentVariable(ctx, errorCollector, false, "test-repo", "production", "TEST_VAR", "new-value")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "environment production not found")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("error path: variable not found", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an environment but without the variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name:      "production",
					Variables: map[string]string{},
				}
				return envs
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to update non-existent variable
		remoteImpl.UpdateRepositoryEnvironmentVariable(ctx, errorCollector, false, "test-repo", "production", "TEST_VAR", "new-value")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "variable TEST_VAR not found")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("happy path: dry run mode", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an environment and existing variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name: "production",
					Variables: map[string]string{
						"TEST_VAR": "old-value",
					},
				}
				return envs
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Update variable in dry run mode
		remoteImpl.UpdateRepositoryEnvironmentVariable(ctx, errorCollector, true, "test-repo", "production", "TEST_VAR", "new-value")

		// Verify no errors occurred
		assert.Empty(t, errorCollector.Errors)

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion

		// Verify variable was updated in repository environment
		envs := repo.Environments.GetEntity()
		assert.NotNil(t, envs)
		assert.Equal(t, 1, len(envs))
		assert.NotNil(t, envs["production"])
		assert.Equal(t, "new-value", envs["production"].Variables["TEST_VAR"])
	})

	t.Run("error path: API error", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{
			shouldError:  true,
			errorMessage: "API error",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an environment and existing variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name: "production",
					Variables: map[string]string{
						"TEST_VAR": "old-value",
					},
				}
				return envs
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to update variable
		remoteImpl.UpdateRepositoryEnvironmentVariable(ctx, errorCollector, false, "test-repo", "production", "TEST_VAR", "new-value")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "API error")

		// Verify API call was made
		assert.Equal(t, 3, mockClient.callCount) // 1 for getGHESVersion, 1 for updating variable
		assert.Equal(t, "/repos/myorg/test-repo/environments/production/variables/TEST_VAR", mockClient.lastEndpoint)
		assert.Equal(t, "", mockClient.lastParameters)
		assert.Equal(t, "PATCH", mockClient.lastMethod)
		assert.Equal(t, map[string]interface{}{
			"name":  "TEST_VAR",
			"value": "new-value",
		}, mockClient.lastBody)

		// Verify variable was not updated in repository environment
		envs := repo.Environments.GetEntity()
		assert.NotNil(t, envs)
		assert.Equal(t, 1, len(envs))
		assert.NotNil(t, envs["production"])
		assert.Equal(t, "old-value", envs["production"].Variables["TEST_VAR"])
	})
}

func TestDeleteRepositoryEnvironmentVariable(t *testing.T) {
	t.Run("happy path: delete existing variable from repository environment", func(t *testing.T) {
		// Setup mock client
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an environment and existing variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name: "production",
					Variables: map[string]string{
						"TEST_VAR": "value",
					},
				}
				return envs
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Delete variable
		remoteImpl.DeleteRepositoryEnvironmentVariable(ctx, errorCollector, false, "test-repo", "production", "TEST_VAR")

		// Verify no errors occurred
		assert.Empty(t, errorCollector.Errors)

		// Verify API call was made correctly
		assert.Equal(t, 3, mockClient.callCount) // 1 for getGHESVersion, 1 for deleting variable
		assert.Equal(t, "/repos/myorg/test-repo/environments/production/variables/TEST_VAR", mockClient.lastEndpoint)
		assert.Equal(t, "", mockClient.lastParameters)
		assert.Equal(t, "DELETE", mockClient.lastMethod)
		assert.Nil(t, mockClient.lastBody)

		// Verify variable was removed from repository environment
		envs := repo.Environments.GetEntity()
		assert.NotNil(t, envs)
		assert.Equal(t, 1, len(envs))
		assert.NotNil(t, envs["production"])
		assert.Empty(t, envs["production"].Variables)
	})

	t.Run("error path: repository not found", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Try to delete variable from non-existent repository
		remoteImpl.DeleteRepositoryEnvironmentVariable(ctx, errorCollector, false, "non-existent", "production", "TEST_VAR")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "repository non-existent not found")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("error path: environment not found", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository without the environment
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				return make(map[string]*GithubEnvironment)
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to delete variable from non-existent environment
		remoteImpl.DeleteRepositoryEnvironmentVariable(ctx, errorCollector, false, "test-repo", "production", "TEST_VAR")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "environment production not found")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("error path: variable not found", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an environment but without the variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name:      "production",
					Variables: map[string]string{},
				}
				return envs
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to delete non-existent variable
		remoteImpl.DeleteRepositoryEnvironmentVariable(ctx, errorCollector, false, "test-repo", "production", "TEST_VAR")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "variable TEST_VAR not found")

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion
	})

	t.Run("happy path: dry run mode", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an environment and existing variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name: "production",
					Variables: map[string]string{
						"TEST_VAR": "value",
					},
				}
				return envs
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Delete variable in dry run mode
		remoteImpl.DeleteRepositoryEnvironmentVariable(ctx, errorCollector, true, "test-repo", "production", "TEST_VAR")

		// Verify no errors occurred
		assert.Empty(t, errorCollector.Errors)

		// Verify no API calls were made
		assert.Equal(t, 2, mockClient.callCount) // Only getGHESVersion

		// Verify variable was removed from repository environment
		envs := repo.Environments.GetEntity()
		assert.NotNil(t, envs)
		assert.Equal(t, 1, len(envs))
		assert.NotNil(t, envs["production"])
		assert.Empty(t, envs["production"].Variables)
	})

	t.Run("error path: API error", func(t *testing.T) {
		mockClient := &LoadEnvironmentVariablesMockClient{
			shouldError:  true,
			errorMessage: "API error",
		}

		remoteImpl := NewGoliacRemoteImpl(mockClient, "myorg", true)
		ctx := context.TODO()
		errorCollector := observability.NewErrorCollection()

		// Create a repository with an environment and existing variable
		repo := &GithubRepository{
			Name: "test-repo",
			Id:   123,
			Environments: NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				envs := make(map[string]*GithubEnvironment)
				envs["production"] = &GithubEnvironment{
					Name: "production",
					Variables: map[string]string{
						"TEST_VAR": "value",
					},
				}
				return envs
			}),
		}
		remoteImpl.repositories = map[string]*GithubRepository{
			"test-repo": repo,
		}

		// Try to delete variable
		remoteImpl.DeleteRepositoryEnvironmentVariable(ctx, errorCollector, false, "test-repo", "production", "TEST_VAR")

		// Verify error was collected
		assert.NotEmpty(t, errorCollector.Errors)
		assert.Contains(t, errorCollector.Errors[0].Error(), "API error")

		// Verify API call was made
		assert.Equal(t, 3, mockClient.callCount) // 1 for getGHESVersion, 1 for deleting variable
		assert.Equal(t, "/repos/myorg/test-repo/environments/production/variables/TEST_VAR", mockClient.lastEndpoint)
		assert.Equal(t, "", mockClient.lastParameters)
		assert.Equal(t, "DELETE", mockClient.lastMethod)
		assert.Nil(t, mockClient.lastBody)

		// Verify variable was not removed from repository environment
		envs := repo.Environments.GetEntity()
		assert.NotNil(t, envs)
		assert.Equal(t, 1, len(envs))
		assert.NotNil(t, envs["production"])
		assert.Equal(t, "value", envs["production"].Variables["TEST_VAR"])
	})
}
