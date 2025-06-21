package internal

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/goliac-project/goliac/internal/config"
	"github.com/goliac-project/goliac/internal/engine"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/goliac-project/goliac/internal/github"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/goliac-project/goliac/internal/usersync"
	"github.com/goliac-project/goliac/internal/utils"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	GOLIAC_GIT_TAG = "goliac"
)

type GoliacObservability interface {
	SetRemoteObservability(feedback observability.RemoteObservability) error // if you want to get feedback on the loading process
}

/*
 * Goliac is the main interface of the application.
 * It is used to load and validate a goliac repository and apply it to github.
 */
type Goliac interface {
	GoliacObservability

	// will run and apply the reconciliation,
	// it returns an error if something went wrong, and a detailed list of errors and warnings
	Apply(ctx context.Context, errorCollector *observability.ErrorCollection, fs billy.Filesystem, dryrun bool, repositoryUrl, branch string) *engine.UnmanagedResources

	// will clone run the user-plugin to sync users, and will commit to the team repository, return true if a change was done
	UsersUpdate(ctx context.Context, errorCollector *observability.ErrorCollection, fs billy.Filesystem, repositoryUrl, branch string, dryrun bool, force bool) bool

	// flush remote cache
	FlushCache()

	ExternalCreateRepository(ctx context.Context, errorCollector *observability.ErrorCollection, fs billy.Filesystem, githubToken, newRepositoryName, team, visibility, newRepositorydefaultBranch string, repositoryUrl, branch string)

	GetLocal() engine.GoliacLocalResources
	GetRemote() engine.GoliacRemoteResources

	// used by the Github http auth endpoints to validate the token
	GetRemoteClient() github.GitHubClient
}

type GoliacImpl struct {
	local                 engine.GoliacLocal
	remote                engine.GoliacRemoteExecutor
	localGithubClient     github.GitHubClient // github client for team repository operations
	remoteGithubClient    github.GitHubClient // github client for admin operations
	repoconfig            *config.RepositoryConfig
	feedback              observability.RemoteObservability // mostly used for UI progressbar
	actionMutex           sync.Mutex
	cacheDirtyAfterAction bool
}

func NewGoliacImpl() (Goliac, error) {
	remoteGithubClient, err := github.NewGitHubClientImpl(
		config.Config.GithubServer,
		config.Config.GithubAppOrganization,
		config.Config.GithubAppID,
		config.Config.GithubAppPrivateKeyFile,
		config.Config.GithubPersonalAccessToken,
	)
	if err != nil {
		return nil, err
	}

	localGithubClient, err := github.NewGitHubClientImpl(
		config.Config.GithubServer,
		config.Config.GithubAppOrganization,
		config.Config.GithubAppID,
		config.Config.GithubAppPrivateKeyFile,
		config.Config.GithubPersonalAccessToken,
	)
	if err != nil {
		return nil, err
	}

	local := engine.NewGoliacLocalImpl()
	remote := engine.NewGoliacRemoteImpl(
		remoteGithubClient,
		config.Config.GithubAppOrganization,
		config.Config.ManageGithubActionsVariables,
		config.Config.ManageGithubAutolinks,
	)

	usersync.InitPlugins(remoteGithubClient)

	return &GoliacImpl{
		local:                 local,
		remoteGithubClient:    remoteGithubClient,
		localGithubClient:     localGithubClient,
		remote:                remote,
		repoconfig:            &config.RepositoryConfig{},
		feedback:              nil,
		cacheDirtyAfterAction: false,
		actionMutex:           sync.Mutex{},
	}, nil
}

func (g *GoliacImpl) GetLocal() engine.GoliacLocalResources {
	return g.local
}

func (g *GoliacImpl) GetRemote() engine.GoliacRemoteResources {
	return g.remote
}

func (g *GoliacImpl) GetRemoteClient() github.GitHubClient {
	return g.remoteGithubClient
}

func (g *GoliacImpl) SetRemoteObservability(feedback observability.RemoteObservability) error {
	g.feedback = feedback
	g.remote.SetRemoteObservability(feedback)

	if feedback != nil {
		nb, err := g.remote.CountAssets(context.Background(), false)
		if err != nil {
			return fmt.Errorf("error when counting assets: %v", err)
		}
		feedback.Init(nb)
	}
	return nil
}

func (g *GoliacImpl) FlushCache() {
	g.remote.FlushCache()
}

