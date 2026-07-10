package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/goliac-project/goliac/internal/config"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/goliac-project/goliac/internal/utils"
	"github.com/gosimple/slug"
)

type GoliacReconciliatorDatasource interface {
	Users() map[string]string                                                   // key is the login, value is the role (MEMBER, ADMIN)
	Teams() (map[string]*GithubTeamComparable, map[string]bool, error)          // team, externallyManaged, error
	Repositories() (map[string]*GithubRepoComparable, map[string]string, error) // repo, renameTo, error
	RuleSets() (map[string]*GithubRuleSet, error)
	OrgCustomProperties(ctx context.Context) map[string]*config.GithubCustomProperty
}

// githubWorkflowAppBypassMode is the ruleset bypass mode for the Goliac app when
// auto-injected for workflow-scoped repositories (GitHub API: pull_request).
const githubWorkflowAppBypassMode = "pull_request"

type GoliacReconciliatorDatasourceLocal struct {
	local               GoliacLocal
	conf                *config.RepositoryConfig
	teamsreponame       string
	teamsDefaultBranch  string
	reconciliatorFilter *ReconciliatorFilterImpl
	githubAppSlug       string
}

func NewGoliacReconciliatorDatasourceLocal(
	local GoliacLocal,
	teamsreponame string,
	teamsDefaultBranch string,
	isEnterprise bool,
	conf *config.RepositoryConfig,
	githubAppSlug string,
) GoliacReconciliatorDatasource {
	return &GoliacReconciliatorDatasourceLocal{
		local:               local,
		conf:                conf,
		teamsreponame:       teamsreponame,
		teamsDefaultBranch:  teamsDefaultBranch,
		reconciliatorFilter: NewReconciliatorFilter(isEnterprise, conf),
		githubAppSlug:       githubAppSlug,
	}
}

func (d *GoliacReconciliatorDatasourceLocal) Users() map[string]string {
	users := make(map[string]string)
	for _, user := range d.local.Users() {
		users[user.Spec.GithubID] = user.Spec.GithubID
	}
	return users
}

