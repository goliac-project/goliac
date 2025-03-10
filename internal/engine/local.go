package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5"
	goconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/goliac-project/goliac/internal/config"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/goliac-project/goliac/internal/utils"
	"github.com/google/go-github/v55/github"
	"github.com/gosimple/slug"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/oauth2"
	"gopkg.in/yaml.v3"
)

/*
 * GoliacLocal
 * This interface is used to load the goliac organization from a local directory
 * and mount it in memory
 */
type GoliacLocal interface {
	GoliacLocalGit
	GoliacLocalResources
}

type GoliacLocalGit interface {
	Clone(fs billy.Filesystem, accesstoken, repositoryUrl, branch string) error

	// Return commits from tagname to HEAD
	ListCommitsFromTag(tagname string) ([]*object.Commit, error)
	GetHeadCommit() (*object.Commit, error)
	CheckoutCommit(commit *object.Commit) error
	PushTag(tagname string, hash plumbing.Hash, accesstoken string) error

	LoadRepoConfig() (*config.RepositoryConfig, error)

	// Load and Validate from a github repository
	LoadAndValidate(errorCollection *observability.ErrorCollection)
	// whenever someone create/delete a team, we must update the github CODEOWNERS
	UpdateAndCommitCodeOwners(ctx context.Context, repoconfig *config.RepositoryConfig, dryrun bool, accesstoken string, branch string, tagname string, githubOrganization string) error
	// whenever repos are not deleted but archived [reponame], need to be renamed [directorypath:repo definition], or created [directorypath: repo definition]
	UpdateRepos(reposToArchiveList []string, reposToRename map[string]*entity.Repository, accesstoken string, branch string, tagname string) error
	// whenever the users list is changing, reload users and teams, and commit them
	// (force will bypass the max_changesets check)
	// return true if some changes were done
	SyncUsersAndTeams(ctx context.Context, repoconfig *config.RepositoryConfig, plugin UserSyncPlugin, accesstoken string, dryrun bool, force bool, feedback observability.RemoteObservability, errorCollector *observability.ErrorCollection) bool
	Close(fs billy.Filesystem)

	// Load and Validate from a local directory
	LoadAndValidateLocal(fs billy.Filesystem, errorCollection *observability.ErrorCollection)

	UpdateReposViaPullRequest(reposToCreate map[string]*entity.Repository, orgname, reponame, accesstoken, baseBranch, newBranchName string) (*github.PullRequest, error)
	MergePullRequest(pr *github.PullRequest, accesstoken, mainBranch string) error
}

type GoliacLocalResources interface {
	Teams() map[string]*entity.Team              // teamname, team definition
	Repositories() map[string]*entity.Repository // reponame, repo definition
	Users() map[string]*entity.User              // github username, user definition
	ExternalUsers() map[string]*entity.User
	RuleSets() map[string]*entity.RuleSet
}

type GoliacLocalImpl struct {
	teams         map[string]*entity.Team
	repositories  map[string]*entity.Repository
	users         map[string]*entity.User
	externalUsers map[string]*entity.User
	rulesets      map[string]*entity.RuleSet
	repo          *git.Repository
}

func NewGoliacLocalImpl() GoliacLocal {
	return &GoliacLocalImpl{
		teams:         map[string]*entity.Team{},
		repositories:  map[string]*entity.Repository{},
		users:         map[string]*entity.User{},
		externalUsers: map[string]*entity.User{},
		rulesets:      map[string]*entity.RuleSet{},
		repo:          nil,
	}
}

// NewMockGoliacLocalImpl is used for testing purposes
func NewGoliacLocalImplWithRepo(repo *git.Repository) GoliacLocal {
	return &GoliacLocalImpl{
		teams:         map[string]*entity.Team{},
		repositories:  map[string]*entity.Repository{},
		users:         map[string]*entity.User{},
		externalUsers: map[string]*entity.User{},
		rulesets:      map[string]*entity.RuleSet{},
		repo:          repo,
	}
}

