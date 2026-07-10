package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/goliac-project/goliac/internal/config"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/goliac-project/goliac/internal/github"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/gosimple/slug"
	"github.com/hashicorp/go-version"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

const FORLOOP_STOP = 100

type GoliacRemoteResources interface {
	Teams(ctx context.Context, current bool) map[string]*GithubTeam
	RepositoriesSecretsPerRepository(ctx context.Context, repositoryName string) (map[string]*GithubVariable, error)
	EnvironmentSecretsPerRepository(ctx context.Context, environments []string, repositoryName string) (map[string]map[string]*GithubVariable, error)
	// needed to get the HTML URL of the Pages site
	GetRepositoryPages(ctx context.Context, repositoryName string) (*GithubPagesRemote, error)
}

/*
 * GoliacRemote
 * This interface is used to load the goliac organization from a Github
 * and mount it in memory
 */
type GoliacRemote interface {
	// Load from a github repository. continueOnError is used for scaffolding
	Load(ctx context.Context, continueOnError bool) error

	// Flush all assets from the cache
	FlushCache()

	// Flush only the users, and teams from the cache
	FlushCacheUsersTeamsOnly()

	Users(ctx context.Context) map[string]*GithubUser // key is the login, value is the role (MEMBER, ADMIN) + graphqlID
	TeamSlugByName(ctx context.Context) map[string]string
	Teams(ctx context.Context, current bool) map[string]*GithubTeam             // the key is the team slug
	Repositories(ctx context.Context) map[string]*GithubRepository              // the key is the repository name
	TeamRepositories(ctx context.Context) map[string]map[string]*GithubTeamRepo // key is team slug, second key is repo name
	RuleSets(ctx context.Context) map[string]*GithubRuleSet
	AppIds(ctx context.Context) map[string]*GithubApp

	IsEnterprise() bool // check if we are on an Enterprise version, or if we are on GHES 3.11+

	CountAssets(ctx context.Context, warmup bool) (int, error)                                              // return the number of (some) assets that will be loaded (to be used with the RemoteObservability/progress bar)
	SetRemoteObservability(feedback observability.RemoteObservability)                                      // if you want to get feedback on the loading process
	SetFeatureFlags(manageGithubVariables bool, manageGithubAutolinks bool, manageOrgCustomProperties bool) // needed becasue we dont know at construction what are the values

	RepositoriesSecretsPerRepository(ctx context.Context, repositoryName string) (map[string]*GithubVariable, error)
	EnvironmentSecretsPerRepository(ctx context.Context, environments []string, repositoryName string) (map[string]map[string]*GithubVariable, error)

	OrgCustomProperties(ctx context.Context) map[string]*config.GithubCustomProperty

	// GetRepositoryPages returns cached Pages info from the last Load, or fetches GET /repos/{org}/{repo}/pages.
	GetRepositoryPages(ctx context.Context, repositoryName string) (*GithubPagesRemote, error)
}

type GoliacRemoteExecutor interface {
	GoliacRemote
	ReconciliatorExecutor
}

type GithubRepository struct {
	Name                string
	Id                  int
	RefId               string
	Visibility          string                             // public, internal, private
	BoolProperties      map[string]bool                    // archived, allow_auto_merge, delete_branch_on_merge, allow_update_branch, allow_merge_commit, allow_squash_merge, allow_rebase_merge
	ExternalUsers       map[string]string                  // [githubid]permission
	InternalUsers       map[string]string                  // [githubid]permission
	RuleSets            map[string]*GithubRuleSet          // [name]ruleset
	BranchProtections   map[string]*GithubBranchProtection // [pattern]branch protection
	DefaultBranchName   string
	IsFork              bool
	Environments        MappedEntityLazyLoader[*GithubEnvironment] // [name]environment
	RepositoryVariables MappedEntityLazyLoader[string]             // [variableName]variableValue
	// RepositorySecrets    map[string]string            // [variableName]variableValue
	Autolinks                  MappedEntityLazyLoader[*GithubAutolink] // [keyPrefix]autolink
	DefaultMergeCommitMessage  string
	DefaultSquashCommitMessage string
	CustomProperties           map[string]interface{} // [propertyName]propertyValue (string or []string)
	Topics                     []string               // repository topics
	CodeownersContent          string                 // content of .github/CODEOWNERS file
	CodeownersSHA              string                 // SHA of .github/CODEOWNERS file (needed for updates)
	GithubPages                *GithubPagesRemote     // from GET /repos/{org}/{repo}/pages; nil if no site
}

type GithubUser struct {
	Login     string // login, ie github id
	GraphqlId string // graphql id
	Role      string // MEMBER, ADMIN
}

type GithubTeam struct {
	Name        string
	Id          int
	GraphqlId   string
	Slug        string
	Members     []string // user login, aka githubid
	Maintainers []string // user login (that are not in the Members array)
	ParentTeam  *int
}

type GithubApp struct {
	Id        int
	GraphqlId string
	Slug      string
}

type GithubTeamRepo struct {
	Name       string // repository name
	Permission string // possible values: ADMIN, MAINTAIN, WRITE, TRIAGE, READ
}

