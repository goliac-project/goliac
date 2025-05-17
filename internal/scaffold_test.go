package internal

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/goliac-project/goliac/internal/engine"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/goliac-project/goliac/internal/utils"
	"github.com/gosimple/slug"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

type ScaffoldGoliacRemoteMock struct {
	users      map[string]string
	teams      map[string]*engine.GithubTeam
	repos      map[string]*engine.GithubRepository
	teamsRepos map[string]map[string]*engine.GithubTeamRepo
}

func (s *ScaffoldGoliacRemoteMock) Load(ctx context.Context, continueOnError bool) error {
	return nil
}
func (s *ScaffoldGoliacRemoteMock) FlushCache() {
}
func (s *ScaffoldGoliacRemoteMock) FlushCacheUsersTeamsOnly() {
}
func (s *ScaffoldGoliacRemoteMock) Users(ctx context.Context) map[string]string {
	return s.users
}
func (s *ScaffoldGoliacRemoteMock) TeamSlugByName(ctx context.Context) map[string]string {
	slugbyname := make(map[string]string)
	for k, v := range s.teams {
		slugbyname[k] = slug.Make(v.Name)
	}
	return slugbyname
}
func (s *ScaffoldGoliacRemoteMock) Teams(ctx context.Context, current bool) map[string]*engine.GithubTeam {
	return s.teams
}
func (s *ScaffoldGoliacRemoteMock) Repositories(ctx context.Context) map[string]*engine.GithubRepository {
	return s.repos
}
func (s *ScaffoldGoliacRemoteMock) TeamRepositories(ctx context.Context) map[string]map[string]*engine.GithubTeamRepo {
	return s.teamsRepos
}
func (s *ScaffoldGoliacRemoteMock) RuleSets(ctx context.Context) map[string]*engine.GithubRuleSet {
	return nil
}
func (s *ScaffoldGoliacRemoteMock) AppIds(ctx context.Context) map[string]int {
	return nil
}
func (s *ScaffoldGoliacRemoteMock) IsEnterprise() bool {
	return true
}
func (m *ScaffoldGoliacRemoteMock) CountAssets(ctx context.Context, warmup bool) (int, error) {
	return 2*len(m.repos) + len(m.teams) + len(m.users), nil
}
func (g *ScaffoldGoliacRemoteMock) SetRemoteObservability(feedback observability.RemoteObservability) {
}

func (s *ScaffoldGoliacRemoteMock) EnvironmentSecretsPerRepository(ctx context.Context, environments []string, repositoryName string) (map[string]map[string]*engine.GithubVariable, error) {
	return nil, nil
}

func (s *ScaffoldGoliacRemoteMock) RepositoriesSecretsPerRepository(ctx context.Context, repositoryName string) (map[string]*engine.GithubVariable, error) {
	return nil, nil
}

func NewScaffoldGoliacRemoteMock() engine.GoliacRemote {
	users := make(map[string]string)
	teams := make(map[string]*engine.GithubTeam)
	repos := make(map[string]*engine.GithubRepository)
	teamsRepos := make(map[string]map[string]*engine.GithubTeamRepo)

	users["githubid1"] = "githubid1"
	users["githubid2"] = "githubid2"
	users["githubid3"] = "githubid3"
	users["githubid4"] = "githubid4"

	admin := engine.GithubTeam{
		Name:        "admin",
		Slug:        "admin",
		Members:     []string{"githubid1", "githubid2"},
		Maintainers: []string{},
	}
	teams["admin"] = &admin

	regular := engine.GithubTeam{
		Name:        "regular",
		Slug:        "regular",
		Members:     []string{"githubid2", "githubid3"},
		Maintainers: []string{},
	}
	teams["regular"] = &regular

	repo1 := engine.GithubRepository{
		Name:                "repo1",
		Environments:        NewMockMappedEntityLazyLoader(map[string]*engine.GithubEnvironment{}),
		RepositoryVariables: NewMockMappedEntityLazyLoader[string](map[string]string{}),
	}
	repos["repo1"] = &repo1

	repo2 := engine.GithubRepository{
		Name:                "repo2",
		Environments:        NewMockMappedEntityLazyLoader(map[string]*engine.GithubEnvironment{}),
		RepositoryVariables: NewMockMappedEntityLazyLoader[string](map[string]string{}),
	}
	repos["repo2"] = &repo2

	archivedRepo := engine.GithubRepository{
		Name:                "archived_repo",
		Environments:        NewMockMappedEntityLazyLoader(map[string]*engine.GithubEnvironment{}),
		RepositoryVariables: NewMockMappedEntityLazyLoader[string](map[string]string{}),
		BoolProperties:      map[string]bool{"archived": true},
	}
	repos["archived_repo"] = &archivedRepo

	teamRepoRegular := make(map[string]*engine.GithubTeamRepo)
	teamRepoRegular["repo1"] = &engine.GithubTeamRepo{
		Name:       "repo1",
		Permission: "WRITE",
	}
	teamRepoRegular["repo2"] = &engine.GithubTeamRepo{
		Name:       "repo2",
		Permission: "READ",
	}
	teamRepoRegular["archived_repo"] = &engine.GithubTeamRepo{
		Name:       "archived_repo",
		Permission: "WRITE",
	}
	teamsRepos["regular"] = teamRepoRegular

	teamRepoAdmin := make(map[string]*engine.GithubTeamRepo)
	teamRepoAdmin["repo2"] = &engine.GithubTeamRepo{
		Name:       "repo2",
		Permission: "WRITE",
	}
	teamsRepos["admin"] = teamRepoAdmin

	mock := ScaffoldGoliacRemoteMock{
		users:      users,
		teams:      teams,
		repos:      repos,
		teamsRepos: teamsRepos,
	}

	return &mock
}