func (g *GoliacLocalImpl) Teams() map[string]*entity.Team {
	return g.teams
}

func (g *GoliacLocalImpl) Repositories() map[string]*entity.Repository {
	return g.repositories
}

func (g *GoliacLocalImpl) Users() map[string]*entity.User {
	return g.users
}

func (g *GoliacLocalImpl) ExternalUsers() map[string]*entity.User {
	return g.externalUsers
}

func (g *GoliacLocalImpl) RuleSets() map[string]*entity.RuleSet {
	return g.rulesets
}

func (g *GoliacLocalImpl) Clone(fs billy.Filesystem, accesstoken, repositoryUrl, branch string) error {
	if g.repo != nil {
		g.Close(fs)
	}

	// create a temp directory
	tmpDir, err := utils.MkdirTemp(fs, "", "goliac")
	if err != nil {
		return err
	}

	var auth transport.AuthMethod
	if strings.HasPrefix(repositoryUrl, "https://") {
		auth = &http.BasicAuth{
			Username: "x-access-token", // This can be anything except an empty string
			Password: accesstoken,
		}
	} else if strings.HasPrefix(repositoryUrl, "inmemory:///") {
		auth = nil
	} else {
		// ssh clone not supported yet
		return fmt.Errorf("not supported")
	}
	repo, err := git.PlainClone(tmpDir, false, &git.CloneOptions{
		URL:           repositoryUrl,
		Auth:          auth,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
	})
	if err != nil {
		return err
	}
	g.repo = repo

	return err
}

func (g *GoliacLocalImpl) PushTag(tagname string, hash plumbing.Hash, accesstoken string) error {
	// Create or move the tag to the commit
	tagRefName := plumbing.ReferenceName("refs/tags/" + tagname)
	tagRef := plumbing.NewHashReference(tagRefName, hash)
	if err := g.repo.Storer.SetReference(tagRef); err != nil {
		return err
	}

	// Now push the tag to the remote repository
	auth := &http.BasicAuth{
		Username: "x-access-token", // This can be anything except an empty string
		Password: accesstoken,
	}

	// Force push with '+refs/tags/your_tag_name_here:refs/tags/your_tag_name_here'
	pushRefSpec := fmt.Sprintf("+%s:%s", tagRefName, tagRefName)
	err := g.repo.Push(&git.PushOptions{
		RefSpecs: []goconfig.RefSpec{goconfig.RefSpec(pushRefSpec)},
		Auth:     auth,
	})

	if err != nil && err.Error() == "already up-to-date" {
		return nil
	}

	return err
}

func (g *GoliacLocalImpl) CheckoutCommit(commit *object.Commit) error {
	// checkout the branch
	w, err := g.repo.Worktree()
	if err != nil {
		return err
	}
	err = w.Checkout(&git.CheckoutOptions{
		Hash: commit.Hash,
	})
	if err != nil {
		return err
	}
	return nil
}

func (g *GoliacLocalImpl) GetHeadCommit() (*object.Commit, error) {
	// Get reference to the HEAD
	refHead, err := g.repo.Head()
	if err != nil {
		return nil, err
	}

	headCommit, err := g.repo.CommitObject(refHead.Hash())
	if err != nil {
		return nil, err
	}
	return headCommit, nil
}

