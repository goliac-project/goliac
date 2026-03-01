package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/goliac-project/goliac/internal/config"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/google/go-github/v55/github"
	"github.com/gosimple/slug"
	"github.com/stretchr/testify/assert"
)

type GoliacLocalMock struct {
	users     map[string]*entity.User
	externals map[string]*entity.User
	teams     map[string]*entity.Team
	repos     map[string]*entity.Repository
	rulesets  map[string]*entity.RuleSet
	workflows map[string]*entity.Workflow
}

func (m *GoliacLocalMock) Clone(fs billy.Filesystem, accesstoken, repositoryUrl, branch string) error {
	return nil
}
func (m *GoliacLocalMock) ListCommitsFromTag(tagname string) ([]*object.Commit, error) {
	return nil, fmt.Errorf("not tag %s found", tagname)
}
func (m *GoliacLocalMock) GetHeadCommit() (*object.Commit, error) {
	return nil, nil
}
func (m *GoliacLocalMock) CheckoutCommit(commit *object.Commit) error {
	return nil
}
func (m *GoliacLocalMock) PushTag(tagname string, hash plumbing.Hash, accesstoken string) error {
	return nil
}
func (m *GoliacLocalMock) RepoConfig() *config.RepositoryConfig {
	return &config.RepositoryConfig{}
}
func (m *GoliacLocalMock) LoadAndValidate(logsCollector *observability.LogCollection) {
}
func (m *GoliacLocalMock) LoadAndValidateLocal(fs billy.Filesystem, logsCollector *observability.LogCollection) {
}
func (m *GoliacLocalMock) Teams() map[string]*entity.Team {
	return m.teams
}
func (m *GoliacLocalMock) Repositories() map[string]*entity.Repository {
	return m.repos
}
func (m *GoliacLocalMock) Users() map[string]*entity.User {
	return m.users
}
func (m *GoliacLocalMock) ExternalUsers() map[string]*entity.User {
	return m.externals
}
func (m *GoliacLocalMock) RuleSets() map[string]*entity.RuleSet {
	return m.rulesets
}
func (m *GoliacLocalMock) Workflows() map[string]*entity.Workflow {
	return m.workflows
}
func (m *GoliacLocalMock) UpdateAndCommitCodeOwners(ctx context.Context, repoconfig *config.RepositoryConfig, dryrun bool, accesstoken string, branch string, tagname string, githubOrganization string) error {
	return nil
}
func (m *GoliacLocalMock) UpdateRepos(reposToArchiveList []string, reposToRename map[string]*entity.Repository, accesstoken string, branch string, tagname string) error {
	return nil
}
func (m *GoliacLocalMock) UpdateReposViaPullRequest(ctx context.Context, client LocalGithubClient, reposToCreate map[string]*entity.Repository, orgname, reponame, accesstoken, baseBranch, newBranchName string) (*github.PullRequest, error) {
	return nil, nil
}

func (m *GoliacLocalMock) SyncUsersAndTeams(ctx context.Context, repoconfig *config.RepositoryConfig, plugin UserSyncPlugin, accesstoken string, dryrun bool, force bool, feedback observability.RemoteObservability, logsCollector *observability.LogCollection) bool {
	return false
}
func (m *GoliacLocalMock) Close(fs billy.Filesystem) {

}

type GoliacRemoteMock struct {
	users               map[string]*GithubUser
	teams               map[string]*GithubTeam // key is the slug team
	repos               map[string]*GithubRepository
	teamsrepos          map[string]map[string]*GithubTeamRepo // key is the slug team
	rulesets            map[string]*GithubRuleSet
	appids              map[string]*GithubApp
	orgCustomProperties map[string]*config.GithubCustomProperty
}

func (m *GoliacRemoteMock) Load(ctx context.Context, continueOnError bool) error {
	return nil
}
func (m *GoliacRemoteMock) IsEnterprise() bool {
	return true
}
func (m *GoliacRemoteMock) FlushCache() {
}
func (m *GoliacRemoteMock) FlushCacheUsersTeamsOnly() {
}
func (m *GoliacRemoteMock) RuleSets(ctx context.Context) map[string]*GithubRuleSet {
	return m.rulesets
}
func (m *GoliacRemoteMock) Users(ctx context.Context) map[string]*GithubUser {
	return m.users
}

