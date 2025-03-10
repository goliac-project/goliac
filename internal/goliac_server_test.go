package internal

import (
	"context"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/gosimple/slug"
	"github.com/stretchr/testify/assert"

	"github.com/goliac-project/goliac/internal/engine"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/goliac-project/goliac/swagger_gen/restapi/operations/app"
)

type GoliacLocalMock struct {
	teams         map[string]*entity.Team
	repositories  map[string]*entity.Repository
	users         map[string]*entity.User
	externalUsers map[string]*entity.User
	rulesets      map[string]*entity.RuleSet
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

func fixtureGoliacLocal() (*GoliacLocalMock, *GoliacRemoteMock) {
	// local mock
	l := GoliacLocalMock{
		teams:         make(map[string]*entity.Team),
		repositories:  make(map[string]*entity.Repository),
		users:         make(map[string]*entity.User),
		externalUsers: make(map[string]*entity.User),
		rulesets:      make(map[string]*entity.RuleSet),
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

	l.repositories["repoA"] = &repoA
	l.repositories["repoB"] = &repoB

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

type GoliacMock struct {
	local  engine.GoliacLocalResources
	remote engine.GoliacRemoteResources
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
func (g *GoliacMock) ExternalCreateRepository(ctx context.Context, errorCollector *observability.ErrorCollection, fs billy.Filesystem, githubToken, newRepositoryName, team, visibility, newRepositoryDefaultBranch string, repositoryUrl, branch string) {
}
func (g *GoliacMock) SetRemoteObservability(feedback observability.RemoteObservability) error {
	return nil
}
func NewGoliacMock(local engine.GoliacLocalResources, remote engine.GoliacRemoteResources) Goliac {
	mock := GoliacMock{
		local:  local,
		remote: remote,
	}
	return &mock
}

func TestAppGetUsers(t *testing.T) {
	localfixture, remotefixture := fixtureGoliacLocal()
	goliac := NewGoliacMock(localfixture, remotefixture)
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
	goliac := NewGoliacMock(localfixture, remotefixture)
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
	goliac := NewGoliacMock(localfixture, remotefixture)
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
}