// GithubRemoteEnvironment represents a GitHub environment
type GithubRemoteEnvironment struct {
	Id              int    `json:"id"`
	Name            string `json:"name"`
	NodeId          string `json:"node_id"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	ProtectionRules []struct {
		Id                int      `json:"id"`
		NodeId            string   `json:"node_id"`
		Type              string   `json:"type"`
		ReviewerTeams     []string `json:"reviewer_teams,omitempty"`
		ReviewerUsers     []string `json:"reviewer_users,omitempty"`
		WaitTimer         int      `json:"wait_timer,omitempty"`
		PreventSelfReview bool     `json:"prevent_self_review,omitempty"`
	} `json:"protection_rules"`
	DeploymentBranchPolicy struct {
		ProtectedBranches    bool     `json:"protected_branches"`
		CustomBranchPolicies bool     `json:"custom_branch_policies"`
		AllowedBranches      []string `json:"allowed_branches,omitempty"`
	} `json:"deployment_branch_policy"`
}

type GithubVariable struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type GoliacRemoteImpl struct {
	client                    github.GitHubClient
	users                     map[string]*GithubUser
	repositories              map[string]*GithubRepository
	repositoriesByRefId       map[string]*GithubRepository
	teams                     map[string]*GithubTeam
	teamRepos                 map[string]map[string]*GithubTeamRepo
	teamSlugByName            map[string]string
	rulesets                  map[string]*GithubRuleSet
	appIds                    map[string]*GithubApp
	orgCustomProperties       map[string]*config.GithubCustomProperty
	ttlExpireUsers            time.Time
	ttlExpireRepositories     time.Time
	ttlExpireTeams            time.Time
	ttlExpireTeamsRepos       time.Time
	ttlExpireRulesets         time.Time
	ttlExpireAppIds           time.Time
	ttlExpireCustomProperties time.Time
	isEnterprise              bool
	feedback                  observability.RemoteObservability
	loadTeamsMutex            sync.Mutex
	actionMutex               sync.Mutex // used when an action (like a REST CALL to create a repository) is launched while a load is in progress
	configGithubOrg           string
	manageGithubVariables     bool
	manageGithubAutolinks     bool
	manageOrgCustomProperties bool
}

type GHESInfo struct {
	InstalledVersion string `json:"installed_version"`
}

func getGHESVersion(ctx context.Context, client github.GitHubClient) (*GHESInfo, error) {
	body, err := client.CallRestAPI(ctx, "/api/v3", "", "GET", nil, nil)
	if err != nil {
		return nil, err
	}

	var info GHESInfo
	err = json.Unmarshal(body, &info)
	if err != nil {
		return nil, fmt.Errorf("not able to get github org information: %v", err)
	}

	return &info, nil
}

const getAssets = `
query getAssets($orgLogin: String!) {
	organization(login: $orgLogin) {
		repositories{
			totalCount
		}
		teams {
			totalCount
		}
	    membersWithRole {
    		totalCount
    	}
		samlIdentityProvider {
			externalIdentities {
				totalCount
			}
		}
	}
}
`

type GraplQLAssets struct {
	Data struct {
		Organization struct {
			Repositories struct {
				TotalCount int `json:"totalCount"`
			} `json:"repositories"`
			Teams struct {
				TotalCount int `json:"totalCount"`
			} `json:"teams"`
			MembersWithRole struct {
				TotalCount int `json:"totalCount"`
			} `json:"membersWithRole"`
			SamlIdentityProvider struct {
				ExternalIdentities struct {
					TotalCount int `json:"totalCount"`
				} `json:"externalIdentities"`
			} `json:"samlIdentityProvider"`
		} `json:"organization"`
	}
	Errors []struct {
		Path       []interface{} `json:"path"`
		Extensions struct {
			Code         string
			ErrorMessage string
		} `json:"extensions"`
		Message string
	} `json:"errors"`
}

func (g *GoliacRemoteImpl) CountAssets(ctx context.Context, warmup bool) (int, error) {
	variables := make(map[string]interface{})
	variables["orgLogin"] = g.configGithubOrg

	data, err := g.client.QueryGraphQLAPI(ctx, getAssets, variables, nil)
	if err != nil {
		return 0, err
	}
	var gResult GraplQLAssets

	// parse first page
	err = json.Unmarshal(data, &gResult)
	if err != nil {
		return 0, err
	}
	if len(gResult.Errors) > 0 {
		return 0, fmt.Errorf("graphql error on CountAssets: %v (%v)", gResult.Errors[0].Message, gResult.Errors[0].Path)
	}
	totalCount := 2*gResult.Data.Organization.Repositories.TotalCount + // we multiply by 2 because we have the repositories, the teams per repostiory to fetch
		2*gResult.Data.Organization.Teams.TotalCount + // we multiply by 2 because we have the teams and the members per team to fetch
		gResult.Data.Organization.MembersWithRole.TotalCount +
		gResult.Data.Organization.SamlIdentityProvider.ExternalIdentities.TotalCount

	if warmup {
		totalCount += 2 * gResult.Data.Organization.Repositories.TotalCount // we add 2 times because we have the environments per repository, and the variables per repository to fetch
	}

	return totalCount + 1, nil
}

func (g *GoliacRemoteImpl) SetRemoteObservability(feedback observability.RemoteObservability) {
	g.feedback = feedback
}

type OrgInfo struct {
	TwoFactorRequirementEnabled bool `json:"two_factor_requirement_enabled"`
	Plan                        struct {
		Name string `json:"name"` // enterprise
	} `json:"plan"`
}

func getOrgInfo(ctx context.Context, orgname string, client github.GitHubClient) (*OrgInfo, error) {
	body, err := client.CallRestAPI(ctx, "/orgs/"+orgname, "", "GET", nil, nil)
	if err != nil {
		return nil, err
	}

	var info OrgInfo
	err = json.Unmarshal(body, &info)
	if err != nil {
		return nil, fmt.Errorf("not able to get github org information: %v", err)
	}

	return &info, nil
}

func isEnterprise(ctx context.Context, orgname string, client github.GitHubClient) bool {
	// are we on Github Enteprise Server
	if ghesInfo, err := getGHESVersion(ctx, client); err == nil {
		logrus.Debugf("GHES versiob: %s", ghesInfo.InstalledVersion)
		version3_11, err := version.NewVersion("3.11")
		if err != nil {
			return false
		}
		ghesVersion, err := version.NewVersion(ghesInfo.InstalledVersion)
		if err != nil {
			return false
		}
		if ghesVersion.GreaterThanOrEqual(version3_11) {
			return true
		}
	} else if info, err := getOrgInfo(ctx, orgname, client); err == nil {
		logrus.Debugf("Organization plan: %s", info.Plan.Name)
		if info.Plan.Name == "enterprise" {
			return true
		}
	}
	return false
}

func NewGoliacRemoteImpl(client github.GitHubClient,
	configGithubOrg string,
	manageGithubVariables bool,
	manageGithubAutolinks bool,
	manageOrgCustomProperties bool,
) *GoliacRemoteImpl {
	ctx := context.Background()
	return &GoliacRemoteImpl{
		client:                    client,
		users:                     make(map[string]*GithubUser),
		repositories:              make(map[string]*GithubRepository),
		repositoriesByRefId:       make(map[string]*GithubRepository),
		teams:                     make(map[string]*GithubTeam),
		teamRepos:                 make(map[string]map[string]*GithubTeamRepo),
		teamSlugByName:            make(map[string]string),
		rulesets:                  make(map[string]*GithubRuleSet),
		appIds:                    make(map[string]*GithubApp),
		orgCustomProperties:       make(map[string]*config.GithubCustomProperty),
		ttlExpireUsers:            time.Now(),
		ttlExpireRepositories:     time.Now(),
		ttlExpireTeams:            time.Now(),
		ttlExpireTeamsRepos:       time.Now(),
		ttlExpireRulesets:         time.Now(),
		ttlExpireAppIds:           time.Now(),
		ttlExpireCustomProperties: time.Now(),
		isEnterprise:              isEnterprise(ctx, configGithubOrg, client),
		feedback:                  nil,
		configGithubOrg:           configGithubOrg,
		manageGithubVariables:     manageGithubVariables,
		manageGithubAutolinks:     manageGithubAutolinks,
		manageOrgCustomProperties: manageOrgCustomProperties,
	}
}

func (g *GoliacRemoteImpl) SetFeatureFlags(manageGithubVariables bool, manageGithubAutolinks bool, manageOrgCustomProperties bool) {
	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()
	g.manageGithubVariables = manageGithubVariables
	g.manageGithubAutolinks = manageGithubAutolinks
	g.manageOrgCustomProperties = manageOrgCustomProperties
}

func (g *GoliacRemoteImpl) IsEnterprise() bool {
	return g.isEnterprise
}

func (g *GoliacRemoteImpl) FlushCacheUsersTeamsOnly() {
	g.ttlExpireUsers = time.Now()
	g.ttlExpireTeams = time.Now()
}

func (g *GoliacRemoteImpl) FlushCache() {
	g.ttlExpireUsers = time.Now()
	g.ttlExpireRepositories = time.Now()
	g.ttlExpireTeams = time.Now()
	g.ttlExpireTeamsRepos = time.Now()
	g.ttlExpireRulesets = time.Now()
	g.ttlExpireAppIds = time.Now()
	g.ttlExpireCustomProperties = time.Now()
}

func (g *GoliacRemoteImpl) RuleSets(ctx context.Context) map[string]*GithubRuleSet {
	if time.Now().After(g.ttlExpireRulesets) {
		var githubToken *string
		if config.Config.GithubPersonalAccessToken != "" {
			githubToken = &config.Config.GithubPersonalAccessToken
		}
		rulesets, err := g.loadRulesets(ctx, githubToken)
		if err == nil {
			g.rulesets = rulesets
			g.ttlExpireRulesets = time.Now().Add(time.Duration(config.Config.GithubCacheTTL) * time.Second)
		}
	}
	return g.rulesets
}

func (g *GoliacRemoteImpl) AppIds(ctx context.Context) map[string]*GithubApp {
	if time.Now().After(g.ttlExpireAppIds) {
		appIds, err := g.loadAppIds(ctx)
		if err == nil {
			g.appIds = appIds
			g.ttlExpireAppIds = time.Now().Add(time.Duration(config.Config.GithubCacheTTL) * time.Second)
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// copy the map to a safe one
	var rAppIds = make(map[string]*GithubApp)
	for k, v := range g.appIds {
		rAppIds[k] = &GithubApp{
			Id: v.Id,
			// we can infer the node_id, or we can get it from https://api.github.com/apps/<app slug>
			GraphqlId: v.GraphqlId,
			Slug:      v.Slug,
		}
	}
	return rAppIds
}

func (g *GoliacRemoteImpl) Users(ctx context.Context) map[string]*GithubUser {
	if time.Now().After(g.ttlExpireUsers) {
		users, err := g.loadOrgUsers(ctx)
		if err == nil {
			g.users = users
			g.ttlExpireUsers = time.Now().Add(time.Duration(config.Config.GithubCacheTTL) * time.Second)
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// copy the map to a safe one
	var rUsers = make(map[string]*GithubUser)
	for k, v := range g.users {
		rUsers[k] = &GithubUser{
			Login:     v.Login,
			GraphqlId: v.GraphqlId,
			Role:      v.Role,
		}
	}
	return rUsers
}

func (g *GoliacRemoteImpl) TeamSlugByName(ctx context.Context) map[string]string {
	if time.Now().After(g.ttlExpireTeams) {
		teams, teamSlugByName, err := g.loadTeams(ctx)
		if err == nil {
			g.teams = teams
			g.teamSlugByName = teamSlugByName
			g.ttlExpireTeams = time.Now().Add(time.Duration(config.Config.GithubCacheTTL) * time.Second)
		}
	}
	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// copy the map to a safe one
	var rTeamSlugByName = make(map[string]string)
	for k, v := range g.teamSlugByName {
		rTeamSlugByName[k] = v
	}
	return rTeamSlugByName
}

/*
Used to get the teams (and load it if needed)
if current is true, it will return the current in memory teams without checking the TTL (useful for the UI)
*/
func (g *GoliacRemoteImpl) Teams(ctx context.Context, current bool) map[string]*GithubTeam {
	if current && len(g.teams) > 0 {
		return g.teams
	}
	g.loadTeamsMutex.Lock()
	defer g.loadTeamsMutex.Unlock()
	if time.Now().After(g.ttlExpireTeams) {
		teams, teamSlugByName, err := g.loadTeams(ctx)
		if err == nil {
			g.teams = teams
			g.teamSlugByName = teamSlugByName
			g.ttlExpireTeams = time.Now().Add(time.Duration(config.Config.GithubCacheTTL) * time.Second)
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// copy the map to a safe one
	var rTeams = make(map[string]*GithubTeam)
	for k, v := range g.teams {
		rTeams[k] = v
	}
	return rTeams
}

func (g *GoliacRemoteImpl) Repositories(ctx context.Context) map[string]*GithubRepository {
	if time.Now().After(g.ttlExpireRepositories) {
		var githubToken *string
		if config.Config.GithubPersonalAccessToken != "" {
			githubToken = &config.Config.GithubPersonalAccessToken
		}
		repositories, repositoriesByRefIds, err := g.loadRepositories(ctx, githubToken)
		if err == nil {
			g.repositories = repositories
			g.repositoriesByRefId = repositoriesByRefIds
			g.ttlExpireRepositories = time.Now().Add(time.Duration(config.Config.GithubCacheTTL) * time.Second)
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// copy the map to a safe one
	var rRepositories = make(map[string]*GithubRepository)
	for k, v := range g.repositories {
		rRepositories[k] = v
	}
	return rRepositories
}

func (g *GoliacRemoteImpl) TeamRepositories(ctx context.Context) map[string]map[string]*GithubTeamRepo {
	if time.Now().After(g.ttlExpireTeamsRepos) {
		repositories := g.Repositories(ctx)
		teamsrepos, err := g.loadTeamReposConcurrently(ctx, config.Config.GithubConcurrentThreads, repositories)
		if err == nil {
			g.teamRepos = teamsrepos
			g.ttlExpireTeamsRepos = time.Now().Add(time.Duration(config.Config.GithubCacheTTL) * time.Second)
		}
	}
	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// copy the map to a safe one
	var rTeamRepos = make(map[string]map[string]*GithubTeamRepo)
	for k, v := range g.teamRepos {
		rTeamRepos[k] = make(map[string]*GithubTeamRepo)
		for k2, v2 := range v {
			rTeamRepos[k][k2] = v2
		}
	}
	return rTeamRepos
}

const listAllOrgMembers = `
query listAllOrgMembers($orgLogin: String!, $endCursor: String) {
    organization(login: $orgLogin) {
		membersWithRole(first: 100, after: $endCursor) {
		  edges {
            node {
              login
			  id
            }
            role
          }
          pageInfo {
            hasNextPage
            endCursor
          }
          totalCount
        }
    }
}
`

type GraplQLUsers struct {
	Data struct {
		Organization struct {
			MembersWithRole struct {
				Edges []struct {
					Node struct {
						Login string
						Id    string
					} `json:"node"`
					Role string `json:"role"`
				} `json:"edges"`
				PageInfo struct {
					HasNextPage bool
					EndCursor   string
				} `json:"pageInfo"`
				TotalCount int `json:"totalCount"`
			} `json:"membersWithRole"`
		}
	}
	Errors []struct {
		Path       []interface{} `json:"path"`
		Extensions struct {
			Code         string
			ErrorMessage string
		} `json:"extensions"`
		Message string
	} `json:"errors"`
}

/*
loadOrgUsers returns
map[githubid]permission (role)
role can be 'ADMIN', 'MEMBER'
*/
func (g *GoliacRemoteImpl) loadOrgUsers(ctx context.Context) (map[string]*GithubUser, error) {
	logrus.Debug("loading orgUsers")
	users := make(map[string]*GithubUser)

	variables := make(map[string]interface{})
	variables["orgLogin"] = g.configGithubOrg
	variables["endCursor"] = nil

	hasNextPage := true
	count := 0
	for hasNextPage {
		data, err := g.client.QueryGraphQLAPI(ctx, listAllOrgMembers, variables, nil)
		if err != nil {
			return users, err
		}
		var gResult GraplQLUsers

		// parse first page
		err = json.Unmarshal(data, &gResult)
		if err != nil {
			return users, err
		}
		if len(gResult.Errors) > 0 {
			return users, fmt.Errorf("graphql error on loadOrgUsers: %v (%v)", gResult.Errors[0].Message, gResult.Errors[0].Path)
		}

		for _, c := range gResult.Data.Organization.MembersWithRole.Edges {
			users[c.Node.Login] = &GithubUser{
				Login:     c.Node.Login,
				GraphqlId: c.Node.Id,
				Role:      c.Role,
			}
		}

		if g.feedback != nil {
			g.feedback.LoadingAsset("users", len(gResult.Data.Organization.MembersWithRole.Edges))
		}

		hasNextPage = gResult.Data.Organization.MembersWithRole.PageInfo.HasNextPage
		variables["endCursor"] = gResult.Data.Organization.MembersWithRole.PageInfo.EndCursor

		count++
		// sanity check to avoid loops
		if count > FORLOOP_STOP {
			break
		}
	}

	return users, nil
}

const listAllReposInOrg = `
query listAllReposInOrg($orgLogin: String!, $endCursor: String) {
    organization(login: $orgLogin) {
      repositories(first: 10, after: $endCursor) {
        nodes {
          name
		  id
		  databaseId
          isArchived
		  isFork
          visibility
		  autoMergeAllowed
		  mergeCommitAllowed
		  mergeCommitTitle # MERGE_MESSAGE, PR_TITLE
		  mergeCommitMessage # PR_TITLE, PR_BODY, BLANK
		  rebaseMergeAllowed
		  squashMergeAllowed
		  squashMergeCommitTitle # COMMIT_OR_PR_TITLE, PR_TITLE
		  squashMergeCommitMessage # COMMIT_MESSAGES, PR_BODY, BLANK
          deleteBranchOnMerge
          allowUpdateBranch
		  defaultBranchRef {
		    name
		  }
		  repositoryTopics(first: 50) {
		    nodes {
		      topic {
		        name
		      }
		    }
		  }
          directCollaborators: collaborators(affiliation: DIRECT, first: 100) {
            edges {
              node {
                login
              }
              permission
            }
          }
          outsideCollaborators: collaborators(affiliation: OUTSIDE, first: 100) {
            edges {
              node {
                login
              }
              permission
            }
          }
          rulesets(first: 20) {
            nodes {
              databaseId
              source {
                ... on Repository {
				  name
                }
              }
              name
              target
              enforcement
              conditions {
                refName {
                  include
                  exclude
                }
              }
              rules(first:20) {
                nodes {
                  parameters {
                    ... on PullRequestParameters {
                      dismissStaleReviewsOnPush
                      requireCodeOwnerReview
                      requiredApprovingReviewCount
                      requiredReviewThreadResolution
                      requireLastPushApproval
                      allowedMergeMethods
                    }
                    ... on RequiredStatusChecksParameters {
                      requiredStatusChecks {
                        context
                      }
                    }
                    ... on BranchNamePatternParameters {
                      name
                      negate
                      operator
                      pattern
                    }
                    ... on TagNamePatternParameters {
                      name
                      negate
                      operator
                      pattern
                    }
                    ... on MergeQueueParameters {
                      checkResponseTimeoutMinutes
                      groupingStrategy
                      maxEntriesToBuild
                      maxEntriesToMerge
                      mergeMethod
                      minEntriesToMerge
                      minEntriesToMergeWaitMinutes
                    }
                  }
                  type
                }
              }
            }
          }
          branchProtectionRules(first:50) {
            nodes{
			  id
              pattern
              requiresApprovingReviews
              requiredApprovingReviewCount
              dismissesStaleReviews
              requiresCodeOwnerReviews
              requireLastPushApproval
              requiresStatusChecks
              requiresStrictStatusChecks
              requiredStatusCheckContexts
              requiresConversationResolution
              requiresCommitSignatures
              requiresLinearHistory
              allowsForcePushes
              allowsDeletions
              bypassPullRequestAllowances(first:100) {
                nodes {
                  actor {
                    ... on Team {
                      teamSlug:slug
                    }
                    ... on User {
                      userLogin:login
                    }
					... on App {
						appSlug:slug
					}
                  }
                }
              }
            }
          }
        }
        pageInfo {
          hasNextPage
          endCursor
        }
        totalCount
      }
    }
  }
`

type BypassPullRequestAllowanceNode struct {
	Actor struct {
		TeamSlug  string
		UserLogin string
		AppSlug   string
	}
}
type GithubBranchProtection struct {
	Id                             string
	Pattern                        string
	RequiresApprovingReviews       bool
	RequiredApprovingReviewCount   int
	DismissesStaleReviews          bool
	RequiresCodeOwnerReviews       bool
	RequireLastPushApproval        bool
	RequiresStatusChecks           bool
	RequiresStrictStatusChecks     bool
	RequiredStatusCheckContexts    []string
	RequiresConversationResolution bool
	RequiresCommitSignatures       bool
	RequiresLinearHistory          bool
	AllowsForcePushes              bool
	AllowsDeletions                bool
	BypassPullRequestAllowances    struct {
		Nodes []BypassPullRequestAllowanceNode
	}
}

type GraplQLRepositories struct {
	Data struct {
		Organization struct {
			Repositories struct {
				Nodes []struct {
					Name                     string
					Id                       string
					DatabaseId               int
					IsArchived               bool
					IsFork                   bool
					Visibility               string
					AutoMergeAllowed         bool
					MergeCommitAllowed       bool
					MergeCommitTitle         string //MERGE_MESSAGE, PR_TITLE
					MergeCommitMessage       string //PR_TITLE, PR_BODY, BLANK
					RebaseMergeAllowed       bool
					SquashMergeAllowed       bool
					SquashMergeCommitTitle   string // COMMIT_OR_PR_TITLE, PR_TITLE
					SquashMergeCommitMessage string // COMMIT_MESSAGES, PR_BODY, BLANK
					DeleteBranchOnMerge      bool
					AllowUpdateBranch        bool
					DefaultBranchRef         struct {
						Name string
					}
					RepositoryTopics struct {
						Nodes []struct {
							Topic struct {
								Name string
							}
						}
					} `json:"repositoryTopics"`
					DirectCollaborators struct {
						Edges []struct {
							Node struct {
								Login string
							}
							Permission string
						}
					}
					OutsideCollaborators struct {
						Edges []struct {
							Node struct {
								Login string
							}
							Permission string
						}
					}
					Rulesets struct {
						Nodes []GraphQLGithubRuleSet
					}
					BranchProtectionRules struct {
						Nodes []GithubBranchProtection
					}
				} `json:"nodes"`
				PageInfo struct {
					HasNextPage bool
					EndCursor   string
				} `json:"pageInfo"`
				TotalCount int `json:"totalCount"`
			} `json:"repositories"`
		}
	}
	Errors []struct {
		Path       []interface{} `json:"path"`
		Extensions struct {
			Code         string
			ErrorMessage string
		} `json:"extensions"`
		Message string
	} `json:"errors"`
}

/*
 * Loads all repositories from the organization
 * Returns a map of repository name to repository object
 * Returns a map of repository id to repository object
 * Returns an error if the repositories cannot be loaded
 */
func (g *GoliacRemoteImpl) loadRepositories(ctx context.Context, githubToken *string) (map[string]*GithubRepository, map[string]*GithubRepository, error) {
	logrus.Debug("loading repositories")
	repositories := make(map[string]*GithubRepository)
	repositoriesByRefId := make(map[string]*GithubRepository)

	variables := make(map[string]interface{})
	variables["orgLogin"] = g.configGithubOrg
	variables["endCursor"] = nil

	var retErr error
	hasNextPage := true
	count := 0
	for hasNextPage {
		data, err := g.client.QueryGraphQLAPI(ctx, listAllReposInOrg, variables, githubToken)
		if err != nil {
			return repositories, repositoriesByRefId, err
		}
		var gResult GraplQLRepositories

		// parse first page
		err = json.Unmarshal(data, &gResult)
		if err != nil {
			return repositories, repositoriesByRefId, err
		}
		if len(gResult.Errors) > 0 {
			retErr = fmt.Errorf("graphql error on loadRepositories: %v (%v)", gResult.Errors[0].Message, gResult.Errors[0].Path)
		}

		for _, c := range gResult.Data.Organization.Repositories.Nodes {
			repo := &GithubRepository{
				Name:       c.Name,
				Id:         c.DatabaseId,
				RefId:      c.Id,
				Visibility: strings.ToLower(c.Visibility),
				BoolProperties: map[string]bool{
					"archived":               c.IsArchived,
					"allow_auto_merge":       c.AutoMergeAllowed,
					"delete_branch_on_merge": c.DeleteBranchOnMerge,
					"allow_update_branch":    c.AllowUpdateBranch,
					"allow_merge_commit":     c.MergeCommitAllowed,
					"allow_squash_merge":     c.SquashMergeAllowed,
					"allow_rebase_merge":     c.RebaseMergeAllowed,
				},
				ExternalUsers:              make(map[string]string),
				InternalUsers:              make(map[string]string),
				RuleSets:                   make(map[string]*GithubRuleSet),
				BranchProtections:          make(map[string]*GithubBranchProtection),
				DefaultBranchName:          c.DefaultBranchRef.Name,
				IsFork:                     c.IsFork,
				DefaultMergeCommitMessage:  "Default message",
				DefaultSquashCommitMessage: "Default message",
				CustomProperties:           make(map[string]interface{}),
				Topics:                     []string{},
			}
			if c.MergeCommitTitle == "PR_TITLE" && c.MergeCommitMessage == "PR_BODY" {
				repo.DefaultMergeCommitMessage = "Pull request title and description"
			} else if c.MergeCommitTitle == "PR_TITLE" && c.MergeCommitMessage == "BLANK" {
				repo.DefaultMergeCommitMessage = "Pull request title"
			} else {
				repo.DefaultMergeCommitMessage = "Default message"
			}

			if c.SquashMergeCommitTitle == "PR_TITLE" && c.SquashMergeCommitMessage == "PR_BODY" {
				repo.DefaultSquashCommitMessage = "Pull request title and description"
			} else if c.SquashMergeCommitTitle == "PR_TITLE" && c.SquashMergeCommitMessage == "BLANK" {
				repo.DefaultSquashCommitMessage = "Pull request title"
			} else if c.SquashMergeCommitTitle == "PR_TITLE" && c.SquashMergeCommitMessage == "COMMIT_MESSAGES" {
				repo.DefaultSquashCommitMessage = "Pull request title and commit details"
			} else {
				repo.DefaultSquashCommitMessage = "Default message"
			}

			// if the repository has not been populated yet
			if repo.DefaultBranchName == "" {
				repo.DefaultBranchName = "main"
			}
			for _, outsideCollaborator := range c.OutsideCollaborators.Edges {
				repo.ExternalUsers[outsideCollaborator.Node.Login] = outsideCollaborator.Permission
			}
			for _, internalCollaborator := range c.DirectCollaborators.Edges {
				repo.InternalUsers[internalCollaborator.Node.Login] = internalCollaborator.Permission
			}
			for _, ruleset := range c.Rulesets.Nodes {
				// if the source is the repository itself, it is not a organization ruleset
				// we add the ruleset
				if ruleset.Source.Name == c.Name {
					repo.RuleSets[ruleset.Name] = g.fromGraphQLToGithubRuleset(&ruleset)
				}
			}
			for _, branchProtection := range c.BranchProtectionRules.Nodes {
				repo.BranchProtections[branchProtection.Pattern] = &branchProtection
			}
			topics := []string{}
			for _, rt := range c.RepositoryTopics.Nodes {
				topics = append(topics, rt.Topic.Name)
			}
			repo.Topics = topics
			repositories[c.Name] = repo
			repositoriesByRefId[c.Id] = repo
		}

		if g.feedback != nil {
			g.feedback.LoadingAsset("repositories", len(gResult.Data.Organization.Repositories.Nodes))
		}

		hasNextPage = gResult.Data.Organization.Repositories.PageInfo.HasNextPage
		variables["endCursor"] = gResult.Data.Organization.Repositories.PageInfo.EndCursor

		count++
		// sanity check to avoid loops
		if count > FORLOOP_STOP {
			break
		}
	}

	if g.manageGithubVariables {
		for reponame, repo := range repositories {
			repo.RepositoryVariables = NewRemoteLazyLoader[string](func() map[string]string {
				ctx := context.Background()
				if g.feedback != nil {
					g.feedback.Extend(1)
					g.feedback.LoadingAsset("repo_variable", 1)
				}
				variables, err := g.loadVariablesPerRepository(ctx, repo)
				if err != nil {
					logrus.Errorf("error loading variables for repository %s: %v", reponame, err)
					return map[string]string{}
				}
				variablesMap := make(map[string]string)
				for name, variable := range variables {
					variablesMap[name] = variable.Value
				}
				return variablesMap
			})
		}

		// repoSecretsPerRepo, err := g.loadRepositoriesSecrets(ctx, config.Config.GithubConcurrentThreads, repositories)
		// if err != nil {
		// 	return repositories, repositoriesByRefId, err
		// }
		// for repo, envSecrets := range repoSecretsPerRepo {
		// 	repositories[repo].RepositorySecrets = envSecrets
		// }

		for reponame, repo := range repositories {
			repo.Environments = NewRemoteLazyLoader[*GithubEnvironment](func() map[string]*GithubEnvironment {
				ctx := context.Background()
				if g.feedback != nil {
					g.feedback.Extend(1)
					g.feedback.LoadingAsset("repo_environment", 1)
				}

				envs, err := g.loadEnvironmentsPerRepository(ctx, repo)
				if err != nil {
					logrus.Errorf("error loading environments for repository %s: %v", reponame, err)
					return map[string]*GithubEnvironment{}
				}
				envsMap := make(map[string]*GithubEnvironment)
				for name, env := range envs {
					envsMap[name] = env
					envvars, err := g.loadEnvironmentVariablesForEnvironmentRepository(ctx, repo.Name, name)
					if err != nil {
						logrus.Errorf("error loading variables for environment %s: %v", name, err)
						return map[string]*GithubEnvironment{}
					}
					env.Variables = envvars
				}

				return envsMap
			})
		}

		// envSecretsPerRepo, err := g.loadEnvironmentSecrets(ctx, config.Config.GithubConcurrentThreads, repositories)
		// if err == nil {
		// 	for repo, envSecrets := range envSecretsPerRepo {
		// 		repositories[repo].EnvironmentSecrets = envSecrets
		// 	}
		// }
	} else {
		// MappedEntityLazyLoader is an interface; zero value is nil and GetEntity() would panic.
		for _, repo := range repositories {
			repo.RepositoryVariables = NewLocalLazyLoader[string](map[string]string{})
			repo.Environments = NewLocalLazyLoader[*GithubEnvironment](map[string]*GithubEnvironment{})
		}
	}

	if g.manageGithubAutolinks {
		for reponame, repo := range repositories {
			repo.Autolinks = NewRemoteLazyLoader[*GithubAutolink](func() map[string]*GithubAutolink {
				ctx := context.Background()
				if g.feedback != nil {
					g.feedback.Extend(1)
					g.feedback.LoadingAsset("repo_autolink", 1)
				}
				autolinks, err := g.loadAutolinksPerRepository(ctx, repo)
				if err != nil {
					logrus.Errorf("error loading autolinks for repository %s: %v", reponame, err)
					return map[string]*GithubAutolink{}
				}
				return autolinks
			})
		}
	}

	if g.manageOrgCustomProperties {
		// Load custom properties for all repositories
		customPropsPerRepo, err := g.loadCustomPropertiesConcurrently(ctx, config.Config.GithubConcurrentThreads, repositories)
		if err != nil {
			logrus.Warnf("error loading custom properties: %v", err)
			if retErr == nil {
				retErr = fmt.Errorf("error loading repository custom properties: %w", err)
			}
		} else {
			for reponame, customProps := range customPropsPerRepo {
				if repo, ok := repositories[reponame]; ok {
					repo.CustomProperties = customProps
				}
			}
		}
	}

	if err := g.loadRepositoryCodeownersConcurrently(ctx, config.Config.GithubConcurrentThreads, repositories); err != nil {
		logrus.Warnf("error loading CODEOWNERS: %v", err)
		if retErr == nil {
			retErr = fmt.Errorf("error loading repository CODEOWNERS: %w", err)
		}
	}

	if err := g.loadRepositoryPagesConcurrently(ctx, config.Config.GithubConcurrentThreads, repositories); err != nil {
		logrus.Warnf("error loading GitHub Pages: %v", err)
		if retErr == nil {
			retErr = fmt.Errorf("error loading repository GitHub Pages: %w", err)
		}
	}

	return repositories, repositoriesByRefId, retErr
}

const listAllTeamsInOrg = `
query listAllTeamsInOrg($orgLogin: String!, $endCursor: String) {
    organization(login: $orgLogin) {
      teams(first: 100, after: $endCursor) {
        nodes {
          name
		  id
		  databaseId
          slug
		  parentTeam {
		    databaseId
		  }
        }
        pageInfo {
          hasNextPage
          endCursor
        }
        totalCount
      }
    }
  }
`

type GraplQLTeams struct {
	Data struct {
		Organization struct {
			Teams struct {
				Nodes []struct {
					Name       string
					Id         string
					DatabaseId int `json:"databaseId"`
					Slug       string
					ParentTeam struct {
						DatabaseId int `json:"databaseId"`
					} `json:"parentTeam"`
				} `json:"nodes"`
				PageInfo struct {
					HasNextPage bool
					EndCursor   string
				} `json:"pageInfo"`
				TotalCount int `json:"totalCount"`
			} `json:"teams"`
		}
	}
	Errors []struct {
		Path       []interface{} `json:"path"`
		Extensions struct {
			Code         string
			ErrorMessage string
		} `json:"extensions"`
		Message string
	} `json:"errors"`
}

func (g *GoliacRemoteImpl) loadAppIds(ctx context.Context) (map[string]*GithubApp, error) {
	logrus.Debug("loading appIds")
	appIds := map[string]*GithubApp{}
	type Installation struct {
		TotalCount    int `json:"total_count"`
		Installations []struct {
			Id      int    `json:"id"`
			AppId   int    `json:"app_id"`
			Name    string `json:"name"`
			AppSlug string `json:"app_slug"`
		} `json:"installations"`
	}

	// https://docs.github.com/en/enterprise-cloud@latest/rest/orgs/orgs?apiVersion=2022-11-28#list-app-installations-for-an-organization
	body, err := g.client.CallRestAPI(ctx,
		fmt.Sprintf("/orgs/%s/installations", g.configGithubOrg),
		"page=1&per_page=30",
		"GET",
		nil,
		nil)

	if err != nil {
		return nil, fmt.Errorf("not able to list github apps: %v. %s", err, string(body))
	}

	var installations Installation
	err = json.Unmarshal(body, &installations)
	if err != nil {
		return nil, fmt.Errorf("not able to list github apps: %v", err)
	}

	for _, i := range installations.Installations {
		appIds[i.AppSlug] = &GithubApp{
			Id: i.AppId,
			// we can infer the node_id, or we can get it from https://api.github.com/apps/<app slug>
			GraphqlId: base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("03:App%d", i.AppId))),
			Slug:      i.AppSlug,
		}
	}

	if installations.TotalCount > 30 {
		// we need to paginate
		for i := 2; i <= (installations.TotalCount/30)+1; i++ {
			body, err := g.client.CallRestAPI(ctx,
				fmt.Sprintf("/orgs/%s/installations", g.configGithubOrg),
				fmt.Sprintf("page=%d&per_page=30", i),
				"GET",
				nil,
				nil)

			if err != nil {
				return nil, fmt.Errorf("not able to list next page github apps: %v. %s", err, string(body))
			}

			var installations Installation
			err = json.Unmarshal(body, &installations)
			if err != nil {
				return nil, fmt.Errorf("not able to list github apps: %v", err)
			}

			for _, i := range installations.Installations {
				appIds[i.AppSlug] = &GithubApp{
					Id: i.AppId,
					// we can infer the node_id, or we can get it from https://api.github.com/apps/<app slug>
					GraphqlId: base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("03:App%d", i.AppId))),
					Slug:      i.AppSlug,
				}
			}
		}
	}

	return appIds, nil
}

// Load from a github repository. continueOnError is used for scaffolding
func (g *GoliacRemoteImpl) Load(ctx context.Context, continueOnError bool) error {
	var childSpan trace.Span
	if config.Config.OpenTelemetryEnabled {
		ctx, childSpan = otel.Tracer("goliac").Start(ctx, "Load")
		defer childSpan.End()
	}
	var retErr error

	if time.Now().After(g.ttlExpireAppIds) {
		appIds, err := g.loadAppIds(ctx)
		if err != nil {
			if !continueOnError {
				return err
			}
			logrus.Debugf("Error loading app ids: %v", err)
			retErr = fmt.Errorf("error loading app ids: %v", err)
		}
		g.appIds = appIds
		g.ttlExpireAppIds = time.Now().Add(time.Duration(config.Config.GithubCacheTTL) * time.Second)
	}

	// the lock is here to avoid multiple calls to loadTeams (and it can be long)
	// especially coming from the UI
	g.loadTeamsMutex.Lock()
	if time.Now().After(g.ttlExpireTeams) {
		teams, teamSlugByName, err := g.loadTeams(ctx)
		if err != nil {
			if !continueOnError {
				g.loadTeamsMutex.Unlock()
				return err
			}
			logrus.Debugf("Error loading teams: %v", err)
			retErr = fmt.Errorf("error loading teams: %v", err)
		}
		g.teams = teams
		g.teamSlugByName = teamSlugByName
		g.ttlExpireTeams = time.Now().Add(time.Duration(config.Config.GithubCacheTTL) * time.Second)
	}
	g.loadTeamsMutex.Unlock()

	if time.Now().After(g.ttlExpireUsers) {
		users, err := g.loadOrgUsers(ctx)
		if err != nil {
			if !continueOnError {
				return err
			}
			logrus.Debugf("Error loading users: %v", err)
			retErr = fmt.Errorf("error loading users: %v", err)
		}
		g.users = users
		g.ttlExpireUsers = time.Now().Add(time.Duration(config.Config.GithubCacheTTL) * time.Second)
	}

	if time.Now().After(g.ttlExpireRepositories) {
		var githubToken *string
		if config.Config.GithubPersonalAccessToken != "" {
			githubToken = &config.Config.GithubPersonalAccessToken
		}
		repositories, repositoriesByRefId, err := g.loadRepositories(ctx, githubToken)
		if err != nil {
			if !continueOnError {
				return err
			}
			logrus.Debugf("Error loading repositories: %v", err)
			retErr = fmt.Errorf("error loading repositories: %v", err)
		}
		g.repositories = repositories
		g.repositoriesByRefId = repositoriesByRefId
		g.ttlExpireRepositories = time.Now().Add(time.Duration(config.Config.GithubCacheTTL) * time.Second)
	}

	// let's load the rulesets after the repositories because I need the repository refs
	if time.Now().After(g.ttlExpireRulesets) {
		var githubToken *string
		if config.Config.GithubPersonalAccessToken != "" {
			githubToken = &config.Config.GithubPersonalAccessToken
		}
		rulesets, err := g.loadRulesets(ctx, githubToken)
		if err != nil {
			if !continueOnError {
				return err
			}
			logrus.Debugf("Error loading rulesets: %v", err)
			retErr = fmt.Errorf("error loading rulesets: %v", err)
		}
		g.rulesets = rulesets
		g.ttlExpireRulesets = time.Now().Add(time.Duration(config.Config.GithubCacheTTL) * time.Second)
	}

	if time.Now().After(g.ttlExpireTeamsRepos) {
		teamsrepos, err := g.loadTeamReposConcurrently(ctx, config.Config.GithubConcurrentThreads, g.repositories)
		if err != nil {
			if !continueOnError {
				return err
			}
			logrus.Debugf("Error loading teams-repos: %v", err)
			retErr = fmt.Errorf("error loading teams-repos: %v", err)
		}
		g.teamRepos = teamsrepos
		g.ttlExpireTeamsRepos = time.Now().Add(time.Duration(config.Config.GithubCacheTTL) * time.Second)
	}

	logrus.Debugf("Nb remote users: %d", len(g.users))
	logrus.Debugf("Nb remote teams: %d", len(g.teams))
	logrus.Debugf("Nb remote repositories: %d", len(g.repositories))

	return retErr
}

func (g *GoliacRemoteImpl) loadTeamReposConcurrently(ctx context.Context, maxGoroutines int64, repositories map[string]*GithubRepository) (map[string]map[string]*GithubTeamRepo, error) {
	var childSpan trace.Span
	if config.Config.OpenTelemetryEnabled {
		ctx, childSpan = otel.Tracer("goliac").Start(ctx, "loadTeamReposConcurrently")
		defer childSpan.End()
	}
	logrus.Debug("loading teamReposConcurrently")

	resourcePerRepo, err := concurrentCall[*GithubTeamRepo](ctx, maxGoroutines, repositories, "teams_repos", g.loadTeamRepos, g.feedback)
	if err != nil {
		return nil, err
	}
	resourceRepos := make(map[string]map[string]*GithubTeamRepo)

	// we have all the resources per repo, now we need to invert the map
	for repository, repos := range resourcePerRepo {
		for team, repo := range repos {
			if _, ok := resourceRepos[team]; ok {
				resourceRepos[team][repository] = repo
			} else {
				resourceRepos[team] = map[string]*GithubTeamRepo{repository: repo}
			}
		}
	}

	return resourceRepos, nil
}

type TeamsRepoResponse struct {
	Name       string `json:"name"`
	Permission string `json:"permission"`
	Slug       string `json:"slug"`
}

/*
loadTeamRepos returns
map[teamSlug]repoinfo
*/
func (g *GoliacRemoteImpl) loadTeamRepos(ctx context.Context, repository *GithubRepository) (map[string]*GithubTeamRepo, error) {
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2022-11-28#list-repository-teams
	teamsrepo := make(map[string]*GithubTeamRepo)

	var teams []TeamsRepoResponse

	page := 1
	for page == 1 || len(teams) == 30 {
		data, err := g.client.CallRestAPI(
			ctx,
			"/repos/"+g.configGithubOrg+"/"+repository.Name+"/teams",
			fmt.Sprintf("page=%d&per_page=30", page),
			"GET",
			nil,
			nil)
		if err != nil {
			return nil, fmt.Errorf("not able to list teams for repo %s: %v", repository.Name, err)
		}

		err = json.Unmarshal(data, &teams)
		if err != nil {
			return nil, fmt.Errorf("not able to unmarshall teams for repo %s: %v", repository.Name, err)
		}

		for _, t := range teams {
			permission := ""
			switch t.Permission {
			case "admin":
				permission = "ADMIN"
			case "push":
				permission = "WRITE"
			case "pull":
				permission = "READ"
			}
			teamsrepo[t.Slug] = &GithubTeamRepo{
				Name:       repository.Name,
				Permission: permission,
			}
		}

		page++

		// sanity check to avoid loops
		if page > FORLOOP_STOP {
			break
		}
	}

	return teamsrepo, nil
}

// func (g *GoliacRemoteImpl) loadEnvironments(ctx context.Context, maxGoroutines int64, repositories map[string]*GithubRepository) (map[string]map[string]*GithubEnvironment, error) {
// 	var childSpan trace.Span
// 	if config.Config.OpenTelemetryEnabled {
// 		ctx, childSpan = otel.Tracer("goliac").Start(ctx, "loadEnvironmentsRepositories")
// 		defer childSpan.End()
// 	}
// 	logrus.Debug("loading environmentsRepositories")

// 	return concurrentCall(ctx, maxGoroutines, repositories, "environments_repos", g.loadEnvironmentsPerRepository, g.feedback)
// }

type EnvironmentsResponse struct {
	TotalCount   int                        `json:"total_count"`
	Environments []*GithubRemoteEnvironment `json:"environments"`
}

func (g *GoliacRemoteImpl) loadEnvironmentsPerRepository(ctx context.Context, repository *GithubRepository) (map[string]*GithubEnvironment, error) {
	// https://docs.github.com/en/enterprise-cloud@latest/rest/deployments/environments?apiVersion=2022-11-28#list-environments
	envs := make(map[string]*GithubEnvironment)
	respenvs := EnvironmentsResponse{}

	page := 1
	for page == 1 || len(respenvs.Environments) == 30 {
		data, err := g.client.CallRestAPI(ctx, "/repos/"+g.configGithubOrg+"/"+repository.Name+"/environments", fmt.Sprintf("page=%d&per_page=30", page), "GET", nil, nil)
		if err != nil {
			return nil, fmt.Errorf("not able to list environments for repo %s: %v", repository.Name, err)
		}

		err = json.Unmarshal(data, &respenvs)
		if err != nil {
			return nil, fmt.Errorf("not able to unmarshall environments for repo %s: %v", repository.Name, err)
		}

		for _, e := range respenvs.Environments {
			envs[e.Name] = &GithubEnvironment{
				Name:      e.Name,
				Variables: map[string]string{},
			}
		}

		page++

		// sanity check to avoid loops
		if page > FORLOOP_STOP {
			break
		}
	}

	return envs, nil
}

// func (g *GoliacRemoteImpl) loadRepositoriesVariables(ctx context.Context, maxGoroutines int64, repositories map[string]*GithubRepository) (map[string]map[string]*GithubVariable, error) {
// 	var childSpan trace.Span
// 	if config.Config.OpenTelemetryEnabled {
// 		ctx, childSpan = otel.Tracer("goliac").Start(ctx, "loadVariablesRepositories")
// 		defer childSpan.End()
// 	}
// 	logrus.Debug("loading variablesRepositories")

// 	return concurrentCall(ctx, maxGoroutines, repositories, "action_variables_repos", g.loadVariablesPerRepository, g.feedback)
// }

type VariablesResponse struct {
	TotalCount int               `json:"total_count"`
	Variables  []*GithubVariable `json:"variables"`
}

func (g *GoliacRemoteImpl) loadVariablesPerRepository(ctx context.Context, repository *GithubRepository) (map[string]*GithubVariable, error) {
	// https://docs.github.com/en/enterprise-cloud@latest/rest/actions/variables?apiVersion=2022-11-28#list-repository-variables
	variables := make(map[string]*GithubVariable)
	respenvs := VariablesResponse{}

	page := 1
	for page == 1 || len(respenvs.Variables) == 30 {
		data, err := g.client.CallRestAPI(ctx, "/repos/"+g.configGithubOrg+"/"+repository.Name+"/actions/variables", fmt.Sprintf("page=%d&per_page=30", page), "GET", nil, nil)
		if err != nil {
			return nil, fmt.Errorf("not able to list action variables for repo %s: %v", repository.Name, err)
		}

		err = json.Unmarshal(data, &respenvs)
		if err != nil {
			return nil, fmt.Errorf("not able to unmarshall action variables for repo %s: %v", repository.Name, err)
		}

		for _, e := range respenvs.Variables {
			variables[e.Name] = e
		}

		page++

		// sanity check to avoid loops
		if page > FORLOOP_STOP {
			break
		}
	}

	return variables, nil
}

// loadRepositoryCodeownersConcurrently fetches .github/CODEOWNERS for each non-archived repository
// so remote comparisons and reconciliation use the same content as GitHub (not only after an apply).
// Archived repositories are skipped (CodeownersContent remains empty). Any error from
// GetRepositoryCodeowners fails the whole operation.
func (g *GoliacRemoteImpl) loadRepositoryCodeownersConcurrently(ctx context.Context, maxGoroutines int64, repositories map[string]*GithubRepository) error {
	var childSpan trace.Span
	if config.Config.OpenTelemetryEnabled {
		ctx, childSpan = otel.Tracer("goliac").Start(ctx, "loadRepositoryCodeowners")
		defer childSpan.End()
	}
	logrus.Debug("loading repository CODEOWNERS files")

	repositoriesToLoad := make([]*GithubRepository, 0, len(repositories))
	for _, repo := range repositories {
		if repo.BoolProperties["archived"] {
			continue
		}
		repositoriesToLoad = append(repositoriesToLoad, repo)
	}
	if g.feedback != nil {
		g.feedback.Extend(len(repositoriesToLoad))
	}

	var wg sync.WaitGroup

	reposChan := make(chan *GithubRepository, len(repositories))
	errChan := make(chan error, 1)

	if maxGoroutines < 1 {
		maxGoroutines = 1
	}

	for i := int64(0); i < maxGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range reposChan {
				content, sha, err := g.GetRepositoryCodeowners(ctx, repo.Name)
				if err != nil {
					select {
					case errChan <- fmt.Errorf("CODEOWNERS for %s: %w", repo.Name, err):
					default:
					}
					return
				}
				repo.CodeownersContent = content
				repo.CodeownersSHA = sha
				if g.feedback != nil {
					g.feedback.LoadingAsset("repo_codeowners", 1)
				}
			}
		}()
	}

	for _, repo := range repositoriesToLoad {
		reposChan <- repo
	}
	close(reposChan)

	wg.Wait()

	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}

// loadRepositoryPagesConcurrently fetches GitHub Pages metadata for each non-archived repository.
func (g *GoliacRemoteImpl) loadRepositoryPagesConcurrently(ctx context.Context, maxGoroutines int64, repositories map[string]*GithubRepository) error {
	var childSpan trace.Span
	if config.Config.OpenTelemetryEnabled {
		ctx, childSpan = otel.Tracer("goliac").Start(ctx, "loadRepositoryPages")
		defer childSpan.End()
	}
	logrus.Debug("loading repository GitHub Pages metadata")

	repositoriesToLoad := make([]*GithubRepository, 0, len(repositories))
	for _, repo := range repositories {
		if repo.BoolProperties["archived"] {
			continue
		}
		repositoriesToLoad = append(repositoriesToLoad, repo)
	}
	if g.feedback != nil {
		g.feedback.Extend(len(repositoriesToLoad))
	}

	var wg sync.WaitGroup
	reposChan := make(chan *GithubRepository, len(repositories))
	errChan := make(chan error, 1)

	if maxGoroutines < 1 {
		maxGoroutines = 1
	}

	for i := int64(0); i < maxGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range reposChan {
				pages, err := g.fetchRepositoryPages(ctx, repo.Name)
				if err != nil {
					select {
					case errChan <- fmt.Errorf("GitHub Pages for %s: %w", repo.Name, err):
					default:
					}
					return
				}
				repo.GithubPages = pages
				if g.feedback != nil {
					g.feedback.LoadingAsset("repo_pages", 1)
				}
			}
		}()
	}

	for _, repo := range repositoriesToLoad {
		reposChan <- repo
	}
	close(reposChan)

	wg.Wait()

	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}

func (g *GoliacRemoteImpl) fetchRepositoryPages(ctx context.Context, repositoryName string) (*GithubPagesRemote, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/pages", g.configGithubOrg, repositoryName)
	body, err := g.client.CallRestAPI(ctx, endpoint, "", "GET", nil, nil)
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "Not Found") {
			return nil, nil
		}
		return nil, err
	}
	var pages GithubPagesRemote
	if err := json.Unmarshal(body, &pages); err != nil {
		return nil, err
	}
	return &pages, nil
}

// GetRepositoryPages implements GoliacRemote.
func (g *GoliacRemoteImpl) GetRepositoryPages(ctx context.Context, repositoryName string) (*GithubPagesRemote, error) {
	g.actionMutex.Lock()
	repo, ok := g.repositories[repositoryName]
	g.actionMutex.Unlock()
	if ok && repo != nil && repo.GithubPages != nil {
		return repo.GithubPages, nil
	}
	return g.fetchRepositoryPages(ctx, repositoryName)
}

func (g *GoliacRemoteImpl) loadCustomPropertiesConcurrently(ctx context.Context, maxGoroutines int64, repositories map[string]*GithubRepository) (map[string]map[string]interface{}, error) {
	var childSpan trace.Span
	if config.Config.OpenTelemetryEnabled {
		ctx, childSpan = otel.Tracer("goliac").Start(ctx, "loadCustomProperties")
		defer childSpan.End()
	}
	logrus.Debug("loading custom properties")

	loadCustomPropertiesPerRepo := func(ctx context.Context, repository *GithubRepository) (map[string]interface{}, error) {
		return g.loadCustomPropertiesPerRepository(ctx, repository)
	}

	// Use a simple map[string]interface{} as the resource type
	customPropsPerRepo := make(map[string]map[string]interface{})

	var wg sync.WaitGroup
	var wg2 sync.WaitGroup

	reposChan := make(chan *GithubRepository, len(repositories))
	errChan := make(chan error, 1)
	propsChan := make(chan struct {
		repoName string
		props    map[string]interface{}
	}, len(repositories))
	if g.feedback != nil {
		g.feedback.Extend(len(repositories))
	}

	if maxGoroutines < 1 {
		maxGoroutines = 1
	}

	for i := int64(0); i < maxGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range reposChan {
				props, err := loadCustomPropertiesPerRepo(ctx, repo)
				if err != nil {
					select {
					case errChan <- err:
					default:
					}
					return
				}
				propsChan <- struct {
					repoName string
					props    map[string]interface{}
				}{repo.Name, props}
			}
		}()
	}

	for _, repo := range repositories {
		reposChan <- repo
	}
	close(reposChan)

	wg2.Add(1)
	go func() {
		defer wg2.Done()
		for r := range propsChan {
			if g.feedback != nil {
				g.feedback.LoadingAsset("custom_properties", 1)
			}
			customPropsPerRepo[r.repoName] = r.props
		}
	}()

	wg.Wait()
	close(propsChan)
	wg2.Wait()

	select {
	case err := <-errChan:
		return customPropsPerRepo, err
	default:
		return customPropsPerRepo, nil
	}
}

func (g *GoliacRemoteImpl) loadCustomPropertiesPerRepository(ctx context.Context, repository *GithubRepository) (map[string]interface{}, error) {
	// https://docs.github.com/en/rest/repos/custom-properties?apiVersion=2022-11-28#get-all-custom-property-values-for-a-repository
	customProps := make(map[string]interface{})

	data, err := g.client.CallRestAPI(ctx, fmt.Sprintf("/repos/%s/%s/properties/values", g.configGithubOrg, repository.Name), "", "GET", nil, nil)
	if err != nil {
		// If the endpoint returns 404, it might mean custom properties are not enabled or the repo has none
		// We'll return an empty map in that case
		if strings.Contains(err.Error(), "404") {
			return customProps, nil
		}
		return nil, fmt.Errorf("not able to list custom properties for repo %s: %v", repository.Name, err)
	}

	var properties []struct {
		PropertyName string      `json:"property_name"`
		Value        interface{} `json:"value"`
	}

	err = json.Unmarshal(data, &properties)
	if err != nil {
		return nil, fmt.Errorf("not able to unmarshal custom properties for repo %s: %v", repository.Name, err)
	}

	for _, prop := range properties {
		customProps[prop.PropertyName] = prop.Value
	}

	return customProps, nil
}

func (g *GoliacRemoteImpl) OrgCustomProperties(ctx context.Context) map[string]*config.GithubCustomProperty {
	if time.Now().After(g.ttlExpireCustomProperties) {
		customProperties, err := g.loadOrgCustomProperties(ctx)
		if err == nil {
			g.orgCustomProperties = customProperties
			g.ttlExpireCustomProperties = time.Now().Add(time.Duration(config.Config.GithubCacheTTL) * time.Second)
		}
	}
	return g.orgCustomProperties
}

func (g *GoliacRemoteImpl) loadOrgCustomProperties(ctx context.Context) (map[string]*config.GithubCustomProperty, error) {
	// https://docs.github.com/en/rest/orgs/custom-properties?apiVersion=2022-11-28#get-all-custom-properties-for-an-organization
	logrus.Debug("loading org custom properties")
	customProperties := make(map[string]*config.GithubCustomProperty)

	data, err := g.client.CallRestAPI(ctx, fmt.Sprintf("/orgs/%s/properties/schema", g.configGithubOrg), "", "GET", nil, nil)
	if err != nil {
		// If the endpoint returns 404, it might mean custom properties are not enabled
		// We'll return an empty map in that case
		if strings.Contains(err.Error(), "404") {
			return customProperties, nil
		}
		return nil, fmt.Errorf("not able to list custom properties for org %s: %v", g.configGithubOrg, err)
	}

	var properties []config.GithubCustomProperty
	err = json.Unmarshal(data, &properties)
	if err != nil {
		return nil, fmt.Errorf("not able to unmarshal custom properties for org %s: %v", g.configGithubOrg, err)
	}

	for _, prop := range properties {
		propCopy := prop // create a copy to avoid taking address of loop variable
		customProperties[prop.PropertyName] = &propCopy
	}

	return customProperties, nil
}

func (g *GoliacRemoteImpl) CreateOrUpdateOrgCustomProperty(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, property *config.GithubCustomProperty) {
	// https://docs.github.com/en/rest/orgs/custom-properties?apiVersion=2022-11-28#create-or-update-a-custom-property-for-an-organization
	if !dryrun {
		body := map[string]interface{}{
			"value_type": property.ValueType,
		}
		if property.Required {
			body["required"] = property.Required
		}
		if property.DefaultValue != "" {
			body["default_value"] = property.DefaultValue
		}
		if property.Description != "" {
			body["description"] = property.Description
		}
		if len(property.AllowedValues) > 0 {
			body["allowed_values"] = property.AllowedValues
		}
		if property.ValuesEditableBy != "" {
			body["values_editable_by"] = property.ValuesEditableBy
		}

		responseBody, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/orgs/%s/properties/schema/%s", g.configGithubOrg, property.PropertyName),
			"",
			"PUT",
			body,
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to create or update custom property %s for org: %v. %s", property.PropertyName, err, string(responseBody)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// Update local cache
	propCopy := *property
	g.orgCustomProperties[property.PropertyName] = &propCopy
}

func (g *GoliacRemoteImpl) DeleteOrgCustomProperty(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, propertyName string) {
	// https://docs.github.com/en/rest/orgs/custom-properties?apiVersion=2022-11-28#remove-a-custom-property-for-an-organization
	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/orgs/%s/properties/schema/%s", g.configGithubOrg, propertyName),
			"",
			"DELETE",
			nil,
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to delete custom property %s from org: %v. %s", propertyName, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// Update local cache
	delete(g.orgCustomProperties, propertyName)
}

// func (g *GoliacRemoteImpl) loadEnvironmentVariables(ctx context.Context, maxGoroutines int64, repositories map[string]*GithubRepository) (map[string]map[string]map[string]*GithubVariable, error) {
// 	var childSpan trace.Span
// 	if config.Config.OpenTelemetryEnabled {
// 		ctx, childSpan = otel.Tracer("goliac").Start(ctx, "loadEnvironmentVariables")
// 		defer childSpan.End()
// 	}
// 	logrus.Debug("loading environmentVariables")

// 	return concurrentCall(ctx, maxGoroutines, repositories, "environment_variables_repos", g.loadEnvironmentVariablesPerRepository, nil)
// }

type EnvironmentsVariablesResponse struct {
	TotalCount int               `json:"total_count"`
	Variables  []*GithubVariable `json:"variables"`
}

// func (g *GoliacRemoteImpl) loadEnvironmentVariablesPerRepository(ctx context.Context, repository *GithubRepository) (map[string]map[string]*GithubVariable, error) {
// 	// https://docs.github.com/en/enterprise-cloud@latest/rest/actions/variables?apiVersion=2022-11-28#list-environment-variables
// 	envvars := make(map[string]map[string]*GithubVariable)
// 	respenvs := EnvironmentsVariablesResponse{}

// 	for _, environment := range repository.Environments {
// 		envvars[environment.Name] = make(map[string]*GithubVariable)
// 		page := 1
// 		for page == 1 || len(respenvs.Variables) == 30 {
// 			data, err := g.client.CallRestAPI(ctx, "/repos/"+g.configGithubOrg+"/"+repository.Name+"/environments/"+environment.Name+"/variables", fmt.Sprintf("page=%d&per_page=30", page), "GET", nil, nil)
// 			if err != nil {
// 				return nil, fmt.Errorf("not able to list environments for repo %s: %v", repository.Name, err)
// 			}

// 			err = json.Unmarshal(data, &respenvs)
// 			if err != nil {
// 				return nil, fmt.Errorf("not able to unmarshall environments for repo %s: %v", repository.Name, err)
// 			}

// 			for _, e := range respenvs.Variables {
// 				envvars[environment.Name][e.Name] = e
// 			}

// 			page++

// 			// sanity check to avoid loops
// 			if page > FORLOOP_STOP {
// 				break
// 			}
// 		}
// 	}

// 	return envvars, nil
// }

func (g *GoliacRemoteImpl) loadEnvironmentVariablesForEnvironmentRepository(ctx context.Context, repositoryName string, environmentName string) (map[string]string, error) {
	// https://docs.github.com/en/enterprise-cloud@latest/rest/actions/variables?apiVersion=2022-11-28#list-environment-variables
	respenvs := EnvironmentsVariablesResponse{}

	envvars := make(map[string]string)
	page := 1
	for page == 1 || len(respenvs.Variables) == 30 {
		data, err := g.client.CallRestAPI(ctx, "/repos/"+g.configGithubOrg+"/"+repositoryName+"/environments/"+environmentName+"/variables", fmt.Sprintf("page=%d&per_page=30", page), "GET", nil, nil)
		if err != nil {
			return nil, fmt.Errorf("not able to list environments for repo %s: %v", repositoryName, err)
		}

		err = json.Unmarshal(data, &respenvs)
		if err != nil {
			return nil, fmt.Errorf("not able to unmarshall environments for repo %s: %v", repositoryName, err)
		}

		for _, e := range respenvs.Variables {
			envvars[e.Name] = e.Value
		}

		page++

		// sanity check to avoid loops
		if page > FORLOOP_STOP {
			break
		}
	}

	return envvars, nil
}

type AutolinksResponse struct {
	Id             int    `json:"id"`
	KeyPrefix      string `json:"key_prefix"`
	UrlTemplate    string `json:"url_template"`
	IsAlphanumeric bool   `json:"is_alphanumeric"`
}

func (g *GoliacRemoteImpl) loadAutolinksPerRepository(ctx context.Context, repository *GithubRepository) (map[string]*GithubAutolink, error) {
	// https://docs.github.com/en/enterprise-cloud@latest/rest/repos/autolinks?apiVersion=2022-11-28#list-repository-autolinks
	autolinks := []AutolinksResponse{}

	data, err := g.client.CallRestAPI(ctx, "/repos/"+g.configGithubOrg+"/"+repository.Name+"/autolinks", "", "GET", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("not able to list autolinks for repo %s: %v", repository.Name, err)
	}

	err = json.Unmarshal(data, &autolinks)
	if err != nil {
		return nil, fmt.Errorf("not able to unmarshall autolinks for repo %s: %v", repository.Name, err)
	}

	autolinksMap := make(map[string]*GithubAutolink)
	for _, autolink := range autolinks {
		autolinksMap[autolink.KeyPrefix] = &GithubAutolink{
			Id:             autolink.Id,
			KeyPrefix:      autolink.KeyPrefix,
			UrlTemplate:    autolink.UrlTemplate,
			IsAlphanumeric: autolink.IsAlphanumeric,
		}
	}

	return autolinksMap, nil
}

// func (g *GoliacRemoteImpl) loadRepositoriesSecrets(ctx context.Context, maxGoroutines int64, repositories map[string]*GithubRepository) (map[string]map[string]*GithubVariable, error) {
// 	var childSpan trace.Span
// 	if config.Config.OpenTelemetryEnabled {
// 		ctx, childSpan = otel.Tracer("goliac").Start(ctx, "loadRepositoriesSecrets")
// 		defer childSpan.End()
// 	}
// 	logrus.Debug("loading repositoriesSecrets")

// 	return concurrentCall(ctx, maxGoroutines, repositories, "repositories_secrets_repos", g.loadRepositoriesSecretsPerRepository, g.feedback)
// }

type RepositoriesSecretsResponse struct {
	TotalCount int               `json:"total_count"`
	Secrets    []*GithubVariable `json:"secrets"`
}

func (g *GoliacRemoteImpl) RepositoriesSecretsPerRepository(ctx context.Context, repositoryName string) (map[string]*GithubVariable, error) {
	// https://docs.github.com/en/enterprise-cloud@latest/rest/actions/secrets?apiVersion=2022-11-28#list-repository-secrets
	envsecrets := make(map[string]*GithubVariable)
	respenvs := RepositoriesSecretsResponse{}

	page := 1
	for page == 1 || len(respenvs.Secrets) == 30 {
		data, err := g.client.CallRestAPI(ctx, "/repos/"+g.configGithubOrg+"/"+repositoryName+"/actions/secrets", fmt.Sprintf("page=%d&per_page=30", page), "GET", nil, nil)
		if err != nil {
			return nil, fmt.Errorf("not able to list repositories secrets for repo %s: %v", repositoryName, err)
		}

		err = json.Unmarshal(data, &respenvs)
		if err != nil {
			return nil, fmt.Errorf("not able to unmarshall repositories secrets for repo %s: %v", repositoryName, err)
		}

		for _, e := range respenvs.Secrets {
			envsecrets[e.Name] = e
		}

		page++

		// sanity check to avoid loops
		if page > FORLOOP_STOP {
			break
		}
	}

	return envsecrets, nil
}

// func (g *GoliacRemoteImpl) loadEnvironmentSecrets(ctx context.Context, maxGoroutines int64, repositories map[string]*GithubRepository) (map[string]map[string]map[string]*GithubVariable, error) {
// 	var childSpan trace.Span
// 	if config.Config.OpenTelemetryEnabled {
// 		ctx, childSpan = otel.Tracer("goliac").Start(ctx, "loadEnvironmentSecrets")
// 		defer childSpan.End()
// 	}
// 	logrus.Debug("loading environmentSecrets")

// 	return concurrentCall(ctx, maxGoroutines, repositories, "environment_secrets_repos", g.loadEnvironmentSecretsPerRepository, nil)
// }

type EnvironmentsSecretsResponse struct {
	TotalCount int               `json:"total_count"`
	Secrets    []*GithubVariable `json:"secrets"`
}

func (g *GoliacRemoteImpl) EnvironmentSecretsPerRepository(ctx context.Context, environments []string, repositoryName string) (map[string]map[string]*GithubVariable, error) {
	// https://docs.github.com/en/enterprise-cloud@latest/rest/actions/secrets?apiVersion=2022-11-28#list-environment-secrets
	envsecrets := make(map[string]map[string]*GithubVariable)
	respenvs := EnvironmentsSecretsResponse{}

	for _, environment := range environments {
		envsecrets[environment] = make(map[string]*GithubVariable)
		page := 1
		for page == 1 || len(respenvs.Secrets) == 30 {
			data, err := g.client.CallRestAPI(ctx, "/repos/"+g.configGithubOrg+"/"+repositoryName+"/environments/"+environment+"/secrets", fmt.Sprintf("page=%d&per_page=30", page), "GET", nil, nil)
			if err != nil {
				return nil, fmt.Errorf("not able to list environments for repo %s: %v", repositoryName, err)
			}

			err = json.Unmarshal(data, &respenvs)
			if err != nil {
				return nil, fmt.Errorf("not able to unmarshall environments for repo %s: %v", repositoryName, err)
			}

			for _, e := range respenvs.Secrets {
				envsecrets[environment][e.Name] = e
			}

			page++

			// sanity check to avoid loops
			if page > FORLOOP_STOP {
				break
			}
		}
	}

	return envsecrets, nil
}

const listAllTeamMembersInOrg = `
query listAllTeamMembersInOrg($orgLogin: String!, $teamSlug: String!, $endCursor: String) {
    organization(login: $orgLogin) {
      team(slug: $teamSlug) {
        members(first: 100, membership: IMMEDIATE, after: $endCursor) {
          edges {
            node {
              login
            }
            role
          }
          pageInfo {
            hasNextPage
            endCursor
          }
          totalCount
        }
      }
    }
  }
`

type GraplQLTeamMembers struct {
	Data struct {
		Organization struct {
			Team struct {
				Members struct {
					Edges []struct {
						Node struct {
							Login string
						}
						Role string
					} `json:"edges"`
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					} `json:"pageInfo"`
					TotalCount int `json:"totalCount"`
				} `json:"members"`
			} `json:"team"`
		}
	}
	Errors []struct {
		Path       []interface{} `json:"path"`
		Extensions struct {
			Code         string
			ErrorMessage string
		} `json:"extensions"`
		Message string
	} `json:"errors"`
}

func (g *GoliacRemoteImpl) loadTeams(ctx context.Context) (map[string]*GithubTeam, map[string]string, error) {
	var childSpan trace.Span
	if config.Config.OpenTelemetryEnabled {
		ctx, childSpan = otel.Tracer("goliac").Start(ctx, "loadTeams")
		defer childSpan.End()
	}
	logrus.Debug("loading teams")
	teams := make(map[string]*GithubTeam)
	teamSlugByName := make(map[string]string)

	variables := make(map[string]interface{})
	variables["orgLogin"] = g.configGithubOrg
	variables["endCursor"] = nil

	hasNextPage := true
	count := 0
	for hasNextPage {
		data, err := g.client.QueryGraphQLAPI(ctx, listAllTeamsInOrg, variables, nil)
		if err != nil {
			return teams, teamSlugByName, err
		}
		var gResult GraplQLTeams

		// parse first page
		err = json.Unmarshal(data, &gResult)
		if err != nil {
			return teams, teamSlugByName, err
		}
		if len(gResult.Errors) > 0 {
			return teams, teamSlugByName, fmt.Errorf("graphql error on loadTeams: %v (%v)", gResult.Errors[0].Message, gResult.Errors[0].Path)
		}

		for _, c := range gResult.Data.Organization.Teams.Nodes {
			team := GithubTeam{
				Name:      c.Name,
				GraphqlId: c.Id,
				Id:        c.DatabaseId,
				Slug:      c.Slug,
			}
			if c.ParentTeam.DatabaseId != 0 {
				parentId := c.ParentTeam.DatabaseId
				team.ParentTeam = &parentId
			}
			teams[c.Slug] = &team
			teamSlugByName[c.Name] = c.Slug
		}

		if g.feedback != nil {
			g.feedback.LoadingAsset("teams", len(gResult.Data.Organization.Teams.Nodes))
		}

		hasNextPage = gResult.Data.Organization.Teams.PageInfo.HasNextPage
		variables["endCursor"] = gResult.Data.Organization.Teams.PageInfo.EndCursor

		count++
		// sanity check to avoid loops
		if count > FORLOOP_STOP {
			break
		}
	}

	// load team's members
	if config.Config.GithubConcurrentThreads <= 1 {
		for _, t := range teams {
			err := g.loadTeamsMembers(ctx, t)
			if err != nil {
				return teams, teamSlugByName, err
			}
			if g.feedback != nil {
				g.feedback.LoadingAsset("teams_members", 1)
			}
		}
	} else {
		var wg sync.WaitGroup

		// Create buffered channels
		teamsChan := make(chan *GithubTeam, len(teams))
		errChan := make(chan error, 1) // will hold the first error

		// Create worker goroutines
		for i := int64(0); i < config.Config.GithubConcurrentThreads; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for t := range teamsChan {
					err := g.loadTeamsMembers(ctx, t)
					if err != nil {
						// Try to report the error
						select {
						case errChan <- err:
						default:
						}
						return
					}
					if g.feedback != nil {
						g.feedback.LoadingAsset("teams_members", 1)
					}
				}
			}()
		}

		// Send teams to teamsChan
		for _, t := range teams {
			teamsChan <- t
		}
		close(teamsChan)

		// Wait for all goroutines to finish
		wg.Wait()

		// Check if any goroutine returned an error
		select {
		case err := <-errChan:
			return teams, teamSlugByName, err
		default:
			//nop
		}
	}

	return teams, teamSlugByName, nil
}

func (g *GoliacRemoteImpl) loadTeamsMembers(ctx context.Context, t *GithubTeam) error {
	variables := make(map[string]interface{})
	variables["orgLogin"] = g.configGithubOrg
	variables["endCursor"] = nil
	variables["teamSlug"] = t.Slug

	hasNextPage := true
	count := 0
	for hasNextPage {
		data, err := g.client.QueryGraphQLAPI(ctx, listAllTeamMembersInOrg, variables, nil)
		if err != nil {
			return err
		}
		var gResult GraplQLTeamMembers

		// parse first page
		err = json.Unmarshal(data, &gResult)
		if err != nil {
			return err
		}
		if len(gResult.Errors) > 0 {
			return fmt.Errorf("graphql error on loadTeams members: %v (%v)", gResult.Errors[0].Message, gResult.Errors[0].Path)
		}

		for _, c := range gResult.Data.Organization.Team.Members.Edges {
			if c.Role == "MAINTAINER" {
				t.Maintainers = append(t.Maintainers, c.Node.Login)
			} else {
				t.Members = append(t.Members, c.Node.Login)
			}
		}

		hasNextPage = gResult.Data.Organization.Team.Members.PageInfo.HasNextPage
		variables["endCursor"] = gResult.Data.Organization.Team.Members.PageInfo.EndCursor

		count++
		// sanity check to avoid loops
		if count > FORLOOP_STOP {
			break
		}
	}
	return nil
}

const listRulesets = `
query listRulesets ($orgLogin: String!) {
	organization(login: $orgLogin) {
	  rulesets(first: 100) {
		nodes {
		  databaseId
		  name
		  target
		  enforcement
		  bypassActors(first:100) {
			actors:nodes {
# Note: not able to find the bypassActors
# It is working as a human, but not as a Github App
# The  field 'actor' is restricted for GitHub Apps,
# even with full administration permissions
			  actor {
				... on App {
					databaseId
					name
				}
				... on Team {
					teamslug:slug
				}
			  }
			  bypassMode
			}
		  }
		  conditions {
			refName {
			  include
			  exclude
			}
			repositoryName {
			  exclude
			  include
			}
			repositoryId {
				repositoryIds
			}
		  }
		  rules(first:100) {
			nodes {
				parameters {
					... on PullRequestParameters {
						dismissStaleReviewsOnPush
						requireCodeOwnerReview
						requiredApprovingReviewCount
						requiredReviewThreadResolution
						requireLastPushApproval
						allowedMergeMethods
					}
					... on RequiredStatusChecksParameters {
						requiredStatusChecks {
							context
						}
					}
					... on BranchNamePatternParameters {
						name
						negate
						operator
						pattern
					}
					... on TagNamePatternParameters {
						name
						negate
						operator
						pattern
					}
					... on MergeQueueParameters {
						checkResponseTimeoutMinutes
						groupingStrategy
						maxEntriesToBuild
						maxEntriesToMerge
						mergeMethod
						minEntriesToMerge
						minEntriesToMergeWaitMinutes
					}
				}
				type
			}
		  }
		}
		pageInfo {
            hasNextPage
            endCursor
		}
		totalCount
	  }
	}
  }
`

type GithubRuleSetActor struct {
	Actor struct {
		DatabaseId int
		Name       string
		TeamSlug   string
	}
	BypassMode string // ALWAYS, PULL_REQUEST
}

type GithubRuleSetRuleStatusCheck struct {
	Context string
	// IntegrationId int
}

type GithubRuleSetRule struct {
	Parameters struct {
		// PullRequestParameters
		DismissStaleReviewsOnPush      bool
		RequireCodeOwnerReview         bool
		RequiredApprovingReviewCount   int
		RequiredReviewThreadResolution bool
		RequireLastPushApproval        bool
		AllowedMergeMethods            []string

		// RequiredStatusChecksParameters
		RequiredStatusChecks             []GithubRuleSetRuleStatusCheck
		StrictRequiredStatusChecksPolicy bool

		// BranchNamePatternParameters / TagNamePatternParameters
		Name     string
		Negate   bool
		Operator string
		Pattern  string

		// MergeQueueParameters
		CheckResponseTimeoutMinutes  int
		GroupingStrategy             string // ALLGREEN, HEADGREEN
		MaxEntriesToBuild            int
		MaxEntriesToMerge            int
		MergeMethod                  string // MERGE, REBASE, SQUASH
		MinEntriesToMerge            int
		MinEntriesToMergeWaitMinutes int
	}
	ID   int
	Type string // CREATION, UPDATE, DELETION, REQUIRED_LINEAR_HISTORY, REQUIRED_DEPLOYMENTS, REQUIRED_SIGNATURES, PULL_REQUEST, REQUIRED_STATUS_CHECKS, NON_FAST_FORWARD, COMMIT_MESSAGE_PATTERN, COMMIT_AUTHOR_EMAIL_PATTERN, COMMITTER_EMAIL_PATTERN, BRANCH_NAME_PATTERN, TAG_NAME_PATTERN, MERGE_QUEUE
}

type GraphQLGithubRuleSet struct {
	DatabaseId int
	Source     struct {
		Name string
	}
	Name         string
	Target       string // BRANCH, TAG
	Enforcement  string // DISABLED, ACTIVE, EVALUATE
	BypassActors struct {
		Actors []GithubRuleSetActor
	}
	Conditions struct {
		RefName struct { // target branches
			Include []string // ~DEFAULT_BRANCH, ~ALL,
			Exclude []string
		}
		RepositoryName struct { // regex
			Include   []string
			Exclude   []string
			Protected bool
		}
		RepositoryId struct { // per repo
			RepositoryIds []string
		}
	}
	Rules struct {
		Nodes []GithubRuleSetRule
	}
}

type GraplQLRuleSets struct {
	Data struct {
		Organization struct {
			Rulesets struct {
				Nodes    []GraphQLGithubRuleSet
				PageInfo struct {
					HasNextPage bool
					EndCursor   string
				} `json:"pageInfo"`
				TotalCount int `json:"totalCount"`
			} `json:"rulesets"`
		}
	}
	Errors []struct {
		Path       []string `json:"path"`
		Extensions struct {
			Code         string
			ErrorMessage string
		} `json:"extensions"`
		Message string
	} `json:"errors"`
}

type GithubRuleSet struct {
	Name        string
	Id          int               // for tracking purpose
	Enforcement string            // disabled, active, evaluate
	BypassApps  map[string]string // appname, mode (always, pull_request)
	BypassTeams map[string]string // teamslug, mode (always, pull_request)

	OnInclude []string // ~DEFAULT_BRANCH, ~ALL, branch_name, ...
	OnExclude []string //  branch_name, ...

	Rules map[string]entity.RuleSetParameters

	Repositories []string // only used for organization rulesets
}

func (g *GoliacRemoteImpl) fromGraphQLToGithubRuleset(src *GraphQLGithubRuleSet) *GithubRuleSet {
	ruleset := GithubRuleSet{
		Name:         src.Name,
		Id:           src.DatabaseId,
		Enforcement:  strings.ToLower(src.Enforcement),
		BypassApps:   map[string]string{},
		BypassTeams:  map[string]string{},
		OnInclude:    []string{},
		OnExclude:    []string{},
		Rules:        map[string]entity.RuleSetParameters{},
		Repositories: []string{},
	}
	for _, include := range src.Conditions.RefName.Include {
		if strings.HasPrefix(include, "refs/heads/") {
			ruleset.OnInclude = append(ruleset.OnInclude, include[11:])
		} else {
			ruleset.OnInclude = append(ruleset.OnInclude, include)
		}
	}
	for _, exclude := range src.Conditions.RefName.Exclude {
		if strings.HasPrefix(exclude, "refs/heads/") {
			ruleset.OnExclude = append(ruleset.OnExclude, exclude[11:])
		} else {
			ruleset.OnExclude = append(ruleset.OnExclude, exclude)
		}
	}

	for _, b := range src.BypassActors.Actors {
		if b.Actor.TeamSlug != "" {
			ruleset.BypassTeams[b.Actor.TeamSlug] = strings.ToLower(b.BypassMode)
		} else {
			appslug := slug.Make(b.Actor.Name)
			ruleset.BypassApps[appslug] = strings.ToLower(b.BypassMode)
		}
	}

	for _, r := range src.Rules.Nodes {
		rule := entity.RuleSetParameters{
			DismissStaleReviewsOnPush:        r.Parameters.DismissStaleReviewsOnPush,
			RequireCodeOwnerReview:           r.Parameters.RequireCodeOwnerReview,
			RequiredApprovingReviewCount:     r.Parameters.RequiredApprovingReviewCount,
			RequiredReviewThreadResolution:   r.Parameters.RequiredReviewThreadResolution,
			RequireLastPushApproval:          r.Parameters.RequireLastPushApproval,
			AllowedMergeMethods:              r.Parameters.AllowedMergeMethods,
			StrictRequiredStatusChecksPolicy: r.Parameters.StrictRequiredStatusChecksPolicy,
			Name:                             r.Parameters.Name,
			Negate:                           r.Parameters.Negate,
			Operator:                         r.Parameters.Operator,
			Pattern:                          r.Parameters.Pattern,
			CheckResponseTimeoutMinutes:      r.Parameters.CheckResponseTimeoutMinutes,
			GroupingStrategy:                 r.Parameters.GroupingStrategy,
			MaxEntriesToBuild:                r.Parameters.MaxEntriesToBuild,
			MaxEntriesToMerge:                r.Parameters.MaxEntriesToMerge,
			MergeMethod:                      r.Parameters.MergeMethod,
			MinEntriesToMerge:                r.Parameters.MinEntriesToMerge,
			MinEntriesToMergeWaitMinutes:     r.Parameters.MinEntriesToMergeWaitMinutes,
		}
		for _, s := range r.Parameters.RequiredStatusChecks {
			rule.RequiredStatusChecks = append(rule.RequiredStatusChecks, s.Context)
		}
		ruleset.Rules[strings.ToLower(r.Type)] = rule
	}

	for _, r := range src.Conditions.RepositoryId.RepositoryIds {
		if repo, ok := g.repositoriesByRefId[r]; ok {
			ruleset.Repositories = append(ruleset.Repositories, repo.Name)
		}
	}

	return &ruleset
}

func (g *GoliacRemoteImpl) loadRulesets(ctx context.Context, githubToken *string) (map[string]*GithubRuleSet, error) {
	var childSpan trace.Span
	if config.Config.OpenTelemetryEnabled {
		ctx, childSpan = otel.Tracer("goliac").Start(ctx, "loadRulesets")
		defer childSpan.End()
	}
	logrus.Debug("loading rulesets")
	variables := make(map[string]interface{})
	variables["orgLogin"] = g.configGithubOrg
	variables["endCursor"] = nil

	rulesets := make(map[string]*GithubRuleSet)

	hasNextPage := true
	count := 0
	for hasNextPage {
		data, err := g.client.QueryGraphQLAPI(ctx, listRulesets, variables, githubToken)
		if err != nil {
			return rulesets, fmt.Errorf("failed to load rulesets: %v", err)
		}
		var gResult GraplQLRuleSets

		// parse first page
		err = json.Unmarshal(data, &gResult)
		if err != nil {
			return rulesets, fmt.Errorf("failed to unmarshal rulesets: %v (%s)", err, string(data))
		}
		if len(gResult.Errors) > 0 {
			return rulesets, fmt.Errorf("graphql error on loadRulesets: %v (%v)", gResult.Errors[0].Message, gResult.Errors[0].Path)
		}

		for _, c := range gResult.Data.Organization.Rulesets.Nodes {
			rulesets[c.Name] = g.fromGraphQLToGithubRuleset(&c)
		}

		hasNextPage = gResult.Data.Organization.Rulesets.PageInfo.HasNextPage
		variables["endCursor"] = gResult.Data.Organization.Rulesets.PageInfo.EndCursor

		count++
		// sanity check to avoid loops
		if count > FORLOOP_STOP {
			break
		}
	}

	return rulesets, nil
}

func (g *GoliacRemoteImpl) prepareRuleset(ruleset *GithubRuleSet) map[string]interface{} {
	bypassActors := make([]map[string]interface{}, 0)

	for appname, mode := range ruleset.BypassApps {
		// let's find the app id based on the app slug name
		if appId, ok := g.appIds[appname]; ok {
			bypassActor := map[string]interface{}{
				"actor_id":    appId.Id,
				"actor_type":  "Integration",
				"bypass_mode": mode,
			}
			bypassActors = append(bypassActors, bypassActor)
		}
	}
	for teamslug, mode := range ruleset.BypassTeams {
		if team, ok := g.teams[teamslug]; ok {
			bypassActor := map[string]interface{}{
				"actor_id":    team.Id,
				"actor_type":  "Team",
				"bypass_mode": mode,
			}
			bypassActors = append(bypassActors, bypassActor)
		}
	}

	repoIds := []int{}
	for _, r := range ruleset.Repositories {
		if rid, ok := g.repositories[r]; ok {
			repoIds = append(repoIds, rid.Id)
		}
	}
	include := []string{}
	if ruleset.OnInclude != nil {
		for _, i := range ruleset.OnInclude {
			if strings.HasPrefix(i, "~") {
				include = append(include, i)
			} else {
				include = append(include, "refs/heads/"+i)
			}
		}
	}
	exclude := []string{}
	if ruleset.OnExclude != nil {
		for _, e := range ruleset.OnExclude {
			if strings.HasPrefix(e, "~") {
				exclude = append(exclude, e)
			} else {
				exclude = append(exclude, "refs/heads/"+e)
			}
		}
	}
	conditions := map[string]interface{}{
		"ref_name": map[string]interface{}{
			"include": include,
			"exclude": exclude,
		},
	}
	if len(repoIds) > 0 {
		conditions["repository_id"] = map[string]interface{}{
			"repository_ids": repoIds,
		}
	}

	rules := make([]map[string]interface{}, 0)
	for ruletype, rule := range ruleset.Rules {
		switch ruletype {
		case "required_signatures":
			rules = append(rules, map[string]interface{}{
				"type": "required_signatures",
			})
		case "creation":
			rules = append(rules, map[string]interface{}{
				"type": "creation",
			})
		case "update":
			rules = append(rules, map[string]interface{}{
				"type": "update",
			})
		case "deletion":
			rules = append(rules, map[string]interface{}{
				"type": "deletion",
			})
		case "pull_request":
			params := map[string]interface{}{
				"dismiss_stale_reviews_on_push":     rule.DismissStaleReviewsOnPush,
				"require_code_owner_review":         rule.RequireCodeOwnerReview,
				"required_approving_review_count":   rule.RequiredApprovingReviewCount,
				"required_review_thread_resolution": rule.RequiredReviewThreadResolution,
				"require_last_push_approval":        rule.RequireLastPushApproval,
			}
			if len(rule.AllowedMergeMethods) > 0 {
				mergeMethodsLower := make([]string, len(rule.AllowedMergeMethods))
				for i, m := range rule.AllowedMergeMethods {
					mergeMethodsLower[i] = strings.ToLower(m)
				}
				params["allowed_merge_methods"] = mergeMethodsLower
			}
			rules = append(rules, map[string]interface{}{
				"type":       "pull_request",
				"parameters": params,
			})
		case "merge_queue":
			params := map[string]interface{}{}
			if rule.CheckResponseTimeoutMinutes > 0 {
				params["check_response_timeout_minutes"] = rule.CheckResponseTimeoutMinutes
			}
			if rule.GroupingStrategy != "" {
				params["grouping_strategy"] = strings.ToUpper(rule.GroupingStrategy)
			}
			if rule.MaxEntriesToBuild > 0 {
				params["max_entries_to_build"] = rule.MaxEntriesToBuild
			}
			if rule.MaxEntriesToMerge > 0 {
				params["max_entries_to_merge"] = rule.MaxEntriesToMerge
			}
			if rule.MergeMethod != "" {
				params["merge_method"] = strings.ToUpper(rule.MergeMethod)
			}
			if rule.MinEntriesToMerge > 0 {
				params["min_entries_to_merge"] = rule.MinEntriesToMerge
			}
			if rule.MinEntriesToMergeWaitMinutes > 0 {
				params["min_entries_to_merge_wait_minutes"] = rule.MinEntriesToMergeWaitMinutes
			}
			rules = append(rules, map[string]interface{}{
				"type":       "merge_queue",
				"parameters": params,
			})
		case "required_status_checks":
			statusChecks := make([]map[string]interface{}, 0)
			for _, s := range rule.RequiredStatusChecks {
				statusChecks = append(statusChecks, map[string]interface{}{
					"context": s,
				})
			}
			if len(statusChecks) == 0 {
				logrus.Warnf("skipping required_status_checks rule in ruleset %q: GitHub requires at least one status check context", ruleset.Name)
				break
			}
			rules = append(rules, map[string]interface{}{
				"type": "required_status_checks",
				"parameters": map[string]interface{}{
					"required_status_checks":               statusChecks,
					"strict_required_status_checks_policy": rule.StrictRequiredStatusChecksPolicy,
				},
			})
		case "non_fast_forward":
			rules = append(rules, map[string]interface{}{
				"type": "non_fast_forward",
			})
		case "required_linear_history":
			rules = append(rules, map[string]interface{}{
				"type": "required_linear_history",
			})
		case "branch_name_pattern":
			rules = append(rules, map[string]interface{}{
				"type": "branch_name_pattern",
				"parameters": map[string]interface{}{
					"name":     rule.Name,
					"negate":   rule.Negate,
					"operator": rule.Operator,
					"pattern":  rule.Pattern,
				},
			})
		case "tag_name_pattern":
			rules = append(rules, map[string]interface{}{
				"type": "tag_name_pattern",
				"parameters": map[string]interface{}{
					"name":     rule.Name,
					"negate":   rule.Negate,
					"operator": rule.Operator,
					"pattern":  rule.Pattern,
				},
			})
		}
	}

	payload := map[string]interface{}{
		"name":          ruleset.Name,
		"target":        "branch",
		"enforcement":   ruleset.Enforcement,
		"bypass_actors": bypassActors,
		"conditions":    conditions,
		"rules":         rules,
	}
	return payload
}

func (g *GoliacRemoteImpl) AddRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, ruleset *GithubRuleSet) {
	// add ruleset
	// https://docs.github.com/en/enterprise-cloud@latest/rest/orgs/rules?apiVersion=2022-11-28#create-an-organization-repository-ruleset

	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/orgs/%s/rulesets", g.configGithubOrg),
			"",
			"POST",
			g.prepareRuleset(ruleset),
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to add ruleset to org: %v. %s", err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	g.rulesets[ruleset.Name] = ruleset
}

func (g *GoliacRemoteImpl) UpdateRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, ruleset *GithubRuleSet) {
	// update ruleset
	// https://docs.github.com/en/enterprise-cloud@latest/rest/orgs/rules?apiVersion=2022-11-28#update-an-organization-repository-ruleset

	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/orgs/%s/rulesets/%d", g.configGithubOrg, ruleset.Id),
			"",
			"PUT",
			g.prepareRuleset(ruleset),
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to update ruleset %d to org: %v. %s", ruleset.Id, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	g.rulesets[ruleset.Name] = ruleset
}

func (g *GoliacRemoteImpl) DeleteRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, rulesetid int) {
	// remove ruleset
	// https://docs.github.com/en/enterprise-cloud@latest/rest/orgs/rules?apiVersion=2022-11-28#delete-an-organization-repository-ruleset

	if !dryrun {
		_, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/orgs/%s/rulesets/%d", g.configGithubOrg, rulesetid),
			"",
			"DELETE",
			nil,
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to remove ruleset from org: %v", err))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	for _, r := range g.rulesets {
		if r.Id == rulesetid {
			delete(g.rulesets, r.Name)
			break
		}
	}
}

func (g *GoliacRemoteImpl) AddRepositoryRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, ruleset *GithubRuleSet) {
	// add repository ruleset
	// https://docs.github.com/en/rest/repos/rules?apiVersion=2022-11-28#create-a-repository-ruleset

	g.actionMutex.Lock()
	repo := g.repositories[reponame]
	if repo == nil {
		logsCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/repos/%s/%s/rulesets", g.configGithubOrg, reponame),
			"",
			"POST",
			g.prepareRuleset(ruleset),
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to add ruleset to repository %s: %v. %s", reponame, err, string(body)))
			return
		}
		type AddRepositoryRulesetResponse struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		var response AddRepositoryRulesetResponse
		err = json.Unmarshal(body, &response)
		if err == nil {
			ruleset.Id = response.ID
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()
	repo = g.repositories[reponame]

	if repo != nil {
		if repo.RuleSets == nil {
			repo.RuleSets = make(map[string]*GithubRuleSet)
		}
		repo.RuleSets[ruleset.Name] = ruleset
	}
}

func (g *GoliacRemoteImpl) UpdateRepositoryRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, ruleset *GithubRuleSet) {
	// update repository ruleset
	// https://docs.github.com/en/rest/repos/rules?apiVersion=2022-11-28#update-a-repository-ruleset
	g.actionMutex.Lock()
	repo := g.repositories[reponame]
	if repo == nil {
		logsCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	found := false
	for _, r := range repo.RuleSets {
		if r.Id == ruleset.Id {
			found = true
			break
		}
	}

	if !found {
		logsCollector.AddError(fmt.Errorf("ruleset %d not found in repository %s", ruleset.Id, reponame))
		return
	}

	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/repos/%s/%s/rulesets/%d", g.configGithubOrg, reponame, ruleset.Id),
			"",
			"PUT",
			g.prepareRuleset(ruleset),
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to update ruleset %d to repository %s: %v. %s", ruleset.Id, reponame, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	repo = g.repositories[reponame]
	if repo != nil {
		if repo.RuleSets == nil {
			repo.RuleSets = make(map[string]*GithubRuleSet)
		}
		repo.RuleSets[ruleset.Name] = ruleset
	}
}

func (g *GoliacRemoteImpl) DeleteRepositoryRuleset(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, rulesetid int) {
	// remove repository ruleset
	// https://docs.github.com/en/rest/repos/rules?apiVersion=2022-11-28#delete-a-repository-ruleset

	g.actionMutex.Lock()
	repo := g.repositories[reponame]
	if repo == nil {
		logsCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	found := false
	for _, r := range repo.RuleSets {
		if r.Id == rulesetid {
			found = true
			break
		}
	}

	if !found {
		logsCollector.AddError(fmt.Errorf("ruleset %d not found in repository %s", rulesetid, reponame))
		return
	}

	if !dryrun {
		_, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/repos/%s/%s/rulesets/%d", g.configGithubOrg, reponame, rulesetid),
			"",
			"DELETE",
			nil,
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to remove ruleset %d from repository %s: %v", rulesetid, reponame, err))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()
	repo = g.repositories[reponame]

	if repo != nil {
		for _, r := range repo.RuleSets {
			if r.Id == rulesetid {
				delete(repo.RuleSets, r.Name)
				break
			}
		}
	}
}

const createBranchProtectionRule = `
mutation createBranchProtectionRule(
	$repositoryId: ID!,
	$pattern: String!,
	$requiresApprovingReviews: Boolean!,
	$requiredApprovingReviewCount: Int!,
	$dismissesStaleReviews: Boolean!,
	$requiresCodeOwnerReviews: Boolean!,
	$requireLastPushApproval: Boolean!,
	$requiresStatusChecks: Boolean!,
	$requiresStrictStatusChecks: Boolean!,
	$requiredStatusCheckContexts: [String!],
	$requiresConversationResolution: Boolean!,
	$requiresCommitSignatures: Boolean!,
	$requiresLinearHistory: Boolean!,
	$allowsForcePushes: Boolean!,
	$allowsDeletions: Boolean!,
	$bypassPullRequestActorIds: [ID!]!) {
  createBranchProtectionRule(input: {
		repositoryId: $repositoryId,
    	pattern: $pattern,
		requiresApprovingReviews: $requiresApprovingReviews,
		requiredApprovingReviewCount: $requiredApprovingReviewCount,
		dismissesStaleReviews: $dismissesStaleReviews,
		requiresCodeOwnerReviews: $requiresCodeOwnerReviews,
		requireLastPushApproval: $requireLastPushApproval,
		requiresStatusChecks: $requiresStatusChecks,
		requiresStrictStatusChecks: $requiresStrictStatusChecks,
		requiredStatusCheckContexts: $requiredStatusCheckContexts,
		requiresConversationResolution: $requiresConversationResolution,
		requiresCommitSignatures: $requiresCommitSignatures,
		requiresLinearHistory: $requiresLinearHistory,
		allowsForcePushes: $allowsForcePushes,
		allowsDeletions: $allowsDeletions,
		bypassPullRequestActorIds: $bypassPullRequestActorIds
  }) {
    branchProtectionRule {
	  id
    }
  }
}`

// GraphqlBranchProtectionMutationError is the shared GraphQL error object for branch protection mutations.
type GraphqlBranchProtectionMutationError struct {
	Path       []interface{} `json:"path"`
	Extensions struct {
		Code         string
		ErrorMessage string
	} `json:"extensions"`
	Message string `json:"message"`
}

// GraphqlBranchProtectionRuleCreateMutationResponse is the response body for createBranchProtectionRule.
type GraphqlBranchProtectionRuleCreateMutationResponse struct {
	Data struct {
		CreateBranchProtectionRule struct {
			BranchProtectionRule struct {
				Id string `json:"id"`
			} `json:"branchProtectionRule"`
		} `json:"createBranchProtectionRule"`
	} `json:"data"`
	Errors []GraphqlBranchProtectionMutationError `json:"errors"`
}

// GraphqlBranchProtectionRuleUpdateMutationResponse is the response body for updateBranchProtectionRule.
type GraphqlBranchProtectionRuleUpdateMutationResponse struct {
	Data struct {
		UpdateBranchProtectionRule struct {
			BranchProtectionRule struct {
				Id string `json:"id"`
			} `json:"branchProtectionRule"`
		} `json:"updateBranchProtectionRule"`
	} `json:"data"`
	Errors []GraphqlBranchProtectionMutationError `json:"errors"`
}

// GraphqlBranchProtectionRuleDeleteMutationResponse is the response body for deleteBranchProtectionRule.
type GraphqlBranchProtectionRuleDeleteMutationResponse struct {
	Data struct {
		DeleteBranchProtectionRule struct {
			ClientMutationId *string `json:"clientMutationId"`
		} `json:"deleteBranchProtectionRule"`
	} `json:"data"`
	Errors []GraphqlBranchProtectionMutationError `json:"errors"`
}

func graphqlErrorResourceNotAccessibleByIntegration(msg string) bool {
	return strings.Contains(msg, "Resource not accessible by integration")
}

func branchProtectionMutationErrorsForPATRetry(v any) []GraphqlBranchProtectionMutationError {
	switch x := v.(type) {
	case *GraphqlBranchProtectionRuleCreateMutationResponse:
		return x.Errors
	case *GraphqlBranchProtectionRuleUpdateMutationResponse:
		return x.Errors
	case *GraphqlBranchProtectionRuleDeleteMutationResponse:
		return x.Errors
	default:
		return nil
	}
}

func shouldRetryBranchProtectionMutationWithPAT(res any) bool {
	errs := branchProtectionMutationErrorsForPATRetry(res)
	return len(errs) > 0 &&
		graphqlErrorResourceNotAccessibleByIntegration(errs[0].Message) &&
		config.Config.GithubPersonalAccessToken != ""
}

// queryGraphQLBranchProtectionMutationWithPATRetry runs a branch protection mutation with the installation token,
// and retries once with GOLIAC_GITHUB_PERSONAL_ACCESS_TOKEN if GitHub returns Resource not accessible by integration.
// It is a package-level generic function because Go does not allow type parameters on methods.
func queryGraphQLBranchProtectionMutationWithPATRetry[T any](g *GoliacRemoteImpl, ctx context.Context, query string, variables map[string]interface{}) ([]byte, T, error) {
	var zero T
	body, err := g.client.QueryGraphQLAPI(ctx, query, variables, nil)
	if err != nil {
		return body, zero, err
	}
	var res T
	if err = json.Unmarshal(body, &res); err != nil {
		return body, res, err
	}
	if shouldRetryBranchProtectionMutationWithPAT(&res) {
		logrus.Debugf("retrying branch protection GraphQL mutation with personal access token after integration error")
		pat := config.Config.GithubPersonalAccessToken
		body, err = g.client.QueryGraphQLAPI(ctx, query, variables, &pat)
		if err != nil {
			return body, zero, err
		}
		// Unmarshal into a fresh struct: json.Unmarshal does not clear fields missing from the second JSON,
		// so a success body without "errors" would otherwise keep the first response's Errors slice.
		res = zero
		err = json.Unmarshal(body, &res)
		if err != nil {
			return body, res, err
		}
	}
	return body, res, nil
}

func (g *GoliacRemoteImpl) AddRepositoryBranchProtection(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection) {
	// add repository branch protection
	// https://docs.github.com/en/graphql/reference/mutations#createbranchprotectionrule

	g.actionMutex.Lock()
	repo := g.repositories[reponame]
	if repo == nil {
		logsCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	bypassPullRequestActorIds := []string{}
	users := g.users
	teams := g.teams
	apps := g.appIds
	for _, actor := range branchprotection.BypassPullRequestAllowances.Nodes {
		if actor.Actor.UserLogin != "" {
			for login, u := range users {
				if login == actor.Actor.UserLogin {
					bypassPullRequestActorIds = append(bypassPullRequestActorIds, u.GraphqlId)
				}
			}
		}
		if actor.Actor.TeamSlug != "" {
			for slug, t := range teams {
				if slug == actor.Actor.TeamSlug {
					bypassPullRequestActorIds = append(bypassPullRequestActorIds, t.GraphqlId)
				}
			}
		}
		if actor.Actor.AppSlug != "" {
			for slug, a := range apps {
				if slug == actor.Actor.AppSlug {
					bypassPullRequestActorIds = append(bypassPullRequestActorIds, a.GraphqlId)
				}
			}
		}
	}

	if !dryrun {
		body, res, err := queryGraphQLBranchProtectionMutationWithPATRetry[GraphqlBranchProtectionRuleCreateMutationResponse](g,
			ctx,
			createBranchProtectionRule,
			map[string]interface{}{
				"repositoryId":                   repo.RefId,
				"pattern":                        branchprotection.Pattern,
				"requiresApprovingReviews":       branchprotection.RequiresApprovingReviews,
				"requiredApprovingReviewCount":   branchprotection.RequiredApprovingReviewCount,
				"dismissesStaleReviews":          branchprotection.DismissesStaleReviews,
				"requiresCodeOwnerReviews":       branchprotection.RequiresCodeOwnerReviews,
				"requireLastPushApproval":        branchprotection.RequireLastPushApproval,
				"requiresStatusChecks":           branchprotection.RequiresStatusChecks,
				"requiresStrictStatusChecks":     branchprotection.RequiresStrictStatusChecks,
				"requiredStatusCheckContexts":    branchprotection.RequiredStatusCheckContexts,
				"requiresConversationResolution": branchprotection.RequiresConversationResolution,
				"requiresCommitSignatures":       branchprotection.RequiresCommitSignatures,
				"requiresLinearHistory":          branchprotection.RequiresLinearHistory,
				"allowsForcePushes":              branchprotection.AllowsForcePushes,
				"allowsDeletions":                branchprotection.AllowsDeletions,
				"bypassPullRequestActorIds":      bypassPullRequestActorIds,
			},
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to add branch protection to repository %s: %v. %s", reponame, err, string(body)))
			return
		}
		if len(res.Errors) > 0 {
			logsCollector.AddError(fmt.Errorf("graphql error on AddRepositoryBranchProtection on repository %s: %v (%v)", reponame, res.Errors[0].Message, res.Errors[0].Path))
			return
		}

		branchprotection.Id = res.Data.CreateBranchProtectionRule.BranchProtectionRule.Id
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	repo = g.repositories[reponame]
	if repo != nil {
		if repo.BranchProtections == nil {
			repo.BranchProtections = make(map[string]*GithubBranchProtection)
		}
		repo.BranchProtections[branchprotection.Pattern] = branchprotection
	}
}

const updateBranchProtectionRule = `
mutation updateBranchProtectionRule(
	$branchProtectionRuleId: ID!,
	$pattern: String!,
	$requiresApprovingReviews: Boolean!,
	$requiredApprovingReviewCount: Int!,
	$dismissesStaleReviews: Boolean!,
	$requiresCodeOwnerReviews: Boolean!,
	$requireLastPushApproval: Boolean!,
	$requiresStatusChecks: Boolean!,
	$requiresStrictStatusChecks: Boolean!,
	$requiredStatusCheckContexts: [String!],
	$requiresConversationResolution: Boolean!,
	$requiresCommitSignatures: Boolean!,
	$requiresLinearHistory: Boolean!,
	$allowsForcePushes: Boolean!,
	$allowsDeletions: Boolean!,
	$bypassPullRequestActorIds: [ID!]!) {
  updateBranchProtectionRule(input: {
		branchProtectionRuleId: $branchProtectionRuleId,
    	pattern: $pattern,
		requiresApprovingReviews: $requiresApprovingReviews,
		requiredApprovingReviewCount: $requiredApprovingReviewCount,
		dismissesStaleReviews: $dismissesStaleReviews,
		requiresCodeOwnerReviews: $requiresCodeOwnerReviews,
		requireLastPushApproval: $requireLastPushApproval,
		requiresStatusChecks: $requiresStatusChecks,
		requiresStrictStatusChecks: $requiresStrictStatusChecks,
		requiredStatusCheckContexts: $requiredStatusCheckContexts,
		requiresConversationResolution: $requiresConversationResolution,
		requiresCommitSignatures: $requiresCommitSignatures,
		requiresLinearHistory: $requiresLinearHistory,
		allowsForcePushes: $allowsForcePushes,
		allowsDeletions: $allowsDeletions,
		bypassPullRequestActorIds: $bypassPullRequestActorIds
  }) {
	branchProtectionRule {
      id
    }
  }
}`

func (g *GoliacRemoteImpl) UpdateRepositoryBranchProtection(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection) {
	// update repository branch protection
	// https://docs.github.com/en/graphql/reference/mutations#updatebranchprotectionrule

	g.actionMutex.Lock()
	repo := g.repositories[reponame]
	if repo == nil {
		logsCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	bp := repo.BranchProtections[branchprotection.Pattern]
	if bp == nil {
		logsCollector.AddError(fmt.Errorf("branch protection for repository %s not found", reponame))
		return
	}

	bypassPullRequestActorIds := []string{}
	users := g.users
	teams := g.teams
	apps := g.appIds
	for _, actor := range branchprotection.BypassPullRequestAllowances.Nodes {
		if actor.Actor.UserLogin != "" {
			for login, u := range users {
				if login == actor.Actor.UserLogin {
					bypassPullRequestActorIds = append(bypassPullRequestActorIds, u.GraphqlId)
				}
			}
		}
		if actor.Actor.TeamSlug != "" {
			for slug, t := range teams {
				if slug == actor.Actor.TeamSlug {
					bypassPullRequestActorIds = append(bypassPullRequestActorIds, t.GraphqlId)
				}
			}
		}
		if actor.Actor.AppSlug != "" {
			for slug, a := range apps {
				if slug == actor.Actor.AppSlug {
					bypassPullRequestActorIds = append(bypassPullRequestActorIds, a.GraphqlId)
				}
			}
		}
	}

	if !dryrun {
		statusCheckContexts := branchprotection.RequiredStatusCheckContexts
		if statusCheckContexts == nil {
			statusCheckContexts = []string{}
		}
		body, res, err := queryGraphQLBranchProtectionMutationWithPATRetry[GraphqlBranchProtectionRuleUpdateMutationResponse](g,
			ctx,
			updateBranchProtectionRule,
			map[string]interface{}{
				"branchProtectionRuleId":         branchprotection.Id,
				"pattern":                        branchprotection.Pattern,
				"requiresApprovingReviews":       branchprotection.RequiresApprovingReviews,
				"requiredApprovingReviewCount":   branchprotection.RequiredApprovingReviewCount,
				"dismissesStaleReviews":          branchprotection.DismissesStaleReviews,
				"requiresCodeOwnerReviews":       branchprotection.RequiresCodeOwnerReviews,
				"requireLastPushApproval":        branchprotection.RequireLastPushApproval,
				"requiresStatusChecks":           branchprotection.RequiresStatusChecks,
				"requiresStrictStatusChecks":     branchprotection.RequiresStrictStatusChecks,
				"requiredStatusCheckContexts":    statusCheckContexts,
				"requiresConversationResolution": branchprotection.RequiresConversationResolution,
				"requiresCommitSignatures":       branchprotection.RequiresCommitSignatures,
				"requiresLinearHistory":          branchprotection.RequiresLinearHistory,
				"allowsForcePushes":              branchprotection.AllowsForcePushes,
				"allowsDeletions":                branchprotection.AllowsDeletions,
				"bypassPullRequestActorIds":      bypassPullRequestActorIds,
			},
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to update branch protection for repository %s: %v. %s", reponame, err, string(body)))
			return
		}
		if len(res.Errors) > 0 {
			logsCollector.AddError(fmt.Errorf("graphql error on UpdateRepositoryBranchProtection on repository %s: %v (%v)", reponame, res.Errors[0].Message, res.Errors[0].Path))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	repo = g.repositories[reponame]
	if repo != nil {
		if repo.BranchProtections == nil {
			repo.BranchProtections = make(map[string]*GithubBranchProtection)
		}
		repo.BranchProtections[branchprotection.Pattern] = branchprotection
	}
}

const deleteBranchProtectionRule = `
mutation deleteBranchProtectionRule(
	$branchProtectionRuleId: ID!) {
  deleteBranchProtectionRule(input: {
		branchProtectionRuleId: $branchProtectionRuleId
	}) {
		clientMutationId
  }
}
`

func (g *GoliacRemoteImpl) DeleteRepositoryBranchProtection(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection) {
	// remove repository branch protection
	// https://docs.github.com/en/graphql/reference/mutations#deletebranchprotectionrule

	g.actionMutex.Lock()
	repo := g.repositories[reponame]
	if repo == nil {
		logsCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	bp := repo.BranchProtections[branchprotection.Pattern]
	if bp == nil {
		logsCollector.AddError(fmt.Errorf("branch protection for repository %s not found", reponame))
		return
	}

	if !dryrun {
		body, res, err := queryGraphQLBranchProtectionMutationWithPATRetry[GraphqlBranchProtectionRuleDeleteMutationResponse](g,
			ctx,
			deleteBranchProtectionRule,
			map[string]interface{}{
				"branchProtectionRuleId": branchprotection.Id,
			},
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to delete branch protection for repository %s: %v. %s", reponame, err, string(body)))
			return
		}
		if len(res.Errors) > 0 {
			logsCollector.AddError(fmt.Errorf("graphql error on DeleteRepositoryBranchProtection on repository %s: %v (%v)", reponame, res.Errors[0].Message, res.Errors[0].Path))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	repo = g.repositories[reponame]
	if repo != nil {
		delete(repo.BranchProtections, branchprotection.Pattern)
	}
}

type SetMembershipResponse struct {
	User struct {
		NodeID string `json:"node_id"`
	} `json:"user"`
}

func (g *GoliacRemoteImpl) AddUserToOrg(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, ghuserid string) {
	// add member
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2022-11-28&versionId=free-pro-team%40latest&category=teams&subcategory=teams#set-organization-membership-for-a-user
	graphqlId := ""
	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/orgs/%s/memberships/%s", g.configGithubOrg, ghuserid),
			"",
			"PUT",
			map[string]interface{}{"role": "member"},
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to add user %s to org: %v. %s", ghuserid, err, string(body)))
			return
		}

		var membershipResponse SetMembershipResponse
		if json.Unmarshal(body, &membershipResponse) == nil {
			graphqlId = membershipResponse.User.NodeID
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	g.users[ghuserid] = &GithubUser{
		Login:     ghuserid,
		GraphqlId: graphqlId,
		Role:      "MEMBER",
	}
}

func (g *GoliacRemoteImpl) RemoveUserFromOrg(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, ghuserid string) {
	// remove member
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2022-11-28#remove-organization-membership-for-a-user
	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/orgs/%s/memberships/%s", g.configGithubOrg, ghuserid),
			"",
			"DELETE",
			nil,
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to remove user from org: %v. %s", err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	delete(g.users, ghuserid)
}

type CreateTeamResponse struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (g *GoliacRemoteImpl) CreateTeam(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, teamname string, description string, parentTeam *int, members []string) {
	slugname := slug.Make(teamname)
	teamid := 0

	g.actionMutex.Lock()
	if _, ok := g.teams[slugname]; ok {
		logsCollector.AddError(fmt.Errorf("team %s already exists", teamname))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	// create team
	// https://docs.github.com/en/rest/teams/teams?apiVersion=2022-11-28#create-a-team
	if !dryrun {
		params := map[string]interface{}{
			"name":        teamname,
			"description": description,
			"privacy":     "closed",
		}
		if parentTeam != nil {
			params["parent_team_id"] = parentTeam
		}
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/orgs/%s/teams", g.configGithubOrg),
			"",
			"POST",
			params,
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to create team: %v. %s", err, string(body)))
			return
		}
		var res CreateTeamResponse
		err = json.Unmarshal(body, &res)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to create team: %v", err))
			return
		}

		// add members
		for _, member := range members {
			// https://docs.github.com/en/rest/teams/members?apiVersion=2022-11-28#add-or-update-team-membership-for-a-user
			body, err := g.client.CallRestAPI(
				ctx,
				fmt.Sprintf("orgs/%s/teams/%s/memberships/%s", g.configGithubOrg, res.Slug, member),
				"",
				"PUT",
				map[string]interface{}{"role": "member"},
				nil,
			)
			if err != nil {
				logsCollector.AddError(fmt.Errorf("failed to add team member: %v. %s", err, string(body)))
				return
			}
		}
		slugname = res.Slug
		teamid = res.ID
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	g.teams[slugname] = &GithubTeam{
		Name:        teamname,
		Id:          teamid,
		Slug:        slugname,
		Members:     members,
		Maintainers: []string{},
	}
	g.teamSlugByName[teamname] = slugname
}

// role = member or maintainer (usually we use member)
func (g *GoliacRemoteImpl) UpdateTeamAddMember(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, teamslug string, username string, role string) {
	g.actionMutex.Lock()
	if _, ok := g.teams[teamslug]; !ok {
		logsCollector.AddError(fmt.Errorf("team %s not found", teamslug))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	if username == "" {
		logsCollector.AddError(fmt.Errorf("invalid username"))
		return
	}

	// https://docs.github.com/en/rest/teams/members?apiVersion=2022-11-28#add-or-update-team-membership-for-a-user
	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/orgs/%s/teams/%s/memberships/%s", g.configGithubOrg, teamslug, username),
			"",
			"PUT",
			map[string]interface{}{"role": role},
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to add team member %s to team %s: %v. %s", username, teamslug, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	if role == "maintainer" {
		if team, ok := g.teams[teamslug]; ok {
			// searching for maintainers
			found := false
			for _, m := range team.Maintainers {
				if m == username {
					found = true
					break
				}
			}
			if !found {
				g.teams[teamslug].Maintainers = append(g.teams[teamslug].Maintainers, username)
			}
		}
	} else {
		if team, ok := g.teams[teamslug]; ok {
			// searching for members
			found := false
			for _, m := range team.Members {
				if m == username {
					found = true
					break
				}
			}
			if !found {
				g.teams[teamslug].Members = append(g.teams[teamslug].Members, username)
			}
		}
	}
}

// role = member or maintainer (usually we use member)
func (g *GoliacRemoteImpl) UpdateTeamUpdateMember(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, teamslug string, username string, role string) {
	g.actionMutex.Lock()
	if _, ok := g.teams[teamslug]; !ok {
		logsCollector.AddError(fmt.Errorf("team %s not found", teamslug))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	if username == "" {
		logsCollector.AddError(fmt.Errorf("invalid username"))
		return
	}

	// https://docs.github.com/en/rest/teams/members?apiVersion=2022-11-28#add-or-update-team-membership-for-a-user
	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/orgs/%s/teams/%s/memberships/%s", g.configGithubOrg, teamslug, username),
			"",
			"PUT",
			map[string]interface{}{"role": role},
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to update team member %s in team %s: %v. %s", username, teamslug, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	if role == "maintainer" {
		if team, ok := g.teams[teamslug]; ok {
			// searching for maintainers
			found := false
			for _, m := range team.Maintainers {
				if m == username {
					found = true
					break
				}
			}
			if !found {
				g.teams[teamslug].Maintainers = append(g.teams[teamslug].Maintainers, username)
			}
			// searching for members
			for i, m := range team.Members {
				if m == username {
					g.teams[teamslug].Members = append(g.teams[teamslug].Members[:i], g.teams[teamslug].Members[i+1:]...)
					break
				}
			}
		}
	} else {
		if team, ok := g.teams[teamslug]; ok {
			// searching for members
			found := false
			for _, m := range team.Members {
				if m == username {
					found = true
					break
				}
			}
			if !found {
				g.teams[teamslug].Members = append(g.teams[teamslug].Members, username)
			}
			// searching for maintainers
			for i, m := range team.Maintainers {
				if m == username {
					g.teams[teamslug].Maintainers = append(g.teams[teamslug].Maintainers[:i], g.teams[teamslug].Maintainers[i+1:]...)
					break
				}
			}
		}
	}
}

func (g *GoliacRemoteImpl) UpdateTeamRemoveMember(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, teamslug string, username string) {
	g.actionMutex.Lock()
	if _, ok := g.teams[teamslug]; !ok {
		logsCollector.AddError(fmt.Errorf("team %s not found", teamslug))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	if username == "" {
		logsCollector.AddError(fmt.Errorf("invalid username"))
		return
	}
	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("orgs/%s/teams/%s/memberships/%s", g.configGithubOrg, teamslug, username),
			"",
			"DELETE",
			nil,
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to remove team member %s from team %s: %v. %s", username, teamslug, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	if team, ok := g.teams[teamslug]; ok {
		members := team.Members
		found := false
		// to be sure to remove all the members with the same username
		// we create a map with the members and then we remove the username
		membersMap := make(map[string]bool)
		for _, m := range members {
			membersMap[m] = true
		}
		if _, ok := membersMap[username]; ok {
			found = true
			delete(membersMap, username)
		}
		if found {
			// we recreate the members slice
			members = make([]string, 0, len(membersMap))
			for m := range membersMap {
				members = append(members, m)
			}
			g.teams[teamslug].Members = members
		}
	}
}

func (g *GoliacRemoteImpl) UpdateTeamSetParent(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, teamslug string, parentTeam *int) {
	// set parent's team
	// https://docs.github.com/en/rest/teams/teams?apiVersion=2022-11-28#update-a-team
	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/orgs/%s/teams/%s", g.configGithubOrg, teamslug),
			"",
			"PATCH",
			map[string]interface{}{"parent_team_id": parentTeam},
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to set parent team %s: %v. %s", teamslug, err, string(body)))
			return
		}
	}
}

func (g *GoliacRemoteImpl) DeleteTeam(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, teamslug string) {
	// delete team
	// https://docs.github.com/en/rest/teams/teams?apiVersion=2022-11-28#delete-a-team

	g.actionMutex.Lock()
	if _, ok := g.teams[teamslug]; !ok {
		logsCollector.AddError(fmt.Errorf("team %s not found", teamslug))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/orgs/%s/teams/%s", g.configGithubOrg, teamslug),
			"",
			"DELETE",
			nil,
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to delete team %s: %v. %s", teamslug, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	delete(g.teams, teamslug)
	for name, slug := range g.teamSlugByName {
		if slug == teamslug {
			delete(g.teamSlugByName, name)
		}
	}
}

type CreateRepositoryResponse struct {
	Id     int    `json:"id"`
	NodeId string `json:"node_id"`
}

/*
boolProperties are:
- archived
- allow_auto_merge
- delete_branch_on_merge
- allow_update_branch
- ...
*/
func (g *GoliacRemoteImpl) CreateRepository(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, description string, visibility string, writers []string, readers []string, boolProperties map[string]bool, defaultBranch string, githubToken *string, forkFrom string) {
	repoId := 0
	repoRefId := reponame
	if !dryrun {
		if forkFrom != "" {
			// fork repository
			// https://docs.github.com/en/rest/repos/forks?apiVersion=2022-11-28#create-a-fork

			props := map[string]interface{}{
				"organization": g.configGithubOrg,
				"name":         reponame,
			}
			body, err := g.client.CallRestAPI(
				ctx,
				fmt.Sprintf("/repos/%s/forks", forkFrom),
				"",
				"POST",
				props,
				githubToken, // if nil, we use the default Goliac token
			)
			if err != nil {
				logsCollector.AddError(fmt.Errorf("failed to fork repository %s: %v. %s", reponame, err, string(body)))
				return
			}
			// get the repo id
			var resp CreateRepositoryResponse
			err = json.Unmarshal(body, &resp)
			if err != nil {
				logsCollector.AddError(fmt.Errorf("failed to read the create repository action response for repository %s: %v", reponame, err))
				return
			}
			repoId = resp.Id
			repoRefId = resp.NodeId
		} else {
			// create repository
			// https://docs.github.com/en/rest/repos/repos?apiVersion=2022-11-28#create-an-organization-repository
			props := map[string]interface{}{
				"name":           reponame,
				"description":    description,
				"visibility":     visibility,
				"default_branch": defaultBranch,
			}
			for k, v := range boolProperties {
				props[k] = v
			}

			body, err := g.client.CallRestAPI(
				ctx,
				fmt.Sprintf("/orgs/%s/repos", g.configGithubOrg),
				"",
				"POST",
				props,
				githubToken, // if nil, we use the default Goliac token
			)
			if err != nil {
				logsCollector.AddError(fmt.Errorf("failed to create repository %s: %v. %s", reponame, err, string(body)))
				return
			}

			// get the repo id
			var resp CreateRepositoryResponse
			err = json.Unmarshal(body, &resp)
			if err != nil {
				logsCollector.AddError(fmt.Errorf("failed to read the create repository action response for repository %s: %v", reponame, err))
				return
			}
			repoId = resp.Id
			repoRefId = resp.NodeId
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// update the repositories list
	newRepo := &GithubRepository{
		Name:              reponame,
		Id:                repoId,
		RefId:             repoRefId,
		Visibility:        visibility,
		BoolProperties:    boolProperties,
		DefaultBranchName: defaultBranch,
		IsFork:            forkFrom != "",
		RuleSets:          make(map[string]*GithubRuleSet),
	}
	g.repositories[reponame] = newRepo
	g.repositoriesByRefId[repoRefId] = newRepo

	// add members
	for _, reader := range readers {
		// https://docs.github.com/en/rest/teams/teams?apiVersion=2022-11-28#add-or-update-team-repository-permissions
		if !dryrun {
			body, err := g.client.CallRestAPI(
				ctx,
				fmt.Sprintf("orgs/%s/teams/%s/repos/%s/%s", g.configGithubOrg, reader, g.configGithubOrg, reponame),
				"",
				"PUT",
				map[string]interface{}{"permission": "pull"},
				nil, // we keep the default Goliac token
			)
			if err != nil {
				logsCollector.AddError(fmt.Errorf("failed to create repository %s (and add members): %v. %s", reponame, err, string(body)))
				return
			}
		}

		teamsRepos := g.teamRepos[reader]
		if teamsRepos == nil {
			teamsRepos = make(map[string]*GithubTeamRepo)
		}
		teamsRepos[reponame] = &GithubTeamRepo{
			Name:       reponame,
			Permission: "READ",
		}
		g.teamRepos[reader] = teamsRepos
	}
	for _, writer := range writers {
		// https://docs.github.com/en/rest/teams/teams?apiVersion=2022-11-28#add-or-update-team-repository-permissions
		if !dryrun {
			body, err := g.client.CallRestAPI(
				ctx,
				fmt.Sprintf("orgs/%s/teams/%s/repos/%s/%s", g.configGithubOrg, writer, g.configGithubOrg, reponame),
				"",
				"PUT",
				map[string]interface{}{"permission": "push"},
				nil, // we keep the default Goliac token
			)
			if err != nil {
				logsCollector.AddError(fmt.Errorf("failed to create repository %s (and add members): %v. %s", reponame, err, string(body)))
				return
			}
		}

		teamsRepos := g.teamRepos[writer]
		if teamsRepos == nil {
			teamsRepos = make(map[string]*GithubTeamRepo)
		}
		teamsRepos[reponame] = &GithubTeamRepo{
			Name:       reponame,
			Permission: "WRITE",
		}
		g.teamRepos[writer] = teamsRepos
	}
}

func (g *GoliacRemoteImpl) UpdateRepositoryAddTeamAccess(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, teamslug string, permission string) {
	g.actionMutex.Lock()
	if _, ok := g.repositories[reponame]; !ok {
		logsCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	if _, ok := g.teams[teamslug]; !ok {
		logsCollector.AddError(fmt.Errorf("team %s not found", teamslug))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	// update member
	// https://docs.github.com/en/rest/teams/teams?apiVersion=2022-11-28#add-or-update-team-repository-permissions
	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/orgs/%s/teams/%s/repos/%s/%s", g.configGithubOrg, teamslug, g.configGithubOrg, reponame),
			"",
			"PUT",
			map[string]interface{}{"permission": permission},
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to add team access %s to repository %s: %v. %s", teamslug, reponame, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	teamsRepos := g.teamRepos[teamslug]
	if teamsRepos == nil {
		teamsRepos = make(map[string]*GithubTeamRepo)
	}
	rPermission := "READ"
	if permission == "push" {
		rPermission = "WRITE"
	}
	teamsRepos[reponame] = &GithubTeamRepo{
		Name:       reponame,
		Permission: rPermission,
	}
	g.teamRepos[teamslug] = teamsRepos
}

func (g *GoliacRemoteImpl) UpdateRepositoryUpdateTeamAccess(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, teamslug string, permission string) {
	g.actionMutex.Lock()
	if _, ok := g.repositories[reponame]; !ok {
		logsCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	if _, ok := g.teams[teamslug]; !ok {
		logsCollector.AddError(fmt.Errorf("team %s not found", teamslug))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	// update member
	// https://docs.github.com/en/rest/teams/teams?apiVersion=2022-11-28#add-or-update-team-repository-permissions
	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/orgs/%s/teams/%s/repos/%s/%s", g.configGithubOrg, teamslug, g.configGithubOrg, reponame),
			"",
			"PUT",
			map[string]interface{}{"permission": permission},
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to add team access %s to repository %s: %v. %s", teamslug, reponame, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	teamsRepos := g.teamRepos[teamslug]
	if teamsRepos == nil {
		teamsRepos = make(map[string]*GithubTeamRepo)
	}
	rPermission := "READ"
	if permission == "push" {
		rPermission = "WRITE"
	}
	teamsRepos[reponame] = &GithubTeamRepo{
		Name:       reponame,
		Permission: rPermission,
	}
	g.teamRepos[teamslug] = teamsRepos
}

func (g *GoliacRemoteImpl) UpdateRepositoryRemoveTeamAccess(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, teamslug string) {
	g.actionMutex.Lock()
	if _, ok := g.repositories[reponame]; !ok {
		logsCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	if _, ok := g.teams[teamslug]; !ok {
		logsCollector.AddError(fmt.Errorf("team %s not found", teamslug))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	// delete member
	// https://docs.github.com/en/rest/teams/teams?apiVersion=2022-11-28#remove-a-repository-from-a-team
	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("orgs/%s/teams/%s/repos/%s/%s", g.configGithubOrg, teamslug, g.configGithubOrg, reponame),
			"",
			"DELETE",
			nil,
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to remove team access %s from repository %s: %v. %s", teamslug, reponame, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	teamsRepos := g.teamRepos[teamslug]
	if teamsRepos != nil {
		delete(g.teamRepos[teamslug], reponame)
	}
}

/*
Used for
- visibility (string)
- allow_auto_merge (bool)
- delete_branch_on_merge (bool)
- allow_update_branch (bool)
- archived (bool)
*/
func (g *GoliacRemoteImpl) UpdateRepositoryUpdateProperties(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, properties map[string]interface{}) {
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2022-11-28#update-a-repository
	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("repos/%s/%s", g.configGithubOrg, reponame),
			"",
			"PATCH",
			properties,
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to update repository %s, %v setting: %v. %s", reponame, properties, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	if repo, ok := g.repositories[reponame]; ok {
		for propertyName, propertyValue := range properties {

			if propertyName == "visibility" {
				repo.Visibility = propertyValue.(string)
			} else if propertyName == "default_branch" {
				repo.DefaultBranchName = propertyValue.(string)
			} else if propertyName == "merge_commit_title" {
				value := propertyValue.(string)
				if value == "MERGE_MESSAGE" {
					repo.DefaultMergeCommitMessage = "Default message"
				}

			} else if propertyName == "merge_commit_message" {
				value := propertyValue.(string)
				switch value {
				case "PR_BODY":
					repo.DefaultMergeCommitMessage = "Pull request title and description"
				case "BLANK":
					repo.DefaultMergeCommitMessage = "Pull request title"
				}

			} else if propertyName == "squash_merge_commit_title" {
				value := propertyValue.(string)
				if value == "COMMIT_OR_PR_TITLE" {
					repo.DefaultSquashCommitMessage = "Default message"
				}

			} else if propertyName == "squash_merge_commit_message" {
				value := propertyValue.(string)
				switch value {
				case "PR_BODY":
					repo.DefaultSquashCommitMessage = "Pull request title and description"
				case "BLANK":
					repo.DefaultSquashCommitMessage = "Pull request title"
				case "COMMIT_MESSAGES":
					repo.DefaultSquashCommitMessage = "Pull request title and commit details"
				}

			} else {
				repo.BoolProperties[propertyName] = propertyValue.(bool)
			}
		}
	}
}

func (g *GoliacRemoteImpl) UpdateRepositoryTopics(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, topics []string) {
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2022-11-28#replace-all-repository-topics
	g.actionMutex.Lock()
	repo := g.repositories[reponame]
	if repo == nil {
		logsCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	if topics == nil {
		topics = []string{}
	}

	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/repos/%s/%s/topics", g.configGithubOrg, reponame),
			"",
			"PUT",
			map[string]interface{}{
				"names": topics,
			},
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to update topics for repository %s: %v. %s", reponame, err, string(body)))
			return
		}
	}

	// Update local cache
	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	repo = g.repositories[reponame]
	if repo != nil {
		repo.Topics = topics
	}
}

// GetRepositoryCodeowners fetches the CODEOWNERS file content and SHA from a repository.
// Returns content, sha, error. If the file doesn't exist, returns empty strings with no error.
func (g *GoliacRemoteImpl) GetRepositoryCodeowners(ctx context.Context, reponame string) (string, string, error) {
	data, err := g.client.CallRestAPI(
		ctx,
		fmt.Sprintf("/repos/%s/%s/contents/.github/CODEOWNERS", g.configGithubOrg, reponame),
		"",
		"GET",
		nil,
		nil,
	)
	if err != nil {
		// File doesn't exist - not an error, just means no CODEOWNERS
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "Not Found") {
			return "", "", nil
		}
		return "", "", err
	}

	var fileResponse struct {
		Content  string `json:"content"`
		SHA      string `json:"sha"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(data, &fileResponse); err != nil {
		return "", "", fmt.Errorf("failed to parse CODEOWNERS response for %s: %v", reponame, err)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(fileResponse.Content, "\n", ""))
	if err != nil {
		return "", "", fmt.Errorf("failed to decode CODEOWNERS content for %s: %v", reponame, err)
	}

	return string(decoded), fileResponse.SHA, nil
}