func (m *GoliacRemoteMock) TeamSlugByName(ctx context.Context) map[string]string {
	slugs := make(map[string]string)
	for _, v := range m.teams {
		slugs[v.Name] = slug.Make(v.Name)
	}
	return slugs
}
func (m *GoliacRemoteMock) Teams(ctx context.Context, current bool) map[string]*GithubTeam {
	return m.teams
}
func (m *GoliacRemoteMock) Repositories(ctx context.Context) map[string]*GithubRepository {
	return m.repos
}
func (m *GoliacRemoteMock) RepositoriesByRefId(ctx context.Context) map[string]*GithubRepository {
	return make(map[string]*GithubRepository)
}
func (m *GoliacRemoteMock) TeamRepositories(ctx context.Context) map[string]map[string]*GithubTeamRepo {
	return m.teamsrepos
}
func (m *GoliacRemoteMock) AppIds(ctx context.Context) map[string]*GithubApp {
	return m.appids
}
func (m *GoliacRemoteMock) CountAssets(ctx context.Context, warmup bool) (int, error) {
	return 3, nil
}
func (g *GoliacRemoteMock) SetRemoteObservability(feedback observability.RemoteObservability) {
}
func (m *GoliacRemoteMock) RepositoriesSecretsPerRepository(ctx context.Context, repositoryName string) (map[string]*GithubVariable, error) {
	return nil, nil
}
func (m *GoliacRemoteMock) EnvironmentSecretsPerRepository(ctx context.Context, environments []string, repositoryName string) (map[string]map[string]*GithubVariable, error) {
	return nil, nil
}
func (m *GoliacRemoteMock) OrgCustomProperties(ctx context.Context) map[string]*config.GithubCustomProperty {
	if m.orgCustomProperties == nil {
		return make(map[string]*config.GithubCustomProperty)
	}
	return m.orgCustomProperties
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

type ReconciliatorListenerRecorder struct {
	UsersCreated map[string]string
	UsersRemoved map[string]string

	TeamsCreated      map[string][]string
	TeamMemberAdded   map[string][]string
	TeamMemberRemoved map[string][]string
	TeamMemberUpdated map[string][]string
	TeamParentUpdated map[string]*int
	TeamDeleted       map[string]bool

	RepositoryCreated                    map[string]bool
	RepositoryTeamAdded                  map[string][]string
	RepositoryTeamUpdated                map[string][]string
	RepositoryTeamRemoved                map[string][]string
	RepositoriesDeleted                  map[string]bool
	RepositoriesRenamed                  map[string]bool
	RepositoriesUpdateProperty           map[string]bool
	RepositoriesUpdateArchived           map[string]bool
	RepositoriesSetExternalUser          map[string]string
	RepositoriesRemoveExternalUser       map[string]bool
	RepositoriesRemoveInternalUser       map[string]bool
	RepositoryRuleSetCreated             map[string]map[string]*GithubRuleSet
	RepositoryRuleSetUpdated             map[string]map[string]*GithubRuleSet
	RepositoryRuleSetDeleted             map[string][]int
	RepositoryBranchProtectionCreated    map[string]map[string]*GithubBranchProtection
	RepositoryBranchProtectionUpdated    map[string]map[string]*GithubBranchProtection
	RepositoryBranchProtectionDeleted    map[string]map[string]*GithubBranchProtection
	RepositoryEnvironmentCreated         map[string]string
	RepositoryEnvironmentDeleted         map[string]string
	RepositoryVariableCreated            map[string]string
	RepositoryVariableUpdated            map[string]string
	RepositoryVariableDeleted            map[string]string
	RepositoryEnvironmentVariableCreated map[string]string
	RepositoryEnvironmentVariableUpdated map[string]string
	RepositoryEnvironmentVariableDeleted map[string]string
	RepositoryAutolinkCreated            map[string]map[string]*GithubAutolink
	RepositoryAutolinkUpdated            map[string]map[string]*GithubAutolink
	RepositoryAutolinkDeleted            map[string][]int

	RuleSetCreated map[string]*GithubRuleSet
	RuleSetUpdated map[string]*GithubRuleSet
	RuleSetDeleted []int

	OrgCustomPropertyCreated map[string]*config.GithubCustomProperty
	OrgCustomPropertyUpdated map[string]*config.GithubCustomProperty
	OrgCustomPropertyDeleted map[string]bool
}

func NewReconciliatorListenerRecorder() *ReconciliatorListenerRecorder {
	r := ReconciliatorListenerRecorder{
		UsersCreated:                         make(map[string]string),
		UsersRemoved:                         make(map[string]string),
		TeamsCreated:                         make(map[string][]string),
		TeamMemberAdded:                      make(map[string][]string),
		TeamMemberRemoved:                    make(map[string][]string),
		TeamMemberUpdated:                    make(map[string][]string),
		TeamParentUpdated:                    make(map[string]*int),
		TeamDeleted:                          make(map[string]bool),
		RepositoryCreated:                    make(map[string]bool),
		RepositoryTeamAdded:                  make(map[string][]string),
		RepositoryTeamUpdated:                make(map[string][]string),
		RepositoryTeamRemoved:                make(map[string][]string),
		RepositoriesDeleted:                  make(map[string]bool),
		RepositoriesRenamed:                  make(map[string]bool),
		RepositoriesUpdateProperty:           make(map[string]bool),
		RepositoriesUpdateArchived:           make(map[string]bool),
		RepositoriesSetExternalUser:          make(map[string]string),
		RepositoriesRemoveExternalUser:       make(map[string]bool),
		RepositoriesRemoveInternalUser:       make(map[string]bool),
		RepositoryRuleSetCreated:             make(map[string]map[string]*GithubRuleSet),
		RepositoryRuleSetUpdated:             make(map[string]map[string]*GithubRuleSet),
		RepositoryRuleSetDeleted:             make(map[string][]int, 0),
		RepositoryBranchProtectionCreated:    make(map[string]map[string]*GithubBranchProtection),
		RepositoryBranchProtectionUpdated:    make(map[string]map[string]*GithubBranchProtection),
		RepositoryBranchProtectionDeleted:    make(map[string]map[string]*GithubBranchProtection),
		RuleSetCreated:                       make(map[string]*GithubRuleSet),
		RuleSetUpdated:                       make(map[string]*GithubRuleSet),
		RuleSetDeleted:                       make([]int, 0),
		OrgCustomPropertyCreated:             make(map[string]*config.GithubCustomProperty),
		OrgCustomPropertyUpdated:             make(map[string]*config.GithubCustomProperty),
		OrgCustomPropertyDeleted:             make(map[string]bool),
		RepositoryEnvironmentCreated:         make(map[string]string),
		RepositoryEnvironmentDeleted:         make(map[string]string),
		RepositoryVariableCreated:            make(map[string]string),
		RepositoryVariableUpdated:            make(map[string]string),
		RepositoryVariableDeleted:            make(map[string]string),
		RepositoryEnvironmentVariableCreated: make(map[string]string),
		RepositoryEnvironmentVariableUpdated: make(map[string]string),
		RepositoryEnvironmentVariableDeleted: make(map[string]string),
		RepositoryAutolinkCreated:            make(map[string]map[string]*GithubAutolink),
		RepositoryAutolinkUpdated:            make(map[string]map[string]*GithubAutolink),
		RepositoryAutolinkDeleted:            make(map[string][]int),
	}
	return &r
}
func (r *ReconciliatorListenerRecorder) AddUserToOrg(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, ghuserid string) {
	r.UsersCreated[ghuserid] = ghuserid
}
func (r *ReconciliatorListenerRecorder) RemoveUserFromOrg(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, ghuserid string) {
	r.UsersRemoved[ghuserid] = ghuserid
}
func (r *ReconciliatorListenerRecorder) CreateTeam(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, teamname string, description string, parentTeam *int, members []string) {
	r.TeamsCreated[teamname] = append(r.TeamsCreated[teamname], members...)
}
func (r *ReconciliatorListenerRecorder) UpdateTeamAddMember(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, teamslug string, username string, role string) {
	r.TeamMemberAdded[teamslug] = append(r.TeamMemberAdded[teamslug], username)
}
func (r *ReconciliatorListenerRecorder) UpdateTeamRemoveMember(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, teamslug string, username string) {
	r.TeamMemberRemoved[teamslug] = append(r.TeamMemberRemoved[teamslug], username)
}
func (r *ReconciliatorListenerRecorder) UpdateTeamUpdateMember(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, teamslug string, username string, role string) {
	r.TeamMemberUpdated[teamslug] = append(r.TeamMemberUpdated[teamslug], username)
}
func (r *ReconciliatorListenerRecorder) UpdateTeamSetParent(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, teamslug string, parentTeam *int) {
	r.TeamParentUpdated[teamslug] = parentTeam
}
func (r *ReconciliatorListenerRecorder) DeleteTeam(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, teamslug string) {
	r.TeamDeleted[teamslug] = true
}
func (r *ReconciliatorListenerRecorder) CreateRepository(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, descrition string, visibility string, writers []string, readers []string, boolProperties map[string]bool, defaultBranch string, githubToken *string, forkFrom string) {
	r.RepositoryCreated[reponame] = true
}
func (r *ReconciliatorListenerRecorder) UpdateRepositoryAddTeamAccess(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, teamslug string, permission string) {
	r.RepositoryTeamAdded[reponame] = append(r.RepositoryTeamAdded[reponame], teamslug)
}
func (r *ReconciliatorListenerRecorder) UpdateRepositoryUpdateTeamAccess(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, teamslug string, permission string) {
	r.RepositoryTeamUpdated[reponame] = append(r.RepositoryTeamUpdated[reponame], teamslug)
}
func (r *ReconciliatorListenerRecorder) UpdateRepositoryRemoveTeamAccess(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, teamslug string) {
	r.RepositoryTeamRemoved[reponame] = append(r.RepositoryTeamRemoved[reponame], teamslug)
}
func (r *ReconciliatorListenerRecorder) DeleteRepository(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string) {
	r.RepositoriesDeleted[reponame] = true
}
func (r *ReconciliatorListenerRecorder) RenameRepository(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, newname string) {
	r.RepositoriesRenamed[reponame] = true
}
func (r *ReconciliatorListenerRecorder) UpdateRepositoryUpdateProperties(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, properties map[string]interface{}) {
	r.RepositoriesUpdateProperty[reponame] = true
}
func (r *ReconciliatorListenerRecorder) UpdateRepositoryCustomProperties(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, propertyName string, propertyValue interface{}) {
	// Track custom property updates if needed for testing
}
func (r *ReconciliatorListenerRecorder) UpdateRepositoryTopics(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, topics []string) {
	// Track topics updates if needed for testing
}
func (r *ReconciliatorListenerRecorder) UpdateRepositorySetExternalUser(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, githubid string, permission string) {
	r.RepositoriesSetExternalUser[githubid] = permission
}
func (r *ReconciliatorListenerRecorder) UpdateRepositoryRemoveExternalUser(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, githubid string) {
	r.RepositoriesRemoveExternalUser[githubid] = true
}
func (r *ReconciliatorListenerRecorder) UpdateRepositoryRemoveInternalUser(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, githubid string) {
	r.RepositoriesRemoveInternalUser[githubid] = true
}
func (r *ReconciliatorListenerRecorder) AddRepositoryRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, ruleset *GithubRuleSet) {
	repo := r.RepositoryRuleSetCreated[reponame]
	if repo == nil {
		repo = make(map[string]*GithubRuleSet)
		r.RepositoryRuleSetCreated[reponame] = repo
	}
	repo[ruleset.Name] = ruleset
}
func (r *ReconciliatorListenerRecorder) UpdateRepositoryRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, ruleset *GithubRuleSet) {
	repo := r.RepositoryRuleSetUpdated[reponame]
	if repo == nil {
		repo = make(map[string]*GithubRuleSet)
		r.RepositoryRuleSetUpdated[reponame] = repo
	}
	repo[ruleset.Name] = ruleset
}
func (r *ReconciliatorListenerRecorder) DeleteRepositoryRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, rulesetid int) {
	repo := r.RepositoryRuleSetDeleted[reponame]
	if repo == nil {
		repo = make([]int, 0)
	}
	repo = append(repo, rulesetid)
	r.RepositoryRuleSetDeleted[reponame] = repo
}
func (r *ReconciliatorListenerRecorder) AddRepositoryBranchProtection(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection) {
	repo := r.RepositoryBranchProtectionCreated[reponame]
	if repo == nil {
		repo = make(map[string]*GithubBranchProtection)
		r.RepositoryBranchProtectionCreated[reponame] = repo
	}
	repo[branchprotection.Pattern] = branchprotection
}
func (r *ReconciliatorListenerRecorder) UpdateRepositoryBranchProtection(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection) {
	repo := r.RepositoryBranchProtectionUpdated[reponame]
	if repo == nil {
		repo = make(map[string]*GithubBranchProtection)
		r.RepositoryBranchProtectionUpdated[reponame] = repo
	}
	repo[branchprotection.Pattern] = branchprotection
}
func (r *ReconciliatorListenerRecorder) DeleteRepositoryBranchProtection(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection) {
	repo := r.RepositoryBranchProtectionDeleted[reponame]
	if repo == nil {
		repo = make(map[string]*GithubBranchProtection)
		r.RepositoryBranchProtectionDeleted[reponame] = repo
	}
	repo[branchprotection.Pattern] = branchprotection
}
func (r *ReconciliatorListenerRecorder) AddRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, ruleset *GithubRuleSet) {
	r.RuleSetCreated[ruleset.Name] = ruleset
}
func (r *ReconciliatorListenerRecorder) UpdateRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, ruleset *GithubRuleSet) {
	r.RuleSetUpdated[ruleset.Name] = ruleset
}
func (r *ReconciliatorListenerRecorder) DeleteRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, rulesetid int) {
	r.RuleSetDeleted = append(r.RuleSetDeleted, rulesetid)
}
func (r *ReconciliatorListenerRecorder) AddRepositoryEnvironment(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, environmentName string) {
	r.RepositoryEnvironmentCreated[repositoryName] = environmentName
}
func (r *ReconciliatorListenerRecorder) DeleteRepositoryEnvironment(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, environmentName string) {
	r.RepositoryEnvironmentDeleted[repositoryName] = environmentName
}
func (r *ReconciliatorListenerRecorder) AddRepositoryVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, variableName string, variableValue string) {
	r.RepositoryVariableCreated[repositoryName] = variableName
}
func (r *ReconciliatorListenerRecorder) UpdateRepositoryVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, variableName string, variableValue string) {
	r.RepositoryVariableUpdated[repositoryName] = variableName
}
func (r *ReconciliatorListenerRecorder) DeleteRepositoryVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, variableName string) {
	r.RepositoryVariableDeleted[repositoryName] = variableName
}
func (r *ReconciliatorListenerRecorder) AddRepositoryEnvironmentVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, environmentName string, variableName string, variableValue string) {
	r.RepositoryEnvironmentVariableCreated[repositoryName] = environmentName
}
func (r *ReconciliatorListenerRecorder) UpdateRepositoryEnvironmentVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, environmentName string, variableName string, variableValue string) {
	r.RepositoryEnvironmentVariableUpdated[repositoryName] = environmentName
}
func (r *ReconciliatorListenerRecorder) DeleteRepositoryEnvironmentVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, environmentName string, variableName string) {
	r.RepositoryEnvironmentVariableDeleted[repositoryName] = environmentName
}
func (r *ReconciliatorListenerRecorder) AddRepositoryAutolink(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, autolink *GithubAutolink) {
	repo := r.RepositoryAutolinkCreated[repositoryName]
	if repo == nil {
		autolinks := make(map[string]*GithubAutolink)
		r.RepositoryAutolinkCreated[repositoryName] = autolinks
	}
	r.RepositoryAutolinkCreated[repositoryName][autolink.KeyPrefix] = autolink
}
func (r *ReconciliatorListenerRecorder) DeleteRepositoryAutolink(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, autolinkId int) {
	repo := r.RepositoryAutolinkDeleted[repositoryName]
	if repo == nil {
		autolinks := make([]int, 0)
		r.RepositoryAutolinkDeleted[repositoryName] = autolinks
	}
	r.RepositoryAutolinkDeleted[repositoryName] = append(r.RepositoryAutolinkDeleted[repositoryName], autolinkId)
}
func (r *ReconciliatorListenerRecorder) UpdateRepositoryAutolink(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, previousAutolinkId int, autolink *GithubAutolink) {
	repo := r.RepositoryAutolinkUpdated[repositoryName]
	if repo == nil {
		autolinks := make(map[string]*GithubAutolink)
		r.RepositoryAutolinkUpdated[repositoryName] = autolinks
	}
	r.RepositoryAutolinkUpdated[repositoryName][autolink.KeyPrefix] = autolink
}
func (r *ReconciliatorListenerRecorder) CreateOrUpdateOrgCustomProperty(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, property *config.GithubCustomProperty) {
	// Track in Created if not already there (first time we see this property)
	if _, exists := r.OrgCustomPropertyCreated[property.PropertyName]; !exists {
		r.OrgCustomPropertyCreated[property.PropertyName] = property
	} else {
		// If already in Created, this is an update
		r.OrgCustomPropertyUpdated[property.PropertyName] = property
	}
}
func (r *ReconciliatorListenerRecorder) DeleteOrgCustomProperty(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, propertyName string) {
	r.OrgCustomPropertyDeleted[propertyName] = true
}
func (r *ReconciliatorListenerRecorder) Begin(logsCollector *observability.LogCollection, dryrun bool) {
}
func (r *ReconciliatorListenerRecorder) Rollback(logsCollector *observability.LogCollection, dryrun bool, err error) {
}
func (r *ReconciliatorListenerRecorder) Commit(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool) error {
	return nil
}

