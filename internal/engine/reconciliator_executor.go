package engine

import (
	"context"

	"github.com/goliac-project/goliac/internal/observability"
)

type ReconciliatorExecutor interface {
	AddUserToOrg(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ghuserid string)
	RemoveUserFromOrg(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ghuserid string)

	CreateTeam(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamname string, description string, parentTeam *int, members []string)
	UpdateTeamAddMember(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string, username string, role string)    // role can be 'member' or 'maintainer'
	UpdateTeamUpdateMember(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string, username string, role string) // role can be 'member' or 'maintainer'
	UpdateTeamRemoveMember(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string, username string)
	UpdateTeamSetParent(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string, parentTeam *int)
	DeleteTeam(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string)

	// CreateRepository can have a githubToken parameter (if null it use Goliac token)
	CreateRepository(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, descrition string, visibility string, writers []string, readers []string, boolProperties map[string]bool, defaultBranch string, githubToken *string, forkFrom string)
	UpdateRepositoryUpdateProperty(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, propertyName string, propertyValue interface{})
	UpdateRepositoryAddTeamAccess(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, teamslug string, permission string)    // permission can be "pull", "push", or "admin" which correspond to read, write, and admin access.
	UpdateRepositoryUpdateTeamAccess(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, teamslug string, permission string) // permission can be "pull", "push", or "admin" which correspond to read, write, and admin access.
	UpdateRepositoryRemoveTeamAccess(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, teamslug string)
	AddRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ruleset *GithubRuleSet)
	UpdateRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ruleset *GithubRuleSet)
	DeleteRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, rulesetid int)
	AddRepositoryRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, ruleset *GithubRuleSet)
	UpdateRepositoryRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, ruleset *GithubRuleSet)
	DeleteRepositoryRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, rulesetid int)
	AddRepositoryBranchProtection(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection)
	UpdateRepositoryBranchProtection(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection)
	DeleteRepositoryBranchProtection(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection)
	UpdateRepositorySetExternalUser(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, githubid string, permission string) // permission can be "pull" or "push"
	UpdateRepositoryRemoveExternalUser(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, githubid string)
	UpdateRepositoryRemoveInternalUser(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, githubid string)
	DeleteRepository(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string)
	RenameRepository(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, newname string)

	Begin(dryrun bool)
	Rollback(dryrun bool, err error)
	Commit(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool) error
}