func (g *GoliacRemoteImpl) UpdateRepositoryCodeowners(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, content string, existingSHA string) {
	var newSHA string
	// https://docs.github.com/en/rest/repos/contents?apiVersion=2022-11-28#create-or-update-file-contents
	if !dryrun {
		body := map[string]interface{}{
			"message": "update CODEOWNERS (managed by Goliac)",
			"content": base64.StdEncoding.EncodeToString([]byte(content)),
		}
		if existingSHA != "" {
			body["sha"] = existingSHA
		}

		respBody, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/repos/%s/%s/contents/.github/CODEOWNERS", g.configGithubOrg, reponame),
			"",
			"PUT",
			body,
			nil,
		)
		if err != nil {
			var errResp struct {
				Message string `json:"message"`
			}
			if len(respBody) > 0 && json.Unmarshal(respBody, &errResp) == nil && errResp.Message != "" {
				logsCollector.AddError(fmt.Errorf("failed to update CODEOWNERS for repository %s: %v (message: %s)", reponame, err, errResp.Message))
			} else {
				logsCollector.AddError(fmt.Errorf("failed to update CODEOWNERS for repository %s: %v", reponame, err))
			}
			return
		}
		var putResp struct {
			Content struct {
				SHA string `json:"sha"`
			} `json:"content"`
		}
		if err := json.Unmarshal(respBody, &putResp); err == nil && putResp.Content.SHA != "" {
			newSHA = putResp.Content.SHA
		} else {
			_, sha, errFetch := g.GetRepositoryCodeowners(ctx, reponame)
			if errFetch != nil {
				logsCollector.AddError(fmt.Errorf("could not determine new CODEOWNERS SHA for repository %s after update (re-fetch: %v)", reponame, errFetch))
			} else if sha != "" {
				newSHA = sha
			} else {
				logsCollector.AddError(fmt.Errorf("CODEOWNERS SHA empty for repository %s after update; local cache may be stale", reponame))
			}
		}
	}

	// Update local cache
	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	repo := g.repositories[reponame]
	if repo != nil {
		repo.CodeownersContent = content
		if newSHA != "" {
			repo.CodeownersSHA = newSHA
		}
	}
}

