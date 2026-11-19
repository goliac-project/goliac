package workflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"

	"github.com/goliac-project/goliac/internal/engine"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/gosimple/slug"
	"gopkg.in/yaml.v3"
)

// GetPlugins returns a map of all the plugins
func GetPlugins() map[string]StepPlugin {
	return map[string]StepPlugin{
		"jira_ticket_creation": NewStepPluginJira(),
		"slack_notification":   NewStepPluginSlack(),
		"dynamodb":             NewStepPluginDynamoDB(),
	}
}

// strip down version of GithubClient
type WorkflowGithubClient interface {
	CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error)
}

// strip down version of Goliac Local
type WorkflowLocalResource interface {
	Workflows() map[string]*entity.Workflow
	Teams() map[string]*entity.Team
	Users() map[string]*entity.User // github username, user definition
	// RepositoryInWorkflow is true if the repository matches at least one loaded workflow's repository scope.
	RepositoryInWorkflow(repository string) bool
}

// strip down version of Goliac Remote (if we have an externally managed team)
type WorkflowRemoteResource interface {
	// return the teams from github (with the slugified name as)
	Teams(ctx context.Context, current bool) map[string]*engine.GithubTeam
}

type StepPlugin interface {
	Execute(ctx context.Context, username, workflowDescription, explanation string, url *url.URL, properties map[string]interface{}) (string, error)
}

type Workflow interface {
	ExecuteWorkflow(ctx context.Context, repoconfigForceMergeworkflows []string, username, workflowName, explanation string, properties map[string]string, dryrun bool) ([]string, error)
}

// WorkflowService is here to select the right workflow
// and check the ACL
type WorkflowService interface {
	GetWorkflow(ctx context.Context, repoconfigForceMergeworkflows []string, workflowName, repo, githubId string) (*entity.Workflow, error)
	CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error)
}

type WorkflowServiceImpl struct {
	organization string
	local        WorkflowLocalResource
	remote       WorkflowRemoteResource
	ghclient     WorkflowGithubClient
}

func NewWorkflowService(organization string, local WorkflowLocalResource, remote WorkflowRemoteResource, ghclient WorkflowGithubClient) WorkflowService {
	return &WorkflowServiceImpl{
		organization: organization,
		local:        local,
		remote:       remote,
		ghclient:     ghclient,
	}
}

// AppSlug is the slug of the GitHub App Goliac runs as ("" when the client does not know it)
func (ws *WorkflowServiceImpl) AppSlug() string {
	if s, ok := ws.ghclient.(interface{ GetAppSlug() string }); ok {
		return s.GetAppSlug()
	}
	return ""
}

func (ws *WorkflowServiceImpl) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	return ws.ghclient.CallRestAPI(ctx, endpoint, parameters, method, body, githubToken)
}

