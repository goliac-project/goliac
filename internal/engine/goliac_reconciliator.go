package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/goliac-project/goliac/internal/config"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/goliac-project/goliac/internal/utils"
)

type UnmanagedResources struct {
	Users                  map[string]bool
	ExternallyManagedTeams map[string]bool
	Teams                  map[string]bool
	Repositories           map[string]bool
	RuleSets               map[string]bool
}

/*
 * GoliacReconciliator is here to sync the local state to the remote state
 */
type GoliacReconciliator interface {
	Reconciliate(ctx context.Context, logsCollector *observability.LogCollection, local GoliacReconciliatorDatasource, remote GoliacReconciliatorDatasource, isEnterprise bool, dryrun bool, manageGithubVariables bool, manageGithubAutolinks bool) (*UnmanagedResources, map[string]*GithubRepoComparable, map[string]string, error)
}

type GoliacReconciliatorImpl struct {
	executor            ReconciliatorExecutor
	reconciliatorFilter ReconciliatorFilter
	repoconfig          *config.RepositoryConfig
	unmanaged           *UnmanagedResources
}

func NewGoliacReconciliatorImpl(isEntreprise bool, executor ReconciliatorExecutor, repoconfig *config.RepositoryConfig) GoliacReconciliator {
	return &GoliacReconciliatorImpl{
		executor:            executor,
		reconciliatorFilter: NewReconciliatorFilter(isEntreprise, repoconfig),
		repoconfig:          repoconfig,
		unmanaged:           nil,
	}
}

// normalizePropertyValue converts a property value to a normalized string for comparison
// Handles strings, numbers, arrays, and nil
func normalizePropertyValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case string:
		return v
	case int:
		return fmt.Sprintf("%d", v)
	case []interface{}:
		// For arrays, convert each element to string and join
		strs := make([]string, len(v))
		for i, elem := range v {
			n := normalizePropertyValue(elem)
			switch n := n.(type) {
			case string:
				strs[i] = n
			case int:
				strs[i] = fmt.Sprintf("%d", n)
			case []string:
				strs[i] = strings.Join(n, ",")
			}
		}
		return strings.Join(strs, ",")
	case []string:
		return strings.Join(v, ",")
	default:
		// For other types (including float64 from YAML unmarshaling), convert to string
		// YAML may unmarshal numbers as float64, so handle that case
		if f, ok := v.(float64); ok {
			// Convert float64 to int string if it's a whole number
			if f == float64(int(f)) {
				return fmt.Sprintf("%d", int(f))
			}
			return fmt.Sprintf("%g", f)
		}
		return fmt.Sprintf("%v", v)
	}
}

func (r *GoliacReconciliatorImpl) Reconciliate(ctx context.Context, logsCollector *observability.LogCollection, local GoliacReconciliatorDatasource, remote GoliacReconciliatorDatasource, isEnterprise bool, dryrun bool, manageGithubVariables bool, manageGithubAutolinks bool) (*UnmanagedResources, map[string]*GithubRepoComparable, map[string]string, error) {
	rremote, err := NewMutableGoliacRemoteImpl(ctx, remote)
	if err != nil {
		return nil, nil, nil, err
	}
	r.Begin(ctx, logsCollector, dryrun)
	unmanaged := &UnmanagedResources{
		Users:                  make(map[string]bool),
		ExternallyManagedTeams: make(map[string]bool),
		Teams:                  make(map[string]bool),
		Repositories:           make(map[string]bool),
		RuleSets:               make(map[string]bool),
	}
	r.unmanaged = unmanaged

	err = r.reconciliateUsers(ctx, logsCollector, local, rremote, dryrun)
	if err != nil {
		r.Rollback(ctx, logsCollector, dryrun, err)
		return nil, nil, nil, err
	}

	err = r.reconciliateTeams(ctx, logsCollector, local, rremote, dryrun)
	if err != nil {
		r.Rollback(ctx, logsCollector, dryrun, err)
		return nil, nil, nil, err
	}

	reposToArchive, reposToRename, err := r.reconciliateRepositories(ctx, logsCollector, local, rremote, dryrun, manageGithubVariables, manageGithubAutolinks)
	if err != nil {
		r.Rollback(ctx, logsCollector, dryrun, err)
		return nil, nil, nil, err
	}

	if isEnterprise {
		err = r.reconciliateRulesets(ctx, logsCollector, local, rremote, r.repoconfig, dryrun)
		if err != nil {
			r.Rollback(ctx, logsCollector, dryrun, err)
			return nil, nil, nil, err
		}
	}

	err = r.reconciliateOrgCustomProperties(ctx, logsCollector, rremote, r.repoconfig, dryrun)
	if err != nil {
		r.Rollback(ctx, logsCollector, dryrun, err)
		return nil, nil, nil, err
	}

	return r.unmanaged, reposToArchive, reposToRename, r.Commit(ctx, logsCollector, dryrun)
}

/*
 * This function sync teams and team's members
 */
func (r *GoliacReconciliatorImpl) reconciliateUsers(ctx context.Context, logsCollector *observability.LogCollection, local GoliacReconciliatorDatasource, remote *MutableGoliacRemoteImpl, dryrun bool) error {
	ghUsers := remote.Users()

	rUsers := make(map[string]string)
	for rUser, membership := range ghUsers {
		rUsers[rUser] = membership
	}

	for _, lUser := range local.Users() {
		_, ok := rUsers[lUser]
		if !ok {
			// deal with non existing remote user
			r.AddUserToOrg(ctx, logsCollector, dryrun, remote, lUser)
		} else {
			delete(rUsers, lUser)
		}
	}

	// remaining (GH) users (aka not found locally)
	for rUser := range rUsers {
		// DELETE User
		r.RemoveUserFromOrg(ctx, logsCollector, dryrun, remote, rUser)
	}
	return nil
}

type GithubTeamComparable struct {
	Name              string
	Slug              string
	Members           []string
	Maintainers       []string // in Github, there are 2 types of roles in a team: maintainers and members. We will remove maintainers from the team
	ExternallyManaged bool
	ParentTeam        *string
	// not comparable
	Id int // only on remote object
}

