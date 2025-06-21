package internal

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/goliac-project/goliac/internal/config"
	"github.com/goliac-project/goliac/internal/engine"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/goliac-project/goliac/internal/usersync"
	"github.com/goliac-project/goliac/internal/utils"
	"github.com/stretchr/testify/assert"
)

//
// fixture
//

type FixtureFunc func(fs billy.Filesystem)

// create a working simple teams repository
func repoFixture1(fs billy.Filesystem) {
	fs.MkdirAll("teams", 0755)
	fs.MkdirAll("archived", 0755)

	// create users
	fs.MkdirAll("users/external", 0755)
	fs.MkdirAll("users/org", 0755)
	fs.MkdirAll("users/protected", 0755)
	utils.WriteFile(fs, "users/org/user1.yaml", []byte(`apiVersion: v1
kind: User
name: user1
spec:
  githubID: github1
`), 0644)
	utils.WriteFile(fs, "users/org/user2.yaml", []byte(`apiVersion: v1
kind: User
name: user2
spec:
  githubID: github2
`), 0644)
	utils.WriteFile(fs, "users/org/user3.yaml", []byte(`apiVersion: v1
kind: User
name: user3
spec:
  githubID: github3
`), 0644)
	utils.WriteFile(fs, "users/org/user4.yaml", []byte(`apiVersion: v1
kind: User
name: user4
spec:
  githubID: github4
`), 0644)

	// create teams
	fs.MkdirAll("teams/team1", 0755)
	utils.WriteFile(fs, "teams/team1/team.yaml", []byte(`apiVersion: v1
kind: Team
name: team1
spec:
  owners:
    - user1
    - user2
`), 0644)
	fs.MkdirAll("teams/team2", 0755)
	utils.WriteFile(fs, "teams/team2/team.yaml", []byte(`apiVersion: v1
kind: Team
name: team2
spec:
  owners:
    - user3
    - user4
`), 0644)

	// create repositories
	utils.WriteFile(fs, "teams/team1/repo1.yaml", []byte(`apiVersion: v1
kind: Repository
name: repo1
`), 0644)
	utils.WriteFile(fs, "teams/team2/repo2.yaml", []byte(`apiVersion: v1
kind: Repository
name: repo2
`), 0644)

	// create goliac.yaml
	utils.WriteFile(fs, "goliac.yaml", []byte(`admin_team: admin

rulesets:
  - default

max_changesets: 50
archive_on_delete: true

destructive_operations:
  repositories: false
  teams: false
  users: false
  rulesets: false

usersync:
  plugin: noop
`), 0644)
	// rulesets
	fs.MkdirAll("rulesets", 0755)
	utils.WriteFile(fs, "rulesets/default.yaml", []byte(`apiVersion: v1
kind: Ruleset
name: default
spec:
  repositories:
    included:
      - ~ALL
  ruleset:
    enforcement: active
    bypassapps:
      - appname: goliac-project-app
        mode: always
    conditions:
      include: 
        - "~DEFAULT_BRANCH"

    rules:
      - ruletype: pull_request
        parameters:
          requiredApprovingReviewCount: 1
`), 0644)

	// create .github/CODEOWNERS
	utils.WriteFile(fs, ".github/CODEOWNERS", []byte(`# DO NOT MODIFY THIS FILE MANUALLY
* @goliac-project/admin
/teams/team1/* @goliac-project/team1-goliac-owners @goliac-project/admin
/teams/team2/* @goliac-project/team2-goliac-owners @goliac-project/admin
`), 0644)
}

// create a working simple teams repository
// - missing user4 in the teams repo
// - using the `fromgithubsaml` user sync plugin
func repoFixture2(fs billy.Filesystem) {
	repoFixture1(fs)

	fs.Remove("users/org/user4.yaml")

	// without user4 owner
	utils.WriteFile(fs, "teams/team2/team.yaml", []byte(`apiVersion: v1
kind: Team
name: team2
spec:
  owners:
    - user3
`), 0644)

}

// create a working simple teams repository
func repoFixtureRename(fs billy.Filesystem) {
	repoFixture1(fs)

	utils.WriteFile(fs, "teams/team2/repo2.yaml", []byte(`apiVersion: v1
kind: Repository
name: repo2
renameTo: repo3
`), 0644)

}

