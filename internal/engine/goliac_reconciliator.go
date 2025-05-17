package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/goliac-project/goliac/internal/config"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/goliac-project/goliac/internal/utils"
	"github.com/gosimple/slug"
	"github.com/sirupsen/logrus"
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
	Reconciliate(ctx context.Context, errorCollector *observability.ErrorCollection, local GoliacLocal, remote GoliacRemote, teamreponame string, branch string, dryrun bool, goliacAdminSlug string, reposToArchive map[string]*GithubRepoComparable, reposToRename map[string]*entity.Repository, manageGithubVariables bool) (*UnmanagedResources, error)
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

func (r *GoliacReconciliatorImpl) Reconciliate(ctx context.Context, errorCollector *observability.ErrorCollection, local GoliacLocal, remote GoliacRemote, teamsreponame string, branch string, dryrun bool, goliacAdminSlug string, reposToArchive map[string]*GithubRepoComparable, reposToRename map[string]*entity.Repository, manageGithubVariables bool) (*UnmanagedResources, error) {
	rremote := NewMutableGoliacRemoteImpl(ctx, remote)
	r.Begin(ctx, dryrun)
	unmanaged := &UnmanagedResources{
		Users:                  make(map[string]bool),
		ExternallyManagedTeams: make(map[string]bool),
		Teams:                  make(map[string]bool),
		Repositories:           make(map[string]bool),
		RuleSets:               make(map[string]bool),
	}
	r.unmanaged = unmanaged

	err := r.reconciliateUsers(ctx, errorCollector, local, rremote, dryrun)
	if err != nil {
		r.Rollback(ctx, dryrun, err)
		return nil, err
	}

	err = r.reconciliateTeams(ctx, errorCollector, local, rremote, dryrun)
	if err != nil {
		r.Rollback(ctx, dryrun, err)
		return nil, err
	}

	err = r.reconciliateRepositories(ctx, errorCollector, local, rremote, teamsreponame, branch, dryrun, reposToArchive, reposToRename, manageGithubVariables)
	if err != nil {
		r.Rollback(ctx, dryrun, err)
		return nil, err
	}

	if remote.IsEnterprise() {
		err = r.reconciliateRulesets(ctx, errorCollector, local, rremote, teamsreponame, r.repoconfig, dryrun)
		if err != nil {
			r.Rollback(ctx, dryrun, err)
			return nil, err
		}
	}

	return r.unmanaged, r.Commit(ctx, errorCollector, dryrun)
}

/*
 * This function sync teams and team's members
 */
func (r *GoliacReconciliatorImpl) reconciliateUsers(ctx context.Context, errorCollector *observability.ErrorCollection, local GoliacLocal, remote *MutableGoliacRemoteImpl, dryrun bool) error {
	ghUsers := remote.Users()

	rUsers := make(map[string]string)
	for u := range ghUsers {
		rUsers[u] = u
	}

	for _, lUser := range local.Users() {
		user, ok := rUsers[lUser.Spec.GithubID]

		if !ok {
			// deal with non existing remote user
			r.AddUserToOrg(ctx, errorCollector, dryrun, remote, lUser.Spec.GithubID)
		} else {
			delete(rUsers, user)
		}
	}

	// remaining (GH) users (aka not found locally)
	for _, rUser := range rUsers {
		// DELETE User
		r.RemoveUserFromOrg(ctx, errorCollector, dryrun, remote, rUser)
	}
	return nil
}

type GithubTeamComparable struct {
	Name        string
	Slug        string
	Members     []string
	Maintainers []string
	ParentTeam  *string
}