/*
This function sync teams and team's members,
*/
func (r *GoliacReconciliatorImpl) reconciliateTeams(ctx context.Context, logsCollector *observability.LogCollection, local GoliacReconciliatorDatasource, remote *MutableGoliacRemoteImpl, dryrun bool) error {
	lTeams, _, err := local.Teams()
	if err != nil {
		return err
	}

	rTeams := remote.Teams()

	// we need to populate -goliac-owners teams with the owners of the team
	// if the team is externally managed
	for slugteamname, team := range rTeams {
		if !strings.HasSuffix(slugteamname, config.Config.GoliacTeamOwnerSuffix) {
			// regular team
			if regularTeam, ok := lTeams[slugteamname]; ok {
				if regularTeam.ExternallyManaged {
					regularTeam.Members = append(regularTeam.Members, team.Members...)

					// let's search for the -goliac-owners team
					if ownersTeam, ok := lTeams[slugteamname+config.Config.GoliacTeamOwnerSuffix]; ok {
						ownersTeam.Members = append(ownersTeam.Members, team.Members...)
					}
				}
			}
		}
	}

	// now we compare local and remote

	compareTeam := func(teamname string, lTeam *GithubTeamComparable, rTeam *GithubTeamComparable) bool {
		if !lTeam.ExternallyManaged {
			if res, _, _ := entity.StringArrayEquivalent(lTeam.Members, rTeam.Members); !res {
				return false
			}
			if res, _, _ := entity.StringArrayEquivalent(lTeam.Maintainers, rTeam.Maintainers); !res {
				return false
			}
		}
		if (lTeam.ParentTeam == nil && rTeam.ParentTeam != nil) ||
			(lTeam.ParentTeam != nil && rTeam.ParentTeam == nil) ||
			(lTeam.ParentTeam != nil && rTeam.ParentTeam != nil && *lTeam.ParentTeam != *rTeam.ParentTeam) {
			return false
		}

		return true
	}

	onAdded := func(key string, lTeam *GithubTeamComparable, rTeam *GithubTeamComparable) {
		// CREATE team

		// it is possible that parent team will be added in 2 pass
		var parentTeam *int
		if lTeam.ParentTeam != nil && rTeams[*lTeam.ParentTeam] != nil {
			parentTeam = &rTeams[*lTeam.ParentTeam].Id
		}
		r.CreateTeam(ctx, logsCollector, dryrun, remote, lTeam.Name, lTeam.Name, parentTeam, lTeam.Members)
	}

	onRemoved := func(key string, lTeam *GithubTeamComparable, rTeam *GithubTeamComparable) {
		// DELETE team
		r.DeleteTeam(ctx, logsCollector, dryrun, remote, rTeam.Slug)
	}

	onChanged := func(slugTeam string, lTeam *GithubTeamComparable, rTeam *GithubTeamComparable) {
		// change membership from maintainers to members
		if !lTeam.ExternallyManaged {
			rmaintainers := make([]string, len(rTeam.Maintainers))
			copy(rmaintainers, rTeam.Maintainers)

			for _, r_maintainer := range rmaintainers {
				found := false
				for _, l_maintainer := range lTeam.Maintainers {
					if r_maintainer == l_maintainer {
						found = true
						break
					}
				}
				if !found {
					// let's downgrade the maintainer to member
					r.UpdateTeamChangeMaintainerToMember(ctx, logsCollector, dryrun, remote, slugTeam, r_maintainer)
					for i, m := range rTeam.Maintainers {
						if m == r_maintainer {
							rTeam.Maintainers = append(rTeam.Maintainers[:i], rTeam.Maintainers[i+1:]...)
							break
						}
					}
					rTeam.Members = append(rTeam.Members, r_maintainer)
				}
			}

			// membership change
			if res, _, _ := entity.StringArrayEquivalent(lTeam.Members, rTeam.Members); !res {
				remoteMembersSnapshot := make([]string, len(rTeam.Members))
				copy(remoteMembersSnapshot, rTeam.Members)
				localMembersSnapshot := make([]string, len(lTeam.Members))
				copy(localMembersSnapshot, lTeam.Members)

				localMembers := make(map[string]bool)
				for _, m := range localMembersSnapshot {
					localMembers[m] = true
				}
				remoteMembers := make(map[string]bool)
				for _, m := range remoteMembersSnapshot {
					remoteMembers[m] = true
				}

				membersToRemove := make([]string, 0)
				for _, m := range remoteMembersSnapshot {
					if _, ok := localMembers[m]; !ok {
						membersToRemove = append(membersToRemove, m)
					}
				}

				membersToAdd := make([]string, 0)
				for _, m := range localMembersSnapshot {
					if _, ok := remoteMembers[m]; !ok {
						membersToAdd = append(membersToAdd, m)
					}
				}
				sort.Strings(membersToRemove)
				sort.Strings(membersToAdd)

				for _, m := range membersToRemove {
					// REMOVE team member
					r.UpdateTeamRemoveMember(ctx, logsCollector, dryrun, remote, slugTeam, m)
				}

				for _, m := range membersToAdd {
					// ADD team member
					r.UpdateTeamAddMember(ctx, logsCollector, dryrun, remote, slugTeam, m, "member")
				}
			}
		}

		// parent team change
		if (lTeam.ParentTeam == nil && rTeam.ParentTeam != nil) ||
			(lTeam.ParentTeam != nil && rTeam.ParentTeam == nil) ||
			(lTeam.ParentTeam != nil && rTeam.ParentTeam != nil && *lTeam.ParentTeam != *rTeam.ParentTeam) {

			var parentTeam *int
			parentTeamName := ""
			if lTeam.ParentTeam != nil && rTeams[*lTeam.ParentTeam] != nil {
				parentTeam = &rTeams[*lTeam.ParentTeam].Id
				parentTeamName = *lTeam.ParentTeam
			}

			r.UpdateTeamSetParent(ctx, logsCollector, dryrun, remote, slugTeam, parentTeam, parentTeamName)
		}
	}

	CompareEntities(lTeams, rTeams, compareTeam, onAdded, onRemoved, onChanged)

	return nil
}

type GithubRepoComparable struct {
	Visibility                 string
	BoolProperties             map[string]bool
	Writers                    []string
	Readers                    []string
	ExternalUserReaders        []string // githubids
	ExternalUserWriters        []string // githubids
	InternalUsers              []string // githubids
	Rulesets                   map[string]*GithubRuleSet
	BranchProtections          map[string]*GithubBranchProtection
	DefaultBranchName          string
	ActionVariables            MappedEntityLazyLoader[string]
	Environments               MappedEntityLazyLoader[*GithubEnvironment]
	Autolinks                  MappedEntityLazyLoader[*GithubAutolink]
	DefaultMergeCommitMessage  string
	DefaultSquashCommitMessage string
	CustomProperties           map[string]interface{} // [propertyName]propertyValue (string or []string)
	Topics                     []string               // repository topics
	// not comparable
	IsFork   bool
	ForkFrom string
}

type GithubEnvironment struct {
	Name      string
	Variables map[string]string
}

type GithubAutolink struct {
	Id             int
	KeyPrefix      string
	UrlTemplate    string
	IsAlphanumeric bool
}

