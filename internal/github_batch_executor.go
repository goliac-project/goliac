package internal

import (
	"context"
	"fmt"

	"github.com/goliac-project/goliac/internal/config"
	"github.com/goliac-project/goliac/internal/engine"
	"github.com/goliac-project/goliac/internal/observability"
)

/**
 * Each command/mutation we want to perform will be isloated into a GithubCommand
 * object, so we can regroup all of them to apply (or cancel) them in batch
 */
type GithubCommand interface {
	Apply(ctx context.Context, errorCollector *observability.ErrorCollection)
}

/*
 * GithubBatchExecutor will collects all commands to apply
 * if there the number of changes to apply is not too big, it will apply on the `Commit()`
 * Usage:
 * gal := NewGithubBatchExecutor(client)
 * gal.Begin()
 * gal.Create...
 * gal.Update...
 * ...
 * gal.Commit()
 */
type GithubBatchExecutor struct {
	client        engine.ReconciliatorExecutor
	maxChangesets int
	commands      []GithubCommand
}

func NewGithubBatchExecutor(client engine.ReconciliatorExecutor, maxChangesets int) *GithubBatchExecutor {
	gal := GithubBatchExecutor{
		client:        client,
		maxChangesets: maxChangesets,
		commands:      make([]GithubCommand, 0),
	}
	return &gal
}

func (g *GithubBatchExecutor) AddUserToOrg(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ghuserid string) {
	g.commands = append(g.commands, &GithubCommandAddUserToOrg{
		client:   g.client,
		dryrun:   dryrun,
		ghuserid: ghuserid,
	})
}

func (g *GithubBatchExecutor) RemoveUserFromOrg(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ghuserid string) {
	g.commands = append(g.commands, &GithubCommandAddUserToOrg{
		client:   g.client,
		dryrun:   dryrun,
		ghuserid: ghuserid,
	})
}

func (g *GithubBatchExecutor) CreateTeam(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamname string, description string, parentTeam *int, members []string) {
	g.commands = append(g.commands, &GithubCommandCreateTeam{
		client:      g.client,
		dryrun:      dryrun,
		teamname:    teamname,
		description: description,
		parentTeam:  parentTeam,
		members:     members,
	})
}

// role = member or maintainer (usually we use member)
func (g *GithubBatchExecutor) UpdateTeamAddMember(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string, username string, role string) {
	g.commands = append(g.commands, &GithubCommandUpdateTeamAddMember{
		client:   g.client,
		dryrun:   dryrun,
		teamslug: teamslug,
		member:   username,
		role:     role,
	})
}

// role = member or maintainer (usually we use member)
func (g *GithubBatchExecutor) UpdateTeamUpdateMember(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string, username string, role string) {
	g.commands = append(g.commands, &GithubCommandUpdateTeamUpdateMember{
		client:   g.client,
		dryrun:   dryrun,
		teamslug: teamslug,
		member:   username,
		role:     role,
	})
}

func (g *GithubBatchExecutor) UpdateTeamRemoveMember(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string, username string) {
	g.commands = append(g.commands, &GithubCommandUpdateTeamRemoveMember{
		client:   g.client,
		dryrun:   dryrun,
		teamslug: teamslug,
		member:   username,
	})
}

func (g *GithubBatchExecutor) UpdateTeamSetParent(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string, parentTeam *int) {
	g.commands = append(g.commands, &GithubCommandUpdateTeamSetParent{
		client:     g.client,
		dryrun:     dryrun,
		teamslug:   teamslug,
		parentTeam: parentTeam,
	})
}

func (g *GithubBatchExecutor) DeleteTeam(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string) {
	g.commands = append(g.commands, &GithubCommandDeleteTeam{
		client:   g.client,
		dryrun:   dryrun,
		teamslug: teamslug,
	})
}

func (g *GithubBatchExecutor) CreateRepository(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, description string, visibility string, writers []string, readers []string, boolProperties map[string]bool, defaultBranch string, githubToken *string) {
	g.commands = append(g.commands, &GithubCommandCreateRepository{
		client:         g.client,
		dryrun:         dryrun,
		reponame:       reponame,
		description:    description,
		visibility:     visibility,
		readers:        readers,
		writers:        writers,
		boolProperties: boolProperties,
		defaultBranch:  defaultBranch,
		githubToken:    githubToken,
	})
}