func (g *GoliacLocalImpl) ListCommitsFromTag(tagname string) ([]*object.Commit, error) {
	if g.repo == nil {
		return nil, fmt.Errorf("git repository not cloned")
	}

	commits := make([]*object.Commit, 0)

	// Get reference to the HEAD
	refHead, err := g.repo.Head()
	if err != nil {
		return nil, err
	}

	headCommit, err := g.repo.CommitObject(refHead.Hash())
	if err != nil {
		return nil, err
	}

	// Get reference to the specific tag
	refTag, err := g.repo.Tag(tagname)
	if err != nil {
		// we can't? stop it and returns the head
		return []*object.Commit{headCommit}, nil
	}

	// Get the commits between HEAD and the specific tag
	commitLog, err := g.repo.Log(&git.LogOptions{
		From:  refHead.Hash(),
		Order: git.LogOrderCommitterTime,
	})
	if err != nil {
		return nil, err
	}

	err = commitLog.ForEach(func(c *object.Commit) error {
		if c.Hash == refTag.Hash() {
			return errors.New("stop iteration") // This is used to stop the iteration
		}
		commits = append(commits, c)

		return nil
	})
	if err != nil && err.Error() != "stop iteration" {
		return commits, err
	}

	// let's invert the order of the commits (from tag to HEAD)
	length := len(commits)
	for i := 0; i < length/2; i++ {
		commits[i], commits[length-1-i] = commits[length-1-i], commits[i]
	}

	return commits, nil
}

func (g *GoliacLocalImpl) Close(fs billy.Filesystem) {
	if g.repo != nil {
		w, err := g.repo.Worktree()
		if err == nil {
			utils.RemoveAll(fs, w.Filesystem.Root())
		}
	}
	g.repo = nil
}

func (g *GoliacLocalImpl) LoadRepoConfig() (*config.RepositoryConfig, error) {
	if g.repo == nil {
		return nil, fmt.Errorf("git repository not cloned")
	}
	w, err := g.repo.Worktree()
	if err != nil {
		return nil, err
	}

	var repoconfig config.RepositoryConfig

	content, err := utils.ReadFile(w.Filesystem, "goliac.yaml")
	if err != nil {
		return nil, fmt.Errorf("not able to find the /goliac.yaml configuration file: %v", err)
	}
	err = yaml.Unmarshal(content, &repoconfig)
	if err != nil {
		return nil, fmt.Errorf("not able to unmarshall the /goliac.yaml configuration file: %v", err)
	}

	return &repoconfig, nil
}

func (g *GoliacLocalImpl) codeowners_regenerate(adminteam string, githubOrganization string) string {
	adminteamname := fmt.Sprintf("@%s/%s", githubOrganization, slug.Make(adminteam))

	codeowners := "# DO NOT MODIFY THIS FILE MANUALLY\n"

	teamsnames := make([]string, 0)
	for _, t := range g.teams {
		teamsnames = append(teamsnames, t.Name)
	}

	codeownersrules := make([]string, 0)
	for _, t := range teamsnames {
		teampath := fmt.Sprintf("/teams/%s/*", g.buildTeamPath(t))
		if strings.Contains(teampath, " ") {
			teampath = strings.ReplaceAll(teampath, " ", "\\ ")
		}
		codeownersrules = append(codeownersrules, fmt.Sprintf("%s @%s/%s%s %s\n", teampath, githubOrganization, slug.Make(t), config.Config.GoliacTeamOwnerSuffix, adminteamname))
	}

	// sort by path length
	// because CODEOWNERS is read from top to bottom and take the latest match
	sort.Slice(codeownersrules, func(i, j int) bool {
		iPath := strings.Split(codeownersrules[i], "*")[0]
		jPath := strings.Split(codeownersrules[j], "*")[0]
		if len(iPath) == len(jPath) {
			return iPath < jPath
		}
		return len(iPath) < len(jPath)
	})

	codeowners += fmt.Sprintf("* %s\n", adminteamname)
	for _, r := range codeownersrules {
		codeowners += r
	}

	return codeowners
}

func (g *GoliacLocalImpl) buildTeamPath(teamname string) string {
	team := g.teams[teamname]
	if team.ParentTeam == nil || *team.ParentTeam == "" {
		return teamname
	}
	return g.buildTeamPath(*team.ParentTeam) + "/" + teamname
}