func (g *GoliacImpl) ExternalCreateRepository(ctx context.Context, errorCollector *observability.ErrorCollection, fs billy.Filesystem, githubToken, newRepositoryName, team, visibility, newRepositoryDefaultBranch string, repositoryUrl, branch string) {

	// we need to lock the actionMutex to avoid concurrent actions
	g.actionMutex.Lock()
	defer g.actionMutex.Unlock()

	g.loadAndValidateGoliacOrganization(ctx, fs, repositoryUrl, branch, errorCollector)
	if errorCollector.HasErrors() {
		return
	}
	g.local.Close(fs)

	// sanity check
	// checking the team exists and the reposirory doesn't (yet)
	lTeams := g.local.Teams()
	if lTeams == nil {
		errorCollector.AddError(fmt.Errorf("teams not found"))
		return
	}
	lTeam := lTeams[team]
	if lTeam == nil {
		errorCollector.AddError(fmt.Errorf("team %s not found", team))
		return
	}
	directoryPath := lTeam.Name
	for lTeam.ParentTeam != nil {
		directoryPath = path.Join(*lTeam.ParentTeam, directoryPath)
		lTeam = lTeams[*lTeam.ParentTeam]
	}
	directoryPath = path.Join("teams", directoryPath)

	if g.local.Repositories()[newRepositoryName] != nil {
		errorCollector.AddError(fmt.Errorf("repository %s already exists", newRepositoryName))
		return
	}

	// first create the Pull Request

	repo := &entity.Repository{}
	repo.ApiVersion = "v1"
	repo.Kind = "Repository"
	repo.Name = newRepositoryName
	repo.Spec.Visibility = visibility
	repo.Spec.DefaultBranchName = newRepositoryDefaultBranch
	repo.Spec.Writers = []string{}
	repo.Spec.Readers = []string{}

	newBranchName := fmt.Sprintf("create_repository_%d", time.Now().Unix())

	orgname, reponame, err := utils.ExtractOrgRepo(repositoryUrl)

	if err != nil {
		errorCollector.AddError(fmt.Errorf("error when extracting org and repo: %v", err))
		return
	}

	// clone the goliac-teams repository
	// and create a PR on behalf of the githubToken
	tmpLocal := engine.NewGoliacLocalImpl()
	err = tmpLocal.Clone(fs, githubToken, repositoryUrl, branch)
	if err != nil {
		errorCollector.AddError(fmt.Errorf("error when cloning the repository: %v", err))
		return
	}

	defer tmpLocal.Close(fs)

	clientOnBehalf := engine.NewLocalGithubClientImpl(ctx, githubToken)

	pr, err := tmpLocal.UpdateReposViaPullRequest(
		ctx,
		clientOnBehalf,
		map[string]*entity.Repository{directoryPath: repo},
		orgname,
		reponame,
		githubToken,
		branch,
		newBranchName,
	)

	if err != nil {
		errorCollector.AddError(fmt.Errorf("error when creating the repository creation PR: %v", err))
		return
	}

	// second let's create the repository

	logrus.WithFields(map[string]interface{}{"dryrun": false, "command": "create_repository"}).Infof("repositoryname: %s", reponame)
	g.remote.CreateRepository(
		ctx,
		errorCollector,
		false,
		newRepositoryName,
		newRepositoryName,
		visibility,
		[]string{team},
		[]string{},
		map[string]bool{
			"delete_branch_on_merge": false,
			"allow_update_branch":    false,
			"archived":               false,
			"allow_auto_merge":       false,
		},
		newRepositoryDefaultBranch,
		&githubToken,
		"",
	)

	if errorCollector.HasErrors() {
		g.cacheDirtyAfterAction = true
		return
	}

	// let's merge the PR with the Goliac accesstoken
	accessToken, err := g.localGithubClient.GetAccessToken(ctx)
	if err != nil {
		errorCollector.AddError(fmt.Errorf("error when getting access token: %v", err))
		return
	}
	clientGoliac := engine.NewLocalGithubClientImpl(ctx, accessToken)

	err = clientGoliac.MergePullRequest(ctx, pr, branch)
	if err != nil {
		errorCollector.AddError(fmt.Errorf("error when merging the PR: %v", err))
		return
	}

	// refresh the cache

	errorCollector.ResetWarnings()
	g.loadAndValidateGoliacOrganization(ctx, fs, repositoryUrl, branch, errorCollector)
	if errorCollector.HasErrors() {
		return
	}
	g.local.Close(fs)

	// make the internal remote cache consistent
	g.cacheDirtyAfterAction = true
}

