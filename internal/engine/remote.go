package engine

import (
	"context"
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

	Users(ctx context.Context) map[string]string // key is the login, value is the role (member, admin)
	TeamSlugByName(ctx context.Context) map[string]string
	Teams(ctx context.Context, current bool) map[string]*GithubTeam             // the key is the team slug
	Repositories(ctx context.Context) map[string]*GithubRepository              // the key is the repository name
	TeamRepositories(ctx context.Context) map[string]map[string]*GithubTeamRepo // key is team slug, second key is repo name
	RuleSets(ctx context.Context) map[string]*GithubRuleSet
	AppIds(ctx context.Context) map[string]int

	IsEnterprise() bool // check if we are on an Enterprise version, or if we are on GHES 3.11+

	CountAssets(ctx context.Context, warmup bool) (int, error)         // return the number of (some) assets that will be loaded (to be used with the RemoteObservability/progress bar)
	SetRemoteObservability(feedback observability.RemoteObservability) // if you want to get feedback on the loading process

	RepositoriesSecretsPerRepository(ctx context.Context, repositoryName string) (map[string]*GithubVariable, error)
	EnvironmentSecretsPerRepository(ctx context.Context, environments []string, repositoryName string) (map[string]map[string]*GithubVariable, error)
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
	BoolProperties      map[string]bool                    // archived, allow_auto_merge, delete_branch_on_merge, allow_update_branch
	ExternalUsers       map[string]string                  // [githubid]permission
	InternalUsers       map[string]string                  // [githubid]permission
	RuleSets            map[string]*GithubRuleSet          // [name]ruleset
	BranchProtections   map[string]*GithubBranchProtection // [pattern]branch protection
	DefaultBranchName   string
	IsFork              bool
	Environments        MappedEntityLazyLoader[*GithubEnvironment] // [name]environment
	RepositoryVariables MappedEntityLazyLoader[string]             // [variableName]variableValue
	// RepositorySecrets    map[string]string            // [variableName]variableValue
}