func (g *GoliacLocalImpl) UpdateRepos(reposToArchiveList []string, reposToRename map[string]*entity.Repository, accesstoken string, branch string, tagname string) error {
	if g.repo == nil {
		return fmt.Errorf("git repository not cloned")
	}

	// Get the HEAD reference
	headRef, err := g.repo.Head()
	if err != nil {
		return err
	}

	if headRef.Name() != plumbing.NewBranchReferenceName(branch) {
		// If not on main, check out the main branch
		worktree, err := g.repo.Worktree()
		if err != nil {
			return err
		}

		err = worktree.Checkout(&git.CheckoutOptions{
			Branch: plumbing.NewBranchReferenceName(branch),
			Create: false,
			Force:  true,
		})
		if err != nil {
			return err
		}
	}

	w, err := g.repo.Worktree()
	if err != nil {
		return err
	}

	if len(reposToArchiveList) != 0 {
		err = w.Filesystem.MkdirAll("archived", 0755)
		if err != nil {
			return err
		}

		for _, reponame := range reposToArchiveList {
			repo := entity.Repository{}
			repo.ApiVersion = "v1"
			repo.Kind = "Repository"
			repo.Name = reponame

			filename := filepath.Join("archived", reponame+".yaml")
			file, err := w.Filesystem.Create(filename)
			if err != nil {
				return fmt.Errorf("not able to create file %s: %v", filename, err)
			}
			defer file.Close()

			encoder := yaml.NewEncoder(file)
			encoder.SetIndent(2)
			err = encoder.Encode(&repo)
			if err != nil {
				return fmt.Errorf("not able to write to file %s: %v", filename, err)
			}

			_, err = w.Add(filename)
			if err != nil {
				return err
			}

			// last, if the repository was not present in Goliac (it was removed from Goliac
			// but still present in Github, and we want to archive it), we must add it back
			// to the list of repositories we manage
			if _, ok := g.repositories[reponame]; !ok {
				g.repositories[reponame] = &repo
			}
		}

		_, err = w.Commit("moving deleted repositories as archived", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Goliac",
				Email: config.Config.GoliacEmail,
				When:  time.Now(),
			},
		})

		if err != nil {
			return err
		}
	}

	if len(reposToRename) != 0 {

		for directoryPath, repository := range reposToRename {
			newRepository := *repository
			newRepository.Name = repository.RenameTo
			newRepository.RenameTo = ""

			filename := filepath.Join(directoryPath, newRepository.Name+".yaml")
			file, err := w.Filesystem.Create(filename)
			if err != nil {
				return fmt.Errorf("not able to create file %s: %v", filename, err)
			}
			defer file.Close()

			encoder := yaml.NewEncoder(file)
			encoder.SetIndent(2)
			err = encoder.Encode(&newRepository)
			if err != nil {
				return fmt.Errorf("not able to write to file %s: %v", filename, err)
			}

			_, err = w.Add(filename)
			if err != nil {
				return err
			}

			_, err = w.Remove(filepath.Join(directoryPath, repository.Name+".yaml"))
			if err != nil {
				return err
			}
		}

		_, err = w.Commit("renaming repositories", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Goliac",
				Email: config.Config.GoliacEmail,
				When:  time.Now(),
			},
		})

		if err != nil {
			return err
		}
	}

	// reloading the new local organization
	errorCollection := observability.NewErrorCollection()
	g.LoadAndValidateLocal(w.Filesystem, errorCollection)
	if errorCollection.HasErrors() {
		return fmt.Errorf("error reloading and validating the local organization: %v", errorCollection)
	}

	err = g.repo.Push(&git.PushOptions{
		RemoteName: "origin",
		Auth: &http.BasicAuth{
			Username: "x-access-token", // This can be anything except an empty string
			Password: accesstoken,
		},
	})

	if err != nil {
		return fmt.Errorf("error pushing to remote: %v", err)
	}

	// push the tagname
	return g.PushTag(tagname, headRef.Hash(), accesstoken)
}