func (g *GoliacRemoteImpl) UpdateRepositoryCustomProperties(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, propertyName string, propertyValue interface{}) {
	// https://docs.github.com/en/rest/repos/custom-properties?apiVersion=2022-11-28#create-or-update-custom-property-values-for-a-repository
	g.actionMutex.Lock()
	repo := g.repositories[reponame]
	if repo == nil {
		logsCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	// Prepare the request body according to GitHub API format
	// Values are already normalized from YAML (string, []string, or null)
	properties := []map[string]interface{}{
		{
			"property_name": propertyName,
			"value":         propertyValue,
		},
	}

	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/repos/%s/%s/properties/values", g.configGithubOrg, reponame),
			"",
			"PATCH",
			map[string]interface{}{
				"properties": properties,
			},
			nil,
		)
		if err != nil {
			// Check for 422 validation errors and report them clearly
			if strings.Contains(err.Error(), "422") {
				logsCollector.AddError(fmt.Errorf("validation error (422) when updating custom property %s for repository %s: %v. Response: %s. This usually means the property value is not allowed by the organization's custom property definition", propertyName, reponame, err, string(body)))
			} else {
				logsCollector.AddError(fmt.Errorf("failed to update custom property %s for repository %s: %v. %s", propertyName, reponame, err, string(body)))
			}
			return
		}
	}

	// Update local cache
	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	repo = g.repositories[reponame]
	if repo != nil {
		if repo.CustomProperties == nil {
			repo.CustomProperties = make(map[string]interface{})
		}
		if propertyValue == nil {
			delete(repo.CustomProperties, propertyName)
		} else {
			repo.CustomProperties[propertyName] = propertyValue
		}
	}
}