func TestReconciliationTeam(t *testing.T) {
	t.Run("happy path: new team", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		newTeam := &entity.Team{}
		newTeam.Name = "new"
		newTeam.Spec.Owners = []string{"new.owner"}
		newTeam.Spec.Members = []string{"new.member"}
		local.teams["new"] = newTeam

		newOwner := entity.User{}
		newOwner.Name = "new.owner"
		newOwner.Spec.GithubID = "new_owner"
		local.users["new.owner"] = &newOwner
		newMember := entity.User{}
		newMember.Name = "new.member"
		newMember.Spec.GithubID = "new_member"
		local.users["new.member"] = &newMember

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		lTeams, _, _ := localDatasource.Teams()
		rTeams, _, _ := remoteDatasource.Teams()
		assert.Equal(t, 2, len(lTeams))
		assert.Equal(t, 0, len(rTeams))

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 2 members created
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 2, len(recorder.TeamsCreated["new"]))
		assert.Equal(t, 1, len(recorder.TeamsCreated["new"+config.Config.GoliacTeamOwnerSuffix]))
	})

	t.Run("happy path: new team with non english slug", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		newTeam := &entity.Team{}
		newTeam.Name = "nouveauté"
		newTeam.Spec.Owners = []string{"new.owner"}
		newTeam.Spec.Members = []string{"new.member"}
		local.teams["nouveauté"] = newTeam

		newOwner := entity.User{}
		newOwner.Name = "new.owner"
		newOwner.Spec.GithubID = "new_owner"
		local.users["new.owner"] = &newOwner
		newMember := entity.User{}
		newMember.Name = "new.member"
		newMember.Spec.GithubID = "new_member"
		local.users["new.member"] = &newMember

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 2 members created
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 2, len(recorder.TeamsCreated["nouveauté"]))
		assert.Equal(t, 1, len(recorder.TeamsCreated["nouveaute"+config.Config.GoliacTeamOwnerSuffix]))
	})

	t.Run("happy path: existing team with new members", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing.owner", "existing.owner2"}
		existingTeam.Spec.Members = []string{"existing.member"}
		local.teams["existing"] = existingTeam

		existing_owner := entity.User{}
		existing_owner.Name = "existing.owner"
		existing_owner.Spec.GithubID = "existing_owner"
		local.users["existing.owner"] = &existing_owner

		existing_owner2 := entity.User{}
		existing_owner2.Name = "existing.owner2"
		existing_owner2.Spec.GithubID = "existing_owner2"
		local.users["existing.owner2"] = &existing_owner2

		existing_member := entity.User{}
		existing_member.Name = "existing.member"
		existing_member.Spec.GithubID = "existing_member"
		local.users["existing.member"] = &existing_member

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		existing := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner", "existing_member"},
		}
		remote.teams["existing"] = existing
		existingowners := &GithubTeam{
			Name:    "existing" + config.Config.GoliacTeamOwnerSuffix,
			Slug:    "existing" + config.Config.GoliacTeamOwnerSuffix,
			Members: []string{"existing_owner", "existing_member"},
		}
		remote.teams["existing"+config.Config.GoliacTeamOwnerSuffix] = existingowners

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 members added
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.TeamsCreated))
		assert.Equal(t, 1, len(recorder.TeamMemberAdded["existing"]))
	})

	t.Run("happy path: existing team with non english slug with new members", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		existingTeam := &entity.Team{}
		existingTeam.Name = "exist ing"
		existingTeam.Spec.Owners = []string{"existing.owner", "existing.owner2"}
		existingTeam.Spec.Members = []string{"existing.member"}
		local.teams["exist ing"] = existingTeam

		existing_owner := entity.User{}
		existing_owner.Name = "existing.owner"
		existing_owner.Spec.GithubID = "existing_owner"
		local.users["existing.owner"] = &existing_owner

		existing_owner2 := entity.User{}
		existing_owner2.Name = "existing.owner2"
		existing_owner2.Spec.GithubID = "existing_owner2"
		local.users["existing.owner2"] = &existing_owner2

		existing_member := entity.User{}
		existing_member.Name = "existing.member"
		existing_member.Spec.GithubID = "existing_member"
		local.users["existing.member"] = &existing_member

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		existing := &GithubTeam{
			Name:    "exist ing",
			Slug:    "exist-ing",
			Members: []string{"existing_owner", "existing_member"},
		}
		remote.teams["exist-ing"] = existing

		existingowners := &GithubTeam{
			Name:    "exist ing" + config.Config.GoliacTeamOwnerSuffix,
			Slug:    "exist-ing" + config.Config.GoliacTeamOwnerSuffix,
			Members: []string{"existing_owner", "existing_member"},
		}
		remote.teams["exist-ing"+config.Config.GoliacTeamOwnerSuffix] = existingowners

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 members added
		ctx := context.TODO()
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, "exist-ing", remote.TeamSlugByName(ctx)["exist ing"])
		assert.Equal(t, 0, len(recorder.TeamsCreated))
		assert.Equal(t, 1, len(recorder.TeamMemberAdded["exist-ing"]))
	})

	t.Run("happy path: new team + adding everyone team", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{
			EveryoneTeamEnabled: true,
		}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		newTeam := &entity.Team{}
		newTeam.Name = "new"
		newTeam.Spec.Owners = []string{"new.owner"}
		newTeam.Spec.Members = []string{"new.member"}
		local.teams["new"] = newTeam

		newOwner := entity.User{}
		newOwner.Name = "new.owner"
		newOwner.Spec.GithubID = "new_owner"
		local.users["new.owner"] = &newOwner
		newMember := entity.User{}
		newMember.Name = "new.member"
		newMember.Spec.GithubID = "new_member"
		local.users["new.member"] = &newMember

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 2 members created
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 2, len(recorder.TeamsCreated["new"]))
		assert.Equal(t, 1, len(recorder.TeamsCreated["new"+config.Config.GoliacTeamOwnerSuffix]))
		// and the everyone team
		assert.Equal(t, 2, len(recorder.TeamsCreated["everyone"]))
	})

	t.Run("happy path: removed team without destructive operation", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		removing := &GithubTeam{
			Name:    "removing",
			Slug:    "removing",
			Members: []string{"existing_owner", "existing_owner"},
		}
		remote.teams["removing"] = removing

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 team deleted
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.TeamDeleted))
	})

	t.Run("happy path: status quo: no new parent to a team", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		lParentTeam := &entity.Team{}
		lParentTeam.Name = "parentTeam"
		lParentTeam.Spec.Owners = []string{"existing_owner"}
		lParentTeam.Spec.Members = []string{}
		local.teams["parentTeam"] = lParentTeam

		lChildTeam := &entity.Team{}
		lChildTeam.Name = "childTeam"
		lChildTeam.Spec.Owners = []string{"existing_owner"}
		lChildTeam.Spec.Members = []string{}
		local.teams["childTeam"] = lChildTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		parentTeam := &GithubTeam{
			Name:    "parentTeam",
			Slug:    "parentteam",
			Members: []string{"existing_owner"},
			Id:      1,
		}

		parentTeamOwners := &GithubTeam{
			Name:    "parentteam" + config.Config.GoliacTeamOwnerSuffix,
			Slug:    "parentteam" + config.Config.GoliacTeamOwnerSuffix,
			Members: []string{"existing_owner"},
			Id:      1,
		}

		childTeam := &GithubTeam{
			Name:    "childTeam",
			Slug:    "childteam",
			Members: []string{"existing_owner"},
			Id:      2,
		}

		childTeamOwners := &GithubTeam{
			Name:    "childTeam" + config.Config.GoliacTeamOwnerSuffix,
			Slug:    "childteam" + config.Config.GoliacTeamOwnerSuffix,
			Members: []string{"existing_owner"},
			Id:      2,
		}

		remote.teams["parentteam"] = parentTeam
		remote.teams["parentteam"+config.Config.GoliacTeamOwnerSuffix] = parentTeamOwners
		remote.teams["childteam"] = childTeam
		remote.teams["childteam"+config.Config.GoliacTeamOwnerSuffix] = childTeamOwners

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 0 parent updated
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.TeamParentUpdated))
	})

	t.Run("happy path: add parent to a team", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		lParentTeam := &entity.Team{}
		lParentTeam.Name = "parentTeam"
		lParentTeam.Spec.Owners = []string{"existing_owner"}
		lParentTeam.Spec.Members = []string{}
		local.teams["parentTeam"] = lParentTeam

		lChildTeam := &entity.Team{}
		lChildTeam.Name = "childTeam"
		lChildTeam.Spec.Owners = []string{"existing_owner"}
		lChildTeam.Spec.Members = []string{}
		// let's put the child under the parent
		parent := "parentTeam"
		lChildTeam.ParentTeam = &parent

		local.teams["childTeam"] = lChildTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		parentTeam := &GithubTeam{
			Name:    "parentTeam",
			Slug:    "parentteam",
			Members: []string{"existing_owner"},
			Id:      1,
		}

		parentTeamOwners := &GithubTeam{
			Name:    "parentteam" + config.Config.GoliacTeamOwnerSuffix,
			Slug:    "parentteam" + config.Config.GoliacTeamOwnerSuffix,
			Members: []string{"existing_owner"},
			Id:      1,
		}

		childTeam := &GithubTeam{
			Name:    "childTeam",
			Slug:    "childteam",
			Members: []string{"existing_owner"},
			Id:      2,
		}

		childTeamOwners := &GithubTeam{
			Name:    "childTeam" + config.Config.GoliacTeamOwnerSuffix,
			Slug:    "childteam" + config.Config.GoliacTeamOwnerSuffix,
			Members: []string{"existing_owner"},
			Id:      2,
		}

		remote.teams["parentteam"] = parentTeam
		remote.teams["parentteam"+config.Config.GoliacTeamOwnerSuffix] = parentTeamOwners
		remote.teams["childteam"] = childTeam
		remote.teams["childteam"+config.Config.GoliacTeamOwnerSuffix] = childTeamOwners

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 team parent updated
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 1, len(recorder.TeamParentUpdated))
	})

	t.Run("happy path: change parent from a team", func(t *testing.T) {
		// here we have remote a parent -> child -> grandchild
		// and locally a parent -> [child, grandchild]
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		lParentTeam := &entity.Team{}
		lParentTeam.Name = "parentTeam"
		lParentTeam.Spec.Owners = []string{"existing_owner"}
		lParentTeam.Spec.Members = []string{}
		local.teams["parentTeam"] = lParentTeam

		lChildTeam := &entity.Team{}
		lChildTeam.Name = "childTeam"
		lChildTeam.Spec.Owners = []string{"existing_owner"}
		lChildTeam.Spec.Members = []string{}
		// let's put the child under the parent
		parenta := "parentTeam"
		lChildTeam.ParentTeam = &parenta

		local.teams["childTeam"] = lChildTeam

		lGrandChildTeam := &entity.Team{}
		lGrandChildTeam.Name = "grandChildTeam"
		lGrandChildTeam.Spec.Owners = []string{"existing_owner"}
		lGrandChildTeam.Spec.Members = []string{}
		// let's put the grand child under the parent
		parentb := "parentTeam"
		lGrandChildTeam.ParentTeam = &parentb

		local.teams["grandChildTeam"] = lGrandChildTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		parentTeam := &GithubTeam{
			Name:    "parentTeam",
			Slug:    "parentteam",
			Members: []string{"existing_owner"},
			Id:      1,
		}

		parentTeamOwners := &GithubTeam{
			Name:    "parentteam" + config.Config.GoliacTeamOwnerSuffix,
			Slug:    "parentteam" + config.Config.GoliacTeamOwnerSuffix,
			Members: []string{"existing_owner"},
			Id:      2,
		}

		rParentA := 1
		childTeam := &GithubTeam{
			Name:       "childTeam",
			Slug:       "childteam",
			Members:    []string{"existing_owner"},
			Id:         3,
			ParentTeam: &rParentA,
		}

		childTeamOwners := &GithubTeam{
			Name:    "childTeam" + config.Config.GoliacTeamOwnerSuffix,
			Slug:    "childteam" + config.Config.GoliacTeamOwnerSuffix,
			Members: []string{"existing_owner"},
			Id:      4,
		}

		rParentB := 3
		grandChildTeam := &GithubTeam{
			Name:       "grandChildTeam",
			Slug:       "grandchildteam",
			Members:    []string{"existing_owner"},
			Id:         5,
			ParentTeam: &rParentB,
		}

		grandChildTeamOwners := &GithubTeam{
			Name:    "grandChildTeam" + config.Config.GoliacTeamOwnerSuffix,
			Slug:    "grandchildteam" + config.Config.GoliacTeamOwnerSuffix,
			Members: []string{"existing_owner"},
			Id:      6,
		}

		remote.teams["parentteam"] = parentTeam
		remote.teams["parentteam"+config.Config.GoliacTeamOwnerSuffix] = parentTeamOwners
		remote.teams["childteam"] = childTeam
		remote.teams["childteam"+config.Config.GoliacTeamOwnerSuffix] = childTeamOwners
		remote.teams["grandchildteam"] = grandChildTeam
		remote.teams["grandchildteam"+config.Config.GoliacTeamOwnerSuffix] = grandChildTeamOwners

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 team parent updated
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 1, len(recorder.TeamParentUpdated))
	})

	t.Run("happy path: removed team", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()
		repoconf := config.RepositoryConfig{}
		repoconf.DestructiveOperations.AllowDestructiveTeams = true
		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)
		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		removing := &GithubTeam{
			Name:    "removing",
			Slug:    "removing",
			Members: []string{"existing_owner", "existing_owner"},
		}
		remote.teams["removing"] = removing

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 team deleted
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 1, len(recorder.TeamDeleted))
	})
}