func (g *GithubBatchExecutor) UpdateRepositoryAddTeamAccess(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, teamslug string, permission string) {
	g.commands = append(g.commands, &GithubCommandUpdateRepositoryAddTeamAccess{
		client:     g.client,
		dryrun:     dryrun,
		reponame:   reponame,
		teamslug:   teamslug,
		permission: permission,
	})
}

func (g *GithubBatchExecutor) UpdateRepositoryUpdateTeamAccess(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, teamslug string, permission string) {
	g.commands = append(g.commands, &GithubCommandUpdateRepositoryUpdateTeamAccess{
		client:     g.client,
		dryrun:     dryrun,
		reponame:   reponame,
		teamslug:   teamslug,
		permission: permission,
	})
}

func (g *GithubBatchExecutor) UpdateRepositoryRemoveTeamAccess(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, teamslug string) {
	g.commands = append(g.commands, &GithubCommandUpdateRepositoryRemoveTeamAccess{
		client:   g.client,
		dryrun:   dryrun,
		reponame: reponame,
		teamslug: teamslug,
	})
}

func (g *GithubBatchExecutor) UpdateRepositoryUpdateProperty(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, propertyName string, propertyValue interface{}) {
	g.commands = append(g.commands, &GithubCommandUpdateRepositoryUpdateProperty{
		client:        g.client,
		dryrun:        dryrun,
		reponame:      reponame,
		propertyName:  propertyName,
		propertyValue: propertyValue,
	})
}

func (g *GithubBatchExecutor) UpdateRepositorySetExternalUser(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, githubid string, permission string) {
	g.commands = append(g.commands, &GithubCommandUpdateRepositorySetExternalUser{
		client:     g.client,
		dryrun:     dryrun,
		reponame:   reponame,
		githubid:   githubid,
		permission: permission,
	})
}

func (g *GithubBatchExecutor) UpdateRepositoryRemoveExternalUser(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, githubid string) {
	g.commands = append(g.commands, &GithubCommandUpdateRepositoryRemoveExternalUser{
		client:   g.client,
		dryrun:   dryrun,
		reponame: reponame,
		githubid: githubid,
	})
}

func (g *GithubBatchExecutor) UpdateRepositoryRemoveInternalUser(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, githubid string) {
	g.commands = append(g.commands, &GithubCommandUpdateRepositoryRemoveInternalUser{
		client:   g.client,
		dryrun:   dryrun,
		reponame: reponame,
		githubid: githubid,
	})
}

func (g *GithubBatchExecutor) AddRepositoryBranchProtection(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, branchprotection *engine.GithubBranchProtection) {
	g.commands = append(g.commands, &GithubCommandAddRepositoryBranchProtection{
		client:           g.client,
		dryrun:           dryrun,
		reponame:         reponame,
		branchprotection: branchprotection,
	})
}
func (g *GithubBatchExecutor) UpdateRepositoryBranchProtection(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, branchprotection *engine.GithubBranchProtection) {
	g.commands = append(g.commands, &GithubCommandUpdateRepositoryBranchProtection{
		client:           g.client,
		dryrun:           dryrun,
		reponame:         reponame,
		branchprotection: branchprotection,
	})
}
func (g *GithubBatchExecutor) DeleteRepositoryBranchProtection(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, branchprotection *engine.GithubBranchProtection) {
	g.commands = append(g.commands, &GithubCommandDeleteRepositoryBranchProtection{
		client:           g.client,
		dryrun:           dryrun,
		reponame:         reponame,
		branchprotection: branchprotection,
	})
}

func (g *GithubBatchExecutor) DeleteRepository(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string) {
	g.commands = append(g.commands, &GithubCommandDeleteRepository{
		client:   g.client,
		dryrun:   dryrun,
		reponame: reponame,
	})
}

func (g *GithubBatchExecutor) RenameRepository(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, newname string) {
	g.commands = append(g.commands, &GithubCommandRenameRepository{
		client:   g.client,
		dryrun:   dryrun,
		reponame: reponame,
		newname:  newname,
	})
}