func (d *GoliacReconciliatorDatasourceLocal) Teams() (map[string]*GithubTeamComparable, map[string]bool, error) {
	// prepare the teams we want (regular and "-goliac-owners"/config.Config.GoliacTeamOwnerSuffix)
	slugTeams := make(map[string]*GithubTeamComparable)
	lTeams := d.local.Teams()
	lUsers := d.local.Users()
	externallyManagedTeams := make(map[string]bool)

	for teamname, teamvalue := range lTeams {
		teamslug := slug.Make(teamname)

		if teamvalue.Spec.ExternallyManaged {
			externallyManagedTeams[teamslug] = true
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
			Name:              teamname,
			Slug:              teamslug,
			Members:           members,
			ExternallyManaged: teamvalue.Spec.ExternallyManaged,
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
	if d.conf.EveryoneTeamEnabled {
		everyone := GithubTeamComparable{
			Name:    "everyone",
			Slug:    "everyone",
			Members: []string{},
		}
		for u := range d.local.Users() {
			everyone.Members = append(everyone.Members, u)
		}
		slugTeams["everyone"] = &everyone
	}
	return slugTeams, externallyManagedTeams, nil
}

func (d *GoliacReconciliatorDatasourceLocal) Repositories() (map[string]*GithubRepoComparable, map[string]string, error) {
	// let's start with the local cloned github-teams repo
	lRepos := make(map[string]*GithubRepoComparable)
	lTeams := d.local.Teams()
	lUsers := d.local.Users()
	renameTo := make(map[string]string)

	localRepositories := make(map[string]*entity.Repository)

	// adding the goliac-teams repo
	teamsRepo := &entity.Repository{}
	teamsRepo.ApiVersion = "v1"
	teamsRepo.Kind = "Repository"
	teamsRepo.Name = d.teamsreponame
	teamsRepo.Spec.Writers = []string{d.conf.AdminTeam}
	teamsRepo.Spec.Readers = []string{}

	teamsRepo.Spec.Visibility = "internal"
	teamsRepo.Spec.DefaultBranchName = d.teamsDefaultBranch

	// hardcode the goliac-admin repository squash and merge
	teamsRepo.Spec.AllowMergeCommit = false
	teamsRepo.Spec.AllowSquashMerge = true
	teamsRepo.Spec.AllowRebaseMerge = false
	teamsRepo.Spec.DeleteBranchOnMerge = true
	teamsRepo.Spec.DefaultMergeCommitMessage = "Default message"
	teamsRepo.Spec.DefaultSquashCommitMessage = "Default message"

	teamsRepo.Spec.DeleteBranchOnMerge = true
	// cf goliac.go:L231-252
	bp := entity.RepositoryBranchProtection{
		Pattern:                      d.teamsDefaultBranch,
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
	localRepositories[d.teamsreponame] = teamsRepo

	// addming regular repositories
	for reponame, repo := range d.local.Repositories() {

		if repo.RenameTo != "" {
			renameTo[reponame] = repo.RenameTo
		}

		localRepositories[reponame] = repo
	}

	for reponame, lRepo := range localRepositories {
		writers := make([]string, 0)
		for _, w := range lRepo.Spec.Writers {
			writers = append(writers, slug.Make(w))
		}
		// add the team owner's name ;-)
		if lRepo.Owner != nil {
			// dobt add it twice
			slugOwner := slug.Make(*lRepo.Owner)
			alreadyAdded := false
			for _, w := range writers {
				if w == slugOwner {
					alreadyAdded = true
					break
				}
			}
			if !alreadyAdded {
				writers = append(writers, slugOwner)
			}
		}
		readers := make([]string, 0)
		for _, r := range lRepo.Spec.Readers {
			// checking if the reader was already added as a writer
			slugReader := slug.Make(r)
			alreadyAdded := false
			for _, w := range writers {
				if w == slugReader {
					alreadyAdded = true
					break
				}
			}
			if !alreadyAdded {
				readers = append(readers, slugReader)
			}
		}

		// special case for the Goliac "teams" repo
		if reponame == d.teamsreponame {
			for teamname := range d.local.Teams() {
				writers = append(writers, slug.Make(teamname)+config.Config.GoliacTeamOwnerSuffix)
			}
		}

		// adding the "everyone" team to each repository
		if d.conf.EveryoneTeamEnabled {
			readers = append(readers, "everyone")
		}

		// adding exernal reader/writer
		eReaders := make([]string, 0)
		for _, r := range lRepo.Spec.ExternalUserReaders {
			if user, ok := d.local.ExternalUsers()[r]; ok {
				eReaders = append(eReaders, user.Spec.GithubID)
			}
		}

		eWriters := make([]string, 0)
		for _, w := range lRepo.Spec.ExternalUserWriters {
			if user, ok := d.local.ExternalUsers()[w]; ok {
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
			for _, u := range bp.BypassPullRequestUsers {
				if githubId, ok := lUsers[u]; ok {
					node := BypassPullRequestAllowanceNode{}
					node.Actor.UserLogin = githubId.Spec.GithubID
					branchprotection.BypassPullRequestAllowances.Nodes = append(branchprotection.BypassPullRequestAllowances.Nodes, node)
				}
			}
			for _, t := range bp.BypassPullRequestTeams {
				if _, ok := lTeams[t]; ok {
					node := BypassPullRequestAllowanceNode{}
					node.Actor.TeamSlug = slug.Make(t)
					branchprotection.BypassPullRequestAllowances.Nodes = append(branchprotection.BypassPullRequestAllowances.Nodes, node)
				}
			}
			for _, a := range bp.BypassPullRequestApps {
				node := BypassPullRequestAllowanceNode{}
				node.Actor.AppSlug = slug.Make(a)
				branchprotection.BypassPullRequestAllowances.Nodes = append(branchprotection.BypassPullRequestAllowances.Nodes, node)
			}
			branchprotections[bp.Pattern] = &branchprotection
		}

		hasCodeowners := len(lRepo.Spec.Codeowners) > 0 || strings.TrimSpace(lRepo.Spec.CodeownersRaw) != ""

		// we need to add the Goliac app on the branch protection bypass
		// if the repository is part of a workflow and if a CODEOWNER review is required
		// the goal is to allow the Goliac app to bypass the CODEOWNER review when the repository is part of a workflow
		// or if the repository defines codeowners and branch protection requires approving reviews
		if d.githubAppSlug != "" && (len(branchprotections) > 0 || len(rulesets) > 0) {
			for _, bp := range branchprotections {
				if (d.local.RepositoryInWorkflow(reponame) && bp.RequiresCodeOwnerReviews) ||
					(hasCodeowners && (bp.RequiredApprovingReviewCount > 0 || bp.RequiresCodeOwnerReviews)) {
					ensureGoliacAppBypassOnBranchProtection(bp, d.githubAppSlug)
				}
			}
			for _, rs := range rulesets {
				if r, ok := rs.Rules["pull_request"]; ok {
					if (d.local.RepositoryInWorkflow(reponame) && r.RequireCodeOwnerReview) ||
						(hasCodeowners && (r.RequireCodeOwnerReview || r.RequiredApprovingReviewCount > 0)) {
						ensureGoliacAppBypassOnRuleset(rs, d.githubAppSlug)
					}
				}
			}
		}

		environments := make(map[string]*GithubEnvironment)
		for _, e := range lRepo.Spec.Environments {
			environments[e.Name] = &GithubEnvironment{
				Name:      e.Name,
				Variables: e.Variables,
			}
		}

		var autolinks MappedEntityLazyLoader[*GithubAutolink]

		if lRepo.Spec.Autolinks != nil {
			autolinksMap := make(map[string]*GithubAutolink)
			for _, a := range *lRepo.Spec.Autolinks {
				autolinksMap[a.KeyPrefix] = &GithubAutolink{
					Id:             0,
					KeyPrefix:      a.KeyPrefix,
					UrlTemplate:    a.UrlTemplate,
					IsAlphanumeric: a.IsAlphanumeric,
				}
			}
			autolinks = NewLocalLazyLoader(autolinksMap)
		}

		// Convert custom properties from local entity to comparable
		customProps := make(map[string]interface{})
		if lRepo.Spec.CustomProperties != nil {
			for k, v := range lRepo.Spec.CustomProperties {
				customProps[k] = v
			}
		}

		// Generate CODEOWNERS content if codeowners entries are defined
		codeownersContent := lRepo.GenerateCodeownersContent(config.Config.GithubAppOrganization)

		lRepos[utils.GithubAnsiString(reponame)] = d.reconciliatorFilter.RepositoryFilter(reponame, &GithubRepoComparable{
			BoolProperties: map[string]bool{
				"archived":               lRepo.Archived,
				"allow_auto_merge":       lRepo.Spec.AllowAutoMerge,
				"allow_merge_commit":     lRepo.Spec.AllowMergeCommit,
				"allow_squash_merge":     lRepo.Spec.AllowSquashMerge,
				"allow_rebase_merge":     lRepo.Spec.AllowRebaseMerge,
				"delete_branch_on_merge": lRepo.Spec.DeleteBranchOnMerge,
				"allow_update_branch":    lRepo.Spec.AllowUpdateBranch,
			},
			Visibility:                 lRepo.Spec.Visibility,
			Readers:                    readers,
			Writers:                    writers,
			ExternalUserReaders:        eReaders,
			ExternalUserWriters:        eWriters,
			InternalUsers:              []string{},
			Rulesets:                   rulesets,
			BranchProtections:          branchprotections,
			DefaultBranchName:          lRepo.Spec.DefaultBranchName,
			Environments:               NewLocalLazyLoader(environments),
			ActionVariables:            NewLocalLazyLoader(lRepo.Spec.ActionsVariables),
			Autolinks:                  autolinks,
			DefaultMergeCommitMessage:  lRepo.Spec.DefaultMergeCommitMessage,
			DefaultSquashCommitMessage: lRepo.Spec.DefaultSquashCommitMessage,
			CustomProperties:           customProps,
			Topics:                     lRepo.Spec.Topics,
			Codeowners:                 codeownersContent,
			GithubPages:                entityGithubPagesToComparable(lRepo.Spec.GithubPages),
			IsFork:                     lRepo.ForkFrom != "",
			ForkFrom:                   lRepo.ForkFrom,
		})
	}
	return lRepos, renameTo, nil
}

func (d *GoliacReconciliatorDatasourceLocal) RuleSets() (map[string]*GithubRuleSet, error) {
	repositories := d.local.Repositories()

	lgrs := map[string]*GithubRuleSet{}
	// prepare local comparable
	for _, confrs := range d.conf.Rulesets {
		rs, ok := d.local.RuleSets()[confrs]
		if !ok {
			return nil, fmt.Errorf("not able to find ruleset %s definition", confrs)
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
		repolist = append(repolist, d.teamsreponame)

		includedRepositories, err := rs.BuildRepositoriesList(repolist)
		if err != nil {
			return nil, fmt.Errorf("not able to parse ruleset regular expression %s: %v", confrs, err)
		}
		// Filter out archived repositories
		nonArchivedRepositories := []string{}
		for _, reponame := range includedRepositories {
			if repo, ok := repositories[reponame]; ok {
				if repo.Archived {
					continue
				}
			}
			nonArchivedRepositories = append(nonArchivedRepositories, reponame)
		}
		grs.Repositories = nonArchivedRepositories

		if d.shouldInjectGoliacBypassOnOrgRuleset(&grs, nonArchivedRepositories) {
			ensureGoliacAppBypassOnRuleset(&grs, d.githubAppSlug)
		}

		lgrs[rs.Name] = &grs
	}
	return lgrs, nil
}

func (d *GoliacReconciliatorDatasourceLocal) shouldInjectGoliacBypassOnOrgRuleset(grs *GithubRuleSet, nonArchivedRepositories []string) bool {
	if d.githubAppSlug == "" || grs == nil {
		return false
	}
	if len(grs.Rules) == 0 || strings.EqualFold(grs.Enforcement, "disabled") {
		return false
	}
	for _, reponame := range nonArchivedRepositories {
		if d.local.RepositoryInWorkflow(reponame) {
			return true
		}
	}
	return false
}

func branchProtectionHasAppBypass(bp *GithubBranchProtection, appSlug string) bool {
	want := slug.Make(appSlug)
	for _, n := range bp.BypassPullRequestAllowances.Nodes {
		if n.Actor.AppSlug != "" && slug.Make(n.Actor.AppSlug) == want {
			return true
		}
	}
	return false
}

func ensureGoliacAppBypassOnBranchProtection(bp *GithubBranchProtection, appSlug string) {
	if appSlug == "" || branchProtectionHasAppBypass(bp, appSlug) {
		return
	}
	node := BypassPullRequestAllowanceNode{}
	node.Actor.AppSlug = appSlug
	bp.BypassPullRequestAllowances.Nodes = append(bp.BypassPullRequestAllowances.Nodes, node)
}

func rulesetHasAppBypass(rs *GithubRuleSet, appSlug string) bool {
	if rs == nil || rs.BypassApps == nil {
		return false
	}
	want := slug.Make(appSlug)
	for k := range rs.BypassApps {
		if slug.Make(k) == want {
			return true
		}
	}
	return false
}

func ensureGoliacAppBypassOnRuleset(rs *GithubRuleSet, appSlug string) {
	if appSlug == "" || rulesetHasAppBypass(rs, appSlug) {
		return
	}
	if rs.BypassApps == nil {
		rs.BypassApps = map[string]string{}
	}
	rs.BypassApps[appSlug] = githubWorkflowAppBypassMode
}

func (d *GoliacReconciliatorDatasourceLocal) OrgCustomProperties(ctx context.Context) map[string]*config.GithubCustomProperty {
	localProps := make(map[string]*config.GithubCustomProperty)
	for _, prop := range d.conf.OrgCustomProperties {
		propCopy := prop
		localProps[prop.PropertyName] = propCopy
	}
	return localProps
}

type GoliacReconciliatorDatasourceRemote struct {
	remote GoliacRemote
}

func NewGoliacReconciliatorDatasourceRemote(remote GoliacRemote) GoliacReconciliatorDatasource {
	return &GoliacReconciliatorDatasourceRemote{remote: remote}
}

func (d *GoliacReconciliatorDatasourceRemote) Users() map[string]string {
	ghUsers := d.remote.Users(context.Background())

	rUsers := make(map[string]string)
	for k, v := range ghUsers {
		rUsers[k] = v.Role
	}
	return rUsers
}

func (d *GoliacReconciliatorDatasourceRemote) Teams() (map[string]*GithubTeamComparable, map[string]bool, error) {
	ghTeams := d.remote.Teams(context.Background(), true)
	rUsers := d.remote.Users(context.Background())

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
			if u, ok := rUsers[m]; ok {
				if u.Role == "ADMIN" {
					members = append(members, m)
				} else {
					maintainers = append(maintainers, m)
				}
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
			Id:          v.Id,
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
	return rTeams, nil, nil
}

func (d *GoliacReconciliatorDatasourceRemote) Repositories() (map[string]*GithubRepoComparable, map[string]string, error) {

	// let's get the remote now
	rRepos := make(map[string]*GithubRepoComparable)

	ghRepos := d.remote.Repositories(context.Background())
	for k, v := range ghRepos {
		repo := &GithubRepoComparable{
			Visibility:                 v.Visibility,
			BoolProperties:             map[string]bool{},
			Writers:                    []string{},
			Readers:                    []string{},
			ExternalUserReaders:        []string{},
			ExternalUserWriters:        []string{},
			InternalUsers:              []string{},
			Rulesets:                   v.RuleSets,
			BranchProtections:          v.BranchProtections,
			DefaultBranchName:          v.DefaultBranchName,
			Environments:               v.Environments,
			ActionVariables:            v.RepositoryVariables,
			Autolinks:                  v.Autolinks,
			DefaultMergeCommitMessage:  v.DefaultMergeCommitMessage,
			DefaultSquashCommitMessage: v.DefaultSquashCommitMessage,
			CustomProperties:           make(map[string]interface{}),
			Topics:                     v.Topics,
			Codeowners:                 v.CodeownersContent,
			CodeownersSHA:              v.CodeownersSHA,
			GithubPages:                githubPagesRemoteToComparable(v.GithubPages),
			IsFork:                     v.IsFork,
		}
		for pk, pv := range v.BoolProperties {
			repo.BoolProperties[pk] = pv
		}

		// Copy custom properties from remote repository
		if v.CustomProperties != nil {
			for k, v := range v.CustomProperties {
				repo.CustomProperties[k] = v
			}
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
	for t, repos := range d.remote.TeamRepositories(context.Background()) {
		for r, p := range repos {
			if rr, ok := rRepos[r]; ok {
				if p.Permission == "WRITE" {
					rr.Writers = append(rr.Writers, t)
				} else {
					rr.Readers = append(rr.Readers, t)
				}
			}
		}
	}
	return rRepos, nil, nil
}

func (d *GoliacReconciliatorDatasourceRemote) RuleSets() (map[string]*GithubRuleSet, error) {
	return d.remote.RuleSets(context.Background()), nil
}

func (d *GoliacReconciliatorDatasourceRemote) OrgCustomProperties(ctx context.Context) map[string]*config.GithubCustomProperty {
	return d.remote.OrgCustomProperties(ctx)
}