func TestReconciliationRepo(t *testing.T) {
	t.Run("happy path: new repo without owner", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()
		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		newRepo := &entity.Repository{}
		newRepo.Name = "new"
		newRepo.Spec.Readers = []string{}
		newRepo.Spec.Writers = []string{}
		local.repos["new"] = newRepo

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:                "teams",
			ExternalUsers:       map[string]string{},
			BoolProperties:      map[string]bool{},
			Environments:        NewMockMappedEntityLazyLoader[*GithubEnvironment](map[string]*GithubEnvironment{}),
			RepositoryVariables: NewMockMappedEntityLazyLoader[string](map[string]string{}),
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 repo created
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 1, len(recorder.RepositoryCreated))
	})

	t.Run("happy path: new repo with owner", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		newRepo := &entity.Repository{}
		newRepo.Name = "new"
		newRepo.Spec.Readers = []string{}
		newRepo.Spec.Writers = []string{}
		owner := "existing"
		newRepo.Owner = &owner
		local.repos["new"] = newRepo

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{"existing_member"}
		local.teams["existing"] = existingTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:                "teams",
			ExternalUsers:       map[string]string{},
			BoolProperties:      map[string]bool{},
			Environments:        NewMockMappedEntityLazyLoader[*GithubEnvironment](map[string]*GithubEnvironment{}),
			RepositoryVariables: NewMockMappedEntityLazyLoader[string](map[string]string{}),
		}
		existing := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner", "existing_member"},
		}
		remote.teams["existing"] = existing

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 repo created
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 1, len(recorder.RepositoryCreated))
	})

	t.Run("happy path: existing repo with new owner (from read to write)", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		lRepo := &entity.Repository{}
		lRepo.Name = "myrepo"
		lRepo.Spec.Readers = []string{}
		lRepo.Spec.Writers = []string{}
		lowner := "existing"
		lRepo.Owner = &lowner
		local.repos["myrepo"] = lRepo

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{"existing_member"}
		local.teams["existing"] = existingTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		existing := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner", "existing_member"},
		}
		remote.teams["existing"] = existing
		rRepo := GithubRepository{
			Name:           "myrepo",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		remote.repos["myrepo"] = &rRepo

		remote.teamsrepos["existing"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing"]["myrepo"] = &GithubTeamRepo{
			Name:       "myrepo",
			Permission: "READ",
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 team updated
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 1, len(recorder.RepositoryTeamRemoved))
		assert.Equal(t, 2, len(recorder.RepositoryTeamAdded))
		assert.Equal(t, 0, len(recorder.RepositoryTeamUpdated))
	})

	t.Run("happy path: existing repo without new owner but with everyone team", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{
			EveryoneTeamEnabled: true,
		}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		lRepo := &entity.Repository{}
		lRepo.Name = "myrepo"
		lRepo.Spec.Readers = []string{}
		lRepo.Spec.Writers = []string{}
		lowner := "existing"
		lRepo.Owner = &lowner
		local.repos["myrepo"] = lRepo

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{"existing_member"}
		local.teams["existing"] = existingTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		existing := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner", "existing_member"},
		}
		remote.teams["existing"] = existing
		rRepo := GithubRepository{
			Name:           "myrepo",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		remote.repos["myrepo"] = &rRepo

		remote.teamsrepos["existing"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing"]["myrepo"] = &GithubTeamRepo{
			Name:       "myrepo",
			Permission: "WRITE",
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 team updated
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 0, len(recorder.RepositoryTeamRemoved))
		// we have a new "everyone" team for the repository + teams repo
		assert.Equal(t, 2, len(recorder.RepositoryTeamAdded))
		assert.Equal(t, 0, len(recorder.RepositoryTeamUpdated))
	})

	t.Run("happy path: add a team to an existing repo", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		lRepo := &entity.Repository{}
		lRepo.Name = "myrepo"
		lRepo.Spec.Readers = []string{"reader"}
		lRepo.Spec.Writers = []string{}
		lowner := "existing"
		lRepo.Owner = &lowner
		local.repos["myrepo"] = lRepo

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{"existing_member"}
		local.teams["existing"] = existingTeam

		readerTeam := &entity.Team{}
		readerTeam.Name = "reader"
		readerTeam.Spec.Owners = []string{"existing_owner"}
		readerTeam.Spec.Members = []string{"existing_member"}
		local.teams["reader"] = readerTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		existing := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner", "existing_member"},
		}
		reader := &GithubTeam{
			Name:    "reader",
			Slug:    "reader",
			Members: []string{"existing_owner", "existing_member"},
		}
		remote.teams["existing"] = existing
		remote.teams["reader"] = reader
		rRepo := GithubRepository{
			Name:           "myrepo",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		remote.repos["myrepo"] = &rRepo

		remote.teamsrepos["existing"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing"]["myrepo"] = &GithubTeamRepo{
			Name:       "myrepo",
			Permission: "ADMIN",
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 team added
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 1, len(recorder.RepositoryTeamRemoved)) // ADMIN -> WRITE
		assert.Equal(t, 2, len(recorder.RepositoryTeamAdded))
		assert.Equal(t, 0, len(recorder.RepositoryTeamUpdated))
	})

	t.Run("happy path: remove a team from an existing repo", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		lRepo := &entity.Repository{}
		lRepo.Name = "myrepo"
		lRepo.Spec.Readers = []string{}
		lRepo.Spec.Writers = []string{}
		lowner := "existing"
		lRepo.Owner = &lowner
		local.repos["myrepo"] = lRepo

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{"existing_member"}
		local.teams["existing"] = existingTeam

		readerTeam := &entity.Team{}
		readerTeam.Name = "reader"
		readerTeam.Spec.Owners = []string{"existing_owner"}
		readerTeam.Spec.Members = []string{"existing_member"}
		local.teams["reader"] = readerTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		existing := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner", "existing_member"},
		}
		reader := &GithubTeam{
			Name:    "reader",
			Slug:    "reader",
			Members: []string{"existing_owner", "existing_member"},
		}
		remote.teams["existing"] = existing
		remote.teams["reader"] = reader
		rRepo := GithubRepository{
			Name:           "myrepo",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		remote.repos["myrepo"] = &rRepo

		remote.teamsrepos["existing"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing"]["myrepo"] = &GithubTeamRepo{
			Name:       "myrepo",
			Permission: "WRITE",
		}
		remote.teamsrepos["reader"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["reader"]["myrepo"] = &GithubTeamRepo{
			Name:       "myrepo",
			Permission: "WRITE",
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 team removed
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 1, len(recorder.RepositoryTeamRemoved))
		assert.Equal(t, 1, len(recorder.RepositoryTeamAdded)) // on teams repo
		assert.Equal(t, 0, len(recorder.RepositoryTeamUpdated))
	})

	t.Run("happy path: remove a team member", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		lRepo := &entity.Repository{}
		lRepo.Name = "myrepo"
		lRepo.Spec.Readers = []string{}
		lRepo.Spec.Writers = []string{}
		lowner := "existing"
		lRepo.Owner = &lowner
		local.repos["myrepo"] = lRepo
		existingUser := entity.User{}
		existingUser.Spec.GithubID = "existing_member"
		local.users["existing_member"] = &existingUser
		existingOwner := entity.User{}
		existingOwner.Spec.GithubID = "existing_owner"
		local.users["existing_owner"] = &existingOwner

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{}
		local.teams["existing"] = existingTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		existing := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner", "existing_member"},
		}
		remote.teams["existing"] = existing
		rRepo := GithubRepository{
			Name:           "myrepo",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		remote.repos["myrepo"] = &rRepo

		remote.teamsrepos["existing"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing"]["myrepo"] = &GithubTeamRepo{
			Name:       "myrepo",
			Permission: "WRITE",
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 member removed
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 0, len(recorder.RepositoryTeamRemoved))
		assert.Equal(t, 1, len(recorder.RepositoryTeamAdded)) // on teams repo
		assert.Equal(t, 0, len(recorder.RepositoryTeamUpdated))
		assert.Equal(t, 1, len(recorder.TeamMemberRemoved))
		assert.ElementsMatch(t, []string{"existing_member"}, recorder.TeamMemberRemoved["existing"])
	})

	t.Run("happy path: remove multiple team members without skipping any", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		lRepo := &entity.Repository{}
		lRepo.Name = "myrepo"
		lRepo.Spec.Readers = []string{}
		lRepo.Spec.Writers = []string{}
		lowner := "existing"
		lRepo.Owner = &lowner
		local.repos["myrepo"] = lRepo

		existingMember := entity.User{}
		existingMember.Spec.GithubID = "existing_member"
		local.users["existing_member"] = &existingMember

		existingOwner := entity.User{}
		existingOwner.Spec.GithubID = "existing_owner"
		local.users["existing_owner"] = &existingOwner

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{"existing_member"}
		local.teams["existing"] = existingTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		existing := &GithubTeam{
			Name: "existing",
			Slug: "existing",
			Members: []string{
				"existing_owner",
				"existing_member",
				"removed_member_1",
				"removed_member_2",
			},
		}
		remote.teams["existing"] = existing
		rRepo := GithubRepository{
			Name:           "myrepo",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		remote.repos["myrepo"] = &rRepo

		remote.teamsrepos["existing"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing"]["myrepo"] = &GithubTeamRepo{
			Name:       "myrepo",
			Permission: "WRITE",
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 0, len(recorder.RepositoryTeamRemoved))
		assert.Equal(t, 1, len(recorder.RepositoryTeamAdded)) // on teams repo
		assert.Equal(t, 0, len(recorder.RepositoryTeamUpdated))
		assert.Equal(t, 1, len(recorder.TeamMemberRemoved))
		assert.ElementsMatch(t, []string{"removed_member_1", "removed_member_2"}, recorder.TeamMemberRemoved["existing"])
		assert.Equal(t, 0, len(recorder.TeamMemberAdded))
	})

	t.Run("happy path: mixed membership changes remove and add exact users", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		lRepo := &entity.Repository{}
		lRepo.Name = "myrepo"
		lRepo.Spec.Readers = []string{}
		lRepo.Spec.Writers = []string{}
		lowner := "existing"
		lRepo.Owner = &lowner
		local.repos["myrepo"] = lRepo

		existingMember := entity.User{}
		existingMember.Spec.GithubID = "existing_member"
		local.users["existing_member"] = &existingMember

		addedMember := entity.User{}
		addedMember.Spec.GithubID = "added_member"
		local.users["added_member"] = &addedMember

		existingOwner := entity.User{}
		existingOwner.Spec.GithubID = "existing_owner"
		local.users["existing_owner"] = &existingOwner

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{"existing_member", "added_member"}
		local.teams["existing"] = existingTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		existing := &GithubTeam{
			Name: "existing",
			Slug: "existing",
			Members: []string{
				"existing_owner",
				"existing_member",
				"removed_member",
			},
		}
		remote.teams["existing"] = existing
		rRepo := GithubRepository{
			Name:           "myrepo",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		remote.repos["myrepo"] = &rRepo

		remote.teamsrepos["existing"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing"]["myrepo"] = &GithubTeamRepo{
			Name:       "myrepo",
			Permission: "WRITE",
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 0, len(recorder.RepositoryTeamRemoved))
		assert.Equal(t, 1, len(recorder.RepositoryTeamAdded)) // on teams repo
		assert.Equal(t, 0, len(recorder.RepositoryTeamUpdated))
		assert.Equal(t, 1, len(recorder.TeamMemberRemoved))
		assert.ElementsMatch(t, []string{"removed_member"}, recorder.TeamMemberRemoved["existing"])
		assert.Equal(t, 1, len(recorder.TeamMemberAdded))
		assert.ElementsMatch(t, []string{"added_member"}, recorder.TeamMemberAdded["existing"])
	})

	t.Run("happy path: update a team member from maintainer to member", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		lRepo := &entity.Repository{}
		lRepo.Name = "myrepo"
		lRepo.Spec.Readers = []string{}
		lRepo.Spec.Writers = []string{}
		lowner := "existing"
		lRepo.Owner = &lowner
		local.repos["myrepo"] = lRepo
		existingUser := entity.User{}
		existingUser.Spec.GithubID = "existing_member"
		local.users["existing_member"] = &existingUser
		existingOwner := entity.User{}
		existingOwner.Spec.GithubID = "existing_owner"
		local.users["existing_owner"] = &existingOwner

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{"existing_member"}
		local.teams["existing"] = existingTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		existing := &GithubTeam{
			Name:        "existing",
			Slug:        "existing",
			Members:     []string{"existing_member"},
			Maintainers: []string{"existing_owner"},
		}
		remote.teams["existing"] = existing
		rRepo := GithubRepository{
			Name:           "myrepo",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		remote.repos["myrepo"] = &rRepo

		remote.teamsrepos["existing"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing"]["myrepo"] = &GithubTeamRepo{
			Name:       "myrepo",
			Permission: "WRITE",
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 member removed
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 0, len(recorder.RepositoryTeamRemoved))
		assert.Equal(t, 1, len(recorder.RepositoryTeamAdded)) // on teams repo
		assert.Equal(t, 0, len(recorder.RepositoryTeamUpdated))
		assert.Equal(t, 0, len(recorder.TeamMemberRemoved))
		assert.Equal(t, 1, len(recorder.TeamMemberUpdated))
	})

	t.Run("happy path: add a team AND add it to an existing repo", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()
		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		lRepo := &entity.Repository{}
		lRepo.Name = "myrepo"
		lRepo.Spec.Readers = []string{"reader"}
		lRepo.Spec.Writers = []string{}
		lowner := "existing"
		lRepo.Owner = &lowner
		local.repos["myrepo"] = lRepo

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{"existing_member"}
		local.teams["existing"] = existingTeam

		readerTeam := &entity.Team{}
		readerTeam.Name = "reader"
		readerTeam.Spec.Owners = []string{"existing_owner"}
		readerTeam.Spec.Members = []string{"existing_member"}
		local.teams["reader"] = readerTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		existing := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner", "existing_member"},
		}
		remote.teams["existing"] = existing
		rRepo := GithubRepository{
			Name:           "myrepo",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		remote.repos["myrepo"] = &rRepo

		remote.teamsrepos["existing"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing"]["myrepo"] = &GithubTeamRepo{
			Name:       "myrepo",
			Permission: "WRITE",
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 repo updated
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 0, len(recorder.RepositoryTeamRemoved))
		assert.Equal(t, 2, len(recorder.RepositoryTeamAdded))
	})

	t.Run("happy path: add a externally managed team AND add it to an existing repo", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()
		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		lRepo := &entity.Repository{}
		lRepo.Name = "myrepo"
		lRepo.Spec.Readers = []string{"newerTeam"}
		lRepo.Spec.Writers = []string{}
		lowner := "existing"
		lRepo.Owner = &lowner
		local.repos["myrepo"] = lRepo

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{"existing_member"}
		local.teams["existing"] = existingTeam

		newerTeam := &entity.Team{}
		newerTeam.Name = "newerTeam"
		newerTeam.Spec.ExternallyManaged = true
		local.teams["newerTeam"] = newerTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		existing := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner", "existing_member"},
		}
		remote.teams["existing"] = existing
		remote.teams["existing"+config.Config.GoliacTeamOwnerSuffix] = existing
		rRepo := GithubRepository{
			Name:           "myrepo",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		remote.repos["myrepo"] = &rRepo

		remote.teamsrepos["existing"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing"]["myrepo"] = &GithubTeamRepo{
			Name:       "myrepo",
			Permission: "WRITE",
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 repo updated
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 2, len(recorder.TeamsCreated)) // the newerTeam-goliac-owners team
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 0, len(recorder.RepositoryTeamRemoved))
		assert.Equal(t, 2, len(recorder.RepositoryTeamAdded))
	})

	t.Run("happy path: existing repo with new external write collaborator", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users:     make(map[string]*entity.User),
			externals: make(map[string]*entity.User),
			teams:     make(map[string]*entity.Team),
			repos:     make(map[string]*entity.Repository),
		}
		outside1 := entity.User{}
		outside1.Name = "outside1"
		outside1.Spec.GithubID = "outside1-githubid"
		local.externals["outside1"] = &outside1

		lRepo := &entity.Repository{}
		lRepo.Name = "myrepo"
		lRepo.Spec.Readers = []string{}
		lRepo.Spec.Writers = []string{}
		lRepo.Spec.ExternalUserWriters = []string{"outside1"}
		lowner := "existing"
		lRepo.Owner = &lowner
		local.repos["myrepo"] = lRepo

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{}
		local.teams["existing"] = existingTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		existing := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner"},
		}
		remote.teams["existing"] = existing
		rRepo := GithubRepository{
			Name:           "myrepo",
			ExternalUsers:  make(map[string]string),
			BoolProperties: make(map[string]bool),
		}
		remote.repos["myrepo"] = &rRepo

		remote.teamsrepos["existing"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing"]["myrepo"] = &GithubTeamRepo{
			Name:       "myrepo",
			Permission: "WRITE",
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 team updated
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 0, len(recorder.RepositoryTeamRemoved))
		assert.Equal(t, 1, len(recorder.RepositoryTeamAdded)) // on teams repo
		assert.Equal(t, 0, len(recorder.RepositoryTeamUpdated))
		assert.Equal(t, 1, len(recorder.RepositoriesSetExternalUser))
		assert.Equal(t, 0, len(recorder.RepositoriesRemoveExternalUser))
	})

	t.Run("happy path: existing repo with deleted external write collaborator", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users:     make(map[string]*entity.User),
			externals: make(map[string]*entity.User),
			teams:     make(map[string]*entity.Team),
			repos:     make(map[string]*entity.Repository),
		}

		lRepo := &entity.Repository{}
		lRepo.Name = "myrepo"
		lRepo.Spec.Readers = []string{}
		lRepo.Spec.Writers = []string{}
		lRepo.Spec.ExternalUserWriters = []string{}
		lowner := "existing"
		lRepo.Owner = &lowner
		local.repos["myrepo"] = lRepo

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{}
		local.teams["existing"] = existingTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		existing := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner"},
		}
		remote.teams["existing"] = existing
		rRepo := GithubRepository{
			Name:           "myrepo",
			ExternalUsers:  make(map[string]string),
			BoolProperties: make(map[string]bool),
		}
		rRepo.ExternalUsers["outside1-githubid"] = "WRITE"
		remote.repos["myrepo"] = &rRepo

		remote.teamsrepos["existing"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing"]["myrepo"] = &GithubTeamRepo{
			Name:       "myrepo",
			Permission: "WRITE",
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 team updated
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 0, len(recorder.RepositoryTeamRemoved))
		assert.Equal(t, 1, len(recorder.RepositoryTeamAdded)) // on teams repo
		assert.Equal(t, 0, len(recorder.RepositoryTeamUpdated))
		assert.Equal(t, 0, len(recorder.RepositoriesSetExternalUser))
		assert.Equal(t, 1, len(recorder.RepositoriesRemoveExternalUser))
	})

	t.Run("happy path: existing repo with changed external write collaborator (from read to write)", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users:     make(map[string]*entity.User),
			externals: make(map[string]*entity.User),
			teams:     make(map[string]*entity.Team),
			repos:     make(map[string]*entity.Repository),
		}

		outside1 := entity.User{}
		outside1.Name = "outside1"
		outside1.Spec.GithubID = "outside1-githubid"
		local.externals["outside1"] = &outside1

		lRepo := &entity.Repository{}
		lRepo.Name = "myrepo"
		lRepo.Spec.Readers = []string{}
		lRepo.Spec.Writers = []string{}
		lRepo.Spec.ExternalUserWriters = []string{}
		lRepo.Spec.ExternalUserReaders = []string{"outside1"}
		lowner := "existing"
		lRepo.Owner = &lowner
		local.repos["myrepo"] = lRepo

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{}
		local.teams["existing"] = existingTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		existing := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner"},
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		remote.teams["existing"] = existing
		rRepo := GithubRepository{
			Name:           "myrepo",
			ExternalUsers:  make(map[string]string),
			BoolProperties: make(map[string]bool),
		}
		rRepo.ExternalUsers["outside1-githubid"] = "WRITE"
		remote.repos["myrepo"] = &rRepo

		remote.teamsrepos["existing"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing"]["myrepo"] = &GithubTeamRepo{
			Name:       "myrepo",
			Permission: "WRITE",
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 team updated
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 0, len(recorder.RepositoriesRenamed))
		assert.Equal(t, 0, len(recorder.RepositoryTeamRemoved))
		assert.Equal(t, 1, len(recorder.RepositoryTeamAdded)) // on teams repo
		assert.Equal(t, 0, len(recorder.RepositoryTeamUpdated))
		assert.Equal(t, 1, len(recorder.RepositoriesSetExternalUser))
		assert.Equal(t, 0, len(recorder.RepositoriesRemoveExternalUser))
	})

	t.Run("happy path: removed repo without destructive operation", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		removing := &GithubRepository{
			Name: "removing",
		}
		remote.repos["removing"] = removing

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 repo deleted
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
	})

	t.Run("happy path: removed repo with archive_on_delete", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()
		repoconf := config.RepositoryConfig{
			ArchiveOnDelete: true,
		}
		repoconf.DestructiveOperations.AllowDestructiveRepositories = true
		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		removing := &GithubRepository{
			Name:           "removing",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		remote.repos["removing"] = removing

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		_, toArchive, _, _ := r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 repo deleted
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 1, len(toArchive))
	})

	t.Run("happy path: removed repo withou archive_on_delete", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()
		repoconf := config.RepositoryConfig{
			ArchiveOnDelete: false,
		}
		repoconf.DestructiveOperations.AllowDestructiveRepositories = true
		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		removing := &GithubRepository{
			Name:           "removing",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		remote.repos["removing"] = removing

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		_, toArchive, _, _ := r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 repo deleted
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 1, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 0, len(toArchive))
	})

	t.Run("happy path: rename repo", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()
		repoconf := config.RepositoryConfig{
			AdminTeam:       "admin-team",
			ArchiveOnDelete: false,
		}
		repoconf.DestructiveOperations.AllowDestructiveRepositories = true
		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		lRepo := &entity.Repository{}
		lRepo.Name = "myrepo"
		lRepo.Spec.Readers = []string{}
		lRepo.Spec.Writers = []string{}
		lRepo.Spec.ExternalUserWriters = []string{}
		lRepo.Spec.AllowMergeCommit = true
		lRepo.Spec.AllowRebaseMerge = true
		lRepo.Spec.AllowSquashMerge = true
		lowner := "existing"
		lRepo.Owner = &lowner
		lRepo.RenameTo = "myrepo2" // HERE we rename the repo
		local.repos["myrepo"] = lRepo

		adminTeam := &entity.Team{}
		adminTeam.Name = "admin-team"
		adminTeam.Spec.Owners = []string{"existing_owner"}
		local.teams["admin-team"] = adminTeam

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{}
		local.teams["existing"] = existingTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		rAdminTeam := &GithubTeam{
			Name:    "admin-team",
			Slug:    "admin-team",
			Members: []string{"admin-team"},
		}
		remote.teams["admin-team"] = rAdminTeam

		rAdminTeamOwners := &GithubTeam{
			Name:    "admin-team-goliac-owners",
			Slug:    "admin-team-goliac-owners",
			Members: []string{"admin-team"},
		}
		remote.teams["admin-team-goliac-owners"] = rAdminTeamOwners

		rExisting := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner"},
		}
		remote.teams["existing"] = rExisting

		remote.repos["goliac-teams"] = &GithubRepository{
			Name:              "goliac-teams",
			Visibility:        "internal",
			DefaultBranchName: "main",
			ExternalUsers:     map[string]string{},
			BoolProperties: map[string]bool{
				"allow_auto_merge":       false,
				"allow_squash_merge":     true,
				"allow_merge_commit":     false,
				"allow_rebase_merge":     false,
				"archived":               false,
				"delete_branch_on_merge": true,
				"allow_update_branch":    false,
			},
			DefaultMergeCommitMessage:  "Default message",
			DefaultSquashCommitMessage: "Default message",
		}

		existingOwner := &GithubTeam{
			Name:    "existing-goliac-owners",
			Slug:    "existing-goliac-owners",
			Members: []string{"existing_owner"},
		}
		remote.teamsrepos["existing-goliac-owners"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing-goliac-owners"]["goliac-teams"] = &GithubTeamRepo{
			Name:       "goliac-teams",
			Permission: "WRITE",
		}

		remote.teams["existing-goliac-owners"] = existingOwner
		rRepo := GithubRepository{
			Name:          "myrepo",
			ExternalUsers: make(map[string]string),
			BoolProperties: map[string]bool{
				"allow_auto_merge":       false,
				"delete_branch_on_merge": false,
				"allow_update_branch":    false,
				"allow_squash_merge":     true,
				"allow_merge_commit":     true,
				"allow_rebase_merge":     true,
				"archived":               false,
			},
		}
		remote.repos["myrepo"] = &rRepo

		remote.teamsrepos["admin-team"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["admin-team"]["goliac-teams"] = &GithubTeamRepo{
			Name:       "goliac-teams",
			Permission: "WRITE",
		}
		remote.teamsrepos["admin-team-goliac-owners"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["admin-team-goliac-owners"]["goliac-teams"] = &GithubTeamRepo{
			Name:       "goliac-teams",
			Permission: "WRITE",
		}

		remote.teamsrepos["existing"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing"]["myrepo"] = &GithubTeamRepo{
			Name:       "myrepo",
			Permission: "WRITE",
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "goliac-teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		_, _, toRename, _ := r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 repo renamed
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 1, len(toRename))
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 1, len(recorder.RepositoriesRenamed))
		assert.Equal(t, 0, len(recorder.RepositoryTeamRemoved))
		assert.Equal(t, 0, len(recorder.RepositoryTeamAdded))
		assert.Equal(t, 0, len(recorder.RepositoryTeamUpdated))
		assert.Equal(t, 0, len(recorder.RepositoriesSetExternalUser))
		assert.Equal(t, 0, len(recorder.RepositoriesRemoveExternalUser))
		assert.Equal(t, 0, len(recorder.RepositoriesUpdateProperty))
	})

	t.Run("happy path: change default branch of the repo", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()
		repoconf := config.RepositoryConfig{
			AdminTeam:       "admin-team",
			ArchiveOnDelete: false,
		}
		repoconf.DestructiveOperations.AllowDestructiveRepositories = true
		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		lRepo := &entity.Repository{}
		lRepo.Name = "myrepo"
		lRepo.Spec.Readers = []string{}
		lRepo.Spec.Writers = []string{}
		lRepo.Spec.ExternalUserWriters = []string{}
		lowner := "existing"
		lRepo.Owner = &lowner
		lRepo.Spec.DefaultBranchName = "master"
		local.repos["myrepo"] = lRepo

		adminTeam := &entity.Team{}
		adminTeam.Name = "admin-team"
		adminTeam.Spec.Owners = []string{"existing_owner"}
		local.teams["admin-team"] = adminTeam

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{}
		local.teams["existing"] = existingTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		rAdminTeam := &GithubTeam{
			Name:    "admin-team",
			Slug:    "admin-team",
			Members: []string{"admin-team"},
		}
		remote.teams["admin-team"] = rAdminTeam

		rAdminTeamOwners := &GithubTeam{
			Name:    "admin-team-goliac-owners",
			Slug:    "admin-team-goliac-owners",
			Members: []string{"admin-team"},
		}
		remote.teams["admin-team-goliac-owners"] = rAdminTeamOwners

		rExisting := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner"},
		}
		remote.teams["existing"] = rExisting

		ghTeams := &GithubRepository{
			Name:              "goliac-teams",
			Visibility:        "internal",
			DefaultBranchName: "main",
			ExternalUsers:     map[string]string{},
			BoolProperties: map[string]bool{
				"allow_auto_merge":       false,
				"archived":               false,
				"allow_squash_merge":     true,
				"allow_merge_commit":     false,
				"allow_rebase_merge":     false,
				"delete_branch_on_merge": true,
				"allow_update_branch":    false,
			},
			DefaultMergeCommitMessage:  "Default message",
			DefaultSquashCommitMessage: "Default message",
		}
		remote.repos["goliac-teams"] = ghTeams

		existingOwner := &GithubTeam{
			Name:    "existing-goliac-owners",
			Slug:    "existing-goliac-owners",
			Members: []string{"existing_owner"},
		}
		remote.teamsrepos["existing-goliac-owners"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing-goliac-owners"]["goliac-teams"] = &GithubTeamRepo{
			Name:       "goliac-teams",
			Permission: "WRITE",
		}

		remote.teams["existing-goliac-owners"] = existingOwner
		rRepo := GithubRepository{
			Name:          "myrepo",
			ExternalUsers: make(map[string]string),
			BoolProperties: map[string]bool{
				"allow_auto_merge":       false,
				"delete_branch_on_merge": false,
				"allow_update_branch":    false,
				"allow_squash_merge":     true,
				"allow_merge_commit":     true,
				"allow_rebase_merge":     true,
				"archived":               false,
			},
			DefaultBranchName: "main",
		}
		remote.repos["myrepo"] = &rRepo

		remote.teamsrepos["admin-team"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["admin-team"]["goliac-teams"] = &GithubTeamRepo{
			Name:       "goliac-teams",
			Permission: "WRITE",
		}
		remote.teamsrepos["admin-team-goliac-owners"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["admin-team-goliac-owners"]["goliac-teams"] = &GithubTeamRepo{
			Name:       "goliac-teams",
			Permission: "WRITE",
		}

		remote.teamsrepos["existing"] = make(map[string]*GithubTeamRepo)
		remote.teamsrepos["existing"]["myrepo"] = &GithubTeamRepo{
			Name:       "myrepo",
			Permission: "WRITE",
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "goliac-teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 repo renamed
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoriesDeleted))
		assert.Equal(t, 0, len(recorder.RepositoriesRenamed))
		assert.Equal(t, 0, len(recorder.RepositoryTeamRemoved))
		assert.Equal(t, 0, len(recorder.RepositoryTeamAdded))
		assert.Equal(t, 0, len(recorder.RepositoryTeamUpdated))
		assert.Equal(t, 0, len(recorder.RepositoriesSetExternalUser))
		assert.Equal(t, 0, len(recorder.RepositoriesRemoveExternalUser))
		assert.Equal(t, 1, len(recorder.RepositoriesUpdateProperty))
	})

}