func (g *GoliacLocalImpl) UpdateReposViaPullRequest(reposToCreate map[string]*entity.Repository, orgname, reponame, accesstoken, baseBranch, newBranchName string) (*github.PullRequest, error) {
	if g.repo == nil {
		return nil, fmt.Errorf("git repository not cloned")
	}

	// If not on main, check out the main branch
	w, err := g.repo.Worktree()
	if err != nil {
		return nil, err
	}

	err = w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(newBranchName),
		Create: true,
	})
	if err != nil {
		return nil, err
	}

	if len(reposToCreate) != 0 {

		for directoryPath, repository := range reposToCreate {
			newRepository := *repository

			filename := filepath.Join(directoryPath, newRepository.Name+".yaml")
			file, err := w.Filesystem.Create(filename)
			if err != nil {
				return nil, fmt.Errorf("not able to create file %s: %v", filename, err)
			}
			defer file.Close()

			encoder := yaml.NewEncoder(file)
			encoder.SetIndent(2)
			err = encoder.Encode(&newRepository)
			if err != nil {
				return nil, fmt.Errorf("not able to write to file %s: %v", filename, err)
			}

			_, err = w.Add(filename)
			if err != nil {
				return nil, err
			}
		}

		_, err = w.Commit("creating repositories", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Goliac",
				Email: config.Config.GoliacEmail,
				When:  time.Now(),
			},
		})

		if err != nil {
			return nil, err
		}
	}

	err = g.repo.Push(&git.PushOptions{
		RemoteName: "origin",
		Auth: &http.BasicAuth{
			Username: "x-access-token", // This can be anything except an empty string
			Password: accesstoken,
		},
		RefSpecs: []goconfig.RefSpec{goconfig.RefSpec("refs/heads/" + newBranchName + ":refs/heads/" + newBranchName)},
	})

	if err != nil {
		return nil, fmt.Errorf("error pushing to remote: %v", err)
	}

	return createPullRequest(orgname, reponame, accesstoken, baseBranch, newBranchName, "Creating new repositories")
}

// Function to create a PR using GitHub API
func createPullRequest(orgname, reponame, accesstoken, baseBranch, branch, title string) (*github.PullRequest, error) {

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accesstoken})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	newPR := &github.NewPullRequest{
		Title: github.String(title),
		Head:  github.String(branch),
		Base:  github.String(baseBranch),
		//		Body:                github.String(prBody),
		MaintainerCanModify: github.Bool(true),
	}

	pr, _, err := client.PullRequests.Create(ctx, orgname, reponame, newPR)
	if err != nil {
		return nil, err
	}
	return pr, nil
}

func (g *GoliacLocalImpl) MergePullRequest(pr *github.PullRequest, accesstoken, mainBranch string) error {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accesstoken})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	// Create review request
	review := &github.PullRequestReviewRequest{
		Body:  github.String("Approving this PR via GitHub App."),
		Event: github.String("APPROVE"),
	}

	// Approve the PR
	_, _, err := client.PullRequests.CreateReview(ctx, pr.GetBase().GetRepo().GetOwner().GetLogin(), pr.GetBase().GetRepo().GetName(), pr.GetNumber(), review)
	if err != nil {
		return fmt.Errorf("failed to approve PR: %v", err)
	}

	// Merge request options
	mergeOpts := &github.PullRequestOptions{
		MergeMethod: "squash", // Options: "merge", "squash", "rebase"
	}

	_, _, err = client.PullRequests.Merge(ctx, pr.GetBase().GetRepo().GetOwner().GetLogin(), pr.GetBase().GetRepo().GetName(), pr.GetNumber(), "Merging PR", mergeOpts)

	if err != nil {
		return err
	}
	return nil
}

/*
 * UpdateAndCommitCodeOwners will collects all teams definition to update the .github/CODEOWNERS file
 * cf https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-code-owners
 */