func NewScaffoldGoliacRemoteMockWithMaintainers() engine.GoliacRemote {
	users := make(map[string]string)
	teams := make(map[string]*engine.GithubTeam)
	repos := make(map[string]*engine.GithubRepository)
	teamsRepos := make(map[string]map[string]*engine.GithubTeamRepo)

	users["githubid1"] = "githubid1"
	users["githubid2"] = "githubid2"
	users["githubid3"] = "githubid3"
	users["githubid4"] = "githubid4"

	admin := engine.GithubTeam{
		Name:        "admin",
		Slug:        "admin",
		Members:     []string{"githubid1", "githubid2"},
		Maintainers: []string{},
	}
	teams["admin"] = &admin

	regular := engine.GithubTeam{
		Name:        "regular",
		Slug:        "regular",
		Members:     []string{"githubid1", "githubid4"},
		Maintainers: []string{"githubid2", "githubid3"},
	}
	teams["regular"] = &regular

	repo1 := engine.GithubRepository{
		Name:                "repo1",
		Environments:        NewMockMappedEntityLazyLoader(map[string]*engine.GithubEnvironment{}),
		RepositoryVariables: NewMockMappedEntityLazyLoader[string](map[string]string{}),
	}
	repos["repo1"] = &repo1

	repo2 := engine.GithubRepository{
		Name:                "repo2",
		Environments:        NewMockMappedEntityLazyLoader(map[string]*engine.GithubEnvironment{}),
		RepositoryVariables: NewMockMappedEntityLazyLoader[string](map[string]string{}),
	}
	repos["repo2"] = &repo2

	teamRepoRegular := make(map[string]*engine.GithubTeamRepo)
	teamRepoRegular["repo1"] = &engine.GithubTeamRepo{
		Name:       "repo1",
		Permission: "WRITE",
	}
	teamRepoRegular["repo2"] = &engine.GithubTeamRepo{
		Name:       "repo2",
		Permission: "READ",
	}
	teamsRepos["regular"] = teamRepoRegular

	teamRepoAdmin := make(map[string]*engine.GithubTeamRepo)
	teamRepoAdmin["repo2"] = &engine.GithubTeamRepo{
		Name:       "repo2",
		Permission: "WRITE",
	}
	teamsRepos["admin"] = teamRepoAdmin

	mock := ScaffoldGoliacRemoteMock{
		users:      users,
		teams:      teams,
		repos:      repos,
		teamsRepos: teamsRepos,
	}

	return &mock
}

func LoadGithubSamlUsersMock(feedback observability.RemoteObservability) (map[string]*entity.User, error) {
	users := make(map[string]*entity.User)
	user1 := &entity.User{}
	user1.ApiVersion = "v1"
	user1.Kind = "User"
	user1.Name = "user1@company.com"
	user1.Spec.GithubID = "githubid1"
	users["user1@company.com"] = user1

	user2 := &entity.User{}
	user2.ApiVersion = "v1"
	user2.Kind = "User"
	user2.Name = "user2@company.com"
	user2.Spec.GithubID = "githubid2"
	users["user2@company.com"] = user2

	user3 := &entity.User{}
	user3.ApiVersion = "v1"
	user3.Kind = "User"
	user3.Name = "user3@company.com"
	user3.Spec.GithubID = "githubid3"
	users["user3@company.com"] = user3

	return users, nil
}