func createTeamRepo(src billy.Filesystem, fixtureFunc FixtureFunc) (*git.Repository, error) {
	masterStorer := filesystem.NewStorage(src, cache.NewObjectLRUDefault())

	// Create a fake bare repository
	repo, err := git.Init(masterStorer, src)
	if err != nil {
		return nil, err
	}

	//
	// Create a new file in the working directory
	//
	fixtureFunc(src)

	// commit
	worktree, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	_, err = worktree.Add(".")
	if err != nil {
		return nil, err
	}
	hash, err := worktree.Commit("Initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Goliac",
			Email: "goliac@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		return nil, err
	}

	// tag as v0.1.0
	_, err = repo.CreateTag("v0.1.0", hash, nil)
	if err != nil {
		return nil, err
	}

	err = repo.Storer.SetReference(plumbing.NewHashReference(plumbing.HEAD, hash))
	if err != nil {
		return nil, err
	}

	return repo, nil
}

/*
 */
func helperCreateAndClone(root billy.Filesystem, src billy.Filesystem, target billy.Filesystem, fixtureFunc FixtureFunc) (*git.Repository, *git.Repository, error) {
	repo, err := createTeamRepo(src, fixtureFunc)
	if err != nil {
		return nil, nil, err
	}

	//
	// trying to clone it
	//

	loader := server.NewFilesystemLoader(root)
	client.InstallProtocol("inmemory", server.NewClient(loader))

	dotGit, err := target.Chroot(".git")
	if err != nil {
		return nil, nil, err
	}
	storer := filesystem.NewStorage(dotGit, cache.NewObjectLRUDefault())

	clonedRepo, err := git.Clone(storer, target, &git.CloneOptions{
		URL:      "inmemory:///src",
		Progress: nil,
	})
	if err != nil {
		return nil, nil, err
	}

	return repo, clonedRepo, nil
}

//
// GitHubClientMock
//

type GitHubClientMock struct {
}

func NewGitHubClientMock() *GitHubClientMock {
	return &GitHubClientMock{}
}

func extractQueryName(query string) string {
	queryRegex := regexp.MustCompile(`query\s+(\w+)\(.*`)
	matches := queryRegex.FindStringSubmatch(query)
	if len(matches) == 2 {
		return matches[1]
	}
	return ""
}

func (c *GitHubClientMock) QueryGraphQLAPI(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	// extract query name
	queryName := extractQueryName(query)

	if queryName == "listSamlUsers" {
		return []byte(`{
			"data": {
				"organization": {
					"samlIdentityProvider": {
						"externalIdentities": {
							"edges": [
								{
									"node": {
										"guid": "guid1",
										"samlIdentity": {
											"nameId": "user1"
										},
										"user": {
											"login": "github1"
										}
									}
								},
								{
									"node": {
										"guid": "guid2",
										"samlIdentity": {
											"nameId": "user2"
										},
										"user": {
											"login": "github2"
										}
									}
								},
								{
									"node": {
										"guid": "guid3",
										"samlIdentity": {
											"nameId": "user3"
										},
										"user": {
											"login": "github3"
										}
									}
								},
								{
									"node": {
										"guid": "guid4",
										"samlIdentity": {
											"nameId": "user4"
										},
										"user": {
											"login": "github4"
										}
									}
								}
							],
							"pageInfo": {
								"hasNextPage": false,
								"endCursor": null
							},
							"totalCount": 4
						}
					}
				}
			}
		}`), nil
	} else {
		assert.Fail(nil, "unexpected query: "+queryName)
	}
	return nil, nil
}

func (c *GitHubClientMock) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	if strings.HasSuffix(endpoint, "/invitations") {
		return []byte(`[]`), nil
	}
	if strings.HasSuffix(endpoint, "/app") {
		return []byte(`{"id": 1, "client_id": "githubAppClientID"}`), nil
	}
	return nil, nil
}
func (c *GitHubClientMock) GetAccessToken(context.Context) (string, error) {
	return "accesstoken", nil
}
func (c *GitHubClientMock) CreateJWT() (string, error) {
	return "", nil
}
func (c *GitHubClientMock) GetAppSlug() string {
	return "goliac-project-app"
}

//
// remote mock
//

type GoliacRemoteExecutorMock struct {
	teams1Members []string
	teams2Members []string
	nbChanges     int
}

// GoliacRemoteExecutorMock
func NewGoliacRemoteExecutorMock() engine.GoliacRemoteExecutor {
	return &GoliacRemoteExecutorMock{
		nbChanges:     0,
		teams1Members: []string{"github1", "github2"},
		teams2Members: []string{"github3", "github4"},
	}
}

