package engine

import (
	"context"

	"github.com/goliac-project/goliac/internal/entity"
	"github.com/gosimple/slug"
)

type MutableEnvironmentLazyLoader struct {
	source MappedEntityLazyLoader[*GithubEnvironment]
	entity map[string]*GithubEnvironment
}

func NewMutableEnvironmentLazyLoader(source MappedEntityLazyLoader[*GithubEnvironment]) *MutableEnvironmentLazyLoader {
	return &MutableEnvironmentLazyLoader{source: source}
}

func (l *MutableEnvironmentLazyLoader) GetEntity() map[string]*GithubEnvironment {
	if l.entity == nil {
		if l.source == nil {
			l.entity = make(map[string]*GithubEnvironment)
		} else {
			e := l.source.GetEntity()
			l.entity = make(map[string]*GithubEnvironment)
			for k, v := range e {
				env := &GithubEnvironment{
					Name:      v.Name,
					Variables: make(map[string]string),
				}
				for k2, v2 := range v.Variables {
					env.Variables[k2] = v2
				}
				l.entity[k] = env
			}
		}
	}
	return l.entity
}

type MutableRepositoryVariableLazyLoader struct {
	source MappedEntityLazyLoader[string]
	entity map[string]string
}

func NewMutableRepositoryVariableLazyLoader(source MappedEntityLazyLoader[string]) *MutableRepositoryVariableLazyLoader {
	return &MutableRepositoryVariableLazyLoader{source: source}
}

func (l *MutableRepositoryVariableLazyLoader) GetEntity() map[string]string {
	if l.entity == nil {
		if l.source == nil {
			l.entity = make(map[string]string)
		} else {
			e := l.source.GetEntity()
			l.entity = make(map[string]string)
			for k, v := range e {
				l.entity[k] = v
			}
		}
	}
	return l.entity
}

/*
 * MutableGoliacRemoteImpl is used by GoliacReconciliatorImpl to update
 * the internal status of Github representation before appyling it for real
 * (or running in drymode)
 */
type MutableGoliacRemoteImpl struct {
	users          map[string]string
	repositories   map[string]*GithubRepository
	teams          map[string]*GithubTeam
	teamRepos      map[string]map[string]*GithubTeamRepo
	teamSlugByName map[string]string
	rulesets       map[string]*GithubRuleSet
	appIds         map[string]int
	remote         GoliacRemote
}

func NewMutableGoliacRemoteImpl(ctx context.Context, remote GoliacRemote) *MutableGoliacRemoteImpl {
	rUsers := make(map[string]string)
	for k, v := range remote.Users(ctx) {
		rUsers[k] = v
	}
	rTeamSlugByName := make(map[string]string)
	for k, v := range remote.TeamSlugByName(ctx) {
		rTeamSlugByName[k] = v
	}
	rTeams := make(map[string]*GithubTeam)
	for k, v := range remote.Teams(ctx, false) {
		ght := *v
		rTeams[k] = &ght
	}

	rRepositories := make(map[string]*GithubRepository)
	for k, v := range remote.Repositories(ctx) {
		ghr := *v
		ghrulesets := make(map[string]*GithubRuleSet)
		for k, v := range ghr.RuleSets {
			ghrulesets[k] = v
		}
		ghr.RuleSets = ghrulesets
		ghbranchprotections := make(map[string]*GithubBranchProtection)
		for k, v := range v.BranchProtections {
			ghbranchprotections[k] = v
		}
		ghr.BranchProtections = ghbranchprotections
		ghr.Environments = NewMutableEnvironmentLazyLoader(
			v.Environments,
		)
		ghr.RepositoryVariables = NewMutableRepositoryVariableLazyLoader(
			v.RepositoryVariables,
		)
		rRepositories[k] = &ghr
		ghExternalUsers := make(map[string]string)
		for k, v := range v.ExternalUsers {
			ghExternalUsers[k] = v
		}
		rRepositories[k].ExternalUsers = ghExternalUsers
		ghInternalUsers := make(map[string]string)
		for k, v := range v.InternalUsers {
			ghInternalUsers[k] = v
		}
		rRepositories[k].InternalUsers = ghInternalUsers
		ghProperties := make(map[string]bool)
		for k, v := range v.BoolProperties {
			ghProperties[k] = v
		}
		rRepositories[k].BoolProperties = ghProperties
	}

	rTeamRepositories := make(map[string]map[string]*GithubTeamRepo)
	for k1, v1 := range remote.TeamRepositories(ctx) {
		repos := make(map[string]*GithubTeamRepo)
		for k2, v2 := range v1 {
			gtr := *v2
			repos[k2] = &gtr
		}
		rTeamRepositories[k1] = repos
	}

	rulesets := make(map[string]*GithubRuleSet)
	for k, v := range remote.RuleSets(ctx) {
		ghRuleset := *v
		ghRuleset.Rules = make(map[string]entity.RuleSetParameters)
		for k, v := range v.Rules {
			ghRuleset.Rules[k] = v
		}
		ghRuleset.Repositories = make([]string, len(v.Repositories))
		copy(ghRuleset.Repositories, v.Repositories)
		ghRuleset.OnInclude = make([]string, len(v.OnInclude))
		copy(ghRuleset.OnInclude, v.OnInclude)
		ghRuleset.OnExclude = make([]string, len(v.OnExclude))
		copy(ghRuleset.OnExclude, v.OnExclude)
		ghRuleset.BypassApps = make(map[string]string)
		for k, v := range v.BypassApps {
			ghRuleset.BypassApps[k] = v
		}
		ghRuleset.BypassTeams = make(map[string]string)
		for k, v := range v.BypassTeams {
			ghRuleset.BypassTeams[k] = v
		}
		rulesets[k] = &ghRuleset
	}

	appids := make(map[string]int)
	for k, v := range remote.AppIds(ctx) {
		appids[k] = v
	}

	return &MutableGoliacRemoteImpl{
		users:          rUsers,
		repositories:   rRepositories,
		teams:          rTeams,
		teamRepos:      rTeamRepositories,
		teamSlugByName: rTeamSlugByName,
		rulesets:       rulesets,
		appIds:         appids,
		remote:         remote,
	}
}

