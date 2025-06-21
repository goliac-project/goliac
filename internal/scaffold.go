package internal

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/goliac-project/goliac/internal/config"
	"github.com/goliac-project/goliac/internal/engine"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/goliac-project/goliac/internal/github"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/goliac-project/goliac/internal/utils"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

type LoadGithubSamlUsers func(observability.RemoteObservability) (map[string]*entity.User, error)

type Scaffold struct {
	remote                     engine.GoliacRemote
	loadUsersFromGithubOrgSaml LoadGithubSamlUsers
	feedback                   observability.RemoteObservability
	githubappname              string
}

func NewScaffold() (*Scaffold, error) {
	githubClient, err := github.NewGitHubClientImpl(
		config.Config.GithubServer,
		config.Config.GithubAppOrganization,
		config.Config.GithubAppID,
		config.Config.GithubAppPrivateKeyFile,
		config.Config.GithubPersonalAccessToken,
	)

	if err != nil {
		return nil, err
	}

	remote := engine.NewGoliacRemoteImpl(githubClient, config.Config.GithubAppOrganization, config.Config.ManageGithubActionsVariables, config.Config.ManageGithubAutolinks)

	loadUsersFromGithubOrgSaml := func(feedback observability.RemoteObservability) (map[string]*entity.User, error) {
		ctx := context.Background()
		return engine.LoadUsersFromGithubOrgSaml(ctx, githubClient, feedback)
	}

	return &Scaffold{
		remote:                     remote,
		loadUsersFromGithubOrgSaml: loadUsersFromGithubOrgSaml,
		feedback:                   nil,
		githubappname:              githubClient.GetAppSlug(),
	}, nil
}

func (s *Scaffold) SetRemoteObservability(feedback observability.RemoteObservability) error {
	s.remote.SetRemoteObservability(feedback)
	s.feedback = feedback
	if feedback != nil {
		nb, err := s.remote.CountAssets(context.Background(), true)
		if err != nil {
			return fmt.Errorf("error when counting assets: %v", err)
		}
		feedback.Init(nb)
	}
	return nil
}

/*
 * Generate will generate a full teams directory structure compatible with Goliac
 */
func (s *Scaffold) Generate(ctx context.Context, rootpath string, adminteam string, usersOnly bool) error {
	if _, err := os.Stat(rootpath); os.IsNotExist(err) {
		// Create the directory if it does not exist
		err := os.MkdirAll(rootpath, 0755)
		if err != nil {
			return fmt.Errorf("error creating directory: %v", err)
		}
	}
	fs := osfs.New(rootpath)

	if err := s.remote.Load(ctx, true); err != nil {
		logrus.Warnf("Not able to load all information from Github: %v, but I will try to continue", err)
	}

	return s.generate(ctx, fs, adminteam, usersOnly)
}

func (s *Scaffold) generate(ctx context.Context, fs billy.Filesystem, adminteam string, usersOnly bool) error {
	utils.RemoveAll(fs, "users")
	utils.RemoveAll(fs, "teams")
	utils.RemoveAll(fs, "rulesets")
	utils.RemoveAll(fs, "archived")
	utils.RemoveAll(fs, "workflows")

	fs.MkdirAll("archived", 0755)
	fs.MkdirAll("rulesets", 0755)
	fs.MkdirAll("teams", 0755)
	fs.MkdirAll("workflows", 0755)

	usermap, err := s.generateUsers(ctx, fs, "users")
	if err != nil {
		return fmt.Errorf("error creaing the users directory: %v", err)
	}

	err = s.generateTeams(ctx, fs, "teams", "archived", usermap, adminteam, usersOnly)
	if err != nil {
		return fmt.Errorf("error creating the teams directory: %v", err)
	}

	if err := s.generateRuleset(fs, "rulesets"); err != nil {
		return fmt.Errorf("error creating the rulesets directory: %v", err)
	}

	if err := s.generateGoliacConf(fs, ".", adminteam); err != nil {
		return fmt.Errorf("error creating the goliac.yaml file: %v", err)
	}

	if err := s.generateGithubAction(fs, "."); err != nil {
		return fmt.Errorf("error creating the .github/workflows/pr.yaml file: %v", err)
	}

	if err := s.generateReadme(fs, "."); err != nil {
		return fmt.Errorf("error creating the README.md file: %v", err)
	}

	if err := s.generateWorkflowStandard(fs, ".", adminteam); err != nil {
		return fmt.Errorf("error creating the workflows/standard.yaml file: %v", err)
	}

	return nil
}