type GithubTeam struct {
	Name        string
	Id          int
	Slug        string
	Members     []string // user login, aka githubid
	Maintainers []string // user login (that are not in the Members array)
	ParentTeam  *int
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
	client                github.GitHubClient
	users                 map[string]string
	repositories          map[string]*GithubRepository
	repositoriesByRefId   map[string]*GithubRepository
	teams                 map[string]*GithubTeam
	teamRepos             map[string]map[string]*GithubTeamRepo
	teamSlugByName        map[string]string
	rulesets              map[string]*GithubRuleSet
	appIds                map[string]int
	ttlExpireUsers        time.Time
	ttlExpireRepositories time.Time
	ttlExpireTeams        time.Time
	ttlExpireTeamsRepos   time.Time
	ttlExpireRulesets     time.Time
	ttlExpireAppIds       time.Time
	isEnterprise          bool
	feedback              observability.RemoteObservability
	loadTeamsMutex        sync.Mutex
	actionMutex           sync.Mutex // used when an action (like a REST CALL to create a repository) is launched while a load is in progress
	configGithubOrg       string
	manageGithubVariables bool
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

	data, err := g.client.QueryGraphQLAPI(ctx, getAssets, variables)
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

	return totalCount, nil
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

func NewGoliacRemoteImpl(client github.GitHubClient, configGithubOrg string, manageGithubVariables bool) *GoliacRemoteImpl {
	ctx := context.Background()
	return &GoliacRemoteImpl{
		client:                client,
		users:                 make(map[string]string),
		repositories:          make(map[string]*GithubRepository),
		repositoriesByRefId:   make(map[string]*GithubRepository),
		teams:                 make(map[string]*GithubTeam),
		teamRepos:             make(map[string]map[string]*GithubTeamRepo),
		teamSlugByName:        make(map[string]string),
		rulesets:              make(map[string]*GithubRuleSet),
		appIds:                make(map[string]int),
		ttlExpireUsers:        time.Now(),
		ttlExpireRepositories: time.Now(),
		ttlExpireTeams:        time.Now(),
		ttlExpireTeamsRepos:   time.Now(),
		ttlExpireRulesets:     time.Now(),
		ttlExpireAppIds:       time.Now(),
		isEnterprise:          isEnterprise(ctx, configGithubOrg, client),
		feedback:              nil,
		configGithubOrg:       configGithubOrg,
		manageGithubVariables: manageGithubVariables,
	}
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
}

func (g *GoliacRemoteImpl) RuleSets(ctx context.Context) map[string]*GithubRuleSet {
	if time.Now().After(g.ttlExpireRulesets) {
		rulesets, err := g.loadRulesets(ctx)
		if err == nil {
			g.rulesets = rulesets
			g.ttlExpireRulesets = time.Now().Add(time.Duration(config.Config.GithubCacheTTL) * time.Second)
		}
	}
	return g.rulesets
}

func (g *GoliacRemoteImpl) AppIds(ctx context.Context) map[string]int {
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
	var rAppIds = make(map[string]int)
	for k, v := range g.appIds {
		rAppIds[k] = v
	}
	return rAppIds
}

func (g *GoliacRemoteImpl) Users(ctx context.Context) map[string]string {
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
	var rUsers = make(map[string]string)
	for k, v := range g.users {
		rUsers[k] = v
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
		repositories, repositoriesByRefIds, err := g.loadRepositories(ctx)
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
query listAllReposInOrg($orgLogin: String!, $endCursor: String) {
    organization(login: $orgLogin) {
		membersWithRole(first: 100, after: $endCursor) {
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
`

type GraplQLUsers struct {
	Data struct {
		Organization struct {
			MembersWithRole struct {
				Edges []struct {
					Node struct {
						Login string
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
func (g *GoliacRemoteImpl) loadOrgUsers(ctx context.Context) (map[string]string, error) {
	logrus.Debug("loading orgUsers")
	users := make(map[string]string)

	variables := make(map[string]interface{})
	variables["orgLogin"] = g.configGithubOrg
	variables["endCursor"] = nil

	hasNextPage := true
	count := 0
	for hasNextPage {
		data, err := g.client.QueryGraphQLAPI(ctx, listAllOrgMembers, variables)
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
			users[c.Node.Login] = c.Role
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
          deleteBranchOnMerge
          allowUpdateBranch
		  defaultBranchRef {
		    name
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
                    }
                    ... on RequiredStatusChecksParameters {
                      requiredStatusChecks {
                        context
                      }
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
}

type GraplQLRepositories struct {
	Data struct {
		Organization struct {
			Repositories struct {
				Nodes []struct {
					Name                string
					Id                  string
					DatabaseId          int
					IsArchived          bool
					IsFork              bool
					Visibility          string
					AutoMergeAllowed    bool
					DeleteBranchOnMerge bool
					AllowUpdateBranch   bool
					DefaultBranchRef    struct {
						Name string
					}
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

func (g *GoliacRemoteImpl) loadRepositories(ctx context.Context) (map[string]*GithubRepository, map[string]*GithubRepository, error) {
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
		data, err := g.client.QueryGraphQLAPI(ctx, listAllReposInOrg, variables)
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
				},
				ExternalUsers:     make(map[string]string),
				InternalUsers:     make(map[string]string),
				RuleSets:          make(map[string]*GithubRuleSet),
				BranchProtections: make(map[string]*GithubBranchProtection),
				DefaultBranchName: c.DefaultBranchRef.Name,
				IsFork:            c.IsFork,
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
	}
	return repositories, repositoriesByRefId, retErr
}

const listAllTeamsInOrg = `
query listAllTeamsInOrg($orgLogin: String!, $endCursor: String) {
    organization(login: $orgLogin) {
      teams(first: 100, after: $endCursor) {
        nodes {
          name
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

func (g *GoliacRemoteImpl) loadAppIds(ctx context.Context) (map[string]int, error) {
	logrus.Debug("loading appIds")
	appIds := map[string]int{}
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
		appIds[i.AppSlug] = i.AppId
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
				appIds[i.AppSlug] = i.AppId
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
		repositories, repositoriesByRefId, err := g.loadRepositories(ctx)
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
		rulesets, err := g.loadRulesets(ctx)
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
		data, err := g.client.QueryGraphQLAPI(ctx, listAllTeamsInOrg, variables)
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
				Name: c.Name,
				Id:   c.DatabaseId,
				Slug: c.Slug,
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
		data, err := g.client.QueryGraphQLAPI(ctx, listAllTeamMembersInOrg, variables)
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
					}
					... on RequiredStatusChecksParameters {
						requiredStatusChecks {
							context
						}
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

		// RequiredStatusChecksParameters
		RequiredStatusChecks             []GithubRuleSetRuleStatusCheck
		StrictRequiredStatusChecksPolicy bool
	}
	ID   int
	Type string // CREATION, UPDATE, DELETION, REQUIRED_LINEAR_HISTORY, REQUIRED_DEPLOYMENTS, REQUIRED_SIGNATURES, PULL_REQUEST, REQUIRED_STATUS_CHECKS, NON_FAST_FORWARD, COMMIT_MESSAGE_PATTERN, COMMIT_AUTHOR_EMAIL_PATTERN, COMMITTER_EMAIL_PATTERN, BRANCH_NAME_PATTERN, TAG_NAME_PATTERN
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
			ruleset.BypassApps[b.Actor.Name] = strings.ToLower(b.BypassMode)
		}
	}

	for _, r := range src.Rules.Nodes {
		rule := entity.RuleSetParameters{
			DismissStaleReviewsOnPush:        r.Parameters.DismissStaleReviewsOnPush,
			RequireCodeOwnerReview:           r.Parameters.RequireCodeOwnerReview,
			RequiredApprovingReviewCount:     r.Parameters.RequiredApprovingReviewCount,
			RequiredReviewThreadResolution:   r.Parameters.RequiredReviewThreadResolution,
			RequireLastPushApproval:          r.Parameters.RequireLastPushApproval,
			StrictRequiredStatusChecksPolicy: r.Parameters.StrictRequiredStatusChecksPolicy,
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

func (g *GoliacRemoteImpl) loadRulesets(ctx context.Context) (map[string]*GithubRuleSet, error) {
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
		data, err := g.client.QueryGraphQLAPI(ctx, listRulesets, variables)
		if err != nil {
			return rulesets, err
		}
		var gResult GraplQLRuleSets

		// parse first page
		err = json.Unmarshal(data, &gResult)
		if err != nil {
			return rulesets, err
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
				"actor_id":    appId,
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
			rules = append(rules, map[string]interface{}{
				"type": "pull_request",
				"parameters": map[string]interface{}{
					"dismiss_stale_reviews_on_push":     rule.DismissStaleReviewsOnPush,
					"require_code_owner_review":         rule.RequireCodeOwnerReview,
					"required_approving_review_count":   rule.RequiredApprovingReviewCount,
					"required_review_thread_resolution": rule.RequiredReviewThreadResolution,
					"require_last_push_approval":        rule.RequireLastPushApproval,
				},
			})
		case "required_status_checks":
			statusChecks := make([]map[string]interface{}, 0)
			for _, s := range rule.RequiredStatusChecks {
				statusChecks = append(statusChecks, map[string]interface{}{
					"context": s,
				})
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

func (g *GoliacRemoteImpl) AddRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ruleset *GithubRuleSet) {
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
			errorCollector.AddError(fmt.Errorf("failed to add ruleset to org: %v. %s", err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	g.rulesets[ruleset.Name] = ruleset
}

func (g *GoliacRemoteImpl) UpdateRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ruleset *GithubRuleSet) {
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
			errorCollector.AddError(fmt.Errorf("failed to update ruleset %d to org: %v. %s", ruleset.Id, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	g.rulesets[ruleset.Name] = ruleset
}

func (g *GoliacRemoteImpl) DeleteRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, rulesetid int) {
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
			errorCollector.AddError(fmt.Errorf("failed to remove ruleset from org: %v", err))
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

func (g *GoliacRemoteImpl) AddRepositoryRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, ruleset *GithubRuleSet) {
	// add repository ruleset
	// https://docs.github.com/en/rest/repos/rules?apiVersion=2022-11-28#create-a-repository-ruleset

	g.actionMutex.Lock()
	repo := g.repositories[reponame]
	if repo == nil {
		errorCollector.AddError(fmt.Errorf("repository %s not found", reponame))
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
			errorCollector.AddError(fmt.Errorf("failed to add ruleset to repository: %v. %s", err, string(body)))
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
		repo.RuleSets[ruleset.Name] = ruleset
	}
}

func (g *GoliacRemoteImpl) UpdateRepositoryRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, ruleset *GithubRuleSet) {
	// update repository ruleset
	// https://docs.github.com/en/rest/repos/rules?apiVersion=2022-11-28#update-a-repository-ruleset
	g.actionMutex.Lock()
	repo := g.repositories[reponame]
	if repo == nil {
		errorCollector.AddError(fmt.Errorf("repository %s not found", reponame))
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
		errorCollector.AddError(fmt.Errorf("ruleset %d not found", ruleset.Id))
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
			errorCollector.AddError(fmt.Errorf("failed to update ruleset %d to repository: %v. %s", ruleset.Id, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	repo = g.repositories[reponame]
	if repo != nil {
		repo.RuleSets[ruleset.Name] = ruleset
	}
}

func (g *GoliacRemoteImpl) DeleteRepositoryRuleset(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, rulesetid int) {
	// remove repository ruleset
	// https://docs.github.com/en/rest/repos/rules?apiVersion=2022-11-28#delete-a-repository-ruleset

	g.actionMutex.Lock()
	repo := g.repositories[reponame]
	if repo == nil {
		errorCollector.AddError(fmt.Errorf("repository %s not found", reponame))
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
		errorCollector.AddError(fmt.Errorf("ruleset %d not found", rulesetid))
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
			errorCollector.AddError(fmt.Errorf("failed to remove ruleset from repository: %v", err))
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
	$allowsDeletions: Boolean!) {
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
		allowsDeletions: $allowsDeletions
  }) {
    branchProtectionRule {
	  id
    }
  }
}`

type GraphqlBranchProtectionRuleCreationResponse struct {
	Data struct {
		CreateBranchProtectionRule struct {
			BranchProtectionRule struct {
				Id string
			}
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

func (g *GoliacRemoteImpl) AddRepositoryBranchProtection(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection) {
	// add repository branch protection
	// https://docs.github.com/en/graphql/reference/mutations#createbranchprotectionrule

	g.actionMutex.Lock()
	repo := g.repositories[reponame]
	if repo == nil {
		errorCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	if !dryrun {
		body, err := g.client.QueryGraphQLAPI(
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
			},
		)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("failed to add branch protection to repository %s: %v. %s", reponame, err, string(body)))
			return
		}

		var res GraphqlBranchProtectionRuleCreationResponse
		err = json.Unmarshal(body, &res)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("failed to add branch protection to repository %s: %v", reponame, err))
			return
		}
		if len(res.Errors) > 0 {
			errorCollector.AddError(fmt.Errorf("graphql error on AddRepositoryBranchProtection: %v (%v)", res.Errors[0].Message, res.Errors[0].Path))
			return
		}

		branchprotection.Id = res.Data.CreateBranchProtectionRule.BranchProtectionRule.Id
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	repo = g.repositories[reponame]
	if repo != nil {
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
	$allowsDeletions: Boolean!) {
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
		allowsDeletions: $allowsDeletions
  }) {
	branchProtectionRule {
      id
    }
  }
}`

func (g *GoliacRemoteImpl) UpdateRepositoryBranchProtection(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection) {
	// update repository branch protection
	// https://docs.github.com/en/graphql/reference/mutations#updatebranchprotectionrule

	g.actionMutex.Lock()
	repo := g.repositories[reponame]
	if repo == nil {
		errorCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	bp := repo.BranchProtections[branchprotection.Pattern]
	if bp == nil {
		errorCollector.AddError(fmt.Errorf("branch protection for repository %s not found", reponame))
		return
	}

	if !dryrun {
		body, err := g.client.QueryGraphQLAPI(
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
				"requiredStatusCheckContexts":    branchprotection.RequiredStatusCheckContexts,
				"requiresConversationResolution": branchprotection.RequiresConversationResolution,
				"requiresCommitSignatures":       branchprotection.RequiresCommitSignatures,
				"requiresLinearHistory":          branchprotection.RequiresLinearHistory,
				"allowsForcePushes":              branchprotection.AllowsForcePushes,
				"allowsDeletions":                branchprotection.AllowsDeletions,
			},
		)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("failed to update branch protection for repository %s: %v. %s", reponame, err, string(body)))
			return
		}

		var res GraphqlBranchProtectionRuleCreationResponse
		err = json.Unmarshal(body, &res)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("failed to update branch protection for repository %s: %v", reponame, err))
			return
		}
		if len(res.Errors) > 0 {
			errorCollector.AddError(fmt.Errorf("graphql error on UpdateRepositoryBranchProtection: %v (%v)", res.Errors[0].Message, res.Errors[0].Path))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	repo = g.repositories[reponame]
	if repo != nil {
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

func (g *GoliacRemoteImpl) DeleteRepositoryBranchProtection(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, branchprotection *GithubBranchProtection) {
	// remove repository branch protection
	// https://docs.github.com/en/graphql/reference/mutations#deletebranchprotectionrule

	g.actionMutex.Lock()
	repo := g.repositories[reponame]
	if repo == nil {
		errorCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	bp := repo.BranchProtections[branchprotection.Pattern]
	if bp == nil {
		errorCollector.AddError(fmt.Errorf("branch protection for repository %s not found", reponame))
		return
	}

	if !dryrun {
		body, err := g.client.QueryGraphQLAPI(
			ctx,
			deleteBranchProtectionRule,
			map[string]interface{}{
				"branchProtectionRuleId": branchprotection.Id,
			},
		)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("failed to delete branch protection for repository %s: %v. %s", reponame, err, string(body)))
			return
		}

		var res GraphqlBranchProtectionRuleCreationResponse
		err = json.Unmarshal(body, &res)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("failed to delete branch protection for repository %s: %v", reponame, err))
			return
		}
		if len(res.Errors) > 0 {
			errorCollector.AddError(fmt.Errorf("graphql error on DeleteRepositoryBranchProtection: %v (%v)", res.Errors[0].Message, res.Errors[0].Path))
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

func (g *GoliacRemoteImpl) AddUserToOrg(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ghuserid string) {
	// add member
	// https://docs.github.com/en/rest/teams/teams?apiVersion=2022-11-28#create-a-team
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
			errorCollector.AddError(fmt.Errorf("failed to add user to org: %v. %s", err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	g.users[ghuserid] = ghuserid
}

func (g *GoliacRemoteImpl) RemoveUserFromOrg(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, ghuserid string) {
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
			errorCollector.AddError(fmt.Errorf("failed to remove user from org: %v. %s", err, string(body)))
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

func (g *GoliacRemoteImpl) CreateTeam(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamname string, description string, parentTeam *int, members []string) {
	slugname := slug.Make(teamname)
	teamid := 0

	g.actionMutex.Lock()
	if _, ok := g.teams[slugname]; ok {
		errorCollector.AddError(fmt.Errorf("team %s already exists", teamname))
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
			errorCollector.AddError(fmt.Errorf("failed to create team: %v. %s", err, string(body)))
			return
		}
		var res CreateTeamResponse
		err = json.Unmarshal(body, &res)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("failed to create team: %v", err))
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
				errorCollector.AddError(fmt.Errorf("failed to add team member: %v. %s", err, string(body)))
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
func (g *GoliacRemoteImpl) UpdateTeamAddMember(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string, username string, role string) {
	g.actionMutex.Lock()
	if _, ok := g.teams[teamslug]; !ok {
		errorCollector.AddError(fmt.Errorf("team %s not found", teamslug))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	if username == "" {
		errorCollector.AddError(fmt.Errorf("invalid username"))
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
			errorCollector.AddError(fmt.Errorf("failed to add team member: %v. %s", err, string(body)))
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
func (g *GoliacRemoteImpl) UpdateTeamUpdateMember(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string, username string, role string) {
	g.actionMutex.Lock()
	if _, ok := g.teams[teamslug]; !ok {
		errorCollector.AddError(fmt.Errorf("team %s not found", teamslug))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	if username == "" {
		errorCollector.AddError(fmt.Errorf("invalid username"))
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
			errorCollector.AddError(fmt.Errorf("failed to update team member: %v. %s", err, string(body)))
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

func (g *GoliacRemoteImpl) UpdateTeamRemoveMember(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string, username string) {
	g.actionMutex.Lock()
	if _, ok := g.teams[teamslug]; !ok {
		errorCollector.AddError(fmt.Errorf("team %s not found", teamslug))
		g.actionMutex.Unlock()
		return
	}
	g.actionMutex.Unlock()

	if username == "" {
		errorCollector.AddError(fmt.Errorf("invalid username"))
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
			errorCollector.AddError(fmt.Errorf("failed to remove team member: %v. %s", err, string(body)))
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

func (g *GoliacRemoteImpl) UpdateTeamSetParent(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string, parentTeam *int) {
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
			errorCollector.AddError(fmt.Errorf("failed to set parent team: %v. %s", err, string(body)))
			return
		}
	}
}

func (g *GoliacRemoteImpl) DeleteTeam(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamslug string) {
	// delete team
	// https://docs.github.com/en/rest/teams/teams?apiVersion=2022-11-28#delete-a-team

	g.actionMutex.Lock()
	if _, ok := g.teams[teamslug]; !ok {
		errorCollector.AddError(fmt.Errorf("team %s not found", teamslug))
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
			errorCollector.AddError(fmt.Errorf("failed to delete a team: %v. %s", err, string(body)))
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
func (g *GoliacRemoteImpl) CreateRepository(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, description string, visibility string, writers []string, readers []string, boolProperties map[string]bool, defaultBranch string, githubToken *string, forkFrom string) {
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
				errorCollector.AddError(fmt.Errorf("failed to fork repository: %v. %s", err, string(body)))
				return
			}
			// get the repo id
			var resp CreateRepositoryResponse
			err = json.Unmarshal(body, &resp)
			if err != nil {
				errorCollector.AddError(fmt.Errorf("failed to read the create repository action response: %v", err))
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
				errorCollector.AddError(fmt.Errorf("failed to create repository: %v. %s", err, string(body)))
				return
			}

			// get the repo id
			var resp CreateRepositoryResponse
			err = json.Unmarshal(body, &resp)
			if err != nil {
				errorCollector.AddError(fmt.Errorf("failed to read the create repository action response: %v", err))
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
				errorCollector.AddError(fmt.Errorf("failed to create repository (and add members): %v. %s", err, string(body)))
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
				errorCollector.AddError(fmt.Errorf("failed to create repository (and add members): %v. %s", err, string(body)))
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

func (g *GoliacRemoteImpl) UpdateRepositoryAddTeamAccess(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, teamslug string, permission string) {
	g.actionMutex.Lock()
	if _, ok := g.repositories[reponame]; !ok {
		errorCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	if _, ok := g.teams[teamslug]; !ok {
		errorCollector.AddError(fmt.Errorf("team %s not found", teamslug))
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
			errorCollector.AddError(fmt.Errorf("failed to add team access: %v. %s", err, string(body)))
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

func (g *GoliacRemoteImpl) UpdateRepositoryUpdateTeamAccess(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, teamslug string, permission string) {
	g.actionMutex.Lock()
	if _, ok := g.repositories[reponame]; !ok {
		errorCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	if _, ok := g.teams[teamslug]; !ok {
		errorCollector.AddError(fmt.Errorf("team %s not found", teamslug))
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
			errorCollector.AddError(fmt.Errorf("failed to add team access: %v. %s", err, string(body)))
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

func (g *GoliacRemoteImpl) UpdateRepositoryRemoveTeamAccess(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, teamslug string) {
	g.actionMutex.Lock()
	if _, ok := g.repositories[reponame]; !ok {
		errorCollector.AddError(fmt.Errorf("repository %s not found", reponame))
		g.actionMutex.Unlock()
		return
	}
	if _, ok := g.teams[teamslug]; !ok {
		errorCollector.AddError(fmt.Errorf("team %s not found", teamslug))
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
			errorCollector.AddError(fmt.Errorf("failed to remove team access: %v. %s", err, string(body)))
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
func (g *GoliacRemoteImpl) UpdateRepositoryUpdateProperty(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, propertyName string, propertyValue interface{}) {
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2022-11-28#update-a-repository
	if !dryrun {
		body, err := g.client.CallRestAPI(
			ctx,
			fmt.Sprintf("repos/%s/%s", g.configGithubOrg, reponame),
			"",
			"PATCH",
			map[string]interface{}{propertyName: propertyValue},
			nil,
		)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("failed to update repository %s setting: %v. %s", propertyName, err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	if repo, ok := g.repositories[reponame]; ok {
		if propertyName == "visibility" {
			repo.Visibility = propertyValue.(string)
		} else if propertyName == "default_branch" {
			repo.DefaultBranchName = propertyValue.(string)
		} else {
			repo.BoolProperties[propertyName] = propertyValue.(bool)
		}
	}
}

func (g *GoliacRemoteImpl) UpdateRepositorySetExternalUser(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, githubid string, permission string) {
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
			errorCollector.AddError(fmt.Errorf("failed to set repository collaborator: %v. %s", err, string(body)))
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

func (g *GoliacRemoteImpl) updateRepositoryRemoveUser(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, githubid string) {
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
			errorCollector.AddError(fmt.Errorf("failed to remove repository collaborator: %v. %s", err, string(body)))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	if repo, ok := g.repositories[reponame]; ok {
		delete(repo.ExternalUsers, githubid)
	}
}

func (g *GoliacRemoteImpl) UpdateRepositoryRemoveExternalUser(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, githubid string) {
	g.updateRepositoryRemoveUser(ctx, errorCollector, dryrun, reponame, githubid)
}

func (g *GoliacRemoteImpl) UpdateRepositoryRemoveInternalUser(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, githubid string) {
	g.updateRepositoryRemoveUser(ctx, errorCollector, dryrun, reponame, githubid)
}

func (g *GoliacRemoteImpl) DeleteRepository(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string) {
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
			errorCollector.AddError(fmt.Errorf("failed to delete repository: %v. %s", err, string(body)))
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
func (g *GoliacRemoteImpl) RenameRepository(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, reponame string, newname string) {
	// check if the new name is already used
	if _, ok := g.repositories[newname]; ok {
		errorCollector.AddError(fmt.Errorf("failed to rename the repository %s (to %s): the new name is already used", reponame, newname))
		return
	}

	// update repository
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
			errorCollector.AddError(fmt.Errorf("failed to rename the repository %s (to %s): %v. %s", reponame, newname, err, string(body)))
			return
		}

		g.actionMutex.Lock()
		defer g.actionMutex.Unlock()

		// update the repositories list
		if r, ok := g.repositories[reponame]; ok {
			delete(g.repositoriesByRefId, r.RefId)
			delete(g.repositories, reponame)
			r.Name = newname
			g.repositories[newname] = r
			g.repositoriesByRefId[r.RefId] = r

			for _, tr := range g.teamRepos {
				for rname, r := range tr {
					if rname == reponame {
						delete(tr, rname)
						r.Name = newname
						tr[newname] = r
					}
				}
			}
		}
	}
}

// AddRepositoryEnvironment adds a new environment to a repository
func (g *GoliacRemoteImpl) AddRepositoryEnvironment(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, environmentName string) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		errorCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Check if environment already exists
	if _, exists := repo.Environments.GetEntity()[environmentName]; exists {
		errorCollector.AddError(fmt.Errorf("environment %s already exists in repository %s", environmentName, repositoryName))
		return
	}

	if !dryrun {

		// Call GitHub API to create environment
		// https://docs.github.com/en/rest/deployments/environments?apiVersion=2022-11-28#create-or-update-an-environment
		endpoint := fmt.Sprintf("/repos/%s/%s/environments/%s", g.configGithubOrg, repositoryName, environmentName)

		_, err := g.client.CallRestAPI(ctx, endpoint, "", "PUT", nil, nil)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("failed to create environment %s in repository %s: %v", environmentName, repositoryName, err))
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
func (g *GoliacRemoteImpl) DeleteRepositoryEnvironment(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, environmentName string) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		errorCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Check if environment exists
	if _, exists := repo.Environments.GetEntity()[environmentName]; !exists {
		errorCollector.AddError(fmt.Errorf("environment %s not found in repository %s", environmentName, repositoryName))
		return
	}

	if !dryrun {

		// Call GitHub API to delete environment
		// https://docs.github.com/en/rest/deployments/environments?apiVersion=2022-11-28#delete-an-environment
		endpoint := fmt.Sprintf("/repos/%s/%s/environments/%s", g.configGithubOrg, repositoryName, environmentName)

		_, err := g.client.CallRestAPI(ctx, endpoint, "", "DELETE", nil, nil)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("failed to delete environment %s from repository %s: %v", environmentName, repositoryName, err))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// Update local cache
	delete(repo.Environments.GetEntity(), environmentName)
}

// AddRepositoryVariable adds a new variable to a repository
func (g *GoliacRemoteImpl) AddRepositoryVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, variableName string, variableValue string) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		errorCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Check if variable already exists
	if _, exists := repo.RepositoryVariables.GetEntity()[variableName]; exists {
		errorCollector.AddError(fmt.Errorf("variable %s already exists in repository %s", variableName, repositoryName))
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
			errorCollector.AddError(fmt.Errorf("failed to create variable %s in repository %s: %v", variableName, repositoryName, err))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// Update local cache
	repo.RepositoryVariables.GetEntity()[variableName] = variableValue
}

// UpdateRepositoryVariable updates a variable's value in a repository
func (g *GoliacRemoteImpl) UpdateRepositoryVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, variableName string, variableValue string) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		errorCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Check if variable exists
	if _, exists := repo.RepositoryVariables.GetEntity()[variableName]; !exists {
		errorCollector.AddError(fmt.Errorf("variable %s not found in repository %s", variableName, repositoryName))
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
			errorCollector.AddError(fmt.Errorf("failed to update variable %s in repository %s: %v", variableName, repositoryName, err))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// Update local cache
	repo.RepositoryVariables.GetEntity()[variableName] = variableValue
}

// DeleteRepositoryVariable deletes a variable from a repository
func (g *GoliacRemoteImpl) DeleteRepositoryVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, variableName string) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		errorCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Check if variable exists
	if _, exists := repo.RepositoryVariables.GetEntity()[variableName]; !exists {
		errorCollector.AddError(fmt.Errorf("variable %s not found in repository %s", variableName, repositoryName))
		return
	}

	if !dryrun {

		// Call GitHub API to delete variable
		// https://docs.github.com/en/rest/actions/variables?apiVersion=2022-11-28#delete-a-repository-variable
		endpoint := fmt.Sprintf("/repos/%s/%s/actions/variables/%s", g.configGithubOrg, repositoryName, variableName)

		_, err := g.client.CallRestAPI(ctx, endpoint, "", "DELETE", nil, nil)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("failed to delete variable %s from repository %s: %v", variableName, repositoryName, err))
			return
		}
	}

	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	// Update local cache
	delete(repo.RepositoryVariables.GetEntity(), variableName)
}

// AddRepositoryEnvironmentVariable adds a new variable to a repository environment
func (g *GoliacRemoteImpl) AddRepositoryEnvironmentVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, environmentName string, variableName string, variableValue string) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		errorCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Check if environment exists
	if _, exists := repo.Environments.GetEntity()[environmentName]; !exists {
		errorCollector.AddError(fmt.Errorf("environment %s not found in repository %s", environmentName, repositoryName))
		return
	}

	// Check if variable already exists
	if _, exists := repo.Environments.GetEntity()[environmentName].Variables[variableName]; exists {
		errorCollector.AddError(fmt.Errorf("variable %s already exists in environment %s of repository %s", variableName, environmentName, repositoryName))
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
			errorCollector.AddError(fmt.Errorf("failed to create variable %s in environment %s of repository %s: %v", variableName, environmentName, repositoryName, err))
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
func (g *GoliacRemoteImpl) UpdateRepositoryEnvironmentVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, environmentName string, variableName string, variableValue string) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		errorCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Check if environment exists
	if _, exists := repo.Environments.GetEntity()[environmentName]; !exists {
		errorCollector.AddError(fmt.Errorf("environment %s not found in repository %s", environmentName, repositoryName))
		return
	}

	// Check if variable exists
	if _, exists := repo.Environments.GetEntity()[environmentName].Variables[variableName]; !exists {
		errorCollector.AddError(fmt.Errorf("variable %s not found in environment %s of repository %s", variableName, environmentName, repositoryName))
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
			errorCollector.AddError(fmt.Errorf("failed to update variable %s in environment %s of repository %s: %v", variableName, environmentName, repositoryName, err))
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
func (g *GoliacRemoteImpl) DeleteRepositoryEnvironmentVariable(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, repositoryName string, environmentName string, variableName string) {
	// Check if repository exists
	repo, exists := g.repositories[repositoryName]
	if !exists {
		errorCollector.AddError(fmt.Errorf("repository %s not found", repositoryName))
		return
	}

	// Check if environment exists
	if _, exists := repo.Environments.GetEntity()[environmentName]; !exists {
		errorCollector.AddError(fmt.Errorf("environment %s not found in repository %s", environmentName, repositoryName))
		return
	}

	// Check if variable exists
	if _, exists := repo.Environments.GetEntity()[environmentName].Variables[variableName]; !exists {
		errorCollector.AddError(fmt.Errorf("variable %s not found in environment %s of repository %s", variableName, environmentName, repositoryName))
		return
	}

	if !dryrun {

		// Call GitHub API to delete variable
		// https://docs.github.com/en/rest/actions/variables?apiVersion=2022-11-28#delete-an-environment-variable
		endpoint := fmt.Sprintf("/repos/%s/%s/environments/%s/variables/%s", g.configGithubOrg, repositoryName, environmentName, variableName)

		_, err := g.client.CallRestAPI(ctx, endpoint, "", "DELETE", nil, nil)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("failed to remove variable %s from environment %s in repository %s: %v", variableName, environmentName, repositoryName, err))
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

func (g *GoliacRemoteImpl) Begin(dryrun bool) {
}
func (g *GoliacRemoteImpl) Rollback(dryrun bool, err error) {
}
func (g *GoliacRemoteImpl) Commit(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool) error {
	return nil
}