func (m *MutableGoliacRemoteImpl) Users() map[string]string {
	return m.users
}

func (m *MutableGoliacRemoteImpl) TeamSlugByName() map[string]string {
	return m.teamSlugByName
}

func (m *MutableGoliacRemoteImpl) Teams() map[string]*GithubTeam {
	return m.teams
}
func (m *MutableGoliacRemoteImpl) Repositories() map[string]*GithubRepository {
	return m.repositories
}
func (m *MutableGoliacRemoteImpl) TeamRepositories() map[string]map[string]*GithubTeamRepo {
	return m.teamRepos
}
func (m *MutableGoliacRemoteImpl) RuleSets() map[string]*GithubRuleSet {
	return m.rulesets
}
func (g *MutableGoliacRemoteImpl) AppIds() map[string]int {
	return g.appIds
}

// LISTENER

func (m *MutableGoliacRemoteImpl) AddUserToOrg(ghuserid string) {
	m.users[ghuserid] = ghuserid
}

func (m *MutableGoliacRemoteImpl) RemoveUserFromOrg(ghuserid string) {
	delete(m.users, ghuserid)
}

func (m *MutableGoliacRemoteImpl) CreateTeam(teamname string, description string, members []string) {
	teamslug := slug.Make(teamname)
	t := GithubTeam{
		Name:        teamname,
		Slug:        teamslug,
		Members:     members,
		Maintainers: []string{},
	}
	m.teams[teamslug] = &t
	m.teamSlugByName[teamname] = teamslug
}
func (m *MutableGoliacRemoteImpl) UpdateTeamAddMember(teamslug string, username string, role string) {
	if t, ok := m.teams[teamslug]; ok {
		t.Members = append(t.Members, username)
	}
}
func (m *MutableGoliacRemoteImpl) UpdateTeamRemoveMember(teamslug string, username string) {
	if t, ok := m.teams[teamslug]; ok {
		for i, m := range t.Members {
			if m == username {
				t.Members = append(t.Members[:i], t.Members[i+1:]...)
				return
			}
		}
	}
}
func (m *MutableGoliacRemoteImpl) UpdateTeamUpdateMember(teamslug string, username string, role string) {
	if role == "maintainer" {
		if t, ok := m.teams[teamslug]; ok {
			for i, m := range t.Members {
				if m == username {
					t.Members = append(t.Members[:i], t.Members[i+1:]...)
					t.Maintainers = append(t.Maintainers, username)
					return
				}
			}
		}
	} else { // "member"
		if t, ok := m.teams[teamslug]; ok {
			for i, m := range t.Maintainers {
				if m == username {
					t.Maintainers = append(t.Maintainers[:i], t.Maintainers[i+1:]...)
					t.Members = append(t.Members, username)
					return
				}
			}
		}
	}
}
func (m *MutableGoliacRemoteImpl) UpdateTeamSetParent(ctx context.Context, dryrun bool, teamslug string, parentTeam *int) {
	if t, ok := m.teams[teamslug]; ok {
		t.ParentTeam = parentTeam
	}
}
func (m *MutableGoliacRemoteImpl) DeleteTeam(teamslug string) {
	if t, ok := m.teams[teamslug]; ok {
		teamname := t.Name
		delete(m.teams, teamslug)
		delete(m.teamSlugByName, teamname)
		delete(m.teamRepos, teamslug)
	}
}
func (m *MutableGoliacRemoteImpl) CreateRepository(reponame string, descrition string, visibility string, writers []string, readers []string, boolProperties map[string]bool, defaultBranch string, forkFrom string) {
	r := GithubRepository{
		Name:              reponame,
		Visibility:        visibility,
		BoolProperties:    boolProperties,
		ExternalUsers:     map[string]string{},
		DefaultBranchName: defaultBranch,
		IsFork:            forkFrom != "",
	}
	m.repositories[reponame] = &r
}
func (m *MutableGoliacRemoteImpl) UpdateRepositoryAddTeamAccess(reponame string, teamslug string, permission string) {
	if tr, ok := m.teamRepos[teamslug]; ok {
		tr[reponame] = &GithubTeamRepo{
			Name:       reponame,
			Permission: permission,
		}
	}
}