func TestReconciliationRulesets(t *testing.T) {

	t.Run("happy path: no new ruleset in goliac conf", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()
		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users:    make(map[string]*entity.User),
			teams:    make(map[string]*entity.Team),
			repos:    make(map[string]*entity.Repository),
			rulesets: make(map[string]*entity.RuleSet),
		}

		newRuleset := &entity.RuleSet{}
		newRuleset.Name = "new"
		newRuleset.Spec.Ruleset.Enforcement = "evaluate"
		newRuleset.Spec.Ruleset.Rules = append(newRuleset.Spec.Ruleset.Rules, struct {
			Ruletype   string
			Parameters entity.RuleSetParameters `yaml:"parameters,omitempty"`
		}{
			"required_signatures", entity.RuleSetParameters{},
		})
		local.rulesets["new"] = newRuleset

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 ruleset created
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RuleSetCreated))
		assert.Equal(t, 0, len(recorder.RuleSetUpdated))
		assert.Equal(t, 0, len(recorder.RuleSetDeleted))
	})

	t.Run("happy path: new ruleset", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{
			Rulesets: []string{"new"},
		}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users:    make(map[string]*entity.User),
			teams:    make(map[string]*entity.Team),
			repos:    make(map[string]*entity.Repository),
			rulesets: make(map[string]*entity.RuleSet),
		}

		newRuleset := &entity.RuleSet{}
		newRuleset.Name = "new"
		newRuleset.Spec.Ruleset.Enforcement = "evaluate"
		newRuleset.Spec.Ruleset.Rules = append(newRuleset.Spec.Ruleset.Rules, struct {
			Ruletype   string
			Parameters entity.RuleSetParameters `yaml:"parameters,omitempty"`
		}{
			"required_signatures", entity.RuleSetParameters{},
		})
		local.rulesets["new"] = newRuleset

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 ruleset created
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 1, len(recorder.RuleSetCreated))
		assert.Equal(t, 0, len(recorder.RuleSetUpdated))
		assert.Equal(t, 0, len(recorder.RuleSetDeleted))
	})

	t.Run("happy path: update ruleset (enforcement)", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{
			Rulesets: []string{"update"},
		}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users:    make(map[string]*entity.User),
			teams:    make(map[string]*entity.Team),
			repos:    make(map[string]*entity.Repository),
			rulesets: make(map[string]*entity.RuleSet),
		}

		lRuleset := &entity.RuleSet{}
		lRuleset.Name = "update"
		lRuleset.Spec.Ruleset.Enforcement = "evaluate"
		lRuleset.Spec.Ruleset.Rules = append(lRuleset.Spec.Ruleset.Rules, struct {
			Ruletype   string
			Parameters entity.RuleSetParameters `yaml:"parameters,omitempty"`
		}{
			"required_signatures", entity.RuleSetParameters{},
		})
		local.rulesets["update"] = lRuleset

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		rRuleset := &GithubRuleSet{
			Name:        "update",
			Enforcement: "active",
			Rules:       make(map[string]entity.RuleSetParameters),
		}
		rRuleset.Rules["required_signatures"] = entity.RuleSetParameters{}
		remote.rulesets["update"] = rRuleset

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 ruleset created
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RuleSetCreated))
		assert.Equal(t, 1, len(recorder.RuleSetUpdated))
		assert.Equal(t, 0, len(recorder.RuleSetDeleted))
	})

	t.Run("happy path: delete ruleset", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{
			Rulesets: []string{},
		}
		repoconf.DestructiveOperations.AllowDestructiveRulesets = true

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users:    make(map[string]*entity.User),
			teams:    make(map[string]*entity.Team),
			repos:    make(map[string]*entity.Repository),
			rulesets: make(map[string]*entity.RuleSet),
		}

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		rRuleset := &GithubRuleSet{
			Name:        "delete",
			Enforcement: "active",
			Rules:       make(map[string]entity.RuleSetParameters),
		}
		rRuleset.Rules["required_signatures"] = entity.RuleSetParameters{}
		remote.rulesets["delete"] = rRuleset

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 1 ruleset created
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RuleSetCreated))
		assert.Equal(t, 0, len(recorder.RuleSetUpdated))
		assert.Equal(t, 1, len(recorder.RuleSetDeleted))
	})

	t.Run("happy path: same bypass team in ruleset in goliac conf", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()
		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users:    make(map[string]*entity.User),
			teams:    make(map[string]*entity.Team),
			repos:    make(map[string]*entity.Repository),
			rulesets: make(map[string]*entity.RuleSet),
		}

		ateam := &entity.Team{}
		ateam.Name = "ateam"
		ateam.Spec.Owners = []string{"existing_owner"}
		local.teams["ateam"] = ateam

		newRuleset := &entity.RuleSet{}
		newRuleset.Name = "new"
		newRuleset.Spec.Repositories.Included = []string{"foobar"}
		newRuleset.Spec.Ruleset.Enforcement = "active"
		newRuleset.Spec.Ruleset.Rules = append(newRuleset.Spec.Ruleset.Rules, struct {
			Ruletype   string
			Parameters entity.RuleSetParameters `yaml:"parameters,omitempty"`
		}{
			"required_signatures", entity.RuleSetParameters{},
		})
		newRuleset.Spec.Ruleset.BypassTeams = []struct {
			TeamName string
			Mode     string
		}{
			{TeamName: "ateam", Mode: "pull_request"},
		}
		local.rulesets["new"] = newRuleset

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		rRuleset := &GithubRuleSet{
			Name:        "new",
			Enforcement: "active",
			Rules:       make(map[string]entity.RuleSetParameters),
			BypassTeams: make(map[string]string),
			BypassApps:  make(map[string]string),
		}
		rRuleset.Rules["required_signatures"] = entity.RuleSetParameters{}
		rRuleset.BypassTeams["ateam"] = "pull_request"
		remote.rulesets["new"] = rRuleset

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()

		repoconf.Rulesets = []string{"new"}

		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// 0 ruleset changed
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RuleSetCreated))
		assert.Equal(t, 0, len(recorder.RuleSetUpdated))
		assert.Equal(t, 0, len(recorder.RuleSetDeleted))
	})

}