/*
This function sync repositories and team's repositories permissions
It returns the list of deleted repos that must not be deleted but archived

It returns:
- reposToArchive: list of repos that must not be deleted but archived
- reposToRename: list of repos that must be renamed
- err: error if any
*/
func (r *GoliacReconciliatorImpl) reconciliateRepositories(
	ctx context.Context,
	logsCollector *observability.LogCollection,
	local GoliacReconciliatorDatasource,
	remote *MutableGoliacRemoteImpl,
	dryrun bool,
	manageGithubVariables bool,
	manageGithubAutolinks bool,
) (map[string]*GithubRepoComparable, map[string]string, error) {

	reposToArchive := make(map[string]*GithubRepoComparable)
	reposToRename := make(map[string]string)

	// let's start with the local cloned github-teams repo
	lRepos, toRename, err := local.Repositories()
	if err != nil {
		return reposToArchive, reposToRename, err
	}

	for reponame, renameTo := range toRename {

		r.RenameRepository(ctx, logsCollector, dryrun, remote, reponame, renameTo)

		// in the post action we have to also update the git repository
		lRepos[renameTo] = lRepos[reponame]
		delete(lRepos, reponame)
		reposToRename[reponame] = renameTo
	}

	// let's get the remote now
	rRepos := remote.Repositories()

	// now we compare local (slugTeams) and remote (rTeams)

	compareRepos := func(reponame string, lRepo *GithubRepoComparable, rRepo *GithubRepoComparable) bool {
		archived := lRepo.BoolProperties["archived"]
		if !archived {
			//
			// "nested" rulesets comparison
			//
			onRulesetAdded := func(rulename string, lRuleset *GithubRuleSet, rRuleset *GithubRuleSet) {
				// CREATE repo ruleset
				r.AddRepositoryRuleset(ctx, logsCollector, dryrun, reponame, lRuleset)
			}
			onRulesetRemoved := func(rulename string, lRuleset *GithubRuleSet, rRuleset *GithubRuleSet) {
				// DELETE repo ruleset
				r.DeleteRepositoryRuleset(ctx, logsCollector, dryrun, reponame, rRuleset)
			}
			onRulesetChange := func(rulename string, lRuleset *GithubRuleSet, rRuleset *GithubRuleSet) {
				// UPDATE ruleset
				lRuleset.Id = rRuleset.Id
				r.UpdateRepositoryRuleset(ctx, logsCollector, dryrun, reponame, lRuleset)
			}
			CompareEntities(lRepo.Rulesets, rRepo.Rulesets, compareRulesets, onRulesetAdded, onRulesetRemoved, onRulesetChange)

			//
			// "nested" branchprotections comparison
			//
			onBranchProtectionAdded := func(rulename string, lBp *GithubBranchProtection, rBp *GithubBranchProtection) {
				// CREATE repo branchprotection
				r.AddRepositoryBranchProtection(ctx, logsCollector, dryrun, reponame, lBp)
			}
			onBranchProtectionRemoved := func(rulename string, lBp *GithubBranchProtection, rBp *GithubBranchProtection) {
				// DELETE repo branchprotection
				r.DeleteRepositoryBranchProtection(ctx, logsCollector, dryrun, reponame, rBp)
			}
			onBranchProtectionChange := func(rulename string, lBp *GithubBranchProtection, rBp *GithubBranchProtection) {
				// UPDATE branchprotection
				lBp.Id = rBp.Id
				r.UpdateRepositoryBranchProtection(ctx, logsCollector, dryrun, reponame, lBp)
			}
			CompareEntities(lRepo.BranchProtections, rRepo.BranchProtections, compareBranchProtections, onBranchProtectionAdded, onBranchProtectionRemoved, onBranchProtectionChange)

			if manageGithubVariables {
				//
				// "nested" environments comparison
				//
				onEnvironmentAdded := func(environment string, lEnv *GithubEnvironment, rEnv *GithubEnvironment) {
					// CREATE repo environment
					r.AddRepositoryEnvironment(ctx, logsCollector, dryrun, remote, reponame, environment)
				}
				onEnvironmentChange := func(environment string, lEnv *GithubEnvironment, rEnv *GithubEnvironment) {
					// UPDATE repo environment

					// Check for removed or changed keys
					for name, value := range lEnv.Variables {
						if rValue, ok := rEnv.Variables[name]; !ok {
							r.AddRepositoryEnvironmentVariable(ctx, logsCollector, dryrun, remote, reponame, environment, name, value)
						} else if rValue != value {
							r.UpdateRepositoryEnvironmentVariable(ctx, logsCollector, dryrun, remote, reponame, environment, name, value)
						}
					}

					// Check for added keys
					for name := range rEnv.Variables {
						if _, ok := lEnv.Variables[name]; !ok {
							r.DeleteRepositoryEnvironmentVariable(ctx, logsCollector, dryrun, remote, reponame, environment, name)
						}
					}
				}
				onEnvironmentRemoved := func(environment string, lEnv *GithubEnvironment, rEnv *GithubEnvironment) {
					// DELETE repo environment
					r.DeleteRepositoryEnvironment(ctx, logsCollector, dryrun, remote, reponame, environment)
				}
				CompareEntities(lRepo.Environments.GetEntity(), rRepo.Environments.GetEntity(), compareEnvironments, onEnvironmentAdded, onEnvironmentRemoved, onEnvironmentChange)
			}

			//
			// Reconcile custom properties
			//
			if lRepo.CustomProperties != nil || rRepo.CustomProperties != nil {
				// first let's remove the custom properties that are not defined in the organization custom properties
				var localCustomProperties map[string]interface{}
				if lRepo.CustomProperties != nil {
					localCustomProperties = make(map[string]interface{})
					for propName, localValue := range lRepo.CustomProperties {
						// check if the property is defined in the organization custom properties
						found := false
						for _, orgProp := range r.repoconfig.OrgCustomProperties {
							if orgProp.PropertyName == propName {
								found = true
								break
							}
						}
						if found {
							normalizedValue := normalizePropertyValue(localValue)
							// bug (or feature?) an empty string is not saved in Github
							if normalizedValue != "" {
								localCustomProperties[propName] = normalizedValue
							}
						} else {
							logsCollector.AddWarn(fmt.Errorf("custom property %s is defined in the repository %s but not in the organization custom properties", propName, reponame))
						}
					}
					// we add the default values for the custom properties that are not defined in the repository custom properties
					for _, orgProperty := range r.repoconfig.OrgCustomProperties {
						if _, ok := localCustomProperties[orgProperty.PropertyName]; !ok {
							if orgProperty.DefaultValue != "" {
								localCustomProperties[orgProperty.PropertyName] = normalizePropertyValue(orgProperty.DefaultValue)
							}
						}
					}
				}

				// if the custom properties are different, we need to update the remote repository
				if !utils.DeepEqualUnordered(localCustomProperties, rRepo.CustomProperties) {
					remoteProperties := make(map[string]interface{})
					if rRepo.CustomProperties != nil {
						for propName, remoteValue := range rRepo.CustomProperties {
							remoteProperties[propName] = normalizePropertyValue(remoteValue)
						}
					}
					localProperties := make(map[string]interface{})
					for propName, localValue := range localCustomProperties {
						localProperties[propName] = normalizePropertyValue(localValue)
					}
					// check first for added or updated properties
					for propName, localValue := range localProperties {
						remoteValue, exists := remoteProperties[propName]
						if !exists || !utils.DeepEqualUnordered(localValue, remoteValue) {
							r.UpdateRepositoryCustomProperties(ctx, logsCollector, dryrun, remote, reponame, propName, localValue)
						}
						delete(remoteProperties, propName)
					}
					// check for removed properties
					for propName := range remoteProperties {
						r.UpdateRepositoryCustomProperties(ctx, logsCollector, dryrun, remote, reponame, propName, nil)
					}
				}
			}

			if manageGithubAutolinks {
				// nested autolinks comparison IF it is defined locally
				if lRepo.Autolinks != nil {
					onAutolinkAdded := func(autolinkprefix string, lal *GithubAutolink, ral *GithubAutolink) {
						r.AddRepositoryAutolink(ctx, logsCollector, dryrun, remote, reponame, lal)
					}
					onAutolinkRemoved := func(autolinkprefix string, lal *GithubAutolink, ral *GithubAutolink) {
						r.DeleteRepositoryAutolink(ctx, logsCollector, dryrun, remote, reponame, ral.Id)
					}
					onAutolinkChange := func(autolinkname string, lal *GithubAutolink, ral *GithubAutolink) {
						r.UpdateRepositoryAutolink(ctx, logsCollector, dryrun, remote, reponame, ral.Id, lal)
					}
					CompareEntities(lRepo.Autolinks.GetEntity(), rRepo.Autolinks.GetEntity(), compareAutolinks, onAutolinkAdded, onAutolinkRemoved, onAutolinkChange)
				}
			}
		}

		//
		// now, comparing repo properties
		//
		for lk, lv := range lRepo.BoolProperties {
			if rv, ok := rRepo.BoolProperties[lk]; !ok || rv != lv {
				return false
			}
		}

		if lRepo.Visibility != rRepo.Visibility {
			// check back if the remote repo is a fork
			// in this case, we cannot change the visibility
			if rr, ok := rRepos[reponame]; ok {
				if !rr.IsFork {
					return false
				}
			} else {
				return false
			}
		}

		if lRepo.DefaultBranchName != "" && lRepo.DefaultBranchName != rRepo.DefaultBranchName {
			return false
		}

		if res, _, _ := entity.StringArrayEquivalent(lRepo.Readers, rRepo.Readers); !res {
			return false
		}

		if res, _, _ := entity.StringArrayEquivalent(lRepo.Writers, rRepo.Writers); !res {
			return false
		}

		if len(rRepo.InternalUsers) != 0 {
			return false
		}

		if res, _, _ := entity.StringArrayEquivalent(lRepo.ExternalUserReaders, rRepo.ExternalUserReaders); !res {
			return false
		}

		if res, _, _ := entity.StringArrayEquivalent(lRepo.ExternalUserWriters, rRepo.ExternalUserWriters); !res {
			return false
		}

		if manageGithubVariables {
			if !archived {
				if !utils.DeepEqualUnordered(lRepo.ActionVariables.GetEntity(), rRepo.ActionVariables.GetEntity()) {
					return false
				}
			}
		}

		if lRepo.BoolProperties["allow_merge_commit"] && (lRepo.DefaultMergeCommitMessage != rRepo.DefaultMergeCommitMessage) {
			return false
		}

		if lRepo.BoolProperties["allow_squash_merge"] && (lRepo.DefaultSquashCommitMessage != rRepo.DefaultSquashCommitMessage) {
			return false
		}

		if res, _, _ := entity.StringArrayEquivalent(lRepo.Topics, rRepo.Topics); !res {
			return false
		}

		return true
	}

	onChanged := func(reponame string, lRepo *GithubRepoComparable, rRepo *GithubRepoComparable) {
		// reconciliate repositories boolean properties
		for lk, lv := range lRepo.BoolProperties {
			if rv, ok := rRepo.BoolProperties[lk]; !ok || rv != lv {
				r.UpdateRepositoryUpdateProperties(ctx, logsCollector, dryrun, remote, reponame, map[string]interface{}{lk: lv})
			}
		}
		if lRepo.Visibility != rRepo.Visibility {
			// check back if the remote repo is a fork
			// in this case, we cannot change the visibility
			if rr, ok := rRepos[reponame]; ok {
				if !rr.IsFork {
					r.UpdateRepositoryUpdateProperties(ctx, logsCollector, dryrun, remote, reponame, map[string]interface{}{"visibility": lRepo.Visibility})
				}
			} else {
				r.UpdateRepositoryUpdateProperties(ctx, logsCollector, dryrun, remote, reponame, map[string]interface{}{"visibility": lRepo.Visibility})
			}
		}
		if lRepo.DefaultBranchName != "" && lRepo.DefaultBranchName != rRepo.DefaultBranchName {
			r.UpdateRepositoryUpdateProperties(ctx, logsCollector, dryrun, remote, reponame, map[string]interface{}{"default_branch": lRepo.DefaultBranchName})
		}

		if res, readToRemove, readToAdd := entity.StringArrayEquivalent(lRepo.Readers, rRepo.Readers); !res {
			for _, teamSlug := range readToAdd {
				r.UpdateRepositoryAddTeamAccess(ctx, logsCollector, dryrun, remote, reponame, teamSlug, "pull")
			}
			for _, teamSlug := range readToRemove {
				r.UpdateRepositoryRemoveTeamAccess(ctx, logsCollector, dryrun, remote, reponame, teamSlug)
			}
		}

		if res, writeToRemove, writeToAdd := entity.StringArrayEquivalent(lRepo.Writers, rRepo.Writers); !res {
			for _, teamSlug := range writeToAdd {
				r.UpdateRepositoryAddTeamAccess(ctx, logsCollector, dryrun, remote, reponame, teamSlug, "push")
			}
			for _, teamSlug := range writeToRemove {
				r.UpdateRepositoryRemoveTeamAccess(ctx, logsCollector, dryrun, remote, reponame, teamSlug)
			}
		}

		// internal users
		for _, internalUser := range rRepo.InternalUsers {
			r.UpdateRepositoryRemoveInternalUser(ctx, logsCollector, dryrun, remote, reponame, internalUser)
		}

		// external users
		resEreader, ereaderToRemove, ereaderToAdd := entity.StringArrayEquivalent(lRepo.ExternalUserReaders, rRepo.ExternalUserReaders)
		resEWriter, ewriteToRemove, ewriteToAdd := entity.StringArrayEquivalent(lRepo.ExternalUserWriters, rRepo.ExternalUserWriters)

		if !resEreader {
			for _, eReader := range ereaderToRemove {
				// check if it is added in the writers
				found := false
				for _, eWriter := range ewriteToAdd {
					if eWriter == eReader {
						found = true
						break
					}
				}
				if !found {
					r.UpdateRepositoryRemoveExternalUser(ctx, logsCollector, dryrun, remote, reponame, eReader)
				}
			}
			for _, eReader := range ereaderToAdd {
				r.UpdateRepositorySetExternalUser(ctx, logsCollector, dryrun, remote, reponame, eReader, "pull")
			}
		}

		if !resEWriter {
			for _, eWriter := range ewriteToRemove {
				// check if it is added in the writers
				found := false
				for _, eReader := range ereaderToAdd {
					if eReader == eWriter {
						found = true
						break
					}
				}
				if !found {
					r.UpdateRepositoryRemoveExternalUser(ctx, logsCollector, dryrun, remote, reponame, eWriter)
				}
			}
			for _, eWriter := range ewriteToAdd {
				r.UpdateRepositorySetExternalUser(ctx, logsCollector, dryrun, remote, reponame, eWriter, "push")
			}
		}

		if manageGithubVariables {
			archived := lRepo.BoolProperties["archived"]
			if !archived {
				if !utils.DeepEqualUnordered(lRepo.ActionVariables.GetEntity(), rRepo.ActionVariables.GetEntity()) {

					// Check for removed or changed keys
					for name, value := range lRepo.ActionVariables.GetEntity() {
						if rValue, ok := rRepo.ActionVariables.GetEntity()[name]; !ok {
							r.AddRepositoryVariable(ctx, logsCollector, dryrun, remote, reponame, name, value)
						} else if rValue != value {
							r.UpdateRepositoryVariable(ctx, logsCollector, dryrun, remote, reponame, name, value)
						}
					}

					// Check for added keys
					for name := range rRepo.ActionVariables.GetEntity() {
						if _, ok := lRepo.ActionVariables.GetEntity()[name]; !ok {
							r.DeleteRepositoryVariable(ctx, logsCollector, dryrun, remote, reponame, name)
						}
					}
				}
			}
		}

		if lRepo.BoolProperties["allow_merge_commit"] && (lRepo.DefaultMergeCommitMessage != rRepo.DefaultMergeCommitMessage) {
			switch lRepo.DefaultMergeCommitMessage {
			case "Pull request title":
				r.UpdateRepositoryUpdateProperties(ctx, logsCollector, dryrun, remote, reponame, map[string]interface{}{"merge_commit_title": "PR_TITLE", "merge_commit_message": "BLANK"})
			case "Pull request title and description":
				r.UpdateRepositoryUpdateProperties(ctx, logsCollector, dryrun, remote, reponame, map[string]interface{}{"merge_commit_title": "PR_TITLE", "merge_commit_message": "PR_BODY"})
			default: // Default message
				r.UpdateRepositoryUpdateProperties(ctx, logsCollector, dryrun, remote, reponame, map[string]interface{}{"merge_commit_title": "MERGE_MESSAGE", "merge_commit_message": "PR_TITLE"})
			}
		}

		if lRepo.BoolProperties["allow_squash_merge"] && (lRepo.DefaultSquashCommitMessage != rRepo.DefaultSquashCommitMessage) {
			switch lRepo.DefaultSquashCommitMessage {
			case "Pull request title":
				r.UpdateRepositoryUpdateProperties(ctx, logsCollector, dryrun, remote, reponame, map[string]interface{}{"squash_merge_commit_title": "PR_TITLE", "squash_merge_commit_message": "BLANK"})
			case "Pull request title and description":
				r.UpdateRepositoryUpdateProperties(ctx, logsCollector, dryrun, remote, reponame, map[string]interface{}{"squash_merge_commit_title": "PR_TITLE", "squash_merge_commit_message": "PR_BODY"})
			case "Pull request title and commit details":
				r.UpdateRepositoryUpdateProperties(ctx, logsCollector, dryrun, remote, reponame, map[string]interface{}{"squash_merge_commit_title": "PR_TITLE", "squash_merge_commit_message": "COMMIT_MESSAGES"})
			default: // Default message
				r.UpdateRepositoryUpdateProperties(ctx, logsCollector, dryrun, remote, reponame, map[string]interface{}{"squash_merge_commit_title": "COMMIT_OR_PR_TITLE", "squash_merge_commit_message": "COMMIT_MESSAGES"})
			}
		}

		// reconcile topics
		if res, _, _ := entity.StringArrayEquivalent(lRepo.Topics, rRepo.Topics); !res {
			r.UpdateRepositoryTopics(ctx, logsCollector, dryrun, remote, reponame, lRepo.Topics)
		}
	}

	onAdded := func(reponame string, lRepo *GithubRepoComparable, rRepo *GithubRepoComparable) {
		// CREATE repository

		// if the repo was just archived in a previous commit and we "resume it"
		if aRepo, ok := reposToArchive[reponame]; ok {
			delete(reposToArchive, reponame)
			r.UpdateRepositoryUpdateProperties(ctx, logsCollector, dryrun, remote, reponame, map[string]interface{}{"archived": false})
			// calling onChanged to update the repository permissions
			onChanged(reponame, aRepo, rRepo)
		} else {
			// check if the repo is a fork
			if lr, ok := lRepos[reponame]; ok {
				if lr.IsFork {
					r.CreateRepository(ctx, logsCollector, dryrun, remote, reponame, reponame, lRepo.Visibility, lRepo.Writers, lRepo.Readers, lRepo.BoolProperties, lRepo.DefaultBranchName, lr.ForkFrom)
				} else {
					r.CreateRepository(ctx, logsCollector, dryrun, remote, reponame, reponame, lRepo.Visibility, lRepo.Writers, lRepo.Readers, lRepo.BoolProperties, lRepo.DefaultBranchName, "")
				}
			} else {
				r.CreateRepository(ctx, logsCollector, dryrun, remote, reponame, reponame, lRepo.Visibility, lRepo.Writers, lRepo.Readers, lRepo.BoolProperties, lRepo.DefaultBranchName, "")
			}
		}
	}

	onRemoved := func(reponame string, lRepo *GithubRepoComparable, rRepo *GithubRepoComparable) {
		// here we have a repository that is not listed in the teams repository.
		// we should call DeleteRepository (that will delete if AllowDestructiveRepositories is on).
		// but if we have ArchiveOnDelete...
		if r.repoconfig.ArchiveOnDelete {
			if r.repoconfig.DestructiveOperations.AllowDestructiveRepositories {
				r.UpdateRepositoryUpdateProperties(ctx, logsCollector, dryrun, remote, reponame, map[string]interface{}{"archived": true})
				reposToArchive[reponame] = rRepo
			} else {
				r.unmanaged.Repositories[reponame] = true
			}
		} else {
			r.DeleteRepository(ctx, logsCollector, dryrun, remote, reponame)
		}
	}

	CompareEntities(lRepos, rRepos, compareRepos, onAdded, onRemoved, onChanged)

	return reposToArchive, reposToRename, nil
}