func (g *GoliacLocalImpl) UpdateAndCommitCodeOwners(ctx context.Context, repoconfig *config.RepositoryConfig, dryrun bool, accesstoken string, branch string, tagname string, githubOrganization string) error {
	var childSpan trace.Span
	if config.Config.OpenTelemetryEnabled {
		_, childSpan = otel.GetTracerProvider().Tracer("goliac").Start(ctx, "UpdateAndCommitCodeOwners")
		defer childSpan.End()
	}
	if g.repo == nil {
		return fmt.Errorf("git repository not cloned")
	}
	w, err := g.repo.Worktree()
	if err != nil {
		return err
	}

	err = w.Filesystem.MkdirAll(".github", 0755)
	if err != nil {
		return err
	}

	codeownerpath := filepath.Join(".github", "CODEOWNERS")
	var content []byte

	info, err := w.Filesystem.Stat(codeownerpath)
	if err == nil && !info.IsDir() {
		file, err := w.Filesystem.Open(codeownerpath)
		if err != nil {
			return fmt.Errorf("not able to open .github/CODEOWNERS file: %v", err)
		}
		defer file.Close()

		content, err = io.ReadAll(file)
		if err != nil {
			return fmt.Errorf("not able to open .github/CODEOWNERS file: %v", err)
		}
	} else {
		content = []byte("")
	}

	newContent := g.codeowners_regenerate(repoconfig.AdminTeam, githubOrganization)

	if string(content) != newContent {
		logrus.Info(".github/CODEOWNERS needs to be regenerated")
		if dryrun {
			return nil
		}

		// Get the HEAD reference
		headRef, err := g.repo.Head()
		if err != nil {
			return err
		}

		if headRef.Name() != plumbing.NewBranchReferenceName(branch) {
			// If not on main, check out the main branch
			worktree, err := g.repo.Worktree()
			if err != nil {
				return err
			}

			err = worktree.Checkout(&git.CheckoutOptions{
				Branch: plumbing.NewBranchReferenceName(branch),
				Create: false,
				Force:  true,
			})
			if err != nil {
				return err
			}
		}

		err = utils.WriteFile(w.Filesystem, codeownerpath, []byte(newContent), 0644)
		if err != nil {
			return err
		}

		_, err = w.Add(codeownerpath)
		if err != nil {
			return err
		}

		_, err = w.Commit("update CODEOWNERS", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Goliac",
				Email: config.Config.GoliacEmail,
				When:  time.Now(),
			},
		})

		if err != nil {
			return err
		}

		err = g.repo.Push(&git.PushOptions{
			RemoteName: "origin",
			Auth: &http.BasicAuth{
				Username: "x-access-token", // This can be anything except an empty string
				Password: accesstoken,
			},
		})

		if err != nil {
			return fmt.Errorf("error pushing to remote: %v", err)
		}
	}

	// push the tagname
	if !dryrun {
		// Get the HEAD reference
		headRef, err := g.repo.Head()
		if err != nil {
			return err
		}

		return g.PushTag(tagname, headRef.Hash(), accesstoken)
	}

	return nil
}

/**
 * syncusers will
 * - list the current users list
 * - call the external user sync plugin
 * - collect the difference
 * - returns deleted users, and add/updated users
 */
