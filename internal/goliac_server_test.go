package internal

import (
	"context"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/gosimple/slug"
	"github.com/stretchr/testify/assert"

	"github.com/goliac-project/goliac/internal/config"
	"github.com/goliac-project/goliac/internal/engine"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/goliac-project/goliac/internal/github"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/goliac-project/goliac/internal/workflow"
	"github.com/goliac-project/goliac/swagger_gen/restapi/operations/app"
)

type GoliacLocalMock struct {
	teams         map[string]*entity.Team
	repositories  map[string]*entity.Repository
	users         map[string]*entity.User
	externalUsers map[string]*entity.User
	rulesets      map[string]*entity.RuleSet
	workflows     map[string]*entity.Workflow
	repoconfig    *config.RepositoryConfig
}

func (g *GoliacLocalMock) Teams() map[string]*entity.Team {
	return g.teams
}
func (g *GoliacLocalMock) Repositories() map[string]*entity.Repository {
	return g.repositories
}
func (g *GoliacLocalMock) Users() map[string]*entity.User {
	return g.users
}
func (g *GoliacLocalMock) ExternalUsers() map[string]*entity.User {
	return g.externalUsers
}
func (g *GoliacLocalMock) RuleSets() map[string]*entity.RuleSet {
	return g.rulesets
}
func (g *GoliacLocalMock) Workflows() map[string]*entity.Workflow {
	return g.workflows
}
func (g *GoliacLocalMock) RepoConfig() *config.RepositoryConfig {
	return g.repoconfig
}

func fixtureGoliacLocal() (*GoliacLocalMock, *GoliacRemoteMock) {
	// local mock
	l := GoliacLocalMock{
		teams:         make(map[string]*entity.Team),
		repositories:  make(map[string]*entity.Repository),
		users:         make(map[string]*entity.User),
		externalUsers: make(map[string]*entity.User),
		rulesets:      make(map[string]*entity.RuleSet),
		workflows:     make(map[string]*entity.Workflow),
		repoconfig:    &config.RepositoryConfig{},
	}

	// users
	user1 := entity.User{}
	user1.Name = "user1"
	user1.Spec.GithubID = "github1"

	user2 := entity.User{}
	user2.Name = "user2"
	user2.Spec.GithubID = "github2"

	user3 := entity.User{}
	user3.Name = "user3"
	user3.Spec.GithubID = "github3"

	l.users["user1"] = &user1
	l.users["user2"] = &user2
	l.users["user3"] = &user3

	// external users
	userE1 := entity.User{}
	userE1.Name = "userE1"
	userE1.Spec.GithubID = "githubE1"

	l.externalUsers["userE1"] = &userE1

	// teams
	ateam := entity.Team{}
	ateam.Name = "ateam"
	ateam.Spec.Owners = []string{"user1"}
	ateam.Spec.Members = []string{"user3"}

	mixteam := entity.Team{}
	mixteam.Name = "mixteam"
	mixteam.Spec.Owners = []string{"user1"}
	mixteam.Spec.Members = []string{"userE1"}

	externallyManaged := entity.Team{}
	externallyManaged.Name = "externallyManaged"
	externallyManaged.Spec.ExternallyManaged = true
	externallyManaged.Spec.Owners = []string{}
	externallyManaged.Spec.Members = []string{}

	l.teams["ateam"] = &ateam
	l.teams["mixteam"] = &mixteam
	l.teams["externallyManaged"] = &externallyManaged

	// repositories
	repoA := entity.Repository{}
	repoA.Name = "repoA"
	ownerA := "ateam"
	repoA.Owner = &ownerA
	repoA.Spec.Readers = []string{}
	repoA.Spec.Writers = []string{}

	repoB := entity.Repository{}
	repoB.Name = "repoB"
	ownerB := "mixteam"
	repoB.Owner = &ownerB
	repoB.Spec.Readers = []string{"ateam"}
	repoB.Spec.Writers = []string{}

	// add enviroment and variables
	repoB.Spec.Environments = []entity.RepositoryEnvironment{
		{
			Name: "env1",
			Variables: map[string]string{
				"var1": "value1",
			},
		},
	}
	repoB.Spec.ActionsVariables = map[string]string{
		"var2": "value2",
	}

	l.repositories["repoA"] = &repoA
	l.repositories["repoB"] = &repoB

	// forcemerge workflows
	fmtest := entity.Workflow{}
	fmtest.ApiVersion = "v1"
	fmtest.Kind = "ForcemergeWorkflow"
	fmtest.Name = "fmtest"
	fmtest.Spec.Description = "fmtest"
	fmtest.Spec.WorkflowType = "forcemerge"
	fmtest.Spec.Steps = []struct {
		Name       string                 `yaml:"name"`
		Properties map[string]interface{} `yaml:"properties"`
	}{
		{
			Name: "jira_ticket_creation",
			Properties: map[string]interface{}{
				"project_key": "SRE",
				"issue_type":  "Bug",
			},
		},
	}
	fmtest.Spec.Repositories = struct {
		Allowed []string `yaml:"allowed"`
		Except  []string `yaml:"except"`
	}{
		Allowed: []string{"goliac"},
	}
	fmtest.Spec.Acls = struct {
		Allowed []string `yaml:"allowed"`
		Except  []string `yaml:"except"`
	}{
		Allowed: []string{"test-team"},
	}
	l.workflows["fmtest"] = &fmtest
	l.repoconfig.Workflows = []string{"fmtest"}

	// remote mock
	r := GoliacRemoteMock{
		teams: make(map[string]*engine.GithubTeam),
	}

	r.teams[slug.Make("externallyManaged")] = &engine.GithubTeam{
		Name:    "externallyManaged",
		Slug:    slug.Make("externallyManaged"),
		Members: []string{"github1"},
	}

	return &l, &r
}