func TestReconciliationRepoRulesets(t *testing.T) {

	t.Run("happy path: repo with ruleset", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		newRepo := &entity.Repository{}
		newRepo.Name = "myrepo"
		newRepo.Spec.Readers = []string{}
		newRepo.Spec.Writers = []string{}
		owner := "existing"
		newRepo.Owner = &owner

		lruleset := entity.RepositoryRuleSet{
			Name: "myruleset",
		}
		lruleset.Enforcement = "active"
		lruleset.Conditions.Include = []string{"~DEFAULT_BRANCH"}
		lruleset.Rules = append(lruleset.Rules, struct {
			Ruletype   string
			Parameters entity.RuleSetParameters `yaml:"parameters,omitempty"`
		}{
			"required_signatures", entity.RuleSetParameters{},
		})
		newRepo.Spec.Rulesets = []entity.RepositoryRuleSet{lruleset}
		local.repos["myrepo"] = newRepo

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{"existing_member"}
		local.teams["existing"] = existingTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		existing := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner", "existing_member"},
		}
		remote.teams["existing"] = existing

		myrepo := &GithubRepository{
			Name:  "myrepo",
			Id:    1234,
			RefId: "sdfsf",
			BoolProperties: map[string]bool{
				"private":                true,
				"allow_update_branch":    false,
				"archived":               false,
				"allow_auto_merge":       false,
				"delete_branch_on_merge": false,
			},
			ExternalUsers: make(map[string]string),
			InternalUsers: make(map[string]string),
			RuleSets:      map[string]*GithubRuleSet{},
		}
		rruleset := GithubRuleSet{
			Name:        "myruleset",
			Enforcement: "active",
			OnInclude:   []string{"~DEFAULT_BRANCH"},
			Rules: map[string]entity.RuleSetParameters{
				"required_signatures": {},
			},
		}
		myrepo.RuleSets["myruleset"] = &rruleset

		remote.repos["myrepo"] = myrepo

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoryRuleSetCreated["myrepo"]))
		assert.Equal(t, 0, len(recorder.RepositoryRuleSetUpdated["myrepo"]))
		assert.Equal(t, 0, len(recorder.RepositoryRuleSetDeleted["myrepo"]))
	})

	t.Run("happy path: repo with new ruleset", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		newRepo := &entity.Repository{}
		newRepo.Name = "myrepo"
		newRepo.Spec.Readers = []string{}
		newRepo.Spec.Writers = []string{}
		owner := "existing"
		newRepo.Owner = &owner

		lruleset := entity.RepositoryRuleSet{
			Name: "myruleset",
		}
		lruleset.Enforcement = "active"
		lruleset.Conditions.Include = []string{"~DEFAULT_BRANCH"}
		lruleset.Rules = append(lruleset.Rules, struct {
			Ruletype   string
			Parameters entity.RuleSetParameters `yaml:"parameters,omitempty"`
		}{
			"required_signatures", entity.RuleSetParameters{},
		})
		newRepo.Spec.Rulesets = []entity.RepositoryRuleSet{lruleset}
		local.repos["myrepo"] = newRepo

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{"existing_member"}
		local.teams["existing"] = existingTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		existing := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner", "existing_member"},
		}
		remote.teams["existing"] = existing

		myrepo := &GithubRepository{
			Name:  "myrepo",
			Id:    1234,
			RefId: "sdfsf",
			BoolProperties: map[string]bool{
				"private":                true,
				"allow_update_branch":    false,
				"archived":               false,
				"allow_auto_merge":       false,
				"delete_branch_on_merge": false,
			},
			ExternalUsers: make(map[string]string),
			InternalUsers: make(map[string]string),
			RuleSets:      map[string]*GithubRuleSet{},
		}

		remote.repos["myrepo"] = myrepo

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 1, len(recorder.RepositoryRuleSetCreated["myrepo"]))
		assert.Equal(t, 0, len(recorder.RepositoryRuleSetUpdated["myrepo"]))
		assert.Equal(t, 0, len(recorder.RepositoryRuleSetDeleted["myrepo"]))
	})
}