func (e *GoliacRemoteExecutorMock) Load(ctx context.Context, continueOnError bool) error {
	return nil
}
func (e *GoliacRemoteExecutorMock) FlushCache() {
}
func (e *GoliacRemoteExecutorMock) FlushCacheUsersTeamsOnly() {
}
func (e *GoliacRemoteExecutorMock) Users(ctx context.Context) map[string]string {
	return map[string]string{
		"github1": "member",
		"github2": "member",
		"github3": "member",
		"github4": "member",
	}
}
func (e *GoliacRemoteExecutorMock) TeamSlugByName(ctx context.Context) map[string]string {
	return map[string]string{
		"team1":               "team1",
		"team2":               "team2",
		"team1-goliac-owners": "team1-goliac-owners",
		"team2-goliac-owners": "team2-goliac-owners",
	}
}

func (e *GoliacRemoteExecutorMock) Teams(ctx context.Context, current bool) map[string]*engine.GithubTeam {
	return map[string]*engine.GithubTeam{
		"team1": {
			Slug:        "team1",
			Name:        "team1",
			Members:     e.teams1Members,
			Maintainers: []string{},
		},
		"team2": {
			Slug:        "team2",
			Name:        "team2",
			Members:     e.teams2Members,
			Maintainers: []string{},
		},
		"team1-goliac-owners": {
			Slug:        "team1-goliac-owners",
			Name:        "team1-goliac-owners",
			Members:     e.teams1Members,
			Maintainers: []string{},
		},
		"team2-goliac-owners": {
			Slug:        "team2-goliac-owners",
			Name:        "team2-goliac-owners",
			Members:     e.teams2Members,
			Maintainers: []string{},
		},
	}
}
func (e *GoliacRemoteExecutorMock) Repositories(ctx context.Context) map[string]*engine.GithubRepository {
	return map[string]*engine.GithubRepository{
		"src": {
			Name:       "src", // this is the "teams" repository
			Id:         0,
			RefId:      "MDEwOlJlcG9zaXRvcnkaMTMxNjExOQ==",
			Visibility: "private",
			BoolProperties: map[string]bool{
				"archived":               false,
				"allow_auto_merge":       false,
				"delete_branch_on_merge": true,
				"allow_update_branch":    false,
			},
			ExternalUsers: map[string]string{},
			BranchProtections: map[string]*engine.GithubBranchProtection{
				"master": {
					Pattern:                      "master",
					RequiresApprovingReviews:     true,
					RequiredApprovingReviewCount: 1,
					RequireLastPushApproval:      true,
					RequiresStatusChecks:         true,
					RequiresStrictStatusChecks:   true,
					RequiredStatusCheckContexts:  []string{"validate"},
				},
			},
		},
		"repo1": {
			Name:       "repo1",
			Id:         1,
			RefId:      "MDEwOlJlcG9zaXRvcnkaMTMxNjExOQ==",
			Visibility: "private",
			BoolProperties: map[string]bool{
				"archived":               false,
				"allow_auto_merge":       false,
				"delete_branch_on_merge": false,
				"allow_update_branch":    false,
			},
			ExternalUsers:     map[string]string{},
			DefaultBranchName: "main",
		},
		"repo2": {
			Name:       "repo2",
			Id:         2,
			RefId:      "MDEwOlJlcG9zaXRvcnkaNTcwNDA4Ng==",
			Visibility: "private",
			BoolProperties: map[string]bool{
				"archived":               false,
				"allow_auto_merge":       false,
				"delete_branch_on_merge": false,
				"allow_update_branch":    false,
			},
			ExternalUsers:     map[string]string{},
			DefaultBranchName: "main",
		},
	}
}
func (e *GoliacRemoteExecutorMock) TeamRepositories(ctx context.Context) map[string]map[string]*engine.GithubTeamRepo {
	return map[string]map[string]*engine.GithubTeamRepo{
		"team1": {
			"repo1": &engine.GithubTeamRepo{
				Name:       "repo1",
				Permission: "WRITE",
			},
		},
		"team2": {
			"repo2": &engine.GithubTeamRepo{
				Name:       "repo2",
				Permission: "WRITE",
			},
		},
		"team1-goliac-owners": {
			"src": &engine.GithubTeamRepo{ // the teams repository
				Name:       "src",
				Permission: "WRITE",
			},
		},
		"team2-goliac-owners": {
			"src": &engine.GithubTeamRepo{ // the teams repository
				Name:       "src",
				Permission: "WRITE",
			},
		},
		"admin": {
			"src": &engine.GithubTeamRepo{ // the teams repository
				Name:       "src",
				Permission: "WRITE",
			},
		},
	}
}
func (e *GoliacRemoteExecutorMock) RuleSets(ctx context.Context) map[string]*engine.GithubRuleSet {
	return map[string]*engine.GithubRuleSet{
		"default": {
			Name:        "default",
			Id:          0,
			Enforcement: "active",
			BypassApps: map[string]string{
				"goliac-project-app": "always",
			},
			OnInclude: []string{"~DEFAULT_BRANCH"},
			Rules: map[string]entity.RuleSetParameters{
				"pull_request": {
					RequiredApprovingReviewCount: 1,
				},
			},
			Repositories: []string{"repo1", "repo2", "src"},
		},
	}
}
func (e *GoliacRemoteExecutorMock) AppIds(ctx context.Context) map[string]int {
	return map[string]int{
		"goliac-project-app": 1,
	}
}
func (e *GoliacRemoteExecutorMock) IsEnterprise() bool {
	return true
}
func (m *GoliacRemoteExecutorMock) CountAssets(ctx context.Context, warmup bool) (int, error) {
	return 4, nil
}
func (g *GoliacRemoteExecutorMock) SetRemoteObservability(feedback observability.RemoteObservability) {
}