/*
This function sync teams and team's members,
*/
func (r *GoliacReconciliatorImpl) reconciliateTeams(ctx context.Context, errorCollector *observability.ErrorCollection, local GoliacLocal, remote *MutableGoliacRemoteImpl, dryrun bool) error {
	ghTeams := remote.Teams()
	rUsers := remote.Users()

	ghTeamsPerId := make(map[int]*GithubTeam)
	for _, v := range ghTeams {
		ghTeamsPerId[v.Id] = v
	}

	rTeams := make(map[string]*GithubTeamComparable)
	for k, v := range ghTeams {
		members := make([]string, len(v.Members))
		copy(members, v.Members)
		maintainers := []string{}

		// let's filter admin from the maintainers
		for _, m := range v.Maintainers {
			if rUsers[m] == "ADMIN" {
				members = append(members, m)
			} else {
				maintainers = append(maintainers, m)
			}
		}

		team := &GithubTeamComparable{
			Name:        v.Name,
			Slug:        v.Slug,
			Members:     members,
			Maintainers: maintainers,
			ParentTeam:  nil,
		}
		if v.ParentTeam != nil {
			if parent, ok := ghTeamsPerId[*v.ParentTeam]; ok {
				parentTeam := slug.Make(parent.Name)
				team.ParentTeam = &parentTeam
			}
		}

		// key is the team's slug
		rTeams[k] = team
	}

	// prepare the teams we want (regular and "-goliac-owners"/config.Config.GoliacTeamOwnerSuffix)
	slugTeams := make(map[string]*GithubTeamComparable)
	lTeams := local.Teams()
	lUsers := local.Users()

	for teamname, teamvalue := range lTeams {
		teamslug := slug.Make(teamname)

		// if the team is externally managed, we don't want to touch it
		// we just remove it from the list
		if teamvalue.Spec.ExternallyManaged {
			// let's add it to the special -goliac-owners
			membersOwners := []string{}
			membersMaintainers := []string{}
			if rt, ok := rTeams[teamslug]; ok {
				membersOwners = append(membersOwners, rt.Members...)
				membersMaintainers = append(membersMaintainers, rt.Maintainers...)
			}
			team := &GithubTeamComparable{
				Name:        teamslug + config.Config.GoliacTeamOwnerSuffix,
				Slug:        teamslug + config.Config.GoliacTeamOwnerSuffix,
				Members:     membersOwners,
				Maintainers: membersMaintainers,
			}
			slugTeams[teamslug+config.Config.GoliacTeamOwnerSuffix] = team

			r.unmanaged.ExternallyManagedTeams[teamslug] = true

			// before we skip the team, let's check if the parent team is the same
			// if not, we need to update the parent team

			var lParentTeam *string
			if teamvalue.ParentTeam != nil {
				parentTeam := slug.Make(*teamvalue.ParentTeam)
				lParentTeam = &parentTeam
			}
			rTeam, ok := rTeams[teamslug]
			if ok {
				if (lParentTeam == nil && rTeam.ParentTeam != nil) ||
					(lParentTeam != nil && rTeam.ParentTeam == nil) ||
					(lParentTeam != nil && rTeam.ParentTeam != nil && *lParentTeam != *rTeam.ParentTeam) {

					var parentTeam *int
					parentTeamName := ""
					if lParentTeam != nil && ghTeams[*lParentTeam] != nil {
						parentTeam = &ghTeams[*lParentTeam].Id
						parentTeamName = *lParentTeam
					}

					r.UpdateTeamSetParent(ctx, errorCollector, dryrun, remote, teamslug, parentTeam, parentTeamName)
				}
			}

			// delete the team from the map
			// we don't want to process it anymore (it is externally managed)
			delete(rTeams, teamslug)
			continue
		}

		members := []string{}
		membersOwners := []string{}
		// teamvalue.Spec.Members are not github id
		for _, m := range teamvalue.Spec.Members {
			if u, ok := lUsers[m]; ok {
				members = append(members, u.Spec.GithubID)
			}
		}
		for _, m := range teamvalue.Spec.Owners {
			if u, ok := lUsers[m]; ok {
				members = append(members, u.Spec.GithubID)
				membersOwners = append(membersOwners, u.Spec.GithubID)
			}
		}

		team := &GithubTeamComparable{
			Name:    teamname,
			Slug:    teamslug,
			Members: members,
		}
		if teamvalue.ParentTeam != nil {
			parentTeam := slug.Make(*teamvalue.ParentTeam)
			team.ParentTeam = &parentTeam
		}
		slugTeams[teamslug] = team

		// owners
		team = &GithubTeamComparable{
			Name:        teamslug + config.Config.GoliacTeamOwnerSuffix,
			Slug:        teamslug + config.Config.GoliacTeamOwnerSuffix,
			Members:     membersOwners,
			Maintainers: []string{},
		}
		slugTeams[teamslug+config.Config.GoliacTeamOwnerSuffix] = team
	}

	// adding the "everyone" team
	if r.repoconfig.EveryoneTeamEnabled {
		everyone := GithubTeamComparable{
			Name:    "everyone",
			Slug:    "everyone",
			Members: []string{},
		}
		for u := range local.Users() {
			everyone.Members = append(everyone.Members, u)
		}
		slugTeams["everyone"] = &everyone
	}

	// now we compare local (slugTeams) and remote (rTeams)

	compareTeam := func(teamname string, lTeam *GithubTeamComparable, rTeam *GithubTeamComparable) bool {
		if res, _, _ := entity.StringArrayEquivalent(lTeam.Members, rTeam.Members); !res {
			return false
		}
		if res, _, _ := entity.StringArrayEquivalent(lTeam.Maintainers, rTeam.Maintainers); !res {
			return false
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
		if lTeam.ParentTeam != nil && ghTeams[*lTeam.ParentTeam] != nil {
			parentTeam = &ghTeams[*lTeam.ParentTeam].Id
		}
		r.CreateTeam(ctx, errorCollector, dryrun, remote, lTeam.Name, lTeam.Name, parentTeam, lTeam.Members)
	}

	onRemoved := func(key string, lTeam *GithubTeamComparable, rTeam *GithubTeamComparable) {
		// DELETE team
		r.DeleteTeam(ctx, errorCollector, dryrun, remote, rTeam.Slug)
	}

	onChanged := func(slugTeam string, lTeam *GithubTeamComparable, rTeam *GithubTeamComparable) {
		// change membership from maintainers to members

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
				r.UpdateTeamChangeMaintainerToMember(ctx, errorCollector, dryrun, remote, slugTeam, r_maintainer)
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
			localMembers := make(map[string]bool)
			for _, m := range lTeam.Members {
				localMembers[m] = true
			}

			for _, m := range rTeam.Members {
				if _, ok := localMembers[m]; !ok {
					// REMOVE team member
					r.UpdateTeamRemoveMember(ctx, errorCollector, dryrun, remote, slugTeam, m)
				} else {
					delete(localMembers, m)
				}
			}
			for m := range localMembers {
				// ADD team member
				r.UpdateTeamAddMember(ctx, errorCollector, dryrun, remote, slugTeam, m, "member")
			}
		}

		// parent team change
		if (lTeam.ParentTeam == nil && rTeam.ParentTeam != nil) ||
			(lTeam.ParentTeam != nil && rTeam.ParentTeam == nil) ||
			(lTeam.ParentTeam != nil && rTeam.ParentTeam != nil && *lTeam.ParentTeam != *rTeam.ParentTeam) {

			var parentTeam *int
			parentTeamName := ""
			if lTeam.ParentTeam != nil && ghTeams[*lTeam.ParentTeam] != nil {
				parentTeam = &ghTeams[*lTeam.ParentTeam].Id
				parentTeamName = *lTeam.ParentTeam
			}

			r.UpdateTeamSetParent(ctx, errorCollector, dryrun, remote, slugTeam, parentTeam, parentTeamName)
		}
	}

	CompareEntities(slugTeams, rTeams, compareTeam, onAdded, onRemoved, onChanged)

	return nil
}

type GithubRepoComparable struct {
	Visibility          string
	BoolProperties      map[string]bool
	Writers             []string
	Readers             []string
	ExternalUserReaders []string // githubids
	ExternalUserWriters []string // githubids
	InternalUsers       []string // githubids
	Rulesets            map[string]*GithubRuleSet
	BranchProtections   map[string]*GithubBranchProtection
	DefaultBranchName   string
	ActionVariables     MappedEntityLazyLoader[string]
	Environments        MappedEntityLazyLoader[*GithubEnvironment]
}

type GithubEnvironment struct {
	Name      string
	Variables map[string]string
}

/*
 * This function sync repositories and team's repositories permissions
 * It returns the list of deleted repos that must not be deleted but archived
 */