func TestReconciliationRepoBranchProtection(t *testing.T) {

	t.Run("happy path: repo with branch protection", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		newRepo := &entity.Repository{}
		newRepo.Name = "myrepo"
		newRepo.Spec.Readers = []string{}
		newRepo.Spec.Writers = []string{}
		owner := "existing"
		newRepo.Owner = &owner

		lbranchprotection := entity.RepositoryBranchProtection{
			Pattern: "main",
		}
		lbranchprotection.RequiresCommitSignatures = true
		newRepo.Spec.BranchProtections = []entity.RepositoryBranchProtection{lbranchprotection}
		local.repos["myrepo"] = newRepo

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{"existing_member"}
		local.teams["existing"] = existingTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		existing := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner", "existing_member"},
		}
		remote.teams["existing"] = existing

		myrepo := &GithubRepository{
			Name:  "myrepo",
			Id:    1234,
			RefId: "sdfsf",
			BoolProperties: map[string]bool{
				"private":                true,
				"allow_update_branch":    false,
				"archived":               false,
				"allow_auto_merge":       false,
				"delete_branch_on_merge": false,
			},
			ExternalUsers:     make(map[string]string),
			InternalUsers:     make(map[string]string),
			RuleSets:          map[string]*GithubRuleSet{},
			BranchProtections: map[string]*GithubBranchProtection{},
		}
		rbranchproection := GithubBranchProtection{
			Pattern:                  "main",
			RequiresCommitSignatures: true,
		}
		myrepo.BranchProtections["main"] = &rbranchproection

		remote.repos["myrepo"] = myrepo

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 0, len(recorder.RepositoryBranchProtectionCreated["myrepo"]))
		assert.Equal(t, 0, len(recorder.RepositoryBranchProtectionUpdated["myrepo"]))
		assert.Equal(t, 0, len(recorder.RepositoryBranchProtectionDeleted["myrepo"]))
	})

	t.Run("happy path: repo with new branch protection", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}
		newRepo := &entity.Repository{}
		newRepo.Name = "myrepo"
		newRepo.Spec.Readers = []string{}
		newRepo.Spec.Writers = []string{}
		owner := "existing"
		newRepo.Owner = &owner

		lbranchprotection := entity.RepositoryBranchProtection{
			Pattern: "main",
		}
		lbranchprotection.RequiresCommitSignatures = true
		newRepo.Spec.BranchProtections = []entity.RepositoryBranchProtection{lbranchprotection}
		local.repos["myrepo"] = newRepo

		existingTeam := &entity.Team{}
		existingTeam.Name = "existing"
		existingTeam.Spec.Owners = []string{"existing_owner"}
		existingTeam.Spec.Members = []string{"existing_member"}
		local.teams["existing"] = existingTeam

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}
		remote.repos["teams"] = &GithubRepository{
			Name:           "teams",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
		}
		existing := &GithubTeam{
			Name:    "existing",
			Slug:    "existing",
			Members: []string{"existing_owner", "existing_member"},
		}
		remote.teams["existing"] = existing

		myrepo := &GithubRepository{
			Name:  "myrepo",
			Id:    1234,
			RefId: "sdfsf",
			BoolProperties: map[string]bool{
				"private":                true,
				"allow_update_branch":    false,
				"archived":               false,
				"allow_auto_merge":       false,
				"delete_branch_on_merge": false,
			},
			ExternalUsers:     make(map[string]string),
			InternalUsers:     make(map[string]string),
			RuleSets:          map[string]*GithubRuleSet{},
			BranchProtections: map[string]*GithubBranchProtection{},
		}

		remote.repos["myrepo"] = myrepo

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryCreated))
		assert.Equal(t, 1, len(recorder.RepositoryBranchProtectionCreated["myrepo"]))
		assert.Equal(t, 0, len(recorder.RepositoryBranchProtectionUpdated["myrepo"]))
		assert.Equal(t, 0, len(recorder.RepositoryBranchProtectionDeleted["myrepo"]))
	})
}