func compareBranchProtections(bpname string, lbp *GithubBranchProtection, rbp *GithubBranchProtection) bool {
	if lbp.Pattern != rbp.Pattern {
		return false
	}
	if lbp.RequiresApprovingReviews != rbp.RequiresApprovingReviews {
		return false
	}
	if lbp.RequiresApprovingReviews {
		if lbp.RequiredApprovingReviewCount != rbp.RequiredApprovingReviewCount {
			return false
		}
	}
	if lbp.RequiresApprovingReviews {
		if lbp.DismissesStaleReviews != rbp.DismissesStaleReviews {
			return false
		}
	}
	if lbp.RequiresApprovingReviews {
		if lbp.RequiresCodeOwnerReviews != rbp.RequiresCodeOwnerReviews {
			return false
		}
	}
	if lbp.RequiresApprovingReviews {
		if lbp.RequireLastPushApproval != rbp.RequireLastPushApproval {
			return false
		}
	}
	if lbp.RequiresStatusChecks != rbp.RequiresStatusChecks {
		return false
	}
	if lbp.RequiresStatusChecks {
		if lbp.RequiresStrictStatusChecks != rbp.RequiresStrictStatusChecks {
			return false
		}
		if res, _, _ := entity.StringArrayEquivalent(lbp.RequiredStatusCheckContexts, rbp.RequiredStatusCheckContexts); !res {
			return false
		}
	}
	if lbp.RequiresConversationResolution != rbp.RequiresConversationResolution {
		return false
	}
	if lbp.RequiresCommitSignatures != rbp.RequiresCommitSignatures {
		return false
	}
	if lbp.RequiresLinearHistory != rbp.RequiresLinearHistory {
		return false
	}
	if lbp.AllowsForcePushes != rbp.AllowsForcePushes {
		return false
	}
	if lbp.AllowsDeletions != rbp.AllowsDeletions {
		return false
	}
	leftActorUsers := []string{}
	leftActorTeams := []string{}
	leftActorApps := []string{}
	rightActorUsers := []string{}
	rightActorTeams := []string{}
	rightActorApps := []string{}
	for _, n := range lbp.BypassPullRequestAllowances.Nodes {
		if n.Actor.TeamSlug != "" {
			leftActorTeams = append(leftActorTeams, n.Actor.TeamSlug)
		}
		if n.Actor.UserLogin != "" {
			leftActorUsers = append(leftActorUsers, n.Actor.UserLogin)
		}
		if n.Actor.AppSlug != "" {
			leftActorApps = append(leftActorApps, n.Actor.AppSlug)
		}
	}
	for _, n := range rbp.BypassPullRequestAllowances.Nodes {
		if n.Actor.TeamSlug != "" {
			rightActorTeams = append(rightActorTeams, n.Actor.TeamSlug)
		}
		if n.Actor.UserLogin != "" {
			rightActorUsers = append(rightActorUsers, n.Actor.UserLogin)
		}
		if n.Actor.AppSlug != "" {
			rightActorApps = append(rightActorApps, n.Actor.AppSlug)
		}
	}
	if res, _, _ := entity.StringArrayEquivalent(leftActorUsers, rightActorUsers); !res {
		return false
	}
	if res, _, _ := entity.StringArrayEquivalent(leftActorTeams, rightActorTeams); !res {
		return false
	}
	if res, _, _ := entity.StringArrayEquivalent(leftActorApps, rightActorApps); !res {
		return false
	}

	return true
}