type GoliacRemoteMock struct {
	teams map[string]*engine.GithubTeam
}

func (g *GoliacRemoteMock) Teams(ctx context.Context, current bool) map[string]*engine.GithubTeam {
	return g.teams
}
func (g *GoliacRemoteMock) RepositoriesSecretsPerRepository(ctx context.Context, repositoryName string) (map[string]*engine.GithubVariable, error) {
	if repositoryName == "repoB" {
		return map[string]*engine.GithubVariable{
			"var1": {
				Name: "var1",
			},
		}, nil
	}
	return nil, nil
}

func (g *GoliacRemoteMock) EnvironmentSecretsPerRepository(ctx context.Context, environments []string, repositoryName string) (map[string]map[string]*engine.GithubVariable, error) {
	if repositoryName == "repoB" {
		return map[string]map[string]*engine.GithubVariable{
			"env1": {
				"var1": {
					Name: "var1",
				},
			},
		}, nil
	}
	return nil, nil
}

type GoliacMock struct {
	local  engine.GoliacLocalResources
	remote engine.GoliacRemoteResources
	client *GithubClientMock
}

func (g *GoliacMock) Apply(ctx context.Context, errorCollector *observability.ErrorCollection, fs billy.Filesystem, dryrun bool, repo string, branch string) *engine.UnmanagedResources {
	unmanaged := &engine.UnmanagedResources{
		Users:        make(map[string]bool),
		Teams:        make(map[string]bool),
		Repositories: make(map[string]bool),
		RuleSets:     make(map[string]bool),
	}
	unmanaged.Users["unmanaged"] = true
	return unmanaged
}
func (g *GoliacMock) UsersUpdate(ctx context.Context, errorCollector *observability.ErrorCollection, fs billy.Filesystem, repositoryUrl, branch string, dryrun bool, force bool) bool {
	return false
}
func (g *GoliacMock) FlushCache() {
}