func (g *GithubBatchExecutor) AddRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ruleset *engine.GithubRuleSet) {
	g.commands = append(g.commands, &GithubCommandAddRuletset{
		client:  g.client,
		dryrun:  dryrun,
		ruleset: ruleset,
	})
}

func (g *GithubBatchExecutor) UpdateRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ruleset *engine.GithubRuleSet) {
	g.commands = append(g.commands, &GithubCommandUpdateRuletset{
		client:  g.client,
		dryrun:  dryrun,
		ruleset: ruleset,
	})
}

func (g *GithubBatchExecutor) DeleteRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, rulesetid int) {
	g.commands = append(g.commands, &GithubCommandDeleteRuletset{
		client:    g.client,
		dryrun:    dryrun,
		rulesetid: rulesetid,
	})
}

func (g *GithubBatchExecutor) AddRepositoryRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, ruleset *engine.GithubRuleSet) {
	g.commands = append(g.commands, &GithubCommandAddRepositoryRuletset{
		client:   g.client,
		dryrun:   dryrun,
		reponame: reponame,
		ruleset:  ruleset,
	})
}

func (g *GithubBatchExecutor) UpdateRepositoryRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, ruleset *engine.GithubRuleSet) {
	g.commands = append(g.commands, &GithubCommandUpdateRepositoryRuletset{
		client:   g.client,
		dryrun:   dryrun,
		reponame: reponame,
		ruleset:  ruleset,
	})
}

func (g *GithubBatchExecutor) DeleteRepositoryRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, rulesetid int) {
	g.commands = append(g.commands, &GithubCommandDeleteRepositoryRuletset{
		client:    g.client,
		dryrun:    dryrun,
		reponame:  reponame,
		rulesetid: rulesetid,
	})
}

func (g *GithubBatchExecutor) Begin(dryrun bool) {
	g.commands = make([]GithubCommand, 0)
}
func (g *GithubBatchExecutor) Rollback(dryrun bool, err error) {
	g.commands = make([]GithubCommand, 0)
}
func (g *GithubBatchExecutor) Commit(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool) error {
	if len(g.commands) > g.maxChangesets && !config.Config.MaxChangesetsOverride {
		return fmt.Errorf("more than %d changesets to apply (total of %d), this is suspicious. Aborting (see Goliac troubleshooting guide for help)", g.maxChangesets, len(g.commands))
	}
	for _, c := range g.commands {
		c.Apply(ctx, errorCollector)
	}
	g.commands = make([]GithubCommand, 0)
	return nil
}

type GithubCommandAddUserToOrg struct {
	client   engine.ReconciliatorExecutor
	dryrun   bool
	ghuserid string
}

func (g *GithubCommandAddUserToOrg) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.AddUserToOrg(ctx, errorCollector, g.dryrun, g.ghuserid)
}

type GithubCommandCreateRepository struct {
	client         engine.ReconciliatorExecutor
	dryrun         bool
	reponame       string
	description    string
	visibility     string
	writers        []string
	readers        []string
	boolProperties map[string]bool
	defaultBranch  string
	githubToken    *string
}

func (g *GithubCommandCreateRepository) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.CreateRepository(ctx, errorCollector, g.dryrun, g.reponame, g.description, g.visibility, g.writers, g.readers, g.boolProperties, g.defaultBranch, g.githubToken)
}

type GithubCommandCreateTeam struct {
	client      engine.ReconciliatorExecutor
	dryrun      bool
	teamname    string
	description string
	parentTeam  *int
	members     []string
}

func (g *GithubCommandCreateTeam) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.CreateTeam(ctx, errorCollector, g.dryrun, g.teamname, g.description, g.parentTeam, g.members)
}

type GithubCommandDeleteRepository struct {
	client   engine.ReconciliatorExecutor
	dryrun   bool
	reponame string
}

func (g *GithubCommandDeleteRepository) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.DeleteRepository(ctx, errorCollector, g.dryrun, g.reponame)
}

type GithubCommandRenameRepository struct {
	client   engine.ReconciliatorExecutor
	dryrun   bool
	reponame string
	newname  string
}