func compareEnvironments(environment string, lEnv *GithubEnvironment, rEnv *GithubEnvironment) bool {
	if lEnv.Name != rEnv.Name {
		return false
	}
	if len(lEnv.Variables) != len(rEnv.Variables) {
		return false
	}
	for k, v := range lEnv.Variables {
		if rEnv.Variables[k] != v {
			return false
		}
	}
	return true
}

func compareAutolinks(autolinkname string, lal *GithubAutolink, ral *GithubAutolink) bool {
	if lal.KeyPrefix != ral.KeyPrefix {
		return false
	}
	if lal.UrlTemplate != ral.UrlTemplate {
		return false
	}
	if lal.IsAlphanumeric != ral.IsAlphanumeric {
		return false
	}
	return true
}

/*
used to compare org rulesets but also repo rulesets
*/
func compareRulesets(rulesetname string, lrs *GithubRuleSet, rrs *GithubRuleSet) bool {
	if lrs.Enforcement != rrs.Enforcement {
		return false
	}
	if len(lrs.BypassApps) != len(rrs.BypassApps) {
		return false
	}
	for k, v := range lrs.BypassApps {
		if rrs.BypassApps[k] != v {
			return false
		}
	}
	if len(lrs.BypassTeams) != len(rrs.BypassTeams) {
		return false
	}
	for k, v := range lrs.BypassTeams {
		if rrs.BypassTeams[k] != v {
			return false
		}
	}
	if res, _, _ := entity.StringArrayEquivalent(lrs.OnInclude, rrs.OnInclude); !res {
		return false
	}
	if res, _, _ := entity.StringArrayEquivalent(lrs.OnExclude, rrs.OnExclude); !res {
		return false
	}
	if len(lrs.Rules) != len(rrs.Rules) {
		return false
	}
	for k, v := range lrs.Rules {
		if !entity.CompareRulesetParameters(k, v, rrs.Rules[k]) {
			return false
		}
	}
	if res, _, _ := entity.StringArrayEquivalent(lrs.Repositories, rrs.Repositories); !res {
		return false
	}

	return true
}