func (g *GoliacMock) GetLocal() engine.GoliacLocalResources {
	return g.local
}
func (g *GoliacMock) GetRemote() engine.GoliacRemoteResources {
	return g.remote
}
func (g *GoliacMock) GetRemoteClient() github.GitHubClient {
	return g.client
}
func (g *GoliacMock) ExternalCreateRepository(ctx context.Context, errorCollector *observability.ErrorCollection, fs billy.Filesystem, githubToken, newRepositoryName, team, visibility, newRepositoryDefaultBranch string, repositoryUrl, branch string) {
}
func (g *GoliacMock) SetRemoteObservability(feedback observability.RemoteObservability) error {
	return nil
}
func (g *GoliacMock) ExecuteWorkflow(ctx context.Context, username string, workflowName string, prPathToMerge string, dryrun bool) ([]string, error) {
	return nil, nil
}
func (g *GoliacMock) GetWorkflows() map[string]*entity.Workflow {
	return nil
}
func NewGoliacMock(local engine.GoliacLocalResources, remote engine.GoliacRemoteResources, client *GithubClientMock) Goliac {
	mock := GoliacMock{
		local:  local,
		remote: remote,
		client: client,
	}
	return &mock
}

func TestGetSelfGithubAppClientID(t *testing.T) {
	t.Run("happy path: get clientId", func(t *testing.T) {

		client := GitHubClientMock{}
		appInfo, err := GetSelfGithubAppClientID(&client, "githubAppClientSecret")
		assert.NoError(t, err)
		assert.Equal(t, "githubAppClientID", appInfo.ClientID)
		assert.Equal(t, "githubAppClientSecret", appInfo.ClientSecret)
	})
	t.Run("not happy path: empty client secret", func(t *testing.T) {

		client := GitHubClientMock{}
		appInfo, err := GetSelfGithubAppClientID(&client, "")
		assert.Error(t, err)
		assert.Equal(t, "githubAppClientID", appInfo.ClientID)
		assert.Equal(t, "", appInfo.ClientSecret)
	})
}

func TestGetUnmanaged(t *testing.T) {

	t.Run("happy path: get unmanaged", func(t *testing.T) {
		server := GoliacServerImpl{
			lastUnmanaged: &engine.UnmanagedResources{
				Users:                  make(map[string]bool),
				ExternallyManagedTeams: make(map[string]bool),
				Teams:                  make(map[string]bool),
				Repositories:           make(map[string]bool),
				RuleSets:               make(map[string]bool),
			},
		}
		server.lastUnmanaged.Users["unmanagedUser"] = true
		server.lastUnmanaged.ExternallyManagedTeams["unmanagedExternallyManagedTeam"] = true
		server.lastUnmanaged.Teams["unmanagedTeam"] = true
		server.lastUnmanaged.Repositories["unmanagedRepository"] = true
		server.lastUnmanaged.RuleSets["unmanagedRuleset"] = true

		res := server.GetUnmanaged(app.GetUnmanagedParams{})
		payload := res.(*app.GetUnmanagedOK)
		assert.Equal(t, 1, len(payload.Payload.Users))
		assert.Equal(t, 1, len(payload.Payload.ExternallyManagedTeams))
		assert.Equal(t, 1, len(payload.Payload.Teams))
		assert.Equal(t, 1, len(payload.Payload.Repos))
		assert.Equal(t, 1, len(payload.Payload.Rulesets))

		assert.Equal(t, "unmanagedUser", payload.Payload.Users[0])
		assert.Equal(t, "unmanagedExternallyManagedTeam", payload.Payload.ExternallyManagedTeams[0])
		assert.Equal(t, "unmanagedTeam", payload.Payload.Teams[0])
		assert.Equal(t, "unmanagedRepository", payload.Payload.Repos[0])
		assert.Equal(t, "unmanagedRuleset", payload.Payload.Rulesets[0])
	})
}