func (s *Scaffold) generateWorkflowStandard(fs billy.Filesystem, rootpath string, adminTeam string) error {

	workflow := fmt.Sprintf(`#apiVersion: v1
#kind: Workflow
#name: standard
#spec:
#  description: Geneal breaking glass PR merge workflow
#  workflow_type: forcemerge
#  repositories:
#    allowed:
#      - ~ALL
#  acls:
#    allowed:
#      - %s
#  steps:
#    - name: jira_ticket_creation
#      properties:
#        project_key: SRE
`, adminTeam)
	writeFile(filepath.Join(rootpath, "workflows", "standard.yaml"), []byte(workflow), fs)
	return nil
}

func (s *Scaffold) generateTeams(ctx context.Context, fs billy.Filesystem, teamspath string, archivepath string, usermap map[string]string, adminteam string, usersOnly bool) error {
	teamsRepositories := s.remote.TeamRepositories(ctx)
	teams := s.remote.Teams(ctx, false)
	teamsSlugByName := s.remote.TeamSlugByName(ctx)

	teamsNameBySlug := make(map[string]string)
	for k, v := range teamsSlugByName {
		teamsNameBySlug[v] = k
	}

	teamIds := make(map[int]*engine.GithubTeam)
	for _, t := range teams {
		teamIds[t.Id] = t
	}

	// to ensure only one owner
	repoAdmin := make(map[string]string)
	teamsRepos := make(map[string][]string)
	// to get all teams access per repo
	repoWrite := make(map[string][]string)
	repoRead := make(map[string][]string)

	// let's create the goliac admin team first
	admins := []string{}
	for githubid, role := range s.remote.Users(ctx) {
		if role == "ADMIN" {
			admins = append(admins, githubid)
		}
	}
	teams[adminteam] = &engine.GithubTeam{
		Name:    adminteam,
		Slug:    adminteam,
		Members: admins,
	}
	teamsSlugByName[adminteam] = adminteam
	teamsNameBySlug[adminteam] = adminteam

	// searching for ADMIN first
	for team, tr := range teamsRepositories {
		for reponame, repo := range tr {
			if repo.Permission == "ADMIN" {
				// if there is no admin attached yet to this repo
				if _, ok := repoAdmin[reponame]; !ok {
					repoAdmin[reponame] = team
					teamsRepos[team] = append(teamsRepos[team], reponame)
				}
				repoWrite[reponame] = append(repoWrite[reponame], teamsNameBySlug[team])
			}
		}
	}
	// searching for WRITE second
	for team, tr := range teamsRepositories {
		for reponame, repo := range tr {
			if repo.Permission == "WRITE" {
				// if there is no admin attached yet to this repo
				if _, ok := repoAdmin[reponame]; !ok {
					repoAdmin[reponame] = team
					teamsRepos[team] = append(teamsRepos[team], reponame)
				}
				repoWrite[reponame] = append(repoWrite[reponame], teamsNameBySlug[team])
			}
			if repo.Permission != "ADMIN" && repo.Permission != "WRITE" {
				repoRead[reponame] = append(repoRead[reponame], teamsNameBySlug[team])
			}
		}
	}

	// warmup
	repos := s.remote.Repositories(ctx)
	jobs := make(chan *engine.GithubRepository, len(repos))
	results := make(chan error, len(repos))

	// Start workers
	for w := int64(0); w < config.Config.GithubConcurrentThreads; w++ {
		go func() {
			for r := range jobs {
				r.Environments.GetEntity()
				if s.feedback != nil {
					s.feedback.LoadingAsset(fmt.Sprintf("environments for %s", r.Name), 1)
				}
				r.RepositoryVariables.GetEntity()
				if s.feedback != nil {
					s.feedback.LoadingAsset(fmt.Sprintf("variables for %s", r.Name), 1)
				}
				results <- nil
			}
		}()
	}

	// Send jobs
	for _, r := range repos {
		jobs <- r
	}
	close(jobs)

	// Wait for all jobs to complete
	for i := 0; i < len(repos); i++ {
		<-results
	}

	// end of warmup

	countOrphaned := 0
	// orphan repos should go to the admin team
	for repo := range s.remote.Repositories(ctx) {
		if _, ok := repoAdmin[repo]; !ok {
			logrus.Debugf("repo %s is orphaned, attaching it to the admin (%s) team", repo, adminteam)
			repoAdmin[repo] = adminteam
			teamsRepos[adminteam] = append(teamsRepos[adminteam], repo)
			countOrphaned++
		}
	}
	logrus.Infof("%d orphaned repositories have been added to the admin %s team", countOrphaned, adminteam)

	for team, repos := range teamsRepos {
		// write the team dir
		if t := teams[team]; t != nil {
			if strings.HasSuffix(team, config.Config.GoliacTeamOwnerSuffix) {
				continue
			}
			// if usersOnly
			if usersOnly && team != adminteam {
				continue
			}
			lTeam := entity.Team{}
			lTeam.ApiVersion = "v1"
			lTeam.Kind = "Team"
			lTeam.Name = t.Name

			// if we have 1 or more maintainers in the Github team
			// we will use them as owners
			if len(t.Maintainers) >= 1 {
				for _, m := range t.Maintainers {
					// put the right user name instead of the github id
					lTeam.Spec.Owners = append(lTeam.Spec.Owners, usermap[m])
				}
				for _, m := range t.Members {
					// put the right user name instead of the github id
					lTeam.Spec.Members = append(lTeam.Spec.Members, usermap[m])
				}
			} else {
				for _, m := range t.Maintainers {
					// put the right user name instead of the github id
					lTeam.Spec.Owners = append(lTeam.Spec.Owners, usermap[m])
				}
				// else we put everyone as owners
				for _, m := range t.Members {
					// put the right user name instead of the github id
					lTeam.Spec.Owners = append(lTeam.Spec.Owners, usermap[m])
				}
			}

			teamPath, err := buildTeamPath(teamIds, teams[team])
			if err != nil {
				logrus.Errorf("unable to compute team's path: %v (for team %s)", err, team)
				continue
			}
			fs.MkdirAll(filepath.Join(teamspath, teamPath), 0755)
			if err := writeYamlFile(filepath.Join(teamspath, teamPath, "team.yaml"), &lTeam, fs); err != nil {
				logrus.Errorf("not able to write team file %s in %s: %v", team, teamPath, err)
			}

			// if usersOnly
			if usersOnly {
				continue
			}

			// write repos
			rRepos := s.remote.Repositories(ctx)
			for _, r := range repos {
				lRepo := entity.Repository{}
				lRepo.ApiVersion = "v1"
				lRepo.Kind = "Repository"
				lRepo.Name = r
				lRepo.Spec.Writers = repoWrite[r]
				lRepo.Spec.Readers = repoRead[r]

				if rRepo, ok := rRepos[r]; ok {
					// basic repository properties
					lRepo.Spec.Visibility = rRepo.Visibility
					lRepo.Spec.AllowAutoMerge = rRepo.BoolProperties["allow_auto_merge"]
					lRepo.Spec.DeleteBranchOnMerge = rRepo.BoolProperties["delete_branch_on_merge"]
					lRepo.Spec.AllowUpdateBranch = rRepo.BoolProperties["allow_update_branch"]
					if rRepo.DefaultBranchName != "main" {
						lRepo.Spec.DefaultBranchName = rRepo.DefaultBranchName
					}
					lRepo.Archived = rRepo.BoolProperties["archived"]
					if lRepo.Archived {
						lRepo.Spec.Writers = append(lRepo.Spec.Writers, teams[team].Name)
					}

					// scaffoldling repository rulesets
					rRulesets := rRepo.RuleSets
					if rRulesets != nil {
						lRepo.Spec.Rulesets = make([]entity.RepositoryRuleSet, 0, len(rRulesets))

						for rRulesetname, rRuleset := range rRulesets {
							lRuleset := entity.RepositoryRuleSet{
								Name: rRulesetname,
							}
							lRuleset.Enforcement = rRuleset.Enforcement
							for appname, mode := range rRuleset.BypassApps {
								lRuleset.BypassApps = append(lRuleset.BypassApps, struct {
									AppName string
									Mode    string
								}{
									AppName: appname,
									Mode:    mode,
								})
							}
							for teamslug, mode := range rRuleset.BypassTeams {
								teamname := teamsNameBySlug[teamslug]
								lRuleset.BypassTeams = append(lRuleset.BypassTeams, struct {
									TeamName string
									Mode     string
								}{
									TeamName: teamname,
									Mode:     mode,
								})
							}
							lRuleset.Conditions.Include = rRuleset.OnInclude
							lRuleset.Conditions.Exclude = rRuleset.OnExclude
							for rulename, rulespec := range rRuleset.Rules {
								lRuleset.Rules = append(lRuleset.Rules, struct {
									Ruletype   string
									Parameters entity.RuleSetParameters `yaml:"parameters,omitempty"`
								}{
									Ruletype:   rulename,
									Parameters: rulespec,
								})
							}

							lRepo.Spec.Rulesets = append(lRepo.Spec.Rulesets, lRuleset)
						}
					}

					// scaffoldling repository branch protections
					rBranchprotection := rRepo.BranchProtections
					if rBranchprotection != nil {
						lRepo.Spec.BranchProtections = make([]entity.RepositoryBranchProtection, 0, len(rBranchprotection))

						for rBranchprotectionPattern, rBranchprotection := range rBranchprotection {
							lbranchprotection := entity.RepositoryBranchProtection{
								Pattern: rBranchprotectionPattern,
							}
							lbranchprotection.RequiresApprovingReviews = rBranchprotection.RequiresApprovingReviews
							lbranchprotection.RequiredApprovingReviewCount = rBranchprotection.RequiredApprovingReviewCount
							lbranchprotection.DismissesStaleReviews = rBranchprotection.DismissesStaleReviews
							lbranchprotection.RequiresCodeOwnerReviews = rBranchprotection.RequiresCodeOwnerReviews
							lbranchprotection.RequireLastPushApproval = rBranchprotection.RequireLastPushApproval
							lbranchprotection.RequiresStatusChecks = rBranchprotection.RequiresStatusChecks
							lbranchprotection.RequiresStrictStatusChecks = rBranchprotection.RequiresStrictStatusChecks
							lbranchprotection.RequiredStatusCheckContexts = rBranchprotection.RequiredStatusCheckContexts
							lbranchprotection.RequiresConversationResolution = rBranchprotection.RequiresConversationResolution
							lbranchprotection.RequiresCommitSignatures = rBranchprotection.RequiresCommitSignatures
							lbranchprotection.RequiresLinearHistory = rBranchprotection.RequiresLinearHistory
							lbranchprotection.AllowsForcePushes = rBranchprotection.AllowsForcePushes
							lbranchprotection.AllowsDeletions = rBranchprotection.AllowsDeletions

							lRepo.Spec.BranchProtections = append(lRepo.Spec.BranchProtections, lbranchprotection)
						}
					}

					// scaffoldling repository environments, env variables and variables
					rEnvironments := rRepo.Environments
					if rEnvironments != nil {
						lRepo.Spec.Environments = make([]entity.RepositoryEnvironment, 0, len(rEnvironments.GetEntity()))

						for _, e := range rEnvironments.GetEntity() {
							re := entity.RepositoryEnvironment{
								Name:      e.Name,
								Variables: make(map[string]string),
							}
							for k, v := range e.Variables {
								re.Variables[k] = v
							}
							lRepo.Spec.Environments = append(lRepo.Spec.Environments, re)
						}
					}

					lRepo.Spec.ActionsVariables = make(map[string]string)
					if rRepo.RepositoryVariables != nil {
						for n, v := range rRepo.RepositoryVariables.GetEntity() {
							lRepo.Spec.ActionsVariables[n] = v
						}
					}

					rAutolinks := rRepo.Autolinks
					if rAutolinks != nil && len(rAutolinks.GetEntity()) != 0 {
						autolinksPtr := make([]entity.RepositoryAutolink, 0, len(rAutolinks.GetEntity()))
						lRepo.Spec.Autolinks = &autolinksPtr
						for _, e := range rAutolinks.GetEntity() {
							ra := entity.RepositoryAutolink{
								KeyPrefix:      e.KeyPrefix,
								UrlTemplate:    e.UrlTemplate,
								IsAlphanumeric: e.IsAlphanumeric,
							}
							*lRepo.Spec.Autolinks = append(*lRepo.Spec.Autolinks, ra)
						}
					}
				}

				// removing team name from writer
				for i, t := range lRepo.Spec.Writers {
					if teamsSlugByName[t] == team {
						lRepo.Spec.Writers = append(lRepo.Spec.Writers[:i], lRepo.Spec.Writers[i+1:]...)
						break
					}
				}
				// removing team owner (especially for the special case teams repo)
				for i, t := range lRepo.Spec.Writers {
					if strings.HasSuffix(t, config.Config.GoliacTeamOwnerSuffix) {
						lRepo.Spec.Writers = append(lRepo.Spec.Writers[:i], lRepo.Spec.Writers[i+1:]...)
						break
					}
				}
				if lRepo.Archived {
					if err := writeYamlFile(path.Join(archivepath, r+".yaml"), &lRepo, fs); err != nil {
						logrus.Errorf("not able to write archived repo file %s/%s.yaml: %v", archivepath, r, err)
					}
				} else {
					if err := writeYamlFile(path.Join(teamspath, teamPath, r+".yaml"), &lRepo, fs); err != nil {
						logrus.Errorf("not able to write repo file %s/%s.yaml: %v", team, r, err)
					}
				}
			}
		}
	}

	// if usersOnly
	if usersOnly {
		return nil
	}

	for teamName, slugName := range teamsSlugByName {
		t := teams[slugName]
		if strings.HasSuffix(slugName, config.Config.GoliacTeamOwnerSuffix) {
			continue
		}

		// searching for loney team (ie without repos)
		if _, ok := teamsRepos[slugName]; !ok {
			lTeam := entity.Team{}
			lTeam.ApiVersion = "v1"
			lTeam.Kind = "Team"
			lTeam.Name = teamName

			// if we have 1 or more maintainers in the Github team
			// we will use them as owners
			if len(t.Maintainers) >= 1 {
				for _, m := range t.Maintainers {
					// put the right user name instead of the github id
					lTeam.Spec.Owners = append(lTeam.Spec.Owners, usermap[m])
				}
				for _, m := range t.Members {
					// put the right user name instead of the github id
					lTeam.Spec.Members = append(lTeam.Spec.Members, usermap[m])
				}
			} else {
				for _, m := range t.Maintainers {
					// put the right user name instead of the github id
					lTeam.Spec.Owners = append(lTeam.Spec.Owners, usermap[m])
				}
				// else we put everyone as owners
				for _, m := range t.Members {
					// put the right user name instead of the github id
					lTeam.Spec.Owners = append(lTeam.Spec.Owners, usermap[m])
				}
			}

			teamPath, err := buildTeamPath(teamIds, teams[slugName])
			if err != nil {
				logrus.Errorf("unable to compute team's path: %v (for team %s)", err, teamName)
				continue
			}
			fs.MkdirAll(filepath.Join(teamspath, teamPath), 0755)
			if err := writeYamlFile(filepath.Join(teamspath, teamPath, "team.yaml"), &lTeam, fs); err != nil {
				logrus.Errorf("not able to write team file %s/team.yaml: %v", teamPath, err)
			}

		}
	}

	return nil
}