func (g *GoliacImpl) Apply(ctx context.Context, errorCollector *observability.ErrorCollection, fs billy.Filesystem, dryrun bool, repositoryUrl, branch string) *engine.UnmanagedResources {
	// warm up the cache

	if len(g.local.Repositories()) == 0 || len(g.local.Teams()) == 0 || len(g.local.Users()) == 0 {

		// we need to lock the actionMutex to avoid concurrent actions
		g.actionMutex.Lock()
		g.loadAndValidateGoliacOrganization(ctx, fs, repositoryUrl, branch, errorCollector)
		g.local.Close(fs)

		// we can unlock the actionMutex for now
		g.actionMutex.Unlock()
		if errorCollector.HasErrors() {
			return nil
		}
	}

	if !strings.HasPrefix(repositoryUrl, "https://") &&
		!strings.HasPrefix(repositoryUrl, "inmemory:///") { // <- only for testing purposes
		errorCollector.AddError(fmt.Errorf("local mode is not supported for plan/apply, you must specify the https url of the remote team git repository. Check the documentation"))

		return nil
	}

	u, err := url.Parse(repositoryUrl)
	if err != nil {
		errorCollector.AddError(fmt.Errorf("failed to parse %s: %v", repositoryUrl, err))
		return nil
	}

	teamreponame := strings.TrimSuffix(path.Base(u.Path), filepath.Ext(path.Base(u.Path)))

	// ensure that the team repo is configured to only allow squash and merge
	if !dryrun {
		err := g.forceSquashMergeOnTeamsRepo(ctx, teamreponame, branch)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("error when ensuring PR on %s, repo can only be done via squash and merge: %v", teamreponame, err))
			return nil
		}
	}

	g.cacheDirtyAfterAction = false

	// loading github assets can be long
	err = g.remote.Load(ctx, false)
	if err != nil {
		errorCollector.AddError(fmt.Errorf("error when loading data from Github: %v", err))
		return nil
	}

	g.actionMutex.Lock()

	// the next step is a bit tricky.
	// we need to ensure that the cache is up to date before applying the changes
	// so we need to reload the goliac organization if the cache is dirty
	// i.e. if an external action was done in between.
	// And we need to check again if the cache is dirty after the load
	// that's why there is a while loop here
	for g.cacheDirtyAfterAction {
		g.remote.FlushCache()
		g.cacheDirtyAfterAction = false

		g.actionMutex.Unlock()
		err = g.remote.Load(ctx, false)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("error when loading data from Github: %v", err))
			return nil
		}
		g.actionMutex.Lock()
	}

	defer g.actionMutex.Unlock()
	// load and validate the goliac organization (after github assets have been loaded)
	errorCollector.ResetWarnings()
	g.loadAndValidateGoliacOrganization(ctx, fs, repositoryUrl, branch, errorCollector)
	defer g.local.Close(fs)

	if errorCollector.HasErrors() {
		return nil
	}

	// apply the changes to the github team repository
	unmanaged := g.applyToGithub(ctx, dryrun, config.Config.GithubAppOrganization, teamreponame, branch, config.Config.SyncUsersBeforeApply, errorCollector)

	return unmanaged
}

func (g *GoliacImpl) loadAndValidateGoliacOrganization(ctx context.Context, fs billy.Filesystem, repositoryUrl, branch string, errorCollector *observability.ErrorCollection) {
	var childSpan trace.Span
	if config.Config.OpenTelemetryEnabled {
		// get back the tracer from the context
		ctx, childSpan = otel.Tracer("goliac").Start(ctx, "loadAndValidateGoliacOrganization")
		defer childSpan.End()

		childSpan.SetAttributes(
			attribute.String("repository_url", repositoryUrl),
			attribute.String("branch", branch),
		)
	}

	if strings.HasPrefix(repositoryUrl, "https://") || strings.HasPrefix(repositoryUrl, "git@") || strings.HasPrefix(repositoryUrl, "inmemory:///") {
		accessToken := ""
		var err error
		if strings.HasPrefix(repositoryUrl, "https://") {
			accessToken, err = g.localGithubClient.GetAccessToken(ctx)
			if err != nil {
				errorCollector.AddError(fmt.Errorf("error when getting access token: %v", err))
				return
			}
		}

		err = g.local.Clone(fs, accessToken, repositoryUrl, branch)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("unable to clone: %v", err))
			return
		}
		g.local.LoadAndValidate(errorCollector)
		if errorCollector.HasErrors() {
			return
		}

		repoconfig := g.local.RepoConfig()
		if repoconfig == nil {
			errorCollector.AddError(fmt.Errorf("unable to read goliac.yaml config file"))
			return
		}
		g.repoconfig = repoconfig

	} else {
		// Local
		subfs, err := fs.Chroot(repositoryUrl)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("unable to chroot to %s: %v", repositoryUrl, err))
			return
		}
		g.local.LoadAndValidateLocal(subfs, errorCollector)
	}

	if errorCollector.HasErrors() {
		for _, err := range errorCollector.Errors {
			logrus.Error(err)
		}
		return
	}
}