func (r *GoliacReconciliatorImpl) reconciliateRulesets(ctx context.Context, logsCollector *observability.LogCollection, local GoliacReconciliatorDatasource, remote *MutableGoliacRemoteImpl, conf *config.RepositoryConfig, dryrun bool) error {
	lgrs, err := local.RuleSets()
	if err != nil {
		return err
	}

	// prepare remote comparable
	rgrs := remote.RuleSets()

	// prepare the diff computation

	onAdded := func(rulesetname string, lRuleset *GithubRuleSet, rRuleset *GithubRuleSet) {
		// CREATE ruleset

		r.AddRuleset(ctx, logsCollector, dryrun, lRuleset)
	}

	onRemoved := func(rulesetname string, lRuleset *GithubRuleSet, rRuleset *GithubRuleSet) {
		// DELETE ruleset
		r.DeleteRuleset(ctx, logsCollector, dryrun, rRuleset)
	}

	onChanged := func(rulesetname string, lRuleset *GithubRuleSet, rRuleset *GithubRuleSet) {
		// UPDATE ruleset
		lRuleset.Id = rRuleset.Id
		r.UpdateRuleset(ctx, logsCollector, dryrun, lRuleset)
	}

	CompareEntities(lgrs, rgrs, compareRulesets, onAdded, onRemoved, onChanged)

	return nil
}

func compareOrgCustomProperties(propertyName string, lProperty *config.GithubCustomProperty, rProperty *config.GithubCustomProperty) bool {
	if lProperty.PropertyName != rProperty.PropertyName {
		return false
	}
	if lProperty.ValueType != rProperty.ValueType {
		return false
	}
	if lProperty.Required != rProperty.Required {
		return false
	}
	if lProperty.DefaultValue != rProperty.DefaultValue {
		return false
	}
	if lProperty.Description != rProperty.Description {
		return false
	}
	if res, _, _ := entity.StringArrayEquivalent(lProperty.AllowedValues, rProperty.AllowedValues); !res {
		return false
	}
	if lProperty.ValuesEditableBy != rProperty.ValuesEditableBy {
		return false
	}
	return true
}

func (r *GoliacReconciliatorImpl) reconciliateOrgCustomProperties(ctx context.Context, logsCollector *observability.LogCollection, remote *MutableGoliacRemoteImpl, conf *config.RepositoryConfig, dryrun bool) error {
	// prepare local comparable
	localProps := make(map[string]*config.GithubCustomProperty)
	for _, prop := range conf.OrgCustomProperties {
		localProps[prop.PropertyName] = prop
	}

	// prepare remote comparable
	remoteProps := remote.OrgCustomProperties()

	// prepare the diff computation
	onAdded := func(propertyName string, lProperty *config.GithubCustomProperty, rProperty *config.GithubCustomProperty) {
		// CREATE org custom property
		r.CreateOrUpdateOrgCustomProperty(ctx, logsCollector, dryrun, remote, lProperty)
	}

	onRemoved := func(propertyName string, lProperty *config.GithubCustomProperty, rProperty *config.GithubCustomProperty) {
		// DELETE org custom property
		r.DeleteOrgCustomProperty(ctx, logsCollector, dryrun, remote, propertyName)
	}

	onChanged := func(propertyName string, lProperty *config.GithubCustomProperty, rProperty *config.GithubCustomProperty) {
		// UPDATE org custom property
		r.CreateOrUpdateOrgCustomProperty(ctx, logsCollector, dryrun, remote, lProperty)
	}

	CompareEntities(localProps, remoteProps, compareOrgCustomProperties, onAdded, onRemoved, onChanged)

	return nil
}

func (r *GoliacReconciliatorImpl) AddUserToOrg(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, ghuserid string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "add_user_to_org"}, "ghuserid: %s", ghuserid)
	remote.AddUserToOrg(ghuserid)
	if r.executor != nil {
		r.executor.AddUserToOrg(ctx, logsCollector, dryrun, ghuserid)
	}
}

func (r *GoliacReconciliatorImpl) RemoveUserFromOrg(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, ghuserid string) {
	if r.repoconfig.DestructiveOperations.AllowDestructiveUsers {
		logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "remove_user_from_org"}, "ghuserid: %s", ghuserid)
		remote.RemoveUserFromOrg(ghuserid)
		if r.executor != nil {
			r.executor.RemoveUserFromOrg(ctx, logsCollector, dryrun, ghuserid)
		}
	} else {
		r.unmanaged.Users[ghuserid] = true
	}
}