func NoLoadGithubSamlUsersMock(feedback observability.RemoteObservability) (map[string]*entity.User, error) {
	return nil, fmt.Errorf("not able to fetch SAML data")
}

func TestScaffoldUnit(t *testing.T) {

	// happy path
	t.Run("happy path: test users no SAML", func(t *testing.T) {
		fs := memfs.New()
		// MockGithubClient doesn't support concurrent access

		scaffold := &Scaffold{
			remote:                     NewScaffoldGoliacRemoteMock(),
			loadUsersFromGithubOrgSaml: NoLoadGithubSamlUsersMock,
		}

		ctx := context.TODO()
		users, err := scaffold.generateUsers(ctx, fs, "/users")
		assert.Nil(t, err)
		assert.Equal(t, 4, len(users))

		found, err := utils.Exists(fs, "/users/org/githubid1.yaml")
		assert.Nil(t, err)
		assert.Equal(t, true, found)
	})

	t.Run("happy path: test users SAML", func(t *testing.T) {
		fs := memfs.New()
		// MockGithubClient doesn't support concurrent access

		scaffold := &Scaffold{
			remote:                     NewScaffoldGoliacRemoteMock(),
			loadUsersFromGithubOrgSaml: LoadGithubSamlUsersMock,
		}

		ctx := context.TODO()
		users, err := scaffold.generateUsers(ctx, fs, "/users")
		assert.Nil(t, err)
		assert.Equal(t, 3, len(users))

		found, err := utils.Exists(fs, "/users/org/user1@company.com.yaml")
		assert.Nil(t, err)
		assert.Equal(t, true, found)
	})

	t.Run("happy path: test rulesets", func(t *testing.T) {
		fs := memfs.New()
		// MockGithubClient doesn't support concurrent access

		scaffold := &Scaffold{
			remote:                     NewScaffoldGoliacRemoteMock(),
			loadUsersFromGithubOrgSaml: LoadGithubSamlUsersMock,
		}

		err := scaffold.generateRuleset(fs, "/rulesets")
		assert.Nil(t, err)

		found, err := utils.Exists(fs, "/rulesets/default.yaml")
		assert.Nil(t, err)
		assert.Equal(t, true, found)
	})

	t.Run("happy path: test goliac.conf", func(t *testing.T) {
		fs := memfs.New()
		// MockGithubClient doesn't support concurrent access

		scaffold := &Scaffold{
			remote:                     NewScaffoldGoliacRemoteMock(),
			loadUsersFromGithubOrgSaml: LoadGithubSamlUsersMock,
		}

		err := scaffold.generateGoliacConf(fs, "/", "admin")
		assert.Nil(t, err)

		found, err := utils.Exists(fs, "/goliac.yaml")
		assert.Nil(t, err)
		assert.Equal(t, true, found)
	})

	t.Run("happy path: test github action", func(t *testing.T) {
		fs := memfs.New()
		// MockGithubClient doesn't support concurrent access

		scaffold := &Scaffold{
			remote:                     NewScaffoldGoliacRemoteMock(),
			loadUsersFromGithubOrgSaml: LoadGithubSamlUsersMock,
		}

		err := scaffold.generateGithubAction(fs, "/")
		assert.Nil(t, err)

		found, err := utils.Exists(fs, "/.github/workflows/pr.yaml")
		assert.Nil(t, err)
		assert.Equal(t, true, found)
	})
}

type MockMappedEntityLazyLoader[T any] struct {
	entity map[string]T
}

func (m *MockMappedEntityLazyLoader[T]) GetEntity() map[string]T {
	return m.entity
}

func NewMockMappedEntityLazyLoader[T any](entity map[string]T) *MockMappedEntityLazyLoader[T] {
	return &MockMappedEntityLazyLoader[T]{entity: entity}
}