func syncUsersViaUserPlugin(repoconfig *config.RepositoryConfig, fs billy.Filesystem, userplugin UserSyncPlugin, feedback observability.RemoteObservability, errorCollection *observability.ErrorCollection) ([]string, []string) {
	usersOrgPath := filepath.Join("users", "org")
	orgUsers := entity.ReadUserDirectory(fs, usersOrgPath, errorCollection)
	if errorCollection.HasErrors() {
		return nil, nil
	}

	// use usersync to update the users
	newOrgUsers := userplugin.UpdateUsers(repoconfig, fs, usersOrgPath, feedback, errorCollection)
	if errorCollection.HasErrors() {
		return nil, nil
	}

	// write back to disk
	deletedusers := []string{}
	updatedusers := []string{}
	for username, user := range orgUsers {
		if newuser, ok := newOrgUsers[username]; !ok {
			// deleted user
			deletedusers = append(deletedusers, filepath.Join(usersOrgPath, fmt.Sprintf("%s.yaml", username)))
			fs.Remove(filepath.Join(usersOrgPath, fmt.Sprintf("%s.yaml", username)))
		} else {
			// check if user changed
			if !newuser.Equals(user) {
				// changed user
				file, err := fs.Create(filepath.Join(usersOrgPath, fmt.Sprintf("%s.yaml", username)))
				if err != nil {
					errorCollection.AddError(err)
					return nil, nil
				}
				defer file.Close()

				encoder := yaml.NewEncoder(file)
				encoder.SetIndent(2)
				err = encoder.Encode(newuser)
				if err != nil {
					errorCollection.AddError(err)
					return nil, nil
				}
				updatedusers = append(updatedusers, filepath.Join(usersOrgPath, fmt.Sprintf("%s.yaml", username)))
			}

			delete(newOrgUsers, username)
		}
	}
	for username, user := range newOrgUsers {
		// new user
		file, err := fs.Create(filepath.Join(usersOrgPath, fmt.Sprintf("%s.yaml", username)))
		if err != nil {
			errorCollection.AddError(err)
			return nil, nil
		}
		defer file.Close()

		encoder := yaml.NewEncoder(file)
		encoder.SetIndent(2)
		err = encoder.Encode(user)
		if err != nil {
			errorCollection.AddError(err)
			return nil, nil
		}
		updatedusers = append(updatedusers, filepath.Join(usersOrgPath, fmt.Sprintf("%s.yaml", username)))
	}
	return deletedusers, updatedusers
}

// return true if some changes were done
func (g *GoliacLocalImpl) SyncUsersAndTeams(ctx context.Context, repoconfig *config.RepositoryConfig, userplugin UserSyncPlugin, accesstoken string, dryrun bool, force bool, feedback observability.RemoteObservability, errorCollector *observability.ErrorCollection) bool {
	var childSpan trace.Span
	if config.Config.OpenTelemetryEnabled {
		_, childSpan = otel.GetTracerProvider().Tracer("goliac").Start(ctx, "SyncUsersAndTeams")
		defer childSpan.End()
	}
	if g.repo == nil {
		errorCollector.AddError(fmt.Errorf("git repository not cloned"))
		return false
	}
	w, err := g.repo.Worktree()
	if err != nil {
		errorCollector.AddError(err)
		return false
	}

	//
	// let's update org users
	//

	// Parse all the users in the <orgDirectory>/org-users directory
	deletedusers, addedusers := syncUsersViaUserPlugin(repoconfig, w.Filesystem, userplugin, feedback, errorCollector)
	if errorCollector.HasErrors() {
		return false
	}

	//
	// let's update teams
	//

	g.loadUsers(w.Filesystem, errorCollector)
	if errorCollector.HasErrors() {
		return false
	}

	teamschanged, err := entity.ReadAndAdjustTeamDirectory(w.Filesystem, "teams", g.users)
	if err != nil {
		errorCollector.AddError(err)
		return false
	}

	// check if we have too many changesets
	if !force && len(teamschanged)+len(deletedusers)+len(addedusers) > repoconfig.MaxChangesets {
		errorCollector.AddError(fmt.Errorf("too many changesets (%d) to commit. Please increase max_changesets in goliac.yaml", len(teamschanged)+len(deletedusers)+len(addedusers)))
		return false
	}

	//
	// let's commit
	//
	if len(teamschanged) > 0 || len(deletedusers) > 0 || len(addedusers) > 0 {

		logrus.Info("some users and/or teams must be commited")

		for _, u := range deletedusers {
			logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "remove_user_from_repository"}).Infof("user: %s", u)
			_, err = w.Remove(u)
			if err != nil {
				errorCollector.AddError(err)
				return false
			}
		}

		for _, u := range addedusers {
			logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "add_user_to_repository"}).Infof("user: %s", u)
			_, err = w.Add(u)
			if err != nil {
				errorCollector.AddError(err)
				return false
			}
		}

		for _, t := range teamschanged {
			logrus.WithFields(map[string]interface{}{"dryrun": dryrun, "command": "update_team_to_repository"}).Infof("team: %s", t)
			_, err = w.Add(t)
			if err != nil {
				errorCollector.AddError(err)
				return false
			}
		}

		_, err = w.Commit("update teams and users", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Goliac",
				Email: config.Config.GoliacEmail,
				When:  time.Now(),
			},
		})

		if err != nil {
			errorCollector.AddError(err)
			return false
		}

		if dryrun {
			return false
		}

		// Now push the tag to the remote repository
		auth := &http.BasicAuth{
			Username: "x-access-token", // This can be anything except an empty string
			Password: accesstoken,
		}

		err = g.repo.Push(&git.PushOptions{
			RemoteName: "origin",
			Auth:       auth,
		})

		if err != nil {
			errorCollector.AddError(err)
		}
		return true
	}
	return false
}

