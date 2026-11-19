package workflow

import (
	"context"
	"net/url"
	"strings"

	"github.com/goliac-project/goliac/internal/engine"
	"github.com/goliac-project/goliac/internal/entity"
)

// strip down version of Goliac Local
type LocalResourceMock struct {
	MockWorkflows map[string]*entity.Workflow
	LTeams        map[string]*entity.Team
	LUsers        map[string]*entity.User // map[githubid]githubusername
}

func (m *LocalResourceMock) Workflows() map[string]*entity.Workflow {
	return m.MockWorkflows
}

func (m *LocalResourceMock) RepositoryInWorkflow(repository string) bool {
	return false
}

func (m *LocalResourceMock) Teams() map[string]*entity.Team {
	return m.LTeams
}
func (m *LocalResourceMock) Users() map[string]*entity.User {
	return m.LUsers
}

type RemoteResourceMock struct {
	RTeams map[string]*engine.GithubTeam
}

func (m *RemoteResourceMock) Teams(ctx context.Context, current bool) map[string]*engine.GithubTeam {
	return m.RTeams
}

type GithubClientMock struct {
	LastCallEndpoint string
	LastCallBody     map[string]interface{}
}

func (m *GithubClientMock) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	m.LastCallEndpoint = endpoint
	m.LastCallBody = body
	if method == "GET" && strings.Contains(endpoint, "/pulls/") &&
		!strings.Contains(endpoint, "/merge") && !strings.Contains(endpoint, "/reviews") {
		return []byte(`{"title":"Test PR title"}`), nil
	}
	if method == "PUT" && strings.HasSuffix(endpoint, "/merge") {
		return []byte(`{"merged":true}`), nil
	}
	return nil, nil
}

type StepPluginMock struct{}

func (f *StepPluginMock) Execute(ctx context.Context, username, workflowDescription, explanation string, url *url.URL, properties map[string]interface{}) (string, error) {
	// Mock implementation
	return "mocked_url", nil
}

func NewStepPluginMock() StepPlugin {
	return &StepPluginMock{}
}