func (g *GithubCommandRenameRepository) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.RenameRepository(ctx, errorCollector, g.dryrun, g.reponame, g.newname)
}

type GithubCommandDeleteTeam struct {
	client   engine.ReconciliatorExecutor
	dryrun   bool
	teamslug string
}

func (g *GithubCommandDeleteTeam) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.DeleteTeam(ctx, errorCollector, g.dryrun, g.teamslug)
}

type GithubCommandRemoveUserFromOrg struct {
	client   engine.ReconciliatorExecutor
	dryrun   bool
	ghuserid string
}

func (g *GithubCommandRemoveUserFromOrg) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.RemoveUserFromOrg(ctx, errorCollector, g.dryrun, g.ghuserid)
}

type GithubCommandUpdateRepositoryRemoveTeamAccess struct {
	client   engine.ReconciliatorExecutor
	dryrun   bool
	reponame string
	teamslug string
}

func (g *GithubCommandUpdateRepositoryRemoveTeamAccess) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.UpdateRepositoryRemoveTeamAccess(ctx, errorCollector, g.dryrun, g.reponame, g.teamslug)
}

type GithubCommandUpdateRepositoryAddTeamAccess struct {
	client     engine.ReconciliatorExecutor
	dryrun     bool
	reponame   string
	teamslug   string
	permission string
}

func (g *GithubCommandUpdateRepositoryAddTeamAccess) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.UpdateRepositoryAddTeamAccess(ctx, errorCollector, g.dryrun, g.reponame, g.teamslug, g.permission)
}

type GithubCommandUpdateRepositoryUpdateTeamAccess struct {
	client     engine.ReconciliatorExecutor
	dryrun     bool
	reponame   string
	teamslug   string
	permission string
}

func (g *GithubCommandUpdateRepositoryUpdateTeamAccess) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.UpdateRepositoryUpdateTeamAccess(ctx, errorCollector, g.dryrun, g.reponame, g.teamslug, g.permission)
}

type GithubCommandUpdateRepositorySetExternalUser struct {
	client     engine.ReconciliatorExecutor
	dryrun     bool
	reponame   string
	githubid   string
	permission string
}

func (g *GithubCommandUpdateRepositorySetExternalUser) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.UpdateRepositorySetExternalUser(ctx, errorCollector, g.dryrun, g.reponame, g.githubid, g.permission)
}

type GithubCommandUpdateRepositoryRemoveExternalUser struct {
	client   engine.ReconciliatorExecutor
	dryrun   bool
	reponame string
	githubid string
}

func (g *GithubCommandUpdateRepositoryRemoveExternalUser) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.UpdateRepositoryRemoveExternalUser(ctx, errorCollector, g.dryrun, g.reponame, g.githubid)
}

type GithubCommandUpdateRepositoryRemoveInternalUser struct {
	client   engine.ReconciliatorExecutor
	dryrun   bool
	reponame string
	githubid string
}

func (g *GithubCommandUpdateRepositoryRemoveInternalUser) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.UpdateRepositoryRemoveInternalUser(ctx, errorCollector, g.dryrun, g.reponame, g.githubid)
}

type GithubCommandUpdateRepositoryUpdateProperty struct {
	client        engine.ReconciliatorExecutor
	dryrun        bool
	reponame      string
	propertyName  string
	propertyValue interface{}
}

func (g *GithubCommandUpdateRepositoryUpdateProperty) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.UpdateRepositoryUpdateProperty(ctx, errorCollector, g.dryrun, g.reponame, g.propertyName, g.propertyValue)
}

type GithubCommandUpdateTeamAddMember struct {
	client   engine.ReconciliatorExecutor
	dryrun   bool
	teamslug string
	member   string
	role     string
}

func (g *GithubCommandUpdateTeamAddMember) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.UpdateTeamAddMember(ctx, errorCollector, g.dryrun, g.teamslug, g.member, g.role)
}

type GithubCommandUpdateTeamRemoveMember struct {
	client   engine.ReconciliatorExecutor
	dryrun   bool
	teamslug string
	member   string
}

