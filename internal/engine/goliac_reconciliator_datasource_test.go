package engine

import (
	"testing"

	"github.com/goliac-project/goliac/internal/config"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/stretchr/testify/assert"
)

func TestGoliacReconciliatorDatasourceLocalWorkflowBypassInjection(t *testing.T) {
	wf := &entity.Workflow{}
	wf.Spec.Repositories.Allowed = []string{"myrepo"}

	local := &GoliacLocalImpl{
		teams:         map[string]*entity.Team{},
		repositories:  map[string]*entity.Repository{},
		users:         map[string]*entity.User{},
		externalUsers: map[string]*entity.User{},
		rulesets:      map[string]*entity.RuleSet{},
		workflows:     map[string]*entity.Workflow{"w": wf},
		repoconfig:    &config.RepositoryConfig{},
	}

	repo := &entity.Repository{}
	repo.Spec.BranchProtections = []entity.RepositoryBranchProtection{
		{Pattern: "main", RequiresApprovingReviews: true, RequiresCodeOwnerReviews: true},
	}
	local.repositories["myrepo"] = repo

	repoconf := &config.RepositoryConfig{AdminTeam: "admin"}
	d := NewGoliacReconciliatorDatasourceLocal(local, "teams", "main", true, repoconf, "goliac-app")

	repos, _, err := d.Repositories()
	assert.NoError(t, err)
	r := repos["myrepo"]
	bp := r.BranchProtections["main"]
	found := false
	for _, n := range bp.BypassPullRequestAllowances.Nodes {
		if n.Actor.AppSlug == "goliac-app" {
			found = true
		}
	}
	assert.True(t, found, "Goliac app should be injected as bypass on branch protection")
}