func (g *GoliacRemoteImpl) UpdateRepositorySetExternalUser(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, githubid string, permission string) {
	// https://docs.github.com/en/rest/collaborators/collaborators?apiVersion=2022-11-28#add-a-repository-collaborator
	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("repos/%s/%s/collaborators/%s", g.configGithubOrg, reponame, githubid),
			"",
			"PUT",
			map[string]interface{}{"permission": permission},
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to set repository %s collaborator: %v. %s", reponame, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	if repo, ok := g.repositories[reponame]; ok {
		if permission == "push" {
			repo.ExternalUsers[githubid] = "WRITE"
		} else {
			repo.ExternalUsers[githubid] = "READ"
		}
	}
}

func (g *GoliacRemoteImpl) updateRepositoryRemoveUser(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, githubid string) {
	// https://docs.github.com/en/rest/collaborators/collaborators?apiVersion=2022-11-28#remove-a-repository-collaborator
	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("repos/%s/%s/collaborators/%s", g.configGithubOrg, reponame, githubid),
			"",
			"DELETE",
			nil,
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to remove repository %s collaborator: %v. %s", reponame, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	if repo, ok := g.repositories[reponame]; ok {
		delete(repo.ExternalUsers, githubid)
	}
}

func (g *GoliacRemoteImpl) UpdateRepositoryRemoveExternalUser(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, githubid string) {
	g.updateRepositoryRemoveUser(ctx, logsCollector, dryrun, reponame, githubid)
}