func buildTeamPath(teamIds map[int]*engine.GithubTeam, team *engine.GithubTeam) (string, error) {
	maxRecursive := 100
	fullpath := team.Name

	originalTeam := team.Name

	for maxRecursive > 0 {
		if team.ParentTeam == nil {
			return fullpath, nil
		} else {
			prevTeam := team
			t, ok := teamIds[*team.ParentTeam]
			if !ok {
				return fullpath, fmt.Errorf("not able to find back team's id %d (ie. parent of %s)", *prevTeam.ParentTeam, prevTeam.Name)
			}
			fullpath = path.Join(t.Name, fullpath)
			team = t
			maxRecursive--
		}
	}
	return fullpath, fmt.Errorf("too many resursive loop for team %s. aborting", originalTeam)
}

/*
 * Returns a map[<githubid>]<username>
 */
func (s *Scaffold) generateUsers(ctx context.Context, fs billy.Filesystem, userspath string) (map[string]string, error) {
	fs.MkdirAll(path.Join(userspath, "protected"), 0755)
	fs.MkdirAll(path.Join(userspath, "org"), 0755)
	fs.MkdirAll(path.Join(userspath, "external"), 0755)

	usermap := make(map[string]string)
	// test SAML integration
	users, err := s.loadUsersFromGithubOrgSaml(s.feedback)

	if len(users) > 0 && err == nil {
		logrus.Debug("SAML integration enabled")
		for username, user := range users {
			usermap[user.Spec.GithubID] = username
			if err := writeYamlFile(path.Join(userspath, "org", username+".yaml"), &user, fs); err != nil {
				logrus.Errorf("Not able to write user file org/%s.yaml: %v", username, err)
			}
		}
	} else {
		// fail back on github id
		logrus.Debug("SAML integration disabled")
		for githubid := range s.remote.Users(ctx) {
			usermap[githubid] = githubid
			user := entity.User{}
			user.ApiVersion = "v1"
			user.Kind = "User"
			user.Name = githubid
			user.Spec.GithubID = githubid

			if err := writeYamlFile(path.Join(userspath, "org", githubid+".yaml"), user, fs); err != nil {
				logrus.Errorf("Not able to write user file org/%s.yaml: %v", githubid, err)
			}
		}
	}

	return usermap, nil
}