func TestGetStatistics(t *testing.T) {

	t.Run("happy path: get statistics", func(t *testing.T) {
		server := GoliacServerImpl{
			lastTimeToApply: time.Duration(10 * time.Second),
			lastStatistics: config.GoliacStatistics{
				GithubApiCalls:  123,
				GithubThrottled: 4,
			},
			maxTimeToApply: time.Duration(20 * time.Second),
			maxStatistics: config.GoliacStatistics{
				GithubApiCalls:  567,
				GithubThrottled: 8,
			},
		}

		res := server.GetStatistics(app.GetStatiticsParams{})
		payload := res.(*app.GetStatiticsOK)
		assert.Equal(t, "10s", payload.Payload.LastTimeToApply)
		assert.Equal(t, "20s", payload.Payload.MaxTimeToApply)
		assert.Equal(t, int64(123), payload.Payload.LastGithubAPICalls)
		assert.Equal(t, int64(567), payload.Payload.MaxGithubAPICalls)
		assert.Equal(t, int64(4), payload.Payload.LastGithubThrottled)
		assert.Equal(t, int64(8), payload.Payload.MaxGithubThrottled)
	})
}

func TestAppGetUsers(t *testing.T) {
	localfixture, remotefixture := fixtureGoliacLocal()
	goliac := NewGoliacMock(localfixture, remotefixture, &GithubClientMock{})
	now := time.Now()
	server := GoliacServerImpl{
		goliac:        goliac,
		ready:         true,
		lastSyncTime:  &now,
		lastSyncError: nil,
	}

	t.Run("happy path: get status", func(t *testing.T) {
		res := server.GetStatus(app.GetStatusParams{})
		payload := res.(*app.GetStatusOK)
		assert.Equal(t, int64(2), payload.Payload.NbRepos)
		assert.Equal(t, int64(3), payload.Payload.NbTeams)
		assert.Equal(t, int64(3), payload.Payload.NbUsers)
		assert.Equal(t, int64(1), payload.Payload.NbUsersExternal)
	})

	t.Run("happy path: list users", func(t *testing.T) {
		res := server.GetUsers(app.GetUsersParams{})
		payload := res.(*app.GetUsersOK)
		assert.Equal(t, 3, len(payload.Payload)) // 3 users + 1 external
	})

	t.Run("happy path: get user1", func(t *testing.T) {
		res := server.GetUser(app.GetUserParams{UserID: "user1"})
		payload := res.(*app.GetUserOK)
		assert.Equal(t, 2, len(payload.Payload.Teams))
		assert.Equal(t, 2, len(payload.Payload.Teams))
		assert.Equal(t, 2, len(payload.Payload.Repositories))
	})
}
func TestAppGetTeams(t *testing.T) {
	localfixture, remotefixture := fixtureGoliacLocal()
	goliac := NewGoliacMock(localfixture, remotefixture, &GithubClientMock{})
	now := time.Now()
	server := GoliacServerImpl{
		goliac:        goliac,
		ready:         true,
		lastSyncTime:  &now,
		lastSyncError: nil,
	}

	t.Run("happy path: get teams", func(t *testing.T) {
		res := server.GetTeams(app.GetTeamsParams{})
		payload := res.(*app.GetTeamsOK)
		assert.Equal(t, 3, len(payload.Payload))
	})

	t.Run("happy path: get team ", func(t *testing.T) {
		res := server.GetTeam(app.GetTeamParams{TeamID: "ateam"})
		payload := res.(*app.GetTeamOK)
		assert.Equal(t, "ateam", payload.Payload.Name)
		assert.Equal(t, 1, len(payload.Payload.Owners))
		assert.Equal(t, 1, len(payload.Payload.Members))
	})
	t.Run("not happy path: team not found", func(t *testing.T) {
		res := server.GetTeam(app.GetTeamParams{TeamID: "unknown"})
		assert.NotZero(t, res.(*app.GetTeamDefault))
	})

	t.Run("happy path: get externally managemed team members", func(t *testing.T) {
		res := server.GetTeam(app.GetTeamParams{TeamID: "externallyManaged"})
		payload := res.(*app.GetTeamOK)
		assert.Equal(t, "externallyManaged", payload.Payload.Name)
		assert.Equal(t, 1, len(payload.Payload.Owners))
		assert.Equal(t, 0, len(payload.Payload.Members))
	})
}