func TestEnvironmentsAndVariables(t *testing.T) {
	t.Run("happy path: test environments and variables", func(t *testing.T) {

		users := make(map[string]string)
		teams := make(map[string]*engine.GithubTeam)
		repos := make(map[string]*engine.GithubRepository)
		teamsRepos := make(map[string]map[string]*engine.GithubTeamRepo)

		users["githubid1"] = "githubid1"
		users["githubid2"] = "githubid2"
		users["githubid3"] = "githubid3"
		users["githubid4"] = "githubid4"

		admin := engine.GithubTeam{
			Name:        "admin",
			Slug:        "admin",
			Members:     []string{"githubid1", "githubid2"},
			Maintainers: []string{},
		}
		teams["admin"] = &admin

		regular := engine.GithubTeam{
			Name:        "regular",
			Slug:        "regular",
			Members:     []string{"githubid1", "githubid4"},
			Maintainers: []string{"githubid2", "githubid3"},
		}
		teams["regular"] = &regular

		repo1 := engine.GithubRepository{
			Name:                "repo1",
			Environments:        NewMockMappedEntityLazyLoader(map[string]*engine.GithubEnvironment{}),
			RepositoryVariables: NewMockMappedEntityLazyLoader[string](map[string]string{}),
		}
		repos["repo1"] = &repo1

		repo2 := engine.GithubRepository{
			Name: "repo2",
		}
		repo2.Environments = NewMockMappedEntityLazyLoader[*engine.GithubEnvironment](map[string]*engine.GithubEnvironment{
			"env1": {
				Name: "env1",
			},
			"env2": {
				Name: "env2",
			},
		})
		repo2.Environments.GetEntity()["env1"].Variables = map[string]string{
			"var1": "value1",
		}
		repo2.Environments.GetEntity()["env2"].Variables = map[string]string{
			"var2": "value2",
		}
		repo2.RepositoryVariables = NewMockMappedEntityLazyLoader[string](map[string]string{
			"var2": "value2",
		})

		repos["repo2"] = &repo2

		teamRepoRegular := make(map[string]*engine.GithubTeamRepo)
		teamRepoRegular["repo1"] = &engine.GithubTeamRepo{
			Name:       "repo1",
			Permission: "WRITE",
		}
		teamRepoRegular["repo2"] = &engine.GithubTeamRepo{
			Name:       "repo2",
			Permission: "READ",
		}
		teamsRepos["regular"] = teamRepoRegular

		teamRepoAdmin := make(map[string]*engine.GithubTeamRepo)
		teamRepoAdmin["repo2"] = &engine.GithubTeamRepo{
			Name:       "repo2",
			Permission: "WRITE",
		}
		teamsRepos["admin"] = teamRepoAdmin

		mock := ScaffoldGoliacRemoteMock{
			users:      users,
			teams:      teams,
			repos:      repos,
			teamsRepos: teamsRepos,
		}

		fs := memfs.New()
		// MockGithubClient doesn't support concurrent access

		scaffold := &Scaffold{
			remote:                     &mock,
			loadUsersFromGithubOrgSaml: NoLoadGithubSamlUsersMock,
		}

		err := scaffold.generateTeams(context.TODO(), fs, "/teams", "/archived", users, "admin", false)
		assert.Nil(t, err)

		found, err := utils.Exists(fs, "/teams/admin/team.yaml")
		assert.Nil(t, err)
		assert.Equal(t, true, found)
		found, err = utils.Exists(fs, "/teams/regular/team.yaml")
		assert.Nil(t, err)
		assert.Equal(t, true, found)

		lrepo2, err := utils.ReadFile(fs, "/teams/admin/repo2.yaml")
		assert.Nil(t, err)

		var r2 entity.Repository
		err = yaml.Unmarshal(lrepo2, &r2)
		assert.Nil(t, err)
		assert.Equal(t, "repo2", r2.Name)
		assert.Equal(t, 2, len(r2.Spec.Environments))
		if r2.Spec.Environments[0].Name == "env1" {
			assert.Equal(t, "value1", r2.Spec.Environments[0].Variables["var1"])
		} else {
			assert.Equal(t, "value2", r2.Spec.Environments[0].Variables["var2"])
		}
		if r2.Spec.Environments[1].Name == "env1" {
			assert.Equal(t, "value1", r2.Spec.Environments[1].Variables["var1"])
		} else {
			assert.Equal(t, "value2", r2.Spec.Environments[1].Variables["var2"])
		}
	})
}