func TestReconciliationRepositoryEnvironments(t *testing.T) {
	t.Run("happy path: add new environment to repository", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()
		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		// Create a repository with a new environment
		repo := &entity.Repository{}
		repo.Name = "test-repo"
		repo.Spec.Environments = []entity.RepositoryEnvironment{
			{
				Name: "production",
				Variables: map[string]string{
					"DB_URL": "prod-db-url",
				},
			},
		}
		local.repos["test-repo"] = repo

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		// Add the repository to remote without any environments
		remoteRepo := &GithubRepository{
			Name:           "test-repo",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
			Environments:   NewMockMappedEntityLazyLoader(map[string]*GithubEnvironment{}),
		}
		remote.repos["test-repo"] = remoteRepo

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// Verify environment was added
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, "production", recorder.RepositoryEnvironmentCreated["test-repo"])
	})

	t.Run("happy path: remove environment from repository", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		// Create a repository without any environments
		repo := &entity.Repository{}
		repo.Name = "test-repo"
		repo.Spec.Environments = []entity.RepositoryEnvironment{}
		local.repos["test-repo"] = repo

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		// Add the repository to remote with a production environment
		remoteRepo := &GithubRepository{
			Name:           "test-repo",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
			Environments: NewMockMappedEntityLazyLoader(map[string]*GithubEnvironment{
				"production": {
					Name: "production",
					Variables: map[string]string{
						"DB_URL": "prod-db-url",
					},
				},
			}),
		}
		remote.repos["test-repo"] = remoteRepo

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// Verify environment was removed
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, "production", recorder.RepositoryEnvironmentDeleted["test-repo"])
	})

	t.Run("happy path: update environment variables", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()
		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		// Create a repository with updated environment variables
		repo := &entity.Repository{}
		repo.Name = "test-repo"
		repo.Spec.Environments = []entity.RepositoryEnvironment{
			{
				Name: "production",
				Variables: map[string]string{
					"DB_URL":  "new-prod-db-url",
					"API_KEY": "new-api-key",
				},
			},
		}
		local.repos["test-repo"] = repo

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		// Add the repository to remote with old environment variables
		remoteRepo := &GithubRepository{
			Name:           "test-repo",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
			Environments: NewMockMappedEntityLazyLoader(map[string]*GithubEnvironment{
				"production": {
					Name: "production",
					Variables: map[string]string{
						"DB_URL": "old-prod-db-url",
					},
				},
			}),
		}
		remote.repos["test-repo"] = remoteRepo

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// Verify environment variables were updated
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, "production", recorder.RepositoryEnvironmentVariableUpdated["test-repo"])
	})
}

func TestReconciliationAutolinks(t *testing.T) {
	t.Run("happy path: add new autolink to repository", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()
		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		// Create a repository with a new environment
		repo := &entity.Repository{}
		repo.Name = "test-repo"
		autolinks := []entity.RepositoryAutolink{
			{
				KeyPrefix:      "TICKET-",
				UrlTemplate:    "https://example.com/TICKET?query=<num>",
				IsAlphanumeric: true,
			},
		}
		repo.Spec.Autolinks = &autolinks
		local.repos["test-repo"] = repo

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		// Add the repository to remote without any environments
		remoteRepo := &GithubRepository{
			Name:           "test-repo",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
			Environments:   NewMockMappedEntityLazyLoader(map[string]*GithubEnvironment{}),
			Autolinks:      NewMockMappedEntityLazyLoader(map[string]*GithubAutolink{}),
		}
		remote.repos["test-repo"] = remoteRepo

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// Verify environment was added
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, "https://example.com/TICKET?query=<num>", recorder.RepositoryAutolinkCreated["test-repo"]["TICKET-"].UrlTemplate)
	})

	t.Run("happy path: removing autolink", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()
		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		// Create a repository with a new environment
		repo := &entity.Repository{}
		repo.Name = "test-repo"
		autolinks := []entity.RepositoryAutolink{}
		repo.Spec.Autolinks = &autolinks
		local.repos["test-repo"] = repo

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		// Add the repository to remote without any environments
		remoteRepo := &GithubRepository{
			Name:           "test-repo",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
			Environments:   NewMockMappedEntityLazyLoader(map[string]*GithubEnvironment{}),
			Autolinks:      NewMockMappedEntityLazyLoader(map[string]*GithubAutolink{}),
		}
		remoteRepo.Autolinks.GetEntity()["TICKET-"] = &GithubAutolink{
			Id:             12345,
			KeyPrefix:      "TICKET-",
			UrlTemplate:    "https://example.com/TICKET?query=<num>",
			IsAlphanumeric: true,
		}
		remote.repos["test-repo"] = remoteRepo

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// Verify environment was added
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 12345, recorder.RepositoryAutolinkDeleted["test-repo"][0])
	})

	t.Run("happy path: not removing autolink", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()
		repoconf := config.RepositoryConfig{}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		// Create a repository with a new environment
		repo := &entity.Repository{}
		repo.Name = "test-repo"
		// let's set autolink to nil, to not manage it
		// autolinks := []entity.RepositoryAutolink{}
		repo.Spec.Autolinks = nil
		local.repos["test-repo"] = repo

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		// Add the repository to remote without any environments
		remoteRepo := &GithubRepository{
			Name:           "test-repo",
			ExternalUsers:  map[string]string{},
			BoolProperties: map[string]bool{},
			Environments:   NewMockMappedEntityLazyLoader(map[string]*GithubEnvironment{}),
			Autolinks:      NewMockMappedEntityLazyLoader(map[string]*GithubAutolink{}),
		}
		remoteRepo.Autolinks.GetEntity()["TICKET-"] = &GithubAutolink{
			Id:             12345,
			KeyPrefix:      "TICKET-",
			UrlTemplate:    "https://example.com/TICKET?query=<num>",
			IsAlphanumeric: true,
		}
		remote.repos["test-repo"] = remoteRepo

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// Verify environment was added
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.RepositoryAutolinkDeleted["test-repo"]))
	})

}

func TestReconciliationCustomProperties(t *testing.T) {
	t.Run("happy path: create new org custom property", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{
			OrgCustomProperties: []*config.GithubCustomProperty{
				{
					PropertyName:  "environment",
					ValueType:     "single_select",
					Required:      true,
					DefaultValue:  "production",
					Description:   "Production or development environment",
					AllowedValues: []string{"production", "development", "staging"},
				},
			},
		}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// Verify custom property was created
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 1, len(recorder.OrgCustomPropertyCreated))
		assert.NotNil(t, recorder.OrgCustomPropertyCreated["environment"])
		assert.Equal(t, "single_select", recorder.OrgCustomPropertyCreated["environment"].ValueType)
		assert.Equal(t, 0, len(recorder.OrgCustomPropertyUpdated))
		assert.Equal(t, 0, len(recorder.OrgCustomPropertyDeleted))
	})

	t.Run("happy path: update existing org custom property", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{
			OrgCustomProperties: []*config.GithubCustomProperty{
				{
					PropertyName:  "environment",
					ValueType:     "single_select",
					Required:      true,
					DefaultValue:  "staging", // Changed from production
					Description:   "Production or development environment",
					AllowedValues: []string{"production", "development", "staging"},
				},
			},
		}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		// Set up remote to have the existing property with old value
		existingProperty := &config.GithubCustomProperty{
			PropertyName:  "environment",
			ValueType:     "single_select",
			Required:      true,
			DefaultValue:  "production",
			Description:   "Production or development environment",
			AllowedValues: []string{"production", "development", "staging"},
		}

		// Pre-populate Created to simulate that the property was created in a previous reconciliation
		// This allows the tracking logic to correctly identify this as an update
		recorder.OrgCustomPropertyCreated["environment"] = existingProperty

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
			orgCustomProperties: map[string]*config.GithubCustomProperty{
				"environment": existingProperty,
			},
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// Verify custom property was updated
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 1, len(recorder.OrgCustomPropertyUpdated))
		assert.NotNil(t, recorder.OrgCustomPropertyUpdated["environment"])
		assert.Equal(t, "staging", recorder.OrgCustomPropertyUpdated["environment"].DefaultValue)
	})

	t.Run("happy path: delete org custom property", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{
			OrgCustomProperties: []*config.GithubCustomProperty{},
		}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		existingProperty := &config.GithubCustomProperty{
			PropertyName: "old-property",
			ValueType:    "string",
		}

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
			orgCustomProperties: map[string]*config.GithubCustomProperty{
				"old-property": existingProperty,
			},
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// Verify custom property was deleted
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 1, len(recorder.OrgCustomPropertyDeleted))
		assert.True(t, recorder.OrgCustomPropertyDeleted["old-property"])
	})

	t.Run("happy path: no changes when properties match", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		property := &config.GithubCustomProperty{
			PropertyName:  "environment",
			ValueType:     "single_select",
			Required:      true,
			DefaultValue:  "production",
			Description:   "Production or development environment",
			AllowedValues: []string{"production", "development", "staging"},
		}

		repoconf := config.RepositoryConfig{
			OrgCustomProperties: []*config.GithubCustomProperty{property},
		}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
			orgCustomProperties: map[string]*config.GithubCustomProperty{
				"environment": property,
			},
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// Verify no changes were made
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 0, len(recorder.OrgCustomPropertyCreated))
		assert.Equal(t, 0, len(recorder.OrgCustomPropertyUpdated))
		assert.Equal(t, 0, len(recorder.OrgCustomPropertyDeleted))
	})

	t.Run("happy path: multiple custom properties", func(t *testing.T) {
		recorder := NewReconciliatorListenerRecorder()

		repoconf := config.RepositoryConfig{
			OrgCustomProperties: []*config.GithubCustomProperty{
				{
					PropertyName:  "environment",
					ValueType:     "single_select",
					Required:      true,
					AllowedValues: []string{"production", "development"},
				},
				{
					PropertyName: "tier",
					ValueType:    "string",
					Required:     false,
					Description:  "Service tier classification",
				},
			},
		}

		r := NewGoliacReconciliatorImpl(false, recorder, &repoconf)

		local := GoliacLocalMock{
			users: make(map[string]*entity.User),
			teams: make(map[string]*entity.Team),
			repos: make(map[string]*entity.Repository),
		}

		remote := GoliacRemoteMock{
			users:      make(map[string]*GithubUser),
			teams:      make(map[string]*GithubTeam),
			repos:      make(map[string]*GithubRepository),
			teamsrepos: make(map[string]map[string]*GithubTeamRepo),
			rulesets:   make(map[string]*GithubRuleSet),
			appids:     make(map[string]*GithubApp),
		}

		localDatasource := NewGoliacReconciliatorDatasourceLocal(&local, "teams", "main", true, &repoconf)
		remoteDatasource := NewGoliacReconciliatorDatasourceRemote(&remote)

		logsCollector := observability.NewLogCollection()
		r.Reconciliate(context.TODO(), logsCollector, localDatasource, remoteDatasource, true, false, true, true)

		// Verify both custom properties were created
		assert.False(t, logsCollector.HasErrors())
		assert.Equal(t, 2, len(recorder.OrgCustomPropertyCreated))
		assert.NotNil(t, recorder.OrgCustomPropertyCreated["environment"])
		assert.NotNil(t, recorder.OrgCustomPropertyCreated["tier"])
		assert.Equal(t, 0, len(recorder.OrgCustomPropertyUpdated))
		assert.Equal(t, 0, len(recorder.OrgCustomPropertyDeleted))
	})
}