func (r *GoliacReconciliatorImpl) CreateTeam(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, teamname string, description string, parentTeam *int, members []string) {
	parentTeamId := "nil"
	if parentTeam != nil {
		parentTeamId = fmt.Sprintf("%d", *parentTeam)
	}

	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "create_team"}, "teamname: %s, parentTeam: %s, members: %s", teamname, parentTeamId, strings.Join(members, ","))
	remote.CreateTeam(teamname, description, members, parentTeam)
	if r.executor != nil {
		r.executor.CreateTeam(ctx, logsCollector, dryrun, teamname, description, parentTeam, members)
	}
}
func (r *GoliacReconciliatorImpl) UpdateTeamAddMember(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, teamslug string, ghuserid string, role string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_team_add_member"}, "teamslug: %s, ghuserid: %s, role: %s", teamslug, ghuserid, role)
	remote.UpdateTeamAddMember(teamslug, ghuserid, "member")
	if r.executor != nil {
		r.executor.UpdateTeamAddMember(ctx, logsCollector, dryrun, teamslug, ghuserid, "member")
	}
}
func (r *GoliacReconciliatorImpl) UpdateTeamRemoveMember(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, teamslug string, ghuserid string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_team_remove_member"}, "teamslug: %s, ghuserid: %s", teamslug, ghuserid)
	remote.UpdateTeamRemoveMember(teamslug, ghuserid)
	if r.executor != nil {
		r.executor.UpdateTeamRemoveMember(ctx, logsCollector, dryrun, teamslug, ghuserid)
	}
}
func (r *GoliacReconciliatorImpl) UpdateTeamChangeMaintainerToMember(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, teamslug string, ghuserid string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_team_change_maintainer_to_member"}, "teamslug: %s, ghuserid: %s", teamslug, ghuserid)
	remote.UpdateTeamUpdateMember(teamslug, ghuserid, "member")
	if r.executor != nil {
		r.executor.UpdateTeamUpdateMember(ctx, logsCollector, dryrun, teamslug, ghuserid, "member")
	}
}
func (r *GoliacReconciliatorImpl) UpdateTeamSetParent(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, teamslug string, parentTeam *int, parentTeamName string) {
	parenTeamId := "nil"
	if parentTeam != nil {
		parenTeamId = fmt.Sprintf("%d", *parentTeam)
	}

	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_team_parentteam"}, "teamslug: %s, parentteam: %s (%s)", teamslug, parenTeamId, parentTeamName)
	remote.UpdateTeamSetParent(ctx, dryrun, teamslug, parentTeam)
	if r.executor != nil {
		r.executor.UpdateTeamSetParent(ctx, logsCollector, dryrun, teamslug, parentTeam)
	}
}
func (r *GoliacReconciliatorImpl) DeleteTeam(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, teamslug string) {
	if r.repoconfig.DestructiveOperations.AllowDestructiveTeams {
		logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "delete_team"}, "teamslug: %s", teamslug)
		remote.DeleteTeam(teamslug)
		if r.executor != nil {
			r.executor.DeleteTeam(ctx, logsCollector, dryrun, teamslug)
		}
	} else {
		r.unmanaged.Teams[teamslug] = true
	}
}
func (r *GoliacReconciliatorImpl) CreateRepository(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, descrition string, visibility string, writers []string, readers []string, boolProperties map[string]bool, defaultBranch string, forkFrom string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "create_repository"}, "repositoryname: %s, readers: %s, writers: %s, boolProperties: %v", reponame, strings.Join(readers, ","), strings.Join(writers, ","), boolProperties)
	remote.CreateRepository(reponame, reponame, visibility, writers, readers, boolProperties, defaultBranch, forkFrom)
	if r.executor != nil {
		r.executor.CreateRepository(ctx, logsCollector, dryrun, reponame, reponame, visibility, writers, readers, boolProperties, defaultBranch, nil, forkFrom)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryAddTeamAccess(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, teamslug string, permission string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_add_team"}, "repositoryname: %s, teamslug: %s, permission: %s", reponame, teamslug, permission)
	remote.UpdateRepositoryAddTeamAccess(reponame, teamslug, permission)
	if r.executor != nil {
		r.executor.UpdateRepositoryAddTeamAccess(ctx, logsCollector, dryrun, reponame, teamslug, permission)
	}
}

func (r *GoliacReconciliatorImpl) UpdateRepositoryUpdateTeamAccess(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, teamslug string, permission string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_update_team"}, "repositoryname: %s, teamslug:%s, permission: %s", reponame, teamslug, permission)
	remote.UpdateRepositoryUpdateTeamAccess(reponame, teamslug, permission)
	if r.executor != nil {
		r.executor.UpdateRepositoryUpdateTeamAccess(ctx, logsCollector, dryrun, reponame, teamslug, permission)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryRemoveTeamAccess(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, teamslug string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_remove_team"}, "repositoryname: %s, teamslug:%s", reponame, teamslug)
	remote.UpdateRepositoryRemoveTeamAccess(reponame, teamslug)
	if r.executor != nil {
		r.executor.UpdateRepositoryRemoveTeamAccess(ctx, logsCollector, dryrun, reponame, teamslug)
	}
}

func (r *GoliacReconciliatorImpl) DeleteRepository(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string) {
	if r.repoconfig.DestructiveOperations.AllowDestructiveRepositories {
		logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "delete_repository"}, "repositoryname: %s", reponame)
		remote.DeleteRepository(reponame)
		if r.executor != nil {
			r.executor.DeleteRepository(ctx, logsCollector, dryrun, reponame)
		}
	} else {
		r.unmanaged.Repositories[reponame] = true
	}
}

func (r *GoliacReconciliatorImpl) RenameRepository(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, newname string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "rename_repository"}, "repositoryname: %s newname: %s", reponame, newname)
	remote.RenameRepository(reponame, newname)
	if r.executor != nil {
		r.executor.RenameRepository(ctx, logsCollector, dryrun, reponame, newname)
	}
}

func (r *GoliacReconciliatorImpl) UpdateRepositoryUpdateProperties(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, properties map[string]interface{}) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_update_properties"}, "repositoryname: %s properties: %v", reponame, properties)
	remote.UpdateRepositoryUpdateProperties(reponame, properties)
	if r.executor != nil {
		r.executor.UpdateRepositoryUpdateProperties(ctx, logsCollector, dryrun, reponame, properties)
	}
}