func (g *GithubCommandUpdateTeamRemoveMember) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.UpdateTeamRemoveMember(ctx, errorCollector, g.dryrun, g.teamslug, g.member)
}

type GithubCommandUpdateTeamUpdateMember struct {
	client   engine.ReconciliatorExecutor
	dryrun   bool
	teamslug string
	member   string
	role     string
}

func (g *GithubCommandUpdateTeamUpdateMember) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.UpdateTeamUpdateMember(ctx, errorCollector, g.dryrun, g.teamslug, g.member, g.role)
}

type GithubCommandUpdateTeamSetParent struct {
	client     engine.ReconciliatorExecutor
	dryrun     bool
	teamslug   string
	parentTeam *int
}

func (g *GithubCommandUpdateTeamSetParent) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.UpdateTeamSetParent(ctx, errorCollector, g.dryrun, g.teamslug, g.parentTeam)
}

type GithubCommandAddRepositoryRuletset struct {
	client   engine.ReconciliatorExecutor
	dryrun   bool
	reponame string
	ruleset  *engine.GithubRuleSet
}

func (g *GithubCommandAddRepositoryRuletset) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.AddRepositoryRuleset(ctx, errorCollector, g.dryrun, g.reponame, g.ruleset)
}

type GithubCommandUpdateRepositoryRuletset struct {
	client   engine.ReconciliatorExecutor
	dryrun   bool
	reponame string
	ruleset  *engine.GithubRuleSet
}

func (g *GithubCommandUpdateRepositoryRuletset) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.UpdateRepositoryRuleset(ctx, errorCollector, g.dryrun, g.reponame, g.ruleset)
}

type GithubCommandDeleteRepositoryRuletset struct {
	client    engine.ReconciliatorExecutor
	dryrun    bool
	reponame  string
	rulesetid int
}

func (g *GithubCommandDeleteRepositoryRuletset) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.DeleteRepositoryRuleset(ctx, errorCollector, g.dryrun, g.reponame, g.rulesetid)
}

type GithubCommandAddRepositoryBranchProtection struct {
	client           engine.ReconciliatorExecutor
	dryrun           bool
	reponame         string
	branchprotection *engine.GithubBranchProtection
}

func (g *GithubCommandAddRepositoryBranchProtection) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.AddRepositoryBranchProtection(ctx, errorCollector, g.dryrun, g.reponame, g.branchprotection)
}

type GithubCommandUpdateRepositoryBranchProtection struct {
	client           engine.ReconciliatorExecutor
	dryrun           bool
	reponame         string
	branchprotection *engine.GithubBranchProtection
}

func (g *GithubCommandUpdateRepositoryBranchProtection) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.UpdateRepositoryBranchProtection(ctx, errorCollector, g.dryrun, g.reponame, g.branchprotection)
}

type GithubCommandDeleteRepositoryBranchProtection struct {
	client           engine.ReconciliatorExecutor
	dryrun           bool
	reponame         string
	branchprotection *engine.GithubBranchProtection
}

func (g *GithubCommandDeleteRepositoryBranchProtection) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.DeleteRepositoryBranchProtection(ctx, errorCollector, g.dryrun, g.reponame, g.branchprotection)
}

type GithubCommandAddRuletset struct {
	client  engine.ReconciliatorExecutor
	dryrun  bool
	ruleset *engine.GithubRuleSet
}

func (g *GithubCommandAddRuletset) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.AddRuleset(ctx, errorCollector, g.dryrun, g.ruleset)
}

type GithubCommandUpdateRuletset struct {
	client  engine.ReconciliatorExecutor
	dryrun  bool
	ruleset *engine.GithubRuleSet
}

func (g *GithubCommandUpdateRuletset) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.UpdateRuleset(ctx, errorCollector, g.dryrun, g.ruleset)
}

type GithubCommandDeleteRuletset struct {
	client    engine.ReconciliatorExecutor
	dryrun    bool
	rulesetid int
}

func (g *GithubCommandDeleteRuletset) Apply(ctx context.Context, errorCollector *observability.ErrorCollection) {
	g.client.DeleteRuleset(ctx, errorCollector, g.dryrun, g.rulesetid)
}