func (g *GoliacRemoteImpl) UpdateRepositoryRemoveInternalUser(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, githubid string) {
	g.updateRepositoryRemoveUser(ctx, logsCollector, dryrun, reponame, githubid)
}

func (g *GoliacRemoteImpl) DeleteRepository(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string) {
	// delete repo
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2022-11-28#delete-a-repository
	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/repos/%s/%s", g.configGithubOrg, reponame),
			"",
			"DELETE",
			nil,
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to delete repository %s: %v. %s", reponame, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// update the teams repositories list
	for _, tr := range g.teamRepos {
		for rname := range tr {
			if rname == reponame {
				delete(tr, rname)
			}
		}
	}

	// update the repositories list
	if r, ok := g.repositories[reponame]; ok {
		delete(g.repositoriesByRefId, r.RefId)
		delete(g.repositories, reponame)
	}

}
func (g *GoliacRemoteImpl) RenameRepository(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, reponame string, newname string) {
	// check if the new name is already used
	if _, ok := g.repositories[newname]; ok {
		logsCollector.AddError(fmt.Errorf("failed to rename the repository %s (to %s): the new name is already used", reponame, newname))
		return
	}

	// update repository on GitHub (skipped in dry-run / plan)
	// https://docs.github.com/fr/rest/repos/repos?apiVersion=2022-11-28#update-a-repository
	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("/repos/%s/%s", g.configGithubOrg, reponame),
			"",
			"PATCH",
			map[string]interface{}{"name": newname},
			nil,
		)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to rename the repository %s (to %s): %v. %s", reponame, newname, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// Always rekey the in-memory cache (including dry-run / plan) so later steps resolve repositories by the new name.
	if r, ok := g.repositories[reponame]; ok {
		delete(g.repositoriesByRefId, r.RefId)
		delete(g.repositories, reponame)
		r.Name = newname
		g.repositories[newname] = r
		g.repositoriesByRefId[r.RefId] = r

		for _, tr := range g.teamRepos {
			for rname, teamRepo := range tr {
				if rname == reponame {
					delete(tr, rname)
					teamRepo.Name = newname
					tr[newname] = teamRepo
				}
			}
		}
	}
}