func (r *GoliacReconciliatorImpl) UpdateRepositoryCustomProperties(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, propertyName string, propertyValue interface{}) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_custom_properties"}, "repositoryname: %s, property: %s, value: %v", reponame, propertyName, propertyValue)
	remote.UpdateRepositoryCustomProperties(reponame, propertyName, propertyValue)
	if r.executor != nil {
		r.executor.UpdateRepositoryCustomProperties(ctx, logsCollector, dryrun, reponame, propertyName, propertyValue)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryTopics(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, topics []string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_topics"}, "repositoryname: %s, topics: %v", reponame, topics)
	remote.UpdateRepositoryTopics(reponame, topics)
	if r.executor != nil {
		r.executor.UpdateRepositoryTopics(ctx, logsCollector, dryrun, reponame, topics)
	}
}
func (r *GoliacReconciliatorImpl) AddRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, ruleset *GithubRuleSet) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "add_ruleset"}, "ruleset: %s (id: %d) enforcement: %s", ruleset.Name, ruleset.Id, ruleset.Enforcement)
	if r.executor != nil {
		r.executor.AddRuleset(ctx, logsCollector, dryrun, ruleset)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, ruleset *GithubRuleSet) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_ruleset"}, "ruleset: %s (id: %d) enforcement: %s", ruleset.Name, ruleset.Id, ruleset.Enforcement)
	if r.executor != nil {
		r.executor.UpdateRuleset(ctx, logsCollector, dryrun, ruleset)
	}
}
func (r *GoliacReconciliatorImpl) DeleteRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, ruleset *GithubRuleSet) {
	if r.repoconfig.DestructiveOperations.AllowDestructiveRulesets {
		logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "delete_ruleset"}, "ruleset id:%d", ruleset.Id)
		if r.executor != nil {
			r.executor.DeleteRuleset(ctx, logsCollector, dryrun, ruleset.Id)
		}
	} else {
		r.unmanaged.RuleSets[ruleset.Name] = true
	}
}
func (r *GoliacReconciliatorImpl) AddRepositoryRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, ruleset *GithubRuleSet) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "add_repository_ruleset"}, "repository: %s, ruleset: %s (id: %d) enforcement: %s", reponame, ruleset.Name, ruleset.Id, ruleset.Enforcement)
	if r.executor != nil {
		r.executor.AddRepositoryRuleset(ctx, logsCollector, dryrun, reponame, ruleset)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, ruleset *GithubRuleSet) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_ruleset"}, "repository: %s, ruleset: %s (id: %d) enforcement: %s", reponame, ruleset.Name, ruleset.Id, ruleset.Enforcement)
	if r.executor != nil {
		r.executor.UpdateRepositoryRuleset(ctx, logsCollector, dryrun, reponame, ruleset)
	}
}
func (r *GoliacReconciliatorImpl) DeleteRepositoryRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, ruleset *GithubRuleSet) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "delete_repository_ruleset"}, "repository: %s, ruleset id:%d", reponame, ruleset.Id)
	if r.executor != nil {
		r.executor.DeleteRepositoryRuleset(ctx, logsCollector, dryrun, reponame, ruleset.Id)
	}
}
func (r *GoliacReconciliatorImpl) AddRepositoryBranchProtection(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "add_repository_branchprotection"}, "repository: %s, branchprotection: %s", reponame, branchprotection.Pattern)
	if r.executor != nil {
		r.executor.AddRepositoryBranchProtection(ctx, logsCollector, dryrun, reponame, branchprotection)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryBranchProtection(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_branchprotection"}, "repository: %s, branchprotection: %s", reponame, branchprotection.Pattern)
	if r.executor != nil {
		r.executor.UpdateRepositoryBranchProtection(ctx, logsCollector, dryrun, reponame, branchprotection)
	}
}
func (r *GoliacReconciliatorImpl) DeleteRepositoryBranchProtection(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "delete_repository_branchprotection"}, "repository: %s, branchprotection: %s", reponame, branchprotection.Pattern)
	if r.executor != nil {
		r.executor.DeleteRepositoryBranchProtection(ctx, logsCollector, dryrun, reponame, branchprotection)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositorySetExternalUser(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, collaboatorGithubId string, permission string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_set_external_user"}, "repositoryname: %s collaborator:%s permission:%s", reponame, collaboatorGithubId, permission)
	remote.UpdateRepositorySetExternalUser(reponame, collaboatorGithubId, permission)
	if r.executor != nil {
		r.executor.UpdateRepositorySetExternalUser(ctx, logsCollector, dryrun, reponame, collaboatorGithubId, permission)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryRemoveInternalUser(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, collaboatorGithubId string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_remove_internal_user"}, "repositoryname: %s collaborator:%s", reponame, collaboatorGithubId)
	remote.UpdateRepositoryRemoveInternalUser(reponame, collaboatorGithubId)
	if r.executor != nil {
		r.executor.UpdateRepositoryRemoveInternalUser(ctx, logsCollector, dryrun, reponame, collaboatorGithubId)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryRemoveExternalUser(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, collaboatorGithubId string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_remove_external_user"}, "repositoryname: %s collaborator:%s", reponame, collaboatorGithubId)
	remote.UpdateRepositoryRemoveExternalUser(reponame, collaboatorGithubId)
	if r.executor != nil {
		r.executor.UpdateRepositoryRemoveExternalUser(ctx, logsCollector, dryrun, reponame, collaboatorGithubId)
	}
}
func (r *GoliacReconciliatorImpl) AddRepositoryEnvironment(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, environment string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "add_repository_environment"}, "repository: %s, environment: %s", reponame, environment)
	remote.AddRepositoryEnvironment(reponame, environment)
	if r.executor != nil {
		r.executor.AddRepositoryEnvironment(ctx, logsCollector, dryrun, reponame, environment)
	}
}
func (r *GoliacReconciliatorImpl) DeleteRepositoryEnvironment(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, environment string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "delete_repository_environment"}, "repository: %s, environment: %s", reponame, environment)
	remote.DeleteRepositoryEnvironment(reponame, environment)
	if r.executor != nil {
		r.executor.DeleteRepositoryEnvironment(ctx, logsCollector, dryrun, reponame, environment)
	}
}
func (r *GoliacReconciliatorImpl) AddRepositoryVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, variable string, value string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "add_repository_variable"}, "repository: %s, variable: %s, value: %s", reponame, variable, value)
	remote.AddRepositoryVariable(reponame, variable, value)
	if r.executor != nil {
		r.executor.AddRepositoryVariable(ctx, logsCollector, dryrun, reponame, variable, value)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, variable string, value string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_variable"}, "repository: %s, variable: %s, value: %s", reponame, variable, value)
	remote.UpdateRepositoryVariable(reponame, variable, value)
	if r.executor != nil {
		r.executor.UpdateRepositoryVariable(ctx, logsCollector, dryrun, reponame, variable, value)
	}
}
func (r *GoliacReconciliatorImpl) DeleteRepositoryVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, variable string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "delete_repository_variable"}, "repository: %s, variable: %s", reponame, variable)
	remote.DeleteRepositoryVariable(reponame, variable)
	if r.executor != nil {
		r.executor.DeleteRepositoryVariable(ctx, logsCollector, dryrun, reponame, variable)
	}
}
func (r *GoliacReconciliatorImpl) AddRepositoryEnvironmentVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, environment string, variable string, value string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "add_repository_environment_variable"}, "repository: %s, environment: %s, variable: %s, value: %s", reponame, environment, variable, value)
	remote.AddRepositoryEnvironmentVariable(reponame, environment, variable, value)
	if r.executor != nil {
		r.executor.AddRepositoryEnvironmentVariable(ctx, logsCollector, dryrun, reponame, environment, variable, value)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryEnvironmentVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, environment string, variable string, value string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_environment_variable"}, "repository: %s, environment: %s, variable: %s, value: %s", reponame, environment, variable, value)
	remote.UpdateRepositoryEnvironmentVariable(reponame, environment, variable, value)
	if r.executor != nil {
		r.executor.UpdateRepositoryEnvironmentVariable(ctx, logsCollector, dryrun, reponame, environment, variable, value)
	}
}
func (r *GoliacReconciliatorImpl) DeleteRepositoryEnvironmentVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, environment string, variable string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "delete_repository_environment_variable"}, "repository: %s, environment: %s, variable: %s", reponame, environment, variable)
	remote.DeleteRepositoryEnvironmentVariable(reponame, environment, variable)
	if r.executor != nil {
		r.executor.DeleteRepositoryEnvironmentVariable(ctx, logsCollector, dryrun, reponame, environment, variable)
	}
}
func (r *GoliacReconciliatorImpl) AddRepositoryAutolink(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, autolink *GithubAutolink) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "add_repository_autolink"}, "repository: %s, autolink: %s", reponame, autolink.KeyPrefix)
	remote.AddRepositoryAutolink(reponame, autolink)
	if r.executor != nil {
		r.executor.AddRepositoryAutolink(ctx, logsCollector, dryrun, reponame, autolink)
	}
}
func (r *GoliacReconciliatorImpl) DeleteRepositoryAutolink(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, autolinkId int) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "delete_repository_autolink"}, "repository: %s, autolink id: %d", reponame, autolinkId)
	remote.DeleteRepositoryAutolink(reponame, autolinkId)
	if r.executor != nil {
		r.executor.DeleteRepositoryAutolink(ctx, logsCollector, dryrun, reponame, autolinkId)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryAutolink(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, previousAutolinkId int, autolink *GithubAutolink) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_autolink"}, "repository: %s, autolink: %s", reponame, autolink.KeyPrefix)
	remote.UpdateRepositoryAutolink(reponame, previousAutolinkId, autolink)
	if r.executor != nil {
		r.executor.UpdateRepositoryAutolink(ctx, logsCollector, dryrun, reponame, previousAutolinkId, autolink)
	}
}
func (r *GoliacReconciliatorImpl) CreateOrUpdateOrgCustomProperty(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, property *config.GithubCustomProperty) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "create_or_update_org_custom_property"}, "property: %s, value_type: %s", property.PropertyName, property.ValueType)
	remote.CreateOrUpdateOrgCustomProperty(property)
	if r.executor != nil {
		r.executor.CreateOrUpdateOrgCustomProperty(ctx, logsCollector, dryrun, property)
	}
}
func (r *GoliacReconciliatorImpl) DeleteOrgCustomProperty(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, remote *MutableGoliacRemoteImpl, propertyName string) {
	logsCollector.AddInfo(map[string]interface{}{"dryrun": dryrun, "command": "delete_org_custom_property"}, "property: %s", propertyName)
	remote.DeleteOrgCustomProperty(propertyName)
	if r.executor != nil {
		r.executor.DeleteOrgCustomProperty(ctx, logsCollector, dryrun, propertyName)
	}
}
func (r *GoliacReconciliatorImpl) Begin(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool) {
	logsCollector.AddDebug(map[string]interface{}{"dryrun": dryrun}, "reconciliation begin")
	if r.executor != nil {
		r.executor.Begin(logsCollector, dryrun)
	}
}
func (r *GoliacReconciliatorImpl) Rollback(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, err error) {
	logsCollector.AddDebug(map[string]interface{}{"dryrun": dryrun}, "reconciliation rollback")
	if r.executor != nil {
		r.executor.Rollback(logsCollector, dryrun, err)
	}
}
func (r *GoliacReconciliatorImpl) Commit(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool) error {
	logsCollector.AddDebug(map[string]interface{}{"dryrun": dryrun}, "reconciliation commit")
	if r.executor != nil {
		return r.executor.Commit(ctx, logsCollector, dryrun)
	}
	return nil
}