func (r *GoliacReconciliatorImpl) reconciliateRepositories(ctx context.Context, errorCollector *observability.ErrorCollection, local GoliacLocal, remote *MutableGoliacRemoteImpl, teamsreponame string, branch string, dryrun bool, toArchive map[string]*GithubRepoComparable, reposToRename map[string]*entity.Repository, manageGithubVariables bool) error {

	// let's start with the local cloned github-teams repo
	lRepos := make(map[string]*GithubRepoComparable)

	localRepositories := make(map[string]*entity.Repository)
	for reponame, repo := range local.Repositories() {

		// we rename the repository before we start to reconciliate
		if repo.RenameTo != "" {
			oldName := repo.Name
			renamedRepo := *repo
			renamedRepo.Name = repo.RenameTo
			renamedRepo.RenameTo = ""
			reponame = repo.RenameTo

			r.RenameRepository(ctx, errorCollector, dryrun, remote, repo.Name, repo.RenameTo)

			// in the post action we have to also update the git repository
			reposToRename[oldName] = repo
			repo = &renamedRepo
		}

		localRepositories[reponame] = repo
	}

	// let's get the remote now
	rRepos := make(map[string]*GithubRepoComparable)

	ghRepos := remote.Repositories()
	for k, v := range ghRepos {
		repo := &GithubRepoComparable{
			Visibility:          v.Visibility,
			BoolProperties:      map[string]bool{},
			Writers:             []string{},
			Readers:             []string{},
			ExternalUserReaders: []string{},
			ExternalUserWriters: []string{},
			InternalUsers:       []string{},
			Rulesets:            v.RuleSets,
			BranchProtections:   v.BranchProtections,
			DefaultBranchName:   v.DefaultBranchName,
			Environments:        v.Environments,
			ActionVariables:     v.RepositoryVariables,
		}
		for pk, pv := range v.BoolProperties {
			repo.BoolProperties[pk] = pv
		}

		for cGithubid, cPermission := range v.ExternalUsers {
			if cPermission == "WRITE" {
				repo.ExternalUserWriters = append(repo.ExternalUserWriters, cGithubid)
			} else {
				repo.ExternalUserReaders = append(repo.ExternalUserReaders, cGithubid)
			}
		}

		// we dont want internal collaborators
		for cGithubid := range v.InternalUsers {
			repo.InternalUsers = append(repo.InternalUsers, cGithubid)
		}

		rRepos[k] = repo
	}

	// on the remote object, I have teams->repos, and I need repos->teams
	for t, repos := range remote.TeamRepositories() {
		for r, p := range repos {
			if rr, ok := rRepos[r]; ok {
				if p.Permission == "ADMIN" || p.Permission == "WRITE" {
					rr.Writers = append(rr.Writers, t)
				} else {
					rr.Readers = append(rr.Readers, t)
				}
			}
		}
	}

	// adding the goliac-teams repo
	teamsRepo := &entity.Repository{}
	teamsRepo.ApiVersion = "v1"
	teamsRepo.Kind = "Repository"
	teamsRepo.Name = teamsreponame
	teamsRepo.Spec.Writers = []string{r.repoconfig.AdminTeam}
	teamsRepo.Spec.Readers = []string{}
	teamsRepo.Spec.Visibility = "private"
	teamsRepo.Spec.DefaultBranchName = "main"
	if ghtr, ok := ghRepos[teamsreponame]; ok {
		teamsRepo.Spec.Visibility = ghtr.Visibility
		teamsRepo.Spec.DefaultBranchName = ghtr.DefaultBranchName
	}
	teamsRepo.Spec.DeleteBranchOnMerge = true
	// cf goliac.go:L231-252
	bp := entity.RepositoryBranchProtection{
		Pattern:                      branch,
		RequiresApprovingReviews:     true,
		RequiredApprovingReviewCount: 1,
		RequiresStatusChecks:         true,
		RequiresStrictStatusChecks:   true,
		RequiredStatusCheckContexts:  []string{},
		RequireLastPushApproval:      true,
	}
	if config.Config.ServerGitBranchProtectionRequiredCheck != "" {
		bp.RequiredStatusCheckContexts = append(bp.RequiredStatusCheckContexts, config.Config.ServerGitBranchProtectionRequiredCheck)
	}
	teamsRepo.Spec.BranchProtections = []entity.RepositoryBranchProtection{bp}
	localRepositories[teamsreponame] = teamsRepo

	for reponame, lRepo := range localRepositories {
		writers := make([]string, 0)
		for _, w := range lRepo.Spec.Writers {
			writers = append(writers, slug.Make(w))
		}
		// add the team owner's name ;-)
		if lRepo.Owner != nil {
			writers = append(writers, slug.Make(*lRepo.Owner))
		}
		readers := make([]string, 0)
		for _, r := range lRepo.Spec.Readers {
			// dont add the owner to the readers (if listed)
			if lRepo.Owner != nil && *lRepo.Owner == r {
				continue
			}
			readers = append(readers, slug.Make(r))
		}

		// special case for the Goliac "teams" repo
		if reponame == teamsreponame {
			for teamname := range local.Teams() {
				writers = append(writers, slug.Make(teamname)+config.Config.GoliacTeamOwnerSuffix)
			}
		}

		// adding the "everyone" team to each repository
		if r.repoconfig.EveryoneTeamEnabled {
			readers = append(readers, "everyone")
		}

		// adding exernal reader/writer
		eReaders := make([]string, 0)
		for _, r := range lRepo.Spec.ExternalUserReaders {
			if user, ok := local.ExternalUsers()[r]; ok {
				eReaders = append(eReaders, user.Spec.GithubID)
			}
		}

		eWriters := make([]string, 0)
		for _, w := range lRepo.Spec.ExternalUserWriters {
			if user, ok := local.ExternalUsers()[w]; ok {
				eWriters = append(eWriters, user.Spec.GithubID)
			}
		}

		rulesets := make(map[string]*GithubRuleSet)
		for _, rs := range lRepo.Spec.Rulesets {
			ruleset := GithubRuleSet{
				Name:        rs.Name,
				Enforcement: rs.Enforcement,
				BypassApps:  map[string]string{},
				OnInclude:   rs.Conditions.Include,
				OnExclude:   rs.Conditions.Exclude,
				Rules:       map[string]entity.RuleSetParameters{},
			}
			for _, b := range rs.BypassApps {
				ruleset.BypassApps[b.AppName] = b.Mode
			}
			for _, r := range rs.Rules {
				ruleset.Rules[r.Ruletype] = r.Parameters
			}
			rulesets[rs.Name] = &ruleset
		}

		branchprotections := make(map[string]*GithubBranchProtection)
		for _, bp := range lRepo.Spec.BranchProtections {
			branchprotection := GithubBranchProtection{
				Id:                             "",
				Pattern:                        bp.Pattern,
				RequiresApprovingReviews:       bp.RequiresApprovingReviews,
				RequiredApprovingReviewCount:   bp.RequiredApprovingReviewCount,
				DismissesStaleReviews:          bp.DismissesStaleReviews,
				RequiresCodeOwnerReviews:       bp.RequiresCodeOwnerReviews,
				RequireLastPushApproval:        bp.RequireLastPushApproval,
				RequiresStatusChecks:           bp.RequiresStatusChecks,
				RequiresStrictStatusChecks:     bp.RequiresStrictStatusChecks,
				RequiredStatusCheckContexts:    bp.RequiredStatusCheckContexts,
				RequiresConversationResolution: bp.RequiresConversationResolution,
				RequiresCommitSignatures:       bp.RequiresCommitSignatures,
				RequiresLinearHistory:          bp.RequiresLinearHistory,
				AllowsForcePushes:              bp.AllowsForcePushes,
				AllowsDeletions:                bp.AllowsDeletions,
			}
			branchprotections[bp.Pattern] = &branchprotection
		}

		environments := make(map[string]*GithubEnvironment)
		for _, e := range lRepo.Spec.Environments {
			environments[e.Name] = &GithubEnvironment{
				Name:      e.Name,
				Variables: e.Variables,
			}
		}

		lRepos[utils.GithubAnsiString(reponame)] = r.reconciliatorFilter.RepositoryFilter(reponame, &GithubRepoComparable{
			BoolProperties: map[string]bool{
				"archived":               lRepo.Archived,
				"allow_auto_merge":       lRepo.Spec.AllowAutoMerge,
				"delete_branch_on_merge": lRepo.Spec.DeleteBranchOnMerge,
				"allow_update_branch":    lRepo.Spec.AllowUpdateBranch,
			},
			Visibility:          lRepo.Spec.Visibility,
			Readers:             readers,
			Writers:             writers,
			ExternalUserReaders: eReaders,
			ExternalUserWriters: eWriters,
			InternalUsers:       []string{},
			Rulesets:            rulesets,
			BranchProtections:   branchprotections,
			DefaultBranchName:   lRepo.Spec.DefaultBranchName,
			Environments:        NewLocalLazyLoader(environments),
			ActionVariables:     NewLocalLazyLoader(lRepo.Spec.ActionsVariables),
		})
	}

	// now we compare local (slugTeams) and remote (rTeams)

	compareRepos := func(reponame string, lRepo *GithubRepoComparable, rRepo *GithubRepoComparable) bool {
		archived := lRepo.BoolProperties["archived"]
		if !archived {
			//
			// "nested" rulesets comparison
			//
			onRulesetAdded := func(rulename string, lRuleset *GithubRuleSet, rRuleset *GithubRuleSet) {
				// CREATE repo ruleset
				r.AddRepositoryRuleset(ctx, errorCollector, dryrun, reponame, lRuleset)
			}
			onRulesetRemoved := func(rulename string, lRuleset *GithubRuleSet, rRuleset *GithubRuleSet) {
				// DELETE repo ruleset
				r.DeleteRepositoryRuleset(ctx, errorCollector, dryrun, reponame, rRuleset)
			}
			onRulesetChange := func(rulename string, lRuleset *GithubRuleSet, rRuleset *GithubRuleSet) {
				// UPDATE ruleset
				lRuleset.Id = rRuleset.Id
				r.UpdateRepositoryRuleset(ctx, errorCollector, dryrun, reponame, lRuleset)
			}
			CompareEntities(lRepo.Rulesets, rRepo.Rulesets, compareRulesets, onRulesetAdded, onRulesetRemoved, onRulesetChange)

			//
			// "nested" branchprotections comparison
			//
			onBranchProtectionAdded := func(rulename string, lBp *GithubBranchProtection, rBp *GithubBranchProtection) {
				// CREATE repo branchprotection
				r.AddRepositoryBranchProtection(ctx, errorCollector, dryrun, reponame, lBp)
			}
			onBranchProtectionRemoved := func(rulename string, lBp *GithubBranchProtection, rBp *GithubBranchProtection) {
				// DELETE repo branchprotection
				r.DeleteRepositoryBranchProtection(ctx, errorCollector, dryrun, reponame, rBp)
			}
			onBranchProtectionChange := func(rulename string, lBp *GithubBranchProtection, rBp *GithubBranchProtection) {
				// UPDATE branchprotection
				lBp.Id = rBp.Id
				r.UpdateRepositoryBranchProtection(ctx, errorCollector, dryrun, reponame, lBp)
			}
			CompareEntities(lRepo.BranchProtections, rRepo.BranchProtections, compareBranchProtections, onBranchProtectionAdded, onBranchProtectionRemoved, onBranchProtectionChange)

			if manageGithubVariables {
				//
				// "nested" environments comparison
				//
				onEnvironmentAdded := func(environment string, lEnv *GithubEnvironment, rEnv *GithubEnvironment) {
					// CREATE repo environment
					r.AddRepositoryEnvironment(ctx, errorCollector, dryrun, remote, reponame, environment)
				}
				onEnvironmentChange := func(environment string, lEnv *GithubEnvironment, rEnv *GithubEnvironment) {
					// UPDATE repo environment

					// Check for removed or changed keys
					for name, value := range lEnv.Variables {
						if rValue, ok := rEnv.Variables[name]; !ok {
							r.AddRepositoryEnvironmentVariable(ctx, errorCollector, dryrun, remote, reponame, environment, name, value)
						} else if rValue != value {
							r.UpdateRepositoryEnvironmentVariable(ctx, errorCollector, dryrun, remote, reponame, environment, name, value)
						}
					}

					// Check for added keys
					for name := range rEnv.Variables {
						if _, ok := lEnv.Variables[name]; !ok {
							r.DeleteRepositoryEnvironmentVariable(ctx, errorCollector, dryrun, remote, reponame, environment, name)
						}
					}
				}
				onEnvironmentRemoved := func(environment string, lEnv *GithubEnvironment, rEnv *GithubEnvironment) {
					// DELETE repo environment
					r.DeleteRepositoryEnvironment(ctx, errorCollector, dryrun, remote, reponame, environment)
				}
				CompareEntities(lRepo.Environments.GetEntity(), rRepo.Environments.GetEntity(), compareEnvironments, onEnvironmentAdded, onEnvironmentRemoved, onEnvironmentChange)
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
			if rr, ok := ghRepos[reponame]; ok {
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

		return true
	}

	onChanged := func(reponame string, lRepo *GithubRepoComparable, rRepo *GithubRepoComparable) {
		// reconciliate repositories boolean properties
		for lk, lv := range lRepo.BoolProperties {
			if rv, ok := rRepo.BoolProperties[lk]; !ok || rv != lv {
				r.UpdateRepositoryUpdateProperty(ctx, errorCollector, dryrun, remote, reponame, lk, lv)
			}
		}
		if lRepo.Visibility != rRepo.Visibility {
			// check back if the remote repo is a fork
			// in this case, we cannot change the visibility
			if rr, ok := ghRepos[reponame]; ok {
				if !rr.IsFork {
					r.UpdateRepositoryUpdateProperty(ctx, errorCollector, dryrun, remote, reponame, "visibility", lRepo.Visibility)
				}
			} else {
				r.UpdateRepositoryUpdateProperty(ctx, errorCollector, dryrun, remote, reponame, "visibility", lRepo.Visibility)
			}
		}
		if lRepo.DefaultBranchName != "" && lRepo.DefaultBranchName != rRepo.DefaultBranchName {
			r.UpdateRepositoryUpdateProperty(ctx, errorCollector, dryrun, remote, reponame, "default_branch", lRepo.DefaultBranchName)
		}

		if res, readToRemove, readToAdd := entity.StringArrayEquivalent(lRepo.Readers, rRepo.Readers); !res {
			for _, teamSlug := range readToAdd {
				r.UpdateRepositoryAddTeamAccess(ctx, errorCollector, dryrun, remote, reponame, teamSlug, "pull")
			}
			for _, teamSlug := range readToRemove {
				r.UpdateRepositoryRemoveTeamAccess(ctx, errorCollector, dryrun, remote, reponame, teamSlug)
			}
		}

		if res, writeToRemove, writeToAdd := entity.StringArrayEquivalent(lRepo.Writers, rRepo.Writers); !res {
			for _, teamSlug := range writeToAdd {
				r.UpdateRepositoryAddTeamAccess(ctx, errorCollector, dryrun, remote, reponame, teamSlug, "push")
			}
			for _, teamSlug := range writeToRemove {
				r.UpdateRepositoryRemoveTeamAccess(ctx, errorCollector, dryrun, remote, reponame, teamSlug)
			}
		}

		// internal users
		for _, internalUser := range rRepo.InternalUsers {
			r.UpdateRepositoryRemoveInternalUser(ctx, errorCollector, dryrun, remote, reponame, internalUser)
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
					r.UpdateRepositoryRemoveExternalUser(ctx, errorCollector, dryrun, remote, reponame, eReader)
				}
			}
			for _, eReader := range ereaderToAdd {
				r.UpdateRepositorySetExternalUser(ctx, errorCollector, dryrun, remote, reponame, eReader, "pull")
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
					r.UpdateRepositoryRemoveExternalUser(ctx, errorCollector, dryrun, remote, reponame, eWriter)
				}
			}
			for _, eWriter := range ewriteToAdd {
				r.UpdateRepositorySetExternalUser(ctx, errorCollector, dryrun, remote, reponame, eWriter, "push")
			}
		}

		if manageGithubVariables {
			archived := lRepo.BoolProperties["archived"]
			if !archived {
				if !utils.DeepEqualUnordered(lRepo.ActionVariables.GetEntity(), rRepo.ActionVariables.GetEntity()) {

					// Check for removed or changed keys
					for name, value := range lRepo.ActionVariables.GetEntity() {
						if rValue, ok := rRepo.ActionVariables.GetEntity()[name]; !ok {
							r.AddRepositoryVariable(ctx, errorCollector, dryrun, remote, reponame, name, value)
						} else if rValue != value {
							r.UpdateRepositoryVariable(ctx, errorCollector, dryrun, remote, reponame, name, value)
						}
					}

					// Check for added keys
					for name := range rRepo.ActionVariables.GetEntity() {
						if _, ok := lRepo.ActionVariables.GetEntity()[name]; !ok {
							r.DeleteRepositoryVariable(ctx, errorCollector, dryrun, remote, reponame, name)
						}
					}
				}
			}
		}
	}

	onAdded := func(reponame string, lRepo *GithubRepoComparable, rRepo *GithubRepoComparable) {
		// CREATE repository

		// if the repo was just archived in a previous commit and we "resume it"
		if aRepo, ok := toArchive[reponame]; ok {
			delete(toArchive, reponame)
			r.UpdateRepositoryUpdateProperty(ctx, errorCollector, dryrun, remote, reponame, "archived", false)
			// calling onChanged to update the repository permissions
			onChanged(reponame, aRepo, rRepo)
		} else {
			// check if the repo is a fork
			if lr, ok := localRepositories[reponame]; ok {
				if lr.ForkFrom != "" {
					r.CreateRepository(ctx, errorCollector, dryrun, remote, reponame, reponame, lRepo.Visibility, lRepo.Writers, lRepo.Readers, lRepo.BoolProperties, lRepo.DefaultBranchName, lr.ForkFrom)
				} else {
					r.CreateRepository(ctx, errorCollector, dryrun, remote, reponame, reponame, lRepo.Visibility, lRepo.Writers, lRepo.Readers, lRepo.BoolProperties, lRepo.DefaultBranchName, "")
				}
			} else {
				r.CreateRepository(ctx, errorCollector, dryrun, remote, reponame, reponame, lRepo.Visibility, lRepo.Writers, lRepo.Readers, lRepo.BoolProperties, lRepo.DefaultBranchName, "")
			}
		}
	}

	onRemoved := func(reponame string, lRepo *GithubRepoComparable, rRepo *GithubRepoComparable) {
		// here we have a repository that is not listed in the teams repository.
		// we should call DeleteRepository (that will delete if AllowDestructiveRepositories is on).
		// but if we have ArchiveOnDelete...
		if r.repoconfig.ArchiveOnDelete {
			if r.repoconfig.DestructiveOperations.AllowDestructiveRepositories {
				r.UpdateRepositoryUpdateProperty(ctx, errorCollector, dryrun, remote, reponame, "archived", true)
				toArchive[reponame] = rRepo
			} else {
				r.unmanaged.Repositories[reponame] = true
			}
		} else {
			r.DeleteRepository(ctx, errorCollector, dryrun, remote, reponame)
		}
	}

	CompareEntities(lRepos, rRepos, compareRepos, onAdded, onRemoved, onChanged)

	return nil
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
	if lbp.DismissesStaleReviews != rbp.DismissesStaleReviews {
		return false
	}
	if lbp.RequiresCodeOwnerReviews != rbp.RequiresCodeOwnerReviews {
		return false
	}
	if lbp.RequireLastPushApproval != rbp.RequireLastPushApproval {
		return false
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

func (r *GoliacReconciliatorImpl) reconciliateRulesets(ctx context.Context, errorCollector *observability.ErrorCollection, local GoliacLocal, remote *MutableGoliacRemoteImpl, teamsreponame string, conf *config.RepositoryConfig, dryrun bool) error {
	repositories := local.Repositories()

	lgrs := map[string]*GithubRuleSet{}
	// prepare local comparable
	for _, confrs := range conf.Rulesets {
		rs, ok := local.RuleSets()[confrs]
		if !ok {
			return fmt.Errorf("not able to find ruleset %s definition", confrs)
		}

		grs := GithubRuleSet{
			Name:        rs.Name,
			Enforcement: rs.Spec.Ruleset.Enforcement,
			BypassApps:  map[string]string{},
			BypassTeams: map[string]string{},
			OnInclude:   rs.Spec.Ruleset.Conditions.Include,
			OnExclude:   rs.Spec.Ruleset.Conditions.Exclude,
			Rules:       map[string]entity.RuleSetParameters{},
		}
		for _, b := range rs.Spec.Ruleset.BypassApps {
			grs.BypassApps[b.AppName] = b.Mode
		}
		for _, b := range rs.Spec.Ruleset.BypassTeams {
			teamslug := slug.Make(b.TeamName)
			grs.BypassTeams[teamslug] = b.Mode
		}
		for _, r := range rs.Spec.Ruleset.Rules {
			grs.Rules[r.Ruletype] = r.Parameters
		}
		repolist := []string{}
		for reponame := range repositories {
			repolist = append(repolist, reponame)
		}
		repolist = append(repolist, teamsreponame)

		includedRepositories, err := rs.BuildRepositoriesList(repolist)
		if err != nil {
			return fmt.Errorf("not able to parse ruleset regular expression %s: %v", confrs, err)
		}
		grs.Repositories = includedRepositories

		lgrs[rs.Name] = &grs
	}

	// prepare remote comparable
	rgrs := remote.RuleSets()

	// prepare the diff computation

	onAdded := func(rulesetname string, lRuleset *GithubRuleSet, rRuleset *GithubRuleSet) {
		// CREATE ruleset

		r.AddRuleset(ctx, errorCollector, dryrun, lRuleset)
	}

	onRemoved := func(rulesetname string, lRuleset *GithubRuleSet, rRuleset *GithubRuleSet) {
		// DELETE ruleset
		r.DeleteRuleset(ctx, errorCollector, dryrun, rRuleset)
	}

	onChanged := func(rulesetname string, lRuleset *GithubRuleSet, rRuleset *GithubRuleSet) {
		// UPDATE ruleset
		lRuleset.Id = rRuleset.Id
		r.UpdateRuleset(ctx, errorCollector, dryrun, lRuleset)
	}

	CompareEntities(lgrs, rgrs, compareRulesets, onAdded, onRemoved, onChanged)

	return nil
}

func (r *GoliacReconciliatorImpl) AddUserToOrg(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, ghuserid string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "add_user_to_org"}).Infof("ghuserid: %s", ghuserid)
	remote.AddUserToOrg(ghuserid)
	if r.executor != nil {
		r.executor.AddUserToOrg(ctx, errorCollector, dryrun, ghuserid)
	}
}