func (s *Scaffold) generateRuleset(fs billy.Filesystem, rulesetspath string) error {
	ruleset := fmt.Sprintf(`apiVersion: v1
kind: Ruleset
name: default
spec:
  repositories:
    included:
      - ~ALL
  ruleset:
    enforcement: active
    bypassapps:
      - appname: %s
        mode: always
    conditions:
      include: 
      - "~DEFAULT_BRANCH"
    rules:
      - ruletype: pull_request
        parameters:
          requiredApprovingReviewCount: 1
`, s.githubappname)
	if err := writeFile(path.Join(rulesetspath, "default.yaml"), []byte(ruleset), fs); err != nil {
		return err
	}
	return nil

}

func (s *Scaffold) generateGoliacConf(fs billy.Filesystem, rootpath string, adminteam string) error {
	userplugin := "noop"
	if s.remote.IsEnterprise() {
		userplugin = "fromgithubsaml"
	}

	conf := fmt.Sprintf(`
admin_team: %s

rulesets:
  - default

max_changesets: 50
archive_on_delete: true

destructive_operations:
  repositories: false
  teams: false
  users: false
  rulesets: false

usersync:
  plugin: %s

# visibility_rules:
#   forbid_public_repositories: false
#   forbid_public_repositories_exclusions: [] # reponame or regexp

# workflows:
#   - standard
`, adminteam, userplugin)
	if err := writeFile(filepath.Join(rootpath, "goliac.yaml"), []byte(conf), fs); err != nil {
		return err
	}
	return nil
}