func TestScaffoldFull(t *testing.T) {

	t.Run("happy path: test teams and repos without SAML", func(t *testing.T) {
		fs := memfs.New()
		// MockGithubClient doesn't support concurrent access

		scaffold := &Scaffold{
			remote:                     NewScaffoldGoliacRemoteMock(),
			loadUsersFromGithubOrgSaml: NoLoadGithubSamlUsersMock,
		}

		ctx := context.TODO()
		users, err := scaffold.generateUsers(ctx, fs, "/users")
		assert.Nil(t, err)
		assert.Equal(t, 4, len(users))

		err = scaffold.generateTeams(ctx, fs, "/teams", "/archived", users, "admin", false)
		assert.Nil(t, err)

		found, err := utils.Exists(fs, "/teams/admin/team.yaml")
		assert.Nil(t, err)
		assert.Equal(t, true, found)

		found, err = utils.Exists(fs, "/teams/regular/repo1.yaml")
		assert.Nil(t, err)
		assert.Equal(t, true, found)
	})

	t.Run("happy path: test teams and repos with SAML", func(t *testing.T) {
		fs := memfs.New()
		// MockGithubClient doesn't support concurrent access

		scaffold := &Scaffold{
			remote:                     NewScaffoldGoliacRemoteMock(),
			loadUsersFromGithubOrgSaml: LoadGithubSamlUsersMock,
		}

		ctx := context.TODO()
		users, err := scaffold.generateUsers(ctx, fs, "/users")
		assert.Nil(t, err)
		assert.Equal(t, 3, len(users))

		err = scaffold.generateTeams(ctx, fs, "/teams", "/archived", users, "admin", false)
		assert.Nil(t, err)

		found, err := utils.Exists(fs, "/teams/admin/team.yaml")
		assert.Nil(t, err)
		assert.Equal(t, true, found)

		found, err = utils.Exists(fs, "/teams/regular/repo1.yaml")
		assert.Nil(t, err)
		assert.Equal(t, true, found)

		regularTeam, err := utils.ReadFile(fs, "/teams/regular/team.yaml")
		assert.Nil(t, err)

		var at entity.Team
		err = yaml.Unmarshal(regularTeam, &at)
		assert.Nil(t, err)
		assert.Equal(t, "regular", at.Name)
		assert.Equal(t, 2, len(at.Spec.Owners)) // githubid2,githubid3

		repo1, err := utils.ReadFile(fs, "/teams/regular/repo1.yaml")
		assert.Nil(t, err)

		var r1 entity.Repository
		err = yaml.Unmarshal(repo1, &r1)
		assert.Nil(t, err)
		assert.Equal(t, "repo1", r1.Name)
		assert.Equal(t, 0, len(r1.Spec.Writers)) // regular -> not counted
		assert.Equal(t, 0, len(r1.Spec.Readers))

		repo2, err := utils.ReadFile(fs, "/teams/admin/repo2.yaml")
		assert.Nil(t, err)

		var r2 entity.Repository
		err = yaml.Unmarshal(repo2, &r2)
		assert.Nil(t, err)
		assert.Equal(t, "repo2", r2.Name)
		assert.Equal(t, 1, len(r2.Spec.Readers)) // regular
	})

	t.Run("happy path: test teams with maintainers", func(t *testing.T) {
		fs := memfs.New()
		// MockGithubClient doesn't support concurrent access

		scaffold := &Scaffold{
			remote:                     NewScaffoldGoliacRemoteMockWithMaintainers(),
			loadUsersFromGithubOrgSaml: NoLoadGithubSamlUsersMock,
		}

		ctx := context.TODO()
		users, err := scaffold.generateUsers(ctx, fs, "/users")
		assert.Nil(t, err)
		assert.Equal(t, 4, len(users))

		err = scaffold.generateTeams(ctx, fs, "/teams", "/archived", users, "admin", false)
		assert.Nil(t, err)

		found, err := utils.Exists(fs, "/teams/regular/team.yaml")
		assert.Nil(t, err)
		assert.Equal(t, true, found)

		teamContent, err := utils.ReadFile(fs, "/teams/regular/team.yaml")
		assert.Nil(t, err)

		var teamDefinition entity.Team
		yaml.Unmarshal(teamContent, &teamDefinition)
		assert.Equal(t, 2, len(teamDefinition.Spec.Owners))
		assert.Equal(t, 2, len(teamDefinition.Spec.Members))
	})

	t.Run("happy path: test teams and archived repos", func(t *testing.T) {
		fs := memfs.New()
		// MockGithubClient doesn't support concurrent access

		scaffold := &Scaffold{
			remote:                     NewScaffoldGoliacRemoteMock(),
			loadUsersFromGithubOrgSaml: NoLoadGithubSamlUsersMock,
		}

		ctx := context.TODO()
		users, err := scaffold.generateUsers(ctx, fs, "/users")
		assert.Nil(t, err)
		assert.Equal(t, 4, len(users))

		err = scaffold.generateTeams(ctx, fs, "/teams", "/archived", users, "admin", false)
		assert.Nil(t, err)

		found, err := utils.Exists(fs, "/teams/admin/team.yaml")
		assert.Nil(t, err)
		assert.Equal(t, true, found)

		found, err = utils.Exists(fs, "/archived/archived_repo.yaml")
		assert.Nil(t, err)
		assert.Equal(t, true, found)
	})
}