func (r *GoliacReconciliatorImpl) RemoveUserFromOrg(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, ghuserid string) {
	if r.repoconfig.DestructiveOperations.AllowDestructiveUsers {
		logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "remove_user_from_org"}).Infof("ghuserid: %s", ghuserid)
		remote.RemoveUserFromOrg(ghuserid)
		if r.executor != nil {
			r.executor.RemoveUserFromOrg(ctx, errorCollector, dryrun, ghuserid)
		}
	} else {
		r.unmanaged.Users[ghuserid] = true
	}
}

func (r *GoliacReconciliatorImpl) CreateTeam(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, teamname string, description string, parentTeam *int, members []string) {
	parenTeamId := "nil"
	if parentTeam != nil {
		parenTeamId = fmt.Sprintf("%d", *parentTeam)
	}

	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "create_team"}).Infof("teamname: %s, parentTeam: %s, members: %s", teamname, parenTeamId, strings.Join(members, ","))
	remote.CreateTeam(teamname, description, members)
	if r.executor != nil {
		r.executor.CreateTeam(ctx, errorCollector, dryrun, teamname, description, parentTeam, members)
	}
}
func (r *GoliacReconciliatorImpl) UpdateTeamAddMember(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, teamslug string, ghuserid string, role string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_team_add_member"}).Infof("teamslug: %s, ghuserid: %s, role: %s", teamslug, ghuserid, role)
	remote.UpdateTeamAddMember(teamslug, ghuserid, "member")
	if r.executor != nil {
		r.executor.UpdateTeamAddMember(ctx, errorCollector, dryrun, teamslug, ghuserid, "member")
	}
}
func (r *GoliacReconciliatorImpl) UpdateTeamRemoveMember(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, teamslug string, ghuserid string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_team_remove_member"}).Infof("teamslug: %s, ghuserid: %s", teamslug, ghuserid)
	remote.UpdateTeamRemoveMember(teamslug, ghuserid)
	if r.executor != nil {
		r.executor.UpdateTeamRemoveMember(ctx, errorCollector, dryrun, teamslug, ghuserid)
	}
}
func (r *GoliacReconciliatorImpl) UpdateTeamChangeMaintainerToMember(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, teamslug string, ghuserid string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_team_change_maintainer_to_member"}).Infof("teamslug: %s, ghuserid: %s", teamslug, ghuserid)
	remote.UpdateTeamUpdateMember(teamslug, ghuserid, "member")
	if r.executor != nil {
		r.executor.UpdateTeamUpdateMember(ctx, errorCollector, dryrun, teamslug, ghuserid, "member")
	}
}
func (r *GoliacReconciliatorImpl) UpdateTeamSetParent(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, teamslug string, parentTeam *int, parentTeamName string) {
	parenTeamId := "nil"
	if parentTeam != nil {
		parenTeamId = fmt.Sprintf("%d", *parentTeam)
	}

	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_team_parentteam"}).Infof("teamslug: %s, parentteam: %s (%s)", teamslug, parenTeamId, parentTeamName)
	remote.UpdateTeamSetParent(ctx, dryrun, teamslug, parentTeam)
	if r.executor != nil {
		r.executor.UpdateTeamSetParent(ctx, errorCollector, dryrun, teamslug, parentTeam)
	}
}
func (r *GoliacReconciliatorImpl) DeleteTeam(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, teamslug string) {
	if r.repoconfig.DestructiveOperations.AllowDestructiveTeams {
		logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "delete_team"}).Infof("teamslug: %s", teamslug)
		remote.DeleteTeam(teamslug)
		if r.executor != nil {
			r.executor.DeleteTeam(ctx, errorCollector, dryrun, teamslug)
		}
	} else {
		r.unmanaged.Teams[teamslug] = true
	}
}
func (r *GoliacReconciliatorImpl) CreateRepository(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, descrition string, visibility string, writers []string, readers []string, boolProperties map[string]bool, defaultBranch string, forkFrom string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "create_repository"}).Infof("repositoryname: %s, readers: %s, writers: %s, boolProperties: %v", reponame, strings.Join(readers, ","), strings.Join(writers, ","), boolProperties)
	remote.CreateRepository(reponame, reponame, visibility, writers, readers, boolProperties, defaultBranch, forkFrom)
	if r.executor != nil {
		r.executor.CreateRepository(ctx, errorCollector, dryrun, reponame, reponame, visibility, writers, readers, boolProperties, defaultBranch, nil, forkFrom)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryAddTeamAccess(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, teamslug string, permission string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_add_team"}).Infof("repositoryname: %s, teamslug: %s, permission: %s", reponame, teamslug, permission)
	remote.UpdateRepositoryAddTeamAccess(reponame, teamslug, permission)
	if r.executor != nil {
		r.executor.UpdateRepositoryAddTeamAccess(ctx, errorCollector, dryrun, reponame, teamslug, permission)
	}
}

func (r *GoliacReconciliatorImpl) UpdateRepositoryUpdateTeamAccess(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, teamslug string, permission string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_update_team"}).Infof("repositoryname: %s, teamslug:%s, permission: %s", reponame, teamslug, permission)
	remote.UpdateRepositoryUpdateTeamAccess(reponame, teamslug, permission)
	if r.executor != nil {
		r.executor.UpdateRepositoryUpdateTeamAccess(ctx, errorCollector, dryrun, reponame, teamslug, permission)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryRemoveTeamAccess(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, teamslug string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_remove_team"}).Infof("repositoryname: %s, teamslug:%s", reponame, teamslug)
	remote.UpdateRepositoryRemoveTeamAccess(reponame, teamslug)
	if r.executor != nil {
		r.executor.UpdateRepositoryRemoveTeamAccess(ctx, errorCollector, dryrun, reponame, teamslug)
	}
}

func (r *GoliacReconciliatorImpl) DeleteRepository(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string) {
	if r.repoconfig.DestructiveOperations.AllowDestructiveRepositories {
		logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "delete_repository"}).Infof("repositoryname: %s", reponame)
		remote.DeleteRepository(reponame)
		if r.executor != nil {
			r.executor.DeleteRepository(ctx, errorCollector, dryrun, reponame)
		}
	} else {
		r.unmanaged.Repositories[reponame] = true
	}
}

func (r *GoliacReconciliatorImpl) RenameRepository(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, newname string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "rename_repository"}).Infof("repositoryname: %s newname: %s", reponame, newname)
	remote.RenameRepository(reponame, newname)
	if r.executor != nil {
		r.executor.RenameRepository(ctx, errorCollector, dryrun, reponame, newname)
	}
}