/*
 * To ensure we can parse teams git logs, commit by commit (for auditing purpose),
 * we must ensure that the "squqsh and merge" option is the only option.
 * Else we may append to apply commits that are part of a PR, but wasn't the final PR commit state
 */
func (g *GoliacImpl) forceSquashMergeOnTeamsRepo(ctx context.Context, teamreponame string, branchname string) error {
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2022-11-28#update-a-repository
	_, err := g.remoteGithubClient.CallRestAPI(ctx,
		fmt.Sprintf("/repos/%s/%s", config.Config.GithubAppOrganization, teamreponame),
		"",
		"PATCH",
		map[string]interface{}{
			"allow_merge_commit":     false, // allow merging pull requests with a merge commit
			"allow_rebase_merge":     false, // allow rebase-merging pull requests
			"allow_squash_merge":     true,  // allow squash-merging pull requests
			"delete_branch_on_merge": true,  // automatically deleting head branches when pull requests are merged
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("error when updating the %s repository: %v", teamreponame, err)
	}

	// add an extra branch protection
	contexts := []string{}

	if config.Config.ServerGitBranchProtectionRequiredCheck != "" {
		contexts = append(contexts, config.Config.ServerGitBranchProtectionRequiredCheck)
	}
	// https://docs.github.com/en/rest/branches/branch-protection?apiVersion=2022-11-28#update-branch-protection
	_, err = g.remoteGithubClient.CallRestAPI(ctx,
		fmt.Sprintf("/repos/%s/%s/branches/%s/protection", config.Config.GithubAppOrganization, teamreponame, branchname),
		"",
		"PUT",
		map[string]interface{}{
			"required_status_checks": map[string]interface{}{
				"strict":   true,     // // This ensures branches are up to date before merging
				"contexts": contexts, // Status checks to enforce, see scaffold.go for the job name
			},
			"enforce_admins": nil,
			"required_pull_request_reviews": map[string]interface{}{
				"required_approving_review_count": 1,    // Number of approvals required. Set this to 1 for one review.
				"require_last_push_approval":      true, // most recent push must be approved by someone other than the person who pushed it
			},
			// required_pull_request_reviews could have been
			//{
			// "dismiss_stale_reviews": true,   // Optional: Whether or not approved reviews are dismissed when a new commit is pushed.
			//"require_code_owner_reviews": false,  // Optional: If set, only code owners can approve the PR.
			//"required_approving_review_count": 1   // Number of approvals required. Set this to 1 for one review.
			//},
			"restrictions": nil,
		},
		nil)
	if err != nil {
		return fmt.Errorf("error when updating the branch protection for %s repository: %v", teamreponame, err)
	}
	return nil
}

/*
Apply the changes to the github team repository:
  - sync users if needed (from external sources)
  - apply the changes
  - update the codeowners file
*/
func (g *GoliacImpl) applyToGithub(ctx context.Context, dryrun bool, githubOrganization string, teamreponame string, branch string, syncusersbeforeapply bool, errorCollector *observability.ErrorCollection) *engine.UnmanagedResources {
	var childSpan trace.Span
	if config.Config.OpenTelemetryEnabled {
		// get back the tracer from the context
		ctx, childSpan = otel.Tracer("goliac").Start(ctx, "applyToGithub")
		defer childSpan.End()

		childSpan.SetAttributes(
			attribute.String("github_organization", githubOrganization),
		)
	}

	//
	// prelude
	//

	// we try to sync users before applying the changes
	if syncusersbeforeapply {
		userplugin, found := engine.GetUserSyncPlugin(g.repoconfig.UserSync.Plugin)
		if !found {
			logrus.Warnf("user sync plugin %s not found", g.repoconfig.UserSync.Plugin)
		} else {
			accessToken, err := g.localGithubClient.GetAccessToken(ctx)
			if err != nil {
				errorCollector.AddError(fmt.Errorf("error when getting access token: %v", err))
				return nil
			}
			change := g.local.SyncUsersAndTeams(ctx, g.repoconfig, userplugin, accessToken, dryrun, false, g.feedback, errorCollector)
			if errorCollector.HasErrors() {
				return nil
			}
			if change {
				g.remote.FlushCacheUsersTeamsOnly()
			}
		}
	}

	//
	// main
	//

	// we apply the changes to the github team repository
	unmanaged, err := g.applyCommitsToGithub(ctx, errorCollector, dryrun, teamreponame, branch)
	if err != nil {
		errorCollector.AddError(fmt.Errorf("error when applying to github: %v", err))
		return unmanaged
	}

	//
	// post
	//

	// we update the codeowners file
	if !dryrun {
		accessToken, err := g.localGithubClient.GetAccessToken(ctx)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("error when getting access token: %v", err))
			return unmanaged
		}
		err = g.local.UpdateAndCommitCodeOwners(ctx, g.repoconfig, dryrun, accessToken, branch, GOLIAC_GIT_TAG, githubOrganization)
		if err != nil {
			errorCollector.AddError(fmt.Errorf("error when updating and commiting: %v", err))
			return unmanaged
		}
	}

	return unmanaged
}