func TestAppGetRepositories(t *testing.T) {
	localfixture, remotefixture := fixtureGoliacLocal()
	goliac := NewGoliacMock(localfixture, remotefixture, &GithubClientMock{})
	now := time.Now()
	server := GoliacServerImpl{
		goliac:        goliac,
		ready:         true,
		lastSyncTime:  &now,
		lastSyncError: nil,
	}

	t.Run("happy path: get repositories", func(t *testing.T) {
		res := server.GetRepositories(app.GetRepositoriesParams{})
		payload := res.(*app.GetRepositoriesOK)
		assert.Equal(t, 2, len(payload.Payload))
	})

	t.Run("happy path: get repository ", func(t *testing.T) {
		res := server.GetRepository(app.GetRepositoryParams{RepositoryID: "repoB"})
		payload := res.(*app.GetRepositoryOK)
		assert.Equal(t, "repoB", payload.Payload.Name)
		assert.Equal(t, 2, len(payload.Payload.Teams))
	})

	t.Run("not happy path: repository not found", func(t *testing.T) {
		res := server.GetRepository(app.GetRepositoryParams{RepositoryID: "repoC"})
		assert.NotZero(t, res.(*app.GetRepositoryDefault))
	})

	t.Run("happy path: get repository with secrets", func(t *testing.T) {
		res := server.GetRepository(app.GetRepositoryParams{RepositoryID: "repoB"})
		payload := res.(*app.GetRepositoryOK)
		assert.Equal(t, "repoB", payload.Payload.Name)
		assert.Equal(t, 2, len(payload.Payload.Teams))

		assert.Equal(t, 1, len(payload.Payload.Environments))
		assert.Equal(t, "env1", payload.Payload.Environments[0].Name)
		assert.Equal(t, 1, len(payload.Payload.Environments[0].Secrets))
		assert.Equal(t, "var1", payload.Payload.Environments[0].Secrets[0].Name)
		assert.Equal(t, 1, len(payload.Payload.Environments[0].Variables))
		assert.Equal(t, "var1", payload.Payload.Environments[0].Variables[0].Name)
		assert.Equal(t, "value1", payload.Payload.Environments[0].Variables[0].Value)
		assert.Equal(t, 1, len(payload.Payload.Variables))
		assert.Equal(t, "var2", payload.Payload.Variables[0].Name)
		assert.Equal(t, "value2", payload.Payload.Variables[0].Value)
	})
}

func TestGetCollaborators(t *testing.T) {
	t.Run("happy path: get collaborators", func(t *testing.T) {
		localfixture, remotefixture := fixtureGoliacLocal()
		goliac := NewGoliacMock(localfixture, remotefixture, &GithubClientMock{})
		server := GoliacServerImpl{
			goliac: goliac,
		}

		res := server.GetCollaborators(app.GetCollaboratorsParams{})
		payload := res.(*app.GetCollaboratorsOK)
		assert.Equal(t, 1, len(payload.Payload))
		assert.Equal(t, "userE1", payload.Payload[0].Name)
	})
}

func TestGetCollaborator(t *testing.T) {
	t.Run("happy path: get collaborator", func(t *testing.T) {
		localfixture, remotefixture := fixtureGoliacLocal()
		goliac := NewGoliacMock(localfixture, remotefixture, &GithubClientMock{})
		server := GoliacServerImpl{
			goliac: goliac,
		}

		res := server.GetCollaborator(app.GetCollaboratorParams{CollaboratorID: "userE1"})
		payload := res.(*app.GetCollaboratorOK)
		assert.Equal(t, "githubE1", payload.Payload.Githubid)
		assert.Equal(t, 0, len(payload.Payload.Repositories))
	})

	t.Run("happy path: get collaborator", func(t *testing.T) {
		localfixture, remotefixture := fixtureGoliacLocal()

		lRepos := localfixture.Repositories()
		lRepos["repoB"].Spec.ExternalUserReaders = []string{"userE1"}
		goliac := NewGoliacMock(localfixture, remotefixture, &GithubClientMock{})
		server := GoliacServerImpl{
			goliac: goliac,
		}

		res := server.GetCollaborator(app.GetCollaboratorParams{CollaboratorID: "userE1"})
		payload := res.(*app.GetCollaboratorOK)
		assert.Equal(t, "githubE1", payload.Payload.Githubid)
		assert.Equal(t, 1, len(payload.Payload.Repositories))
		assert.Equal(t, "repoB", payload.Payload.Repositories[0].Name)
	})
}