func (r *GoliacReconciliatorImpl) UpdateRepositoryUpdateProperty(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, propertyName string, propertyValue interface{}) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_update_property"}).Infof("repositoryname: %s %s:%v", reponame, propertyName, propertyValue)
	remote.UpdateRepositoryUpdateProperty(reponame, propertyName, propertyValue)
	if r.executor != nil {
		r.executor.UpdateRepositoryUpdateProperty(ctx, errorCollector, dryrun, reponame, propertyName, propertyValue)
	}
}
func (r *GoliacReconciliatorImpl) AddRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ruleset *GithubRuleSet) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "add_ruleset"}).Infof("ruleset: %s (id: %d) enforcement: %s", ruleset.Name, ruleset.Id, ruleset.Enforcement)
	if r.executor != nil {
		r.executor.AddRuleset(ctx, errorCollector, dryrun, ruleset)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ruleset *GithubRuleSet) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_ruleset"}).Infof("ruleset: %s (id: %d) enforcement: %s", ruleset.Name, ruleset.Id, ruleset.Enforcement)
	if r.executor != nil {
		r.executor.UpdateRuleset(ctx, errorCollector, dryrun, ruleset)
	}
}
func (r *GoliacReconciliatorImpl) DeleteRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ruleset *GithubRuleSet) {
	if r.repoconfig.DestructiveOperations.AllowDestructiveRulesets {
		logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "delete_ruleset"}).Infof("ruleset id:%d", ruleset.Id)
		if r.executor != nil {
			r.executor.DeleteRuleset(ctx, errorCollector, dryrun, ruleset.Id)
		}
	} else {
		r.unmanaged.RuleSets[ruleset.Name] = true
	}
}
func (r *GoliacReconciliatorImpl) AddRepositoryRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, ruleset *GithubRuleSet) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "add_repository_ruleset"}).Infof("repository: %s, ruleset: %s (id: %d) enforcement: %s", reponame, ruleset.Name, ruleset.Id, ruleset.Enforcement)
	if r.executor != nil {
		r.executor.AddRepositoryRuleset(ctx, errorCollector, dryrun, reponame, ruleset)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, ruleset *GithubRuleSet) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_ruleset"}).Infof("repository: %s, ruleset: %s (id: %d) enforcement: %s", reponame, ruleset.Name, ruleset.Id, ruleset.Enforcement)
	if r.executor != nil {
		r.executor.UpdateRepositoryRuleset(ctx, errorCollector, dryrun, reponame, ruleset)
	}
}
func (r *GoliacReconciliatorImpl) DeleteRepositoryRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, ruleset *GithubRuleSet) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "delete_repository_ruleset"}).Infof("repository: %s, ruleset id:%d", reponame, ruleset.Id)
	if r.executor != nil {
		r.executor.DeleteRepositoryRuleset(ctx, errorCollector, dryrun, reponame, ruleset.Id)
	}
}
func (r *GoliacReconciliatorImpl) AddRepositoryBranchProtection(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "add_repository_branchprotection"}).Infof("repository: %s, branchprotection: %s", reponame, branchprotection.Pattern)
	if r.executor != nil {
		r.executor.AddRepositoryBranchProtection(ctx, errorCollector, dryrun, reponame, branchprotection)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryBranchProtection(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_branchprotection"}).Infof("repository: %s, branchprotection: %s", reponame, branchprotection.Pattern)
	if r.executor != nil {
		r.executor.UpdateRepositoryBranchProtection(ctx, errorCollector, dryrun, reponame, branchprotection)
	}
}
func (r *GoliacReconciliatorImpl) DeleteRepositoryBranchProtection(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "delete_repository_branchprotection"}).Infof("repository: %s, branchprotection: %s", reponame, branchprotection.Pattern)
	if r.executor != nil {
		r.executor.DeleteRepositoryBranchProtection(ctx, errorCollector, dryrun, reponame, branchprotection)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositorySetExternalUser(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, collaboatorGithubId string, permission string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_set_external_user"}).Infof("repositoryname: %s collaborator:%s permission:%s", reponame, collaboatorGithubId, permission)
	remote.UpdateRepositorySetExternalUser(reponame, collaboatorGithubId, permission)
	if r.executor != nil {
		r.executor.UpdateRepositorySetExternalUser(ctx, errorCollector, dryrun, reponame, collaboatorGithubId, permission)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryRemoveInternalUser(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, collaboatorGithubId string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_remove_internal_user"}).Infof("repositoryname: %s collaborator:%s", reponame, collaboatorGithubId)
	remote.UpdateRepositoryRemoveInternalUser(reponame, collaboatorGithubId)
	if r.executor != nil {
		r.executor.UpdateRepositoryRemoveInternalUser(ctx, errorCollector, dryrun, reponame, collaboatorGithubId)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryRemoveExternalUser(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, collaboatorGithubId string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_remove_external_user"}).Infof("repositoryname: %s collaborator:%s", reponame, collaboatorGithubId)
	remote.UpdateRepositoryRemoveExternalUser(reponame, collaboatorGithubId)
	if r.executor != nil {
		r.executor.UpdateRepositoryRemoveExternalUser(ctx, errorCollector, dryrun, reponame, collaboatorGithubId)
	}
}
func (r *GoliacReconciliatorImpl) AddRepositoryEnvironment(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, environment string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "add_repository_environment"}).Infof("repository: %s, environment: %s", reponame, environment)
	remote.AddRepositoryEnvironment(reponame, environment)
	if r.executor != nil {
		r.executor.AddRepositoryEnvironment(ctx, errorCollector, dryrun, reponame, environment)
	}
}
func (r *GoliacReconciliatorImpl) DeleteRepositoryEnvironment(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, environment string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "delete_repository_environment"}).Infof("repository: %s, environment: %s", reponame, environment)
	remote.DeleteRepositoryEnvironment(reponame, environment)
	if r.executor != nil {
		r.executor.DeleteRepositoryEnvironment(ctx, errorCollector, dryrun, reponame, environment)
	}
}
func (r *GoliacReconciliatorImpl) AddRepositoryVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, variable string, value string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "add_repository_variable"}).Infof("repository: %s, variable: %s, value: %s", reponame, variable, value)
	remote.AddRepositoryVariable(reponame, variable, value)
	if r.executor != nil {
		r.executor.AddRepositoryVariable(ctx, errorCollector, dryrun, reponame, variable, value)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, variable string, value string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_variable"}).Infof("repository: %s, variable: %s, value: %s", reponame, variable, value)
	remote.UpdateRepositoryVariable(reponame, variable, value)
	if r.executor != nil {
		r.executor.UpdateRepositoryVariable(ctx, errorCollector, dryrun, reponame, variable, value)
	}
}
func (r *GoliacReconciliatorImpl) DeleteRepositoryVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, variable string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "delete_repository_variable"}).Infof("repository: %s, variable: %s", reponame, variable)
	remote.DeleteRepositoryVariable(reponame, variable)
	if r.executor != nil {
		r.executor.DeleteRepositoryVariable(ctx, errorCollector, dryrun, reponame, variable)
	}
}
func (r *GoliacReconciliatorImpl) AddRepositoryEnvironmentVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, environment string, variable string, value string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "add_repository_environment_variable"}).Infof("repository: %s, environment: %s, variable: %s, value: %s", reponame, environment, variable, value)
	remote.AddRepositoryEnvironmentVariable(reponame, environment, variable, value)
	if r.executor != nil {
		r.executor.AddRepositoryEnvironmentVariable(ctx, errorCollector, dryrun, reponame, environment, variable, value)
	}
}
func (r *GoliacReconciliatorImpl) UpdateRepositoryEnvironmentVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, environment string, variable string, value string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_repository_environment_variable"}).Infof("repository: %s, environment: %s, variable: %s, value: %s", reponame, environment, variable, value)
	remote.UpdateRepositoryEnvironmentVariable(reponame, environment, variable, value)
	if r.executor != nil {
		r.executor.UpdateRepositoryEnvironmentVariable(ctx, errorCollector, dryrun, reponame, environment, variable, value)
	}
}
func (r *GoliacReconciliatorImpl) DeleteRepositoryEnvironmentVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, remote *MutableGoliacRemoteImpl, reponame string, environment string, variable string) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "delete_repository_environment_variable"}).Infof("repository: %s, environment: %s, variable: %s", reponame, environment, variable)
	remote.DeleteRepositoryEnvironmentVariable(reponame, environment, variable)
	if r.executor != nil {
		r.executor.DeleteRepositoryEnvironmentVariable(ctx, errorCollector, dryrun, reponame, environment, variable)
	}
}
func (r *GoliacReconciliatorImpl) Begin(ctx context.Context, dryrun bool) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun}).Debugf("reconciliation begin")
	if r.executor != nil {
		r.executor.Begin(dryrun)
	}
}
func (r *GoliacReconciliatorImpl) Rollback(ctx context.Context, dryrun bool, err error) {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun}).Debugf("reconciliation rollback")
	if r.executor != nil {
		r.executor.Rollback(dryrun, err)
	}
}
func (r *GoliacReconciliatorImpl) Commit(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool) error {
	logrus.WithFields(map[string]interface{}{"dryrun": dryrun}).Debugf("reconciliation commit")
	if r.executor != nil {
		return r.executor.Commit(ctx, errorCollector, dryrun)
	}
	return nil
}