func (e *GoliacRemoteExecutorMock) AddUserToOrg(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ghuserid string) {
	fmt.Println("*** AddUserToOrg", ghuserid)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) RemoveUserFromOrg(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ghuserid string) {
	fmt.Println("*** RemoveUserFromOrg", ghuserid)
	e.nbChanges++
}

func (e *GoliacRemoteExecutorMock) CreateTeam(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamname string, description string, parentTeam *int, members []string) {
	fmt.Println("*** CreateTeam", teamname, description, parentTeam, members)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateTeamAddMember(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string, username string, role string) {
	fmt.Println("*** UpdateTeamAddMember", teamslug, username, role)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateTeamUpdateMember(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string, username string, role string) {
	fmt.Println("*** UpdateTeamUpdateMember", teamslug, username, role)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateTeamRemoveMember(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string, username string) {
	fmt.Println("*** UpdateTeamRemoveMember", teamslug, username)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateTeamSetParent(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string, parentTeam *int) {
	fmt.Println("*** UpdateTeamSetParent", teamslug, parentTeam)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) DeleteTeam(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string) {
	fmt.Println("*** DeleteTeam", teamslug)
	e.nbChanges++
}

func (e *GoliacRemoteExecutorMock) CreateRepository(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, descrition string, visibility string, writers []string, readers []string, boolProperties map[string]bool, defaultBranch string, githubToken *string, forkFrom string) {
	fmt.Println("*** CreateRepository", reponame, descrition, visibility, writers, readers, boolProperties, defaultBranch)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateRepositoryUpdateProperty(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, propertyName string, propertyValue interface{}) {
	fmt.Println("*** UpdateRepositoryUpdateProperty", reponame, propertyName, propertyValue)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateRepositoryAddTeamAccess(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, teamslug string, permission string) {
	fmt.Println("*** UpdateRepositoryAddTeamAccess", reponame, teamslug, permission)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateRepositoryUpdateTeamAccess(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, teamslug string, permission string) {
	fmt.Println("*** UpdateRepositoryUpdateTeamAccess", reponame, teamslug, permission)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateRepositoryRemoveTeamAccess(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, teamslug string) {
	fmt.Println("*** UpdateRepositoryRemoveTeamAccess", reponame, teamslug)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) AddRepositoryRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, ruleset *engine.GithubRuleSet) {
	fmt.Println("*** AddRepositoryRuleset", reponame, ruleset.Name)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateRepositoryRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, ruleset *engine.GithubRuleSet) {
	fmt.Println("*** UpdateRepositoryRuleset", reponame, ruleset.Name)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) DeleteRepositoryRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, rulesetid int) {
	fmt.Println("*** DeleteRepositoryRuleset", reponame, rulesetid)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) AddRepositoryBranchProtection(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, branchprotection *engine.GithubBranchProtection) {
	fmt.Println("*** AddRepositoryBranchProtection", reponame, branchprotection.Pattern)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateRepositoryBranchProtection(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, branchprotection *engine.GithubBranchProtection) {
	fmt.Println("*** UpdateRepositoryBranchProtection", reponame, branchprotection.Pattern)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) DeleteRepositoryBranchProtection(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, branchprotection *engine.GithubBranchProtection) {
	fmt.Println("*** DeleteRepositoryBranchProtection", reponame, branchprotection.Pattern)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) AddRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ruleset *engine.GithubRuleSet) {
	fmt.Println("*** AddRuleset", ruleset.Name)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ruleset *engine.GithubRuleSet) {
	fmt.Println("*** UpdateRuleset", ruleset.Name)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) DeleteRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, rulesetid int) {
	fmt.Println("*** DeleteRuleset", rulesetid)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateRepositorySetExternalUser(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, githubid string, permission string) {
	fmt.Println("*** UpdateRepositorySetExternalUser", reponame, githubid, permission)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateRepositoryRemoveExternalUser(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, githubid string) {
	fmt.Println("*** UpdateRepositoryRemoveExternalUser", reponame, githubid)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateRepositoryRemoveInternalUser(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, githubid string) {
	fmt.Println("*** UpdateRepositoryRemoveInternalUser", reponame, githubid)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) DeleteRepository(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string) {
	fmt.Println("*** DeleteRepository", reponame)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) RenameRepository(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, newname string) {
	fmt.Println("*** RenameRepository", reponame, newname)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) EnvironmentSecretsPerRepository(ctx context.Context, environments []string, repositoryName string) (map[string]map[string]*engine.GithubVariable, error) {
	return nil, nil
}
func (e *GoliacRemoteExecutorMock) RepositoriesSecretsPerRepository(ctx context.Context, repositoryName string) (map[string]*engine.GithubVariable, error) {
	return nil, nil
}
func (e *GoliacRemoteExecutorMock) AddRepositoryEnvironment(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, environmentName string) {
	fmt.Println("*** AddRepositoryEnvironment", repositoryName, environmentName)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) DeleteRepositoryEnvironment(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, environmentName string) {
	fmt.Println("*** DeleteRepositoryEnvironment", repositoryName, environmentName)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) AddRepositoryVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, variableName string, variableValue string) {
	fmt.Println("*** AddRepositoryVariable", repositoryName, variableName, variableValue)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateRepositoryVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, variableName string, variableValue string) {
	fmt.Println("*** UpdateRepositoryVariable", repositoryName, variableName, variableValue)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) DeleteRepositoryVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, variableName string) {
	fmt.Println("*** DeleteRepositoryVariable", repositoryName, variableName)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) AddRepositoryEnvironmentVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, environmentName string, variableName string, variableValue string) {
	fmt.Println("*** AddRepositoryEnvironmentVariable", repositoryName, environmentName, variableName, variableValue)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateRepositoryEnvironmentVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, environmentName string, variableName string, variableValue string) {
	fmt.Println("*** UpdateRepositoryEnvironmentVariable", repositoryName, environmentName, variableName, variableValue)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) DeleteRepositoryEnvironmentVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, environmentName string, variableName string) {
	fmt.Println("*** RemoveRepositoryEnvironmentVariable", repositoryName, environmentName, variableName)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) AddRepositoryAutolink(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, autolink *engine.GithubAutolink) {
	fmt.Println("*** AddRepositoryAutolink", repositoryName, autolink.KeyPrefix, autolink.UrlTemplate, autolink.IsAlphanumeric)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) DeleteRepositoryAutolink(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, autolinkId int) {
	fmt.Println("*** DeleteRepositoryAutolink", repositoryName, autolinkId)
	e.nbChanges++
}
func (e *GoliacRemoteExecutorMock) UpdateRepositoryAutolink(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, autolink *engine.GithubAutolink) {
	fmt.Println("*** UpdateRepositoryAutolink", repositoryName, autolink.KeyPrefix, autolink.UrlTemplate, autolink.IsAlphanumeric)
	e.nbChanges++
}

func (e *GoliacRemoteExecutorMock) Begin(dryrun bool) {
}
func (e *GoliacRemoteExecutorMock) Rollback(dryrun bool, err error) {
}
func (e *GoliacRemoteExecutorMock) Commit(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool) error {
	return nil
}

//
// tests
//

func TestGoliacApply(t *testing.T) {

	t.Run("happy path: in-sync teams repo", func(t *testing.T) {

		fs := memfs.New()
		fs.MkdirAll("src", 0755)        // create a fake bare repository
		fs.MkdirAll("teams", 0755)      // create a fake cloned repository
		fs.MkdirAll(os.TempDir(), 0755) // need a tmp folder
		srcsFs, _ := fs.Chroot("src")
		clonedFs, _ := fs.Chroot("teams")
		_, _, err := helperCreateAndClone(fs, srcsFs, clonedFs, repoFixture1)
		assert.Nil(t, err)

		local := engine.NewGoliacLocalImpl()

		errorCollector := observability.NewErrorCollection()
		local.LoadAndValidateLocal(clonedFs, errorCollector)
		assert.Equal(t, false, errorCollector.HasErrors())
		assert.Equal(t, false, errorCollector.HasWarns())

		githubClient := NewGitHubClientMock()
		remote := NewGoliacRemoteExecutorMock().(*GoliacRemoteExecutorMock)

		usersync.InitPlugins(githubClient)

		goliac := GoliacImpl{
			local:              local,
			remote:             remote,
			remoteGithubClient: githubClient,
			localGithubClient:  githubClient,
			repoconfig:         &config.RepositoryConfig{},
		}

		unmanaged := goliac.Apply(context.Background(), errorCollector, fs, false, "inmemory:///src", "master")
		assert.Equal(t, false, errorCollector.HasErrors())
		assert.Equal(t, false, errorCollector.HasWarns())
		assert.NotNil(t, unmanaged)
		assert.Equal(t, 0, remote.nbChanges)
	})

	t.Run("happy path: rename a repo", func(t *testing.T) {

		fs := memfs.New()
		fs.MkdirAll("src", 0755)        // create a fake bare repository
		fs.MkdirAll("teams", 0755)      // create a fake cloned repository
		fs.MkdirAll(os.TempDir(), 0755) // need a tmp folder
		srcsFs, _ := fs.Chroot("src")
		clonedFs, _ := fs.Chroot("teams")
		_, _, err := helperCreateAndClone(fs, srcsFs, clonedFs, repoFixtureRename)
		assert.Nil(t, err)

		local := engine.NewGoliacLocalImpl()

		errorCollector := observability.NewErrorCollection()
		local.LoadAndValidateLocal(clonedFs, errorCollector)
		assert.Equal(t, false, errorCollector.HasErrors())
		assert.Equal(t, false, errorCollector.HasWarns())

		githubClient := NewGitHubClientMock()
		remote := NewGoliacRemoteExecutorMock().(*GoliacRemoteExecutorMock)

		usersync.InitPlugins(githubClient)

		goliac := GoliacImpl{
			local:              local,
			remote:             remote,
			remoteGithubClient: githubClient,
			localGithubClient:  githubClient,
			repoconfig:         &config.RepositoryConfig{},
		}

		unmanaged := goliac.Apply(context.Background(), errorCollector, fs, false, "inmemory:///src", "master")
		assert.Equal(t, false, errorCollector.HasErrors())
		assert.Equal(t, false, errorCollector.HasWarns())
		assert.NotNil(t, unmanaged)
		assert.Equal(t, 1, remote.nbChanges) // 1 team renamed
	})

	t.Run("happy path: user4 to sync", func(t *testing.T) {

		fs := memfs.New()
		fs.MkdirAll("src", 0755)        // create a fake bare repository
		fs.MkdirAll("teams", 0755)      // create a fake cloned repository
		fs.MkdirAll(os.TempDir(), 0755) // need a tmp folder
		srcsFs, _ := fs.Chroot("src")
		clonedFs, _ := fs.Chroot("teams")
		_, _, err := helperCreateAndClone(fs, srcsFs, clonedFs, repoFixture2)
		assert.Nil(t, err)

		local := engine.NewGoliacLocalImpl()

		errorCollector := observability.NewErrorCollection()
		local.LoadAndValidateLocal(clonedFs, errorCollector)
		assert.Equal(t, false, errorCollector.HasErrors())
		assert.Equal(t, true, errorCollector.HasWarns())
		assert.Equal(t, "not enough owners for team filename teams/team2/team.yaml", errorCollector.Warns[0].Error())

		githubClient := NewGitHubClientMock()
		remote := NewGoliacRemoteExecutorMock().(*GoliacRemoteExecutorMock)

		usersync.InitPlugins(githubClient)

		goliac := GoliacImpl{
			local:              local,
			remote:             remote,
			remoteGithubClient: githubClient,
			localGithubClient:  githubClient,
			repoconfig:         &config.RepositoryConfig{},
		}

		unmanaged := goliac.Apply(context.Background(), errorCollector, fs, false, "inmemory:///src", "master")
		assert.Equal(t, false, errorCollector.HasErrors())
		assert.Equal(t, 1, len(errorCollector.Warns))
		assert.NotNil(t, unmanaged)
		assert.Equal(t, 2, remote.nbChanges)

	})
}