type WorkflowMock struct {
	workflowName string
	explanation  string
}

func (w *WorkflowMock) ExecuteWorkflow(ctx context.Context, repoconfigForceMergeworkflows []string, username, workflowName, explanation string, properties map[string]string, dryrun bool) ([]string, error) {
	w.workflowName = workflowName
	w.explanation = explanation
	return nil, nil
}
func TestHandleIssueComment(t *testing.T) {
	t.Run("happy path: handle issue comment without explanation", func(t *testing.T) {
		localfixture, remotefixture := fixtureGoliacLocal()
		fmtest := WorkflowMock{}

		githubClient := &GithubClientMock{}

		goliac := NewGoliacMock(localfixture, remotefixture, githubClient)
		server := GoliacServerImpl{
			goliac: goliac,
			worflowInstances: map[string]workflow.Workflow{
				"forcemerge": &fmtest,
			},
		}

		server.handleIssueComment(context.Background(), "org", "repoB", "https://github.com/org/repoB/pull/123", "userE1", "/forcemerge:fmtest:", 456)
		assert.Equal(t, "", fmtest.workflowName)
		assert.Equal(t, "", fmtest.explanation)

		// config.Config.Organization is empty so we have a '//' here
		assert.Equal(t, "/repos/org/repoB/issues/123/comments", githubClient.lastEndpoint)
		assert.Equal(t, "POST", githubClient.lastMethod)
		assert.Equal(t, "No explanation provided", githubClient.lastBody["body"])
	})

	t.Run("happy path: handle issue comment with explanation", func(t *testing.T) {
		localfixture, remotefixture := fixtureGoliacLocal()
		githubClient := &GithubClientMock{}
		fmtest := WorkflowMock{}
		goliac := NewGoliacMock(localfixture, remotefixture, githubClient)
		server := GoliacServerImpl{
			goliac: goliac,
			worflowInstances: map[string]workflow.Workflow{
				"forcemerge": &fmtest,
			},
		}

		server.handleIssueComment(context.Background(), "org", "repoB", "https://github.com/org/repoB/pull/123", "userE1", "/forcemerge:fmtest: foobar", 123)
		assert.Equal(t, "foobar", fmtest.explanation)
		assert.Equal(t, "fmtest", fmtest.workflowName)

		assert.Equal(t, "Workflow executed successfully", githubClient.lastBody["body"])
	})

	t.Run("happy path: handle issue comment with unknown workflow", func(t *testing.T) {
		localfixture, remotefixture := fixtureGoliacLocal()
		githubClient := &GithubClientMock{}
		goliac := NewGoliacMock(localfixture, remotefixture, githubClient)
		server := GoliacServerImpl{
			goliac: goliac,
		}

		server.handleIssueComment(context.Background(), "org", "repoB", "https://github.com/org/repoB/pull/123", "userE1", "/forcemerge", 123)
		assert.Equal(t, nil, githubClient.lastBody["body"])
	})

	t.Run("happy path: handle issue comment without mentioning a workflow", func(t *testing.T) {
		localfixture, remotefixture := fixtureGoliacLocal()
		githubClient := &GithubClientMock{}
		fmtest := WorkflowMock{}
		goliac := NewGoliacMock(localfixture, remotefixture, githubClient)
		server := GoliacServerImpl{
			goliac: goliac,
			worflowInstances: map[string]workflow.Workflow{
				"forcemerge": &fmtest,
			},
		}

		server.handleIssueComment(context.Background(), "org", "repoB", "https://github.com/org/repoB/pull/123", "userE1", "/forcemerge", 123)
		assert.Equal(t, "Available workflows:\n- `/forcemerge:fmtest: <explanation>`\n", githubClient.lastBody["body"])
	})
}