func TestGoliacReconciliatorDatasourceLocalWorkflowBypassNoDuplicate(t *testing.T) {
	wf := &entity.Workflow{}
	wf.Spec.Repositories.Allowed = []string{"myrepo"}

	local := &GoliacLocalImpl{
		teams:         map[string]*entity.Team{},
		repositories:  map[string]*entity.Repository{},
		users:         map[string]*entity.User{},
		externalUsers: map[string]*entity.User{},
		rulesets:      map[string]*entity.RuleSet{},
		workflows:     map[string]*entity.Workflow{"w": wf},
		repoconfig:    &config.RepositoryConfig{},
	}

	repo := &entity.Repository{}
	repo.Spec.BranchProtections = []entity.RepositoryBranchProtection{
		{
			Pattern:                  "main",
			RequiresApprovingReviews: true,
			BypassPullRequestApps:    []string{"goliac-app"},
		},
	}
	local.repositories["myrepo"] = repo

	repoconf := &config.RepositoryConfig{AdminTeam: "admin"}
	d := NewGoliacReconciliatorDatasourceLocal(local, "teams", "main", true, repoconf, "goliac-app")

	repos, _, err := d.Repositories()
	assert.NoError(t, err)
	r := repos["myrepo"]
	bp := r.BranchProtections["main"]
	count := 0
	for _, n := range bp.BypassPullRequestAllowances.Nodes {
		if n.Actor.AppSlug == "goliac-app" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestGoliacReconciliatorDatasourceLocalShouldInjectOrgRuleset(t *testing.T) {
	wf := &entity.Workflow{}
	wf.Spec.Repositories.Allowed = []string{"target-repo"}

	local := &GoliacLocalImpl{
		teams:         map[string]*entity.Team{},
		repositories:  map[string]*entity.Repository{},
		users:         map[string]*entity.User{},
		externalUsers: map[string]*entity.User{},
		rulesets:      map[string]*entity.RuleSet{},
		workflows:     map[string]*entity.Workflow{"w": wf},
		repoconfig:    &config.RepositoryConfig{},
	}

	repoconf := &config.RepositoryConfig{AdminTeam: "admin"}
	dl := NewGoliacReconciliatorDatasourceLocal(local, "teams", "main", true, repoconf, "goliac-app").(*GoliacReconciliatorDatasourceLocal)

	grs := &GithubRuleSet{
		Enforcement: "active",
		Rules: map[string]entity.RuleSetParameters{
			"pull_request": {},
		},
	}
	assert.True(t, dl.shouldInjectGoliacBypassOnOrgRuleset(grs, []string{"target-repo"}))
	assert.False(t, dl.shouldInjectGoliacBypassOnOrgRuleset(grs, []string{"other-repo"}))
	assert.False(t, dl.shouldInjectGoliacBypassOnOrgRuleset(&GithubRuleSet{Enforcement: "active", Rules: nil}, []string{"target-repo"}))
	assert.False(t, dl.shouldInjectGoliacBypassOnOrgRuleset(&GithubRuleSet{Enforcement: "disabled", Rules: map[string]entity.RuleSetParameters{"pull_request": {}}}, []string{"target-repo"}))
}

func TestGoliacReconciliatorDatasourceLocalCodeownersBypassInjection_ApprovingReviewCount(t *testing.T) {
	local := &GoliacLocalImpl{
		teams:         map[string]*entity.Team{},
		repositories:  map[string]*entity.Repository{},
		users:         map[string]*entity.User{},
		externalUsers: map[string]*entity.User{},
		rulesets:      map[string]*entity.RuleSet{},
		workflows:     map[string]*entity.Workflow{},
		repoconfig:    &config.RepositoryConfig{},
	}

	repo := &entity.Repository{}
	repo.Spec.CodeownersRaw = "* @org/team"
	repo.Spec.BranchProtections = []entity.RepositoryBranchProtection{
		{Pattern: "main", RequiredApprovingReviewCount: 2},
	}
	local.repositories["myrepo"] = repo

	repoconf := &config.RepositoryConfig{AdminTeam: "admin"}
	d := NewGoliacReconciliatorDatasourceLocal(local, "teams", "main", true, repoconf, "goliac-app")

	repos, _, err := d.Repositories()
	assert.NoError(t, err)
	r := repos["myrepo"]
	bp := r.BranchProtections["main"]
	found := false
	for _, n := range bp.BypassPullRequestAllowances.Nodes {
		if n.Actor.AppSlug == "goliac-app" {
			found = true
		}
	}
	assert.True(t, found, "Goliac app should be injected as bypass on branch protection when codeowners are defined")
}

func TestGoliacReconciliatorDatasourceLocalCodeownersBypassInjection_RulesetWithCount(t *testing.T) {
	local := &GoliacLocalImpl{
		teams:         map[string]*entity.Team{},
		repositories:  map[string]*entity.Repository{},
		users:         map[string]*entity.User{},
		externalUsers: map[string]*entity.User{},
		rulesets:      map[string]*entity.RuleSet{},
		workflows:     map[string]*entity.Workflow{},
		repoconfig:    &config.RepositoryConfig{},
	}

	repo := &entity.Repository{}
	repo.Spec.CodeownersRaw = "* @org/team"
	lruleset := entity.RepositoryRuleSet{
		Name: "myruleset",
	}
	lruleset.Enforcement = "active"
	lruleset.Conditions.Include = []string{"~DEFAULT_BRANCH"}
	lruleset.Rules = append(lruleset.Rules, struct {
		Ruletype   string
		Parameters entity.RuleSetParameters `yaml:"parameters,omitempty"`
	}{
		"pull_request", entity.RuleSetParameters{RequiredApprovingReviewCount: 1},
	})
	repo.Spec.Rulesets = []entity.RepositoryRuleSet{lruleset}
	local.repositories["myrepo"] = repo

	repoconf := &config.RepositoryConfig{AdminTeam: "admin"}
	d := NewGoliacReconciliatorDatasourceLocal(local, "teams", "main", true, repoconf, "goliac-app")

	repos, _, err := d.Repositories()
	assert.NoError(t, err)
	r := repos["myrepo"]
	rs := r.Rulesets["myruleset"]
	assert.Equal(t, githubWorkflowAppBypassMode, rs.BypassApps["goliac-app"])
}