func (s *Scaffold) generateGithubAction(fs billy.Filesystem, rootpath string) error {
	fs.MkdirAll(filepath.Join(rootpath, ".github", "workflows"), 0755)

	workflow := `
name: Validate structure

on: [pull_request]

jobs:
  build:
    name: validate
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Verify
        run: docker run -v ${{ github.workspace }}:/work --rm ghcr.io/goliac-project/goliac verify /work
`
	if err := writeFile(filepath.Join(rootpath, ".github", "workflows", "pr.yaml"), []byte(workflow), fs); err != nil {
		return err
	}
	return nil
}

func (s *Scaffold) generateReadme(fs billy.Filesystem, rootpath string) error {
	readme := `
# teams

This repository manage the Github organization through [Goliac](https://github.com/goliac-project/goliac) application

## Create a repository

On a given team subdirectory you can create a repository definition via a yaml file (like ` + "`" + `/teams/foobar/awesome-repository.yaml` + "`" + `):

` + "```" + `
apiVersion: v1
kind: Repository
name: awesome-repository
` + "```" + `

This will create a ` + "`" + `awesome-repository` + "`" + ` repository under your organization, that will be 
- private by default
- writable by all owners/members of this team (in our example ` + "`" + `foobar` + "`" + `)

You can of course tweak that:

` + "```" + `
apiVersion: v1
kind: Repository
name: awesome-repository
spec:
  public: true
  writers:
  - anotherteamA
  - anotherteamB
  readers:
  - anotherteamC
  - anotherteamD
` + "```" + `

In this last example:
- the repository is now publci
- other teams have write (` + "`" + `notherteamA` + "`" + `, ` + "`" + `anotherteamB` + "`" + `) or read (` + "`" + `anotherteamC` + "`" + `, ` + "`" + `anotherteamD` + "`" + `) access

### Create a new team

If you want to create a new team (like ` + "`" + `foobar` + "`" + `), you need to create a PR with a ` + "`" + `/teams/foobar/team.yaml` + "`" + ` file:

` + "```" + `
apiVersion: v1
kind: Team
name: foobar
spec:
  owners:
    - user1
    - user2
  members:
    - user3
    - user4
` + "```" + `

The users defined there are in 2 different categories
- members: are part of the team (and will be writer on all repositories of the team)
- owners: are part of the team (and will be writer on all repositories of the team) AMD can approve PR in the ` + "`" + `foobar` + "`" + ` teams repository (when you want to change a team definition, or when you want to create/update a repository definition)

The users name used are the one defined in the ` + "`" + `/users` + "`" + ` sub directories (like ` + "`" + `alice` + "`" + `)

### Archive a repository

You can archive a repository, by a PR that move the yaml repository file into the ` + "`" + `/archived` + "`" + ` directory

### Special case: externally managed teams

If you want to create a team that is managed outside of Goliac, you can create a team with the ` + "`" + `externallyManaged: true` + "`" + ` flag:

` + "```" + `
apiVersion: v1
kind: Team
name: foobar
spec:
  externallyManaged: true
` + "```" + `

It will mean that the team is managed outside of Goliac, and that Goliac will not touch it.
You can still "attach" repositories to this team, but you will have to manage the team members by yourself.

`
	if err := writeFile(filepath.Join(rootpath, "README.md"), []byte(readme), fs); err != nil {
		return err
	}
	return nil
}

// helper function to write a yaml file (with 2 spaces indentation)
func writeYamlFile(filename string, in interface{}, fs billy.Filesystem) error {
	file, err := fs.Create(filename)
	if err != nil {
		return fmt.Errorf("not able to create file %s: %v", filename, err)
	}
	defer file.Close()

	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)
	err = encoder.Encode(in)
	if err != nil {
		return fmt.Errorf("not able to write to file %s: %v", filename, err)
	}
	return nil
}

// helper function to write a file
func writeFile(filename string, content []byte, fs billy.Filesystem) error {
	file, err := fs.Create(filename)
	if err != nil {
		return fmt.Errorf("not able to create file %s: %v", filename, err)
	}
	defer file.Close()

	_, err = file.Write(content)
	if err != nil {
		return fmt.Errorf("not able to write to file %s: %v", filename, err)
	}
	return nil
}