func (ws *WorkflowServiceImpl) GetWorkflow(ctx context.Context, repoconfigForceMergeworkflows []string, workflowName, repo, githubId string) (*entity.Workflow, error) {
	// check if the workflow is enabled
	if repoconfigForceMergeworkflows == nil {
		return nil, fmt.Errorf("workflows not found")
	}
	found := false
	for _, w := range repoconfigForceMergeworkflows {
		if w == workflowName {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("workflow not found")
	}

	// load the workflow
	forcemergeworkflows := ws.local.Workflows()
	if forcemergeworkflows == nil {
		return nil, fmt.Errorf("workflows not found")
	}

	w, ok := forcemergeworkflows[workflowName]
	if !ok {
		return nil, fmt.Errorf("workflows not found")
	}

	// fmt.Println("githubId", githubId)
	// fmt.Println("users", local.Users())

	// get the username
	username := ""
	if githubId != "" {
		users := ws.local.Users()
		for name, user := range users {
			if user.Spec.GithubID == githubId {
				username = name
				break
			}
		}
	}
	// fmt.Println("username", username)

	// check workflow acl
	teams := []string{}
	rTeams := ws.remote.Teams(ctx, true)

	for _, lTeam := range ws.local.Teams() {
		if lTeam.Spec.ExternallyManaged {
			// get team definition from github
			rTeam := rTeams[slug.Make(lTeam.Name)]
			if rTeam == nil {
				continue
			}
			for _, owner := range rTeam.Maintainers {
				if owner == githubId {
					teams = append(teams, lTeam.Name)
				}
			}
			for _, member := range rTeam.Members {
				if member == githubId {
					teams = append(teams, lTeam.Name)
				}
			}
		} else {
			// get team definition from local
			for _, owner := range lTeam.Spec.Owners {
				if owner == username {
					teams = append(teams, lTeam.Name)
				}
			}
			for _, member := range lTeam.Spec.Members {
				if member == username {
					teams = append(teams, lTeam.Name)
				}
			}
		}
	}

	// check the ACL
	pass, err := ws.passAcl(w, username, teams, repo)
	if err != nil {
		return nil, err
	}
	if !pass {
		return nil, fmt.Errorf("access denied")
	}
	return w, nil
}

func (ws *WorkflowServiceImpl) passAcl(w *entity.Workflow, username string, usernameTeams []string, repository string) (bool, error) {
	repoMatch, err := w.AppliesToRepository(repository)
	if err != nil {
		return false, err
	}
	if !repoMatch {
		return false, nil
	}

	// checking if (one of) the team is allowed

	teamsOwned := make(map[string]bool)
	for _, team := range usernameTeams {
		teamsOwned[team] = false
	}

	if len(w.Spec.Acls.Allowed) > 0 {
		for _, allowed := range w.Spec.Acls.Allowed {
			if allowed == "~ALL" {
				for _, team := range usernameTeams {
					teamsOwned[team] = true
				}
			}
			if allowed == "~GOLIAC_REPOSITORY_APPROVERS" {
				allowedTeams, err := ws.getRepositoryApprovers(repository, username, usernameTeams)
				if err != nil {
					return false, err
				}
				for _, t := range allowedTeams {
					teamsOwned[t] = true
				}
			}
			for _, team := range usernameTeams {
				teamRegex, err := regexp.Match(fmt.Sprintf("^%s$", allowed), []byte(team))
				if err != nil {
					return false, err
				}
				if teamRegex {
					teamsOwned[team] = true
				}
			}
		}
	} else {
		for _, team := range usernameTeams {
			teamsOwned[team] = true
		}
	}

	if len(w.Spec.Acls.Except) > 0 {
		for _, except := range w.Spec.Acls.Except {
			for _, team := range usernameTeams {
				teamRegex, err := regexp.Match(fmt.Sprintf("^%s$", except), []byte(team))
				if err != nil {
					return false, err
				}
				if teamRegex {
					teamsOwned[team] = false
				}
			}
		}
	}

	for _, v := range teamsOwned {
		if v {
			return true, nil
		}
	}

	return false, nil
}

type RepositoryGoliacApprovers struct {
	Teams []string
	Users []string
}

// getRepositoryApprovers gets the repository approvers from the
// '.goliac/forcemerge.approvers' file in the repository
func (ws *WorkflowServiceImpl) getRepositoryApprovers(repository, username string, teams []string) ([]string, error) {
	// Create a context for the API call
	ctx := context.Background()

	// https://docs.github.com/en/rest/repos/contents?apiVersion=2022-11-28#get-repository-content
	// Construct the endpoint to get the file content using GitHub REST API
	// Format: /repos/{owner}/{repo}/contents/{path}
	endpoint := fmt.Sprintf("/repos/%s/%s/contents/.goliac/forcemerge.approvers", ws.organization, repository)

	// Call the GitHub REST API to get the file content
	responseBytes, err := ws.ghclient.CallRestAPI(ctx, endpoint, "", "GET", nil, nil)
	if err != nil {
		// If the file doesn't exist, return empty teams list
		if err.Error() == "unexpected status: 404 Not Found" {
			return []string{}, nil
		}
		return []string{}, err
	}

	// Parse the GitHub API response
	var fileResponse struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}

	if err := json.Unmarshal(responseBytes, &fileResponse); err != nil {
		// If there's an error parsing the response, return empty teams list
		return []string{}, fmt.Errorf("error fetching .goliac/forcemerge.approvers: %v", err)
	}

	// GitHub returns content as base64 encoded
	decodedContent, err := base64.StdEncoding.DecodeString(fileResponse.Content)
	if err != nil {
		return []string{}, fmt.Errorf("error fetching base64 .goliac/forcemerge.approvers: %v", err)
	}

	// Parse the file content to extract teams and users
	var approvers RepositoryGoliacApprovers
	if err := yaml.Unmarshal(decodedContent, &approvers); err != nil {
		// If there's an error parsing the JSON, return empty teams list
		return []string{}, fmt.Errorf("error fetching .goliac/forcemerge.approvers: %v", err)
	}

	var allowedTeams []string
	// Check if the user is in the approvers list
	for _, user := range approvers.Users {
		if user == username {
			// If the user is directly listed as an approver, add a special team
			allowedTeams = append(allowedTeams, "GOLIAC_REPOSITORY_APPROVERS_INDIVIDUALS")
		}
	}

	// Return the intersection of user's teams and approver teams
	for _, team := range teams {
		for _, approverTeam := range approvers.Teams {
			if team == approverTeam {
				allowedTeams = append(allowedTeams, team)
			}
		}
	}

	return allowedTeams, nil
}