// AddRepositoryEnvironment adds a new environment to a repository
func (g *GoliacRemoteImpl) AddRepositoryEnvironment(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, environmentName string) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		logsCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Check if environment already exists
	if _, exists := repo.Environments.GetEntity()[environmentName]; exists {
		logsCollector.AddError(fmt.Errorf("environment %s already exists in repository %s", environmentName, repositoryName))
		return
	}

	if !dryrun {

		// Call GitHub API to create environment
		// https://docs.github.com/en/rest/deployments/environments?apiVersion=2022-11-28#create-or-update-an-environment
		endpoint := fmt.Sprintf("/repos/%s/%s/environments/%s", g.configGithubOrg, repositoryName, environmentName)

		_, err := g.client.CallRestAPI(ctx, endpoint, "", "PUT", nil, nil)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to create environment %s in repository %s: %v", environmentName, repositoryName, err))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// Update local cache
	repo.Environments.GetEntity()[environmentName] = &GithubEnvironment{
		Name:      environmentName,
		Variables: map[string]string{},
	}
}

// DeleteRepositoryEnvironment deletes an environment from a repository
func (g *GoliacRemoteImpl) DeleteRepositoryEnvironment(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, environmentName string) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		logsCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Check if environment exists
	if _, exists := repo.Environments.GetEntity()[environmentName]; !exists {
		logsCollector.AddError(fmt.Errorf("environment %s not found in repository %s", environmentName, repositoryName))
		return
	}

	if !dryrun {

		// Call GitHub API to delete environment
		// https://docs.github.com/en/rest/deployments/environments?apiVersion=2022-11-28#delete-an-environment
		endpoint := fmt.Sprintf("/repos/%s/%s/environments/%s", g.configGithubOrg, repositoryName, environmentName)

		_, err := g.client.CallRestAPI(ctx, endpoint, "", "DELETE", nil, nil)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to delete environment %s from repository %s: %v", environmentName, repositoryName, err))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// Update local cache
	delete(repo.Environments.GetEntity(), environmentName)
}