/*
 * Load the goliac organization from Github (after the repository has been cloned)
 * - read the organization files
 * - validate the organization
 */
func (g *GoliacLocalImpl) LoadAndValidate(errorCollection *observability.ErrorCollection) {
	if g.repo == nil {
		errorCollection.AddError(fmt.Errorf("the repository has not been cloned. Did you called .Clone()?"))
		return
	}

	// read the organization files

	w, err := g.repo.Worktree()
	if err != nil {
		errorCollection.AddError(err)
		return
	}
	g.LoadAndValidateLocal(w.Filesystem, errorCollection)
}

func (g *GoliacLocalImpl) loadUsers(fs billy.Filesystem, errorCollection *observability.ErrorCollection) {

	// Parse all the users in the <orgDirectory>/protected-users directory
	protectedUsers := entity.ReadUserDirectory(fs, filepath.Join("users", "protected"), errorCollection)

	g.users = protectedUsers

	// Parse all the users in the <orgDirectory>/org-users directory
	orgUsers := entity.ReadUserDirectory(fs, filepath.Join("users", "org"), errorCollection)

	// not users? not good
	if orgUsers == nil {
		return
	}

	for k, v := range orgUsers {
		g.users[k] = v
	}

	// Parse all the users in the <orgDirectory>/external-users directory
	externalUsers := entity.ReadUserDirectory(fs, filepath.Join("users", "external"), errorCollection)
	g.externalUsers = externalUsers

	rulesets := entity.ReadRuleSetDirectory(fs, filepath.Join("rulesets"), errorCollection)
	g.rulesets = rulesets
}

/**
 * readOrganization reads all the organization files and returns
 * - a slice of errors that must stop the vlidation process
 * - a slice of warning that must not stop the validation process
 */
func (g *GoliacLocalImpl) LoadAndValidateLocal(fs billy.Filesystem, errorCollection *observability.ErrorCollection) {
	g.loadUsers(fs, errorCollection)

	if errorCollection.HasErrors() {
		return
	}

	// Parse all the teams in the <orgDirectory>/teams directory
	teams := entity.ReadTeamDirectory(fs, "teams", g.users, errorCollection)
	g.teams = teams

	// Parse all repositories in the <orgDirectory>/teams/<teamname> directories
	repos := entity.ReadRepositories(fs, "archived", "teams", g.teams, g.externalUsers, errorCollection)
	g.repositories = repos

	rulesets := entity.ReadRuleSetDirectory(fs, "rulesets", errorCollection)
	g.rulesets = rulesets

	logrus.Debugf("Nb local users: %d", len(g.users))
	logrus.Debugf("Nb local external users: %d", len(g.externalUsers))
	logrus.Debugf("Nb local teams: %d", len(g.teams))
	logrus.Debugf("Nb local repositories: %d", len(g.repositories))
}