func (m *MutableGoliacRemoteImpl) UpdateRepositoryUpdateTeamAccess(reponame string, teamslug string, permission string) {
	if tr, ok := m.teamRepos[teamslug]; ok {
		if r, ok := tr[reponame]; ok {
			r.Permission = permission
		}
	}
}
func (m *MutableGoliacRemoteImpl) UpdateRepositoryRemoveTeamAccess(reponame string, teamslug string) {
	if tr, ok := m.teamRepos[teamslug]; ok {
		delete(tr, reponame)
	}
}
func (m *MutableGoliacRemoteImpl) DeleteRepository(reponame string) {
	delete(m.repositories, reponame)
}

func (m *MutableGoliacRemoteImpl) RenameRepository(reponame string, newname string) {
	r := m.repositories[reponame]

	// it is not supposed to be nil
	if r == nil {
		return
	}
	delete(m.repositories, reponame)
	r.Name = newname
	m.repositories[newname] = r

	for _, tr := range m.teamRepos {
		for rname, r := range tr {
			if rname == reponame {
				delete(tr, rname)
				r.Name = newname
				tr[newname] = r
			}
		}
	}
}

/*
UpdateRepositoryUpdateBoolProperty is used for
- visibility (string)
- archived (bool)
- allow_auto_merge (bool)
- delete_branch_on_merge (bool)
- allow_update_branch (bool)
*/
func (m *MutableGoliacRemoteImpl) UpdateRepositoryUpdateProperty(reponame string, propertyName string, propertyValue interface{}) {
	if r, ok := m.repositories[reponame]; ok {
		if propertyName == "visibility" {
			r.Visibility = propertyValue.(string)
		} else if propertyName == "default_branch" {
			r.DefaultBranchName = propertyValue.(string)
		} else {
			r.BoolProperties[propertyName] = propertyValue.(bool)
		}
	}
}
func (m *MutableGoliacRemoteImpl) UpdateRepositorySetExternalUser(reponame string, collaboatorGithubId string, permission string) {
	if r, ok := m.repositories[reponame]; ok {
		r.ExternalUsers[collaboatorGithubId] = permission
	}
}
func (m *MutableGoliacRemoteImpl) UpdateRepositoryRemoveExternalUser(reponame string, collaboatorGithubId string) {
	if r, ok := m.repositories[reponame]; ok {
		delete(r.ExternalUsers, collaboatorGithubId)
	}
}
func (m *MutableGoliacRemoteImpl) UpdateRepositoryRemoveInternalUser(reponame string, collaboatorGithubId string) {
	if r, ok := m.repositories[reponame]; ok {
		delete(r.InternalUsers, collaboatorGithubId)
	}
}
func (m *MutableGoliacRemoteImpl) AddRepositoryRuleset(reponame string, ruleset *GithubRuleSet) {
	if r, ok := m.repositories[reponame]; ok {
		r.RuleSets[ruleset.Name] = ruleset
	}
}
func (m *MutableGoliacRemoteImpl) UpdateRepositoryRuleset(reponame string, ruleset *GithubRuleSet) {
	if r, ok := m.repositories[reponame]; ok {
		r.RuleSets[ruleset.Name] = ruleset
	}
}
func (m *MutableGoliacRemoteImpl) DeleteRepositoryRuleset(reponame string, rulesetid int) {
	if r, ok := m.repositories[reponame]; ok {
		for _, rs := range r.RuleSets {
			if rs.Id == rulesetid {
				delete(r.RuleSets, rs.Name)
				break
			}
		}
	}
}