// AddRepositoryVariable adds a new variable to a repository
func (g *GoliacRemoteImpl) AddRepositoryVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, variableName string, variableValue string) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		logsCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Check if variable already exists
	if _, exists := repo.RepositoryVariables.GetEntity()[variableName]; exists {
		logsCollector.AddError(fmt.Errorf("variable %s already exists in repository %s", variableName, repositoryName))
		return
	}

	if !dryrun {

		// Call GitHub API to create variable
		// https://docs.github.com/en/rest/actions/variables?apiVersion=2022-11-28#create-a-repository-variable
		endpoint := fmt.Sprintf("/repos/%s/%s/actions/variables", g.configGithubOrg, repositoryName)

		body := map[string]interface{}{
			"name":  variableName,
			"value": variableValue,
		}

		_, err := g.client.CallRestAPI(ctx, endpoint, "", "POST", body, nil)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to create variable %s in repository %s: %v", variableName, repositoryName, err))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// Update local cache
	repo.RepositoryVariables.GetEntity()[variableName] = variableValue
}

// UpdateRepositoryVariable updates a variable's value in a repository
func (g *GoliacRemoteImpl) UpdateRepositoryVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, variableName string, variableValue string) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		logsCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Check if variable exists
	if _, exists := repo.RepositoryVariables.GetEntity()[variableName]; !exists {
		logsCollector.AddError(fmt.Errorf("variable %s not found in repository %s", variableName, repositoryName))
		return
	}

	if !dryrun {

		// Call GitHub API to update variable
		// https://docs.github.com/en/rest/actions/variables?apiVersion=2022-11-28#update-a-repository-variable
		endpoint := fmt.Sprintf("/repos/%s/%s/actions/variables/%s", g.configGithubOrg, repositoryName, variableName)

		body := map[string]interface{}{
			"name":  variableName,
			"value": variableValue,
		}

		_, err := g.client.CallRestAPI(ctx, endpoint, "", "PATCH", body, nil)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to update variable %s in repository %s: %v", variableName, repositoryName, err))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// Update local cache
	repo.RepositoryVariables.GetEntity()[variableName] = variableValue
}

// DeleteRepositoryVariable deletes a variable from a repository
func (g *GoliacRemoteImpl) DeleteRepositoryVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, variableName string) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		logsCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Check if variable exists
	if _, exists := repo.RepositoryVariables.GetEntity()[variableName]; !exists {
		logsCollector.AddError(fmt.Errorf("variable %s not found in repository %s", variableName, repositoryName))
		return
	}

	if !dryrun {

		// Call GitHub API to delete variable
		// https://docs.github.com/en/rest/actions/variables?apiVersion=2022-11-28#delete-a-repository-variable
		endpoint := fmt.Sprintf("/repos/%s/%s/actions/variables/%s", g.configGithubOrg, repositoryName, variableName)

		_, err := g.client.CallRestAPI(ctx, endpoint, "", "DELETE", nil, nil)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to delete variable %s from repository %s: %v", variableName, repositoryName, err))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// Update local cache
	delete(repo.RepositoryVariables.GetEntity(), variableName)
}

// AddRepositoryEnvironmentVariable adds a new variable to a repository environment
func (g *GoliacRemoteImpl) AddRepositoryEnvironmentVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, environmentName string, variableName string, variableValue string) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		logsCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Check if environment exists
	if _, exists := repo.Environments.GetEntity()[environmentName]; !exists {
		logsCollector.AddError(fmt.Errorf("environment %s not found in repository %s", environmentName, repositoryName))
		return
	}

	// Check if variable already exists
	if _, exists := repo.Environments.GetEntity()[environmentName].Variables[variableName]; exists {
		logsCollector.AddError(fmt.Errorf("variable %s already exists in environment %s of repository %s", variableName, environmentName, repositoryName))
		return
	}

	if !dryrun {

		// Call GitHub API to create variable
		// https://docs.github.com/en/rest/actions/variables?apiVersion=2022-11-28#create-an-environment-variable
		endpoint := fmt.Sprintf("/repos/%s/%s/environments/%s/variables", g.configGithubOrg, repositoryName, environmentName)

		body := map[string]interface{}{
			"name":  variableName,
			"value": variableValue,
		}

		_, err := g.client.CallRestAPI(ctx, endpoint, "", "POST", body, nil)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to create variable %s in environment %s of repository %s: %v", variableName, environmentName, repositoryName, err))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// Update local cache
	if repo.Environments.GetEntity()[environmentName] == nil {
		e := &GithubEnvironment{
			Name:      environmentName,
			Variables: map[string]string{},
		}
		repo.Environments.GetEntity()[environmentName] = e
	}
	repo.Environments.GetEntity()[environmentName].Variables[variableName] = variableValue
}

// UpdateRepositoryEnvironmentVariable updates a variable's value in a repository environment
func (g *GoliacRemoteImpl) UpdateRepositoryEnvironmentVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, environmentName string, variableName string, variableValue string) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		logsCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Check if environment exists
	if _, exists := repo.Environments.GetEntity()[environmentName]; !exists {
		logsCollector.AddError(fmt.Errorf("environment %s not found in repository %s", environmentName, repositoryName))
		return
	}

	// Check if variable exists
	if _, exists := repo.Environments.GetEntity()[environmentName].Variables[variableName]; !exists {
		logsCollector.AddError(fmt.Errorf("variable %s not found in environment %s of repository %s", variableName, environmentName, repositoryName))
		return
	}

	if !dryrun {

		// Call GitHub API to update variable
		// https://docs.github.com/en/rest/actions/variables?apiVersion=2022-11-28#update-an-environment-variable
		endpoint := fmt.Sprintf("/repos/%s/%s/environments/%s/variables/%s", g.configGithubOrg, repositoryName, environmentName, variableName)

		body := map[string]interface{}{
			"name":  variableName,
			"value": variableValue,
		}

		_, err := g.client.CallRestAPI(ctx, endpoint, "", "PATCH", body, nil)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to update variable %s in environment %s of repository %s: %v", variableName, environmentName, repositoryName, err))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// Update local cache
	if repo.Environments.GetEntity()[environmentName] == nil {
		e := &GithubEnvironment{
			Name:      environmentName,
			Variables: map[string]string{},
		}
		repo.Environments.GetEntity()[environmentName] = e
	}
	repo.Environments.GetEntity()[environmentName].Variables[variableName] = variableValue
}

// DeleteRepositoryEnvironmentVariable removes a variable from a repository environment
func (g *GoliacRemoteImpl) DeleteRepositoryEnvironmentVariable(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, environmentName string, variableName string) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		logsCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Check if environment exists
	if _, exists := repo.Environments.GetEntity()[environmentName]; !exists {
		logsCollector.AddError(fmt.Errorf("environment %s not found in repository %s", environmentName, repositoryName))
		return
	}

	// Check if variable exists
	if _, exists := repo.Environments.GetEntity()[environmentName].Variables[variableName]; !exists {
		logsCollector.AddError(fmt.Errorf("variable %s not found in environment %s of repository %s", variableName, environmentName, repositoryName))
		return
	}

	if !dryrun {

		// Call GitHub API to delete variable
		// https://docs.github.com/en/rest/actions/variables?apiVersion=2022-11-28#delete-an-environment-variable
		endpoint := fmt.Sprintf("/repos/%s/%s/environments/%s/variables/%s", g.configGithubOrg, repositoryName, environmentName, variableName)

		_, err := g.client.CallRestAPI(ctx, endpoint, "", "DELETE", nil, nil)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to remove variable %s from environment %s in repository %s: %v", variableName, environmentName, repositoryName, err))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// Update local cache
	if repo.Environments.GetEntity()[environmentName] == nil {
		e := &GithubEnvironment{
			Name:      environmentName,
			Variables: map[string]string{},
		}
		repo.Environments.GetEntity()[environmentName] = e
	}
	delete(repo.Environments.GetEntity()[environmentName].Variables, variableName)
}

func (g *GoliacRemoteImpl) AddRepositoryAutolink(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, autolink *GithubAutolink) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		logsCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Call GitHub API to create autolink
	// https://docs.github.com/en/enterprise-cloud@latest/rest/repos/autolinks?apiVersion=2022-11-28#create-an-autolink-reference-for-a-repository
	endpoint := fmt.Sprintf("/repos/%s/%s/autolinks", g.configGithubOrg, repositoryName)

	body := map[string]interface{}{
		"key_prefix":      autolink.KeyPrefix,
		"url_template":    autolink.UrlTemplate,
		"is_alphanumeric": autolink.IsAlphanumeric,
	}

	response, err := g.client.CallRestAPI(ctx, endpoint, "", "POST", body, nil)
	if err != nil {
		logsCollector.AddError(fmt.Errorf("failed to create autolink %s in repository %s: %v", autolink.KeyPrefix, repositoryName, err))
		return
	}

	var responseAutolink AutolinksResponse
	err = json.Unmarshal(response, &responseAutolink)
	if err != nil {
		logsCollector.AddError(fmt.Errorf("failed to unmarshal autolink response for repository %s: %v", repositoryName, err))
		return
	}

	// Update local cache
	repo.Autolinks.GetEntity()[autolink.KeyPrefix] = &GithubAutolink{
		Id:             responseAutolink.Id,
		KeyPrefix:      autolink.KeyPrefix,
		UrlTemplate:    autolink.UrlTemplate,
		IsAlphanumeric: autolink.IsAlphanumeric,
	}
}

func (g *GoliacRemoteImpl) DeleteRepositoryAutolink(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, autolinkId int) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		logsCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Call GitHub API to delete autolink
	// https://docs.github.com/en/enterprise-cloud@latest/rest/repos/autolinks?apiVersion=2022-11-28#delete-an-autolink-reference-from-a-repository
	endpoint := fmt.Sprintf("/repos/%s/%s/autolinks/%d", g.configGithubOrg, repositoryName, autolinkId)

	_, err := g.client.CallRestAPI(ctx, endpoint, "", "DELETE", nil, nil)
	if err != nil {
		logsCollector.AddError(fmt.Errorf("failed to delete autolink %d in repository %s: %v", autolinkId, repositoryName, err))
		return
	}

	// Update local cache
	for key, autolink := range repo.Autolinks.GetEntity() {
		if autolink.Id == autolinkId {
			delete(repo.Autolinks.GetEntity(), key)
			break
		}
	}
}

func (g *GoliacRemoteImpl) UpdateRepositoryAutolink(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, previousAutolinkId int, autolink *GithubAutolink) {
	// we need to delete and add the autolink
	if previousAutolinkId != 0 {
		g.DeleteRepositoryAutolink(ctx, logsCollector, dryrun, repositoryName, previousAutolinkId)
	}
	g.AddRepositoryAutolink(ctx, logsCollector, dryrun, repositoryName, autolink)
}

// callRepositoryGithubPagesAPI calls POST or PUT /repos/{owner}/{repo}/pages.
// When GitHub rejects public: true because private Pages is unavailable, retries once without public.
func (g *GoliacRemoteImpl) callRepositoryGithubPagesAPI(ctx context.Context, repositoryName, method string, pages *GithubPagesComparable, isPut bool) ([]byte, error) {
	var body map[string]interface{}
	if isPut {
		body = githubPagesComparableToRESTPutBody(pages)
	} else {
		body = githubPagesComparableToRESTPostBody(pages)
	}
	endpoint := fmt.Sprintf("/repos/%s/%s/pages", g.configGithubOrg, repositoryName)
	responseBody, err := g.client.CallRestAPI(ctx, endpoint, "", method, body, nil)
	if err == nil {
		return responseBody, nil
	}
	if pages.Visibility == "public" && githubPagesPrivatePagesUnavailable(responseBody) {
		if isPut {
			body = githubPagesComparableToRESTPutBodyWithPublic(pages, false)
		} else {
			body = githubPagesComparableToRESTPostBodyWithPublic(pages, false)
		}
		responseBody, retryErr := g.client.CallRestAPI(ctx, endpoint, "", method, body, nil)
		if retryErr == nil {
			return responseBody, nil
		}
		return responseBody, githubPagesAPIError(retryErr, responseBody)
	}
	return responseBody, githubPagesAPIError(err, responseBody)
}

// putRepositoryGithubPages updates GitHub Pages (PUT /repos/{owner}/{repo}/pages).
func (g *GoliacRemoteImpl) putRepositoryGithubPages(ctx context.Context, repositoryName string, pages *GithubPagesComparable) error {
	_, err := g.callRepositoryGithubPagesAPI(ctx, repositoryName, "PUT", pages, true)
	return err
}

// CreateRepositoryGithubPages enables GitHub Pages (POST /repos/{owner}/{repo}/pages).
func (g *GoliacRemoteImpl) CreateRepositoryGithubPages(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, pages *GithubPagesComparable) {
	if pages == nil {
		return
	}
	g.actionMutex.Lock()
	_, exists := g.repositories[repositoryName]
	g.actionMutex.Unlock()
	if !exists {
		logsCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}
	if !dryrun {
		_, err := g.callRepositoryGithubPagesAPI(ctx, repositoryName, "POST", pages, false)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to create GitHub Pages for repository %s: %v", repositoryName, err))
			return
		}
		if githubPagesNeedsFollowUpPutAfterCreate(pages) {
			if err := g.putRepositoryGithubPages(ctx, repositoryName, pages); err != nil {
				logsCollector.AddError(fmt.Errorf("failed to set GitHub Pages custom domain / HTTPS for repository %s after create: %v", repositoryName, err))
				return
			}
		}
		loaded, err := g.fetchRepositoryPages(ctx, repositoryName)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to refresh GitHub Pages for repository %s after create: %v", repositoryName, err))
			return
		}
		g.actionMutex.Lock()
		if r, ok := g.repositories[repositoryName]; ok {
			r.GithubPages = loaded
		}
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Lock()
	if r, ok := g.repositories[repositoryName]; ok {
		r.GithubPages = GithubPagesRemoteFromComparable(pages)
	}
	g.actionMutex.Unlock()
}

// UpdateRepositoryGithubPages updates GitHub Pages (PUT /repos/{owner}/{repo}/pages).
func (g *GoliacRemoteImpl) UpdateRepositoryGithubPages(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string, pages *GithubPagesComparable) {
	if pages == nil {
		return
	}
	g.actionMutex.Lock()
	_, exists := g.repositories[repositoryName]
	g.actionMutex.Unlock()
	if !exists {
		logsCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}
	if !dryrun {
		if err := g.putRepositoryGithubPages(ctx, repositoryName, pages); err != nil {
			logsCollector.AddError(fmt.Errorf("failed to update GitHub Pages for repository %s: %v", repositoryName, err))
			return
		}
		loaded, err := g.fetchRepositoryPages(ctx, repositoryName)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to refresh GitHub Pages for repository %s after update: %v", repositoryName, err))
			return
		}
		g.actionMutex.Lock()
		if r, ok := g.repositories[repositoryName]; ok {
			r.GithubPages = loaded
		}
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Lock()
	if r, ok := g.repositories[repositoryName]; ok {
		r.GithubPages = GithubPagesRemoteFromComparable(pages)
	}
	g.actionMutex.Unlock()
}

// DeleteRepositoryGithubPages disables GitHub Pages (DELETE /repos/{owner}/{repo}/pages).
func (g *GoliacRemoteImpl) DeleteRepositoryGithubPages(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool, repositoryName string) {
	g.actionMutex.Lock()
	_, exists := g.repositories[repositoryName]
	g.actionMutex.Unlock()
	if !exists {
		logsCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}
	if !dryrun {
		endpoint := fmt.Sprintf("/repos/%s/%s/pages", g.configGithubOrg, repositoryName)
		_, err := g.client.CallRestAPI(ctx, endpoint, "", "DELETE", nil, nil)
		if err != nil {
			logsCollector.AddError(fmt.Errorf("failed to delete GitHub Pages for repository %s: %v", repositoryName, err))
			return
		}
	}
	g.actionMutex.Lock()
	if r, ok := g.repositories[repositoryName]; ok {
		r.GithubPages = nil
	}
	g.actionMutex.Unlock()
}

func (g *GoliacRemoteImpl) Begin(logsCollector *observability.LogCollection, dryrun bool) {
}
func (g *GoliacRemoteImpl) Rollback(logsCollector *observability.LogCollection, dryrun bool, err error) {
}
func (g *GoliacRemoteImpl) Commit(ctx context.Context, logsCollector *observability.LogCollection, dryrun bool) error {
	return nil
}