func (g *GoliacImpl) applyCommitsToGithub(ctx context.Context, errorCollector *observability.ErrorCollection, dryrun bool, teamreponame string, branch string) (*engine.UnmanagedResources, error) {
	var childSpan trace.Span
	if config.Config.OpenTelemetryEnabled {
		ctx, childSpan = otel.Tracer("goliac").Start(ctx, "applyCommitsToGithub")
		defer childSpan.End()
	}

	// if the repo was just archived in a previous commit and we "resume it"
	// so we keep a track of all repos that we want to archive until the end of the process
	reposToArchive := make(map[string]*engine.GithubRepoComparable)
	// map[oldreponame]*entity.Repository
	reposToRename := make(map[string]*entity.Repository)
	var unmanaged *engine.UnmanagedResources

	ga := NewGithubBatchExecutor(g.remote, g.repoconfig.MaxChangesets)
	reconciliator := engine.NewGoliacReconciliatorImpl(g.remote.IsEnterprise(), ga, g.repoconfig)

	commit, err := g.local.GetHeadCommit()
	if err != nil {
		return unmanaged, fmt.Errorf("error when getting head commit: %v", err)
	}

	// the repo has already been cloned (to HEAD) and validated (see loadAndValidateGoliacOrganization)
	// we can now apply the changes to the github team repository
	unmanaged, err = reconciliator.Reconciliate(ctx, errorCollector, g.local, g.remote, teamreponame, branch, dryrun, g.repoconfig.AdminTeam, reposToArchive, reposToRename, config.Config.ManageGithubActionsVariables, config.Config.ManageGithubAutolinks)
	if err != nil {
		return unmanaged, fmt.Errorf("error when reconciliating: %v", err)
	}

	if !dryrun {
		accessToken, err := g.localGithubClient.GetAccessToken(ctx)
		if err != nil {
			return unmanaged, err
		}
		g.local.PushTag(GOLIAC_GIT_TAG, commit.Hash, accessToken)
	}

	accessToken, err := g.localGithubClient.GetAccessToken(ctx)
	if err != nil {
		return unmanaged, err
	}

	// if we have repos to create as archived or to rename
	if (len(reposToArchive) > 0 || len(reposToRename) > 0) && !dryrun {
		reposToArchiveList := make([]string, 0)
		for reponame := range reposToArchive {
			reposToArchiveList = append(reposToArchiveList, reponame)
		}
		err = g.local.UpdateRepos(reposToArchiveList, reposToRename, accessToken, branch, GOLIAC_GIT_TAG)
		if err != nil {
			return unmanaged, fmt.Errorf("error when archiving repos: %v", err)
		}
	}
	return unmanaged, nil
}

func (g *GoliacImpl) UsersUpdate(ctx context.Context, errorCollector *observability.ErrorCollection, fs billy.Filesystem, repositoryUrl, branch string, dryrun bool, force bool) bool {
	accessToken, err := g.localGithubClient.GetAccessToken(ctx)
	if err != nil {
		errorCollector.AddError(fmt.Errorf("error when getting access token: %v", err))
		return false
	}

	err = g.local.Clone(fs, accessToken, repositoryUrl, branch)
	if err != nil {
		errorCollector.AddError(fmt.Errorf("unable to clone: %v", err))
		return false
	}
	defer g.local.Close(fs)

	g.local.LoadAndValidate(errorCollector)
	if errorCollector.HasErrors() {
		return false
	}

	repoconfig := g.local.RepoConfig()
	if repoconfig == nil {
		errorCollector.AddError(fmt.Errorf("unable to read goliac.yaml config file"))
		return false
	}

	userplugin, found := engine.GetUserSyncPlugin(repoconfig.UserSync.Plugin)
	if !found {
		errorCollector.AddError(fmt.Errorf("user sync Plugin %s not found", repoconfig.UserSync.Plugin))
		return false
	}

	return g.local.SyncUsersAndTeams(ctx, repoconfig, userplugin, accessToken, dryrun, force, g.feedback, errorCollector)
}