func (m *MutableGoliacRemoteImpl) AddRuleset(ruleset *GithubRuleSet) {
	m.rulesets[ruleset.Name] = ruleset
}
func (m *MutableGoliacRemoteImpl) UpdateRuleset(ruleset *GithubRuleSet) {
	m.rulesets[ruleset.Name] = ruleset
}
func (m *MutableGoliacRemoteImpl) DeleteRuleset(rulesetid int) {
	for _, rs := range m.rulesets {
		if rs.Id == rulesetid {
			delete(m.rulesets, rs.Name)
			break
		}
	}
}

func (m *MutableGoliacRemoteImpl) AddRepositoryEnvironment(repositoryName string, environmentName string) {
	if r, ok := m.repositories[repositoryName]; ok {
		r.Environments.GetEntity()[environmentName] = &GithubEnvironment{
			Name:      environmentName,
			Variables: map[string]string{},
		}
	}
}
func (m *MutableGoliacRemoteImpl) DeleteRepositoryEnvironment(repositoryName string, environmentName string) {
	delete(m.repositories[repositoryName].Environments.GetEntity(), environmentName)
}

// Repository variables management
func (m *MutableGoliacRemoteImpl) AddRepositoryVariable(repositoryName string, variableName string, variableValue string) {
	if r, ok := m.repositories[repositoryName]; ok {
		r.RepositoryVariables.GetEntity()[variableName] = variableValue
	}
}
func (m *MutableGoliacRemoteImpl) UpdateRepositoryVariable(repositoryName string, variableName string, variableValue string) {
	if r, ok := m.repositories[repositoryName]; ok {
		r.RepositoryVariables.GetEntity()[variableName] = variableValue
	}
}

func (m *MutableGoliacRemoteImpl) DeleteRepositoryVariable(repositoryName string, variableName string) {
	delete(m.repositories[repositoryName].RepositoryVariables.GetEntity(), variableName)
}

// Environment variables management
func (m *MutableGoliacRemoteImpl) AddRepositoryEnvironmentVariable(repositoryName string, environmentName string, variableName string, variableValue string) {
	if r, ok := m.repositories[repositoryName]; ok {
		if r.Environments.GetEntity()[environmentName] == nil {
			r.Environments.GetEntity()[environmentName] = &GithubEnvironment{
				Name:      environmentName,
				Variables: map[string]string{},
			}
		}
		r.Environments.GetEntity()[environmentName].Variables[variableName] = variableValue
	}
}
func (m *MutableGoliacRemoteImpl) UpdateRepositoryEnvironmentVariable(repositoryName string, environmentName string, variableName string, variableValue string) {
	if r, ok := m.repositories[repositoryName]; ok {
		if r.Environments.GetEntity()[environmentName] == nil {
			r.Environments.GetEntity()[environmentName] = &GithubEnvironment{
				Name:      environmentName,
				Variables: map[string]string{},
			}
		}
		r.Environments.GetEntity()[environmentName].Variables[variableName] = variableValue
	}
}
func (m *MutableGoliacRemoteImpl) DeleteRepositoryEnvironmentVariable(repositoryName string, environmentName string, variableName string) {
	if r, ok := m.repositories[repositoryName]; ok {
		if r.Environments.GetEntity()[environmentName] != nil {
			delete(r.Environments.GetEntity()[environmentName].Variables, variableName)
		}
	}
}
func (m *MutableGoliacRemoteImpl) AddRepositoryAutolink(repositoryName string, autolink *GithubAutolink) {
	if r, ok := m.repositories[repositoryName]; ok {
		r.Autolinks.GetEntity()[autolink.KeyPrefix] = autolink
	}
}
func (m *MutableGoliacRemoteImpl) DeleteRepositoryAutolink(repositoryName string, autolinkId int) {
	for key, autolink := range m.repositories[repositoryName].Autolinks.GetEntity() {
		if autolink.Id == autolinkId {
			delete(m.repositories[repositoryName].Autolinks.GetEntity(), key)
			break
		}
	}
}
func (m *MutableGoliacRemoteImpl) UpdateRepositoryAutolink(repositoryName string, autolink *GithubAutolink) {
	if r, ok := m.repositories[repositoryName]; ok {
		r.Autolinks.GetEntity()[autolink.KeyPrefix] = autolink
	}
}
