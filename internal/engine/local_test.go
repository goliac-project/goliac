package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/util"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/goliac-project/goliac/internal/config"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/goliac-project/goliac/internal/utils"
	"github.com/stretchr/testify/assert"
)

func createBasicStructure(fs billy.Filesystem) error {
	err := utils.WriteFile(fs, "goliac.yaml", []byte(`
`), 0644)
	if err != nil {
		return err
	}
	// Create users
	err = fs.MkdirAll("users/org", 0755)
	if err != nil {
		return err
	}

	err = utils.WriteFile(fs, "users/org/user1.yaml", []byte(`
apiVersion: v1
kind: User
name: user1
spec:
  githubID: github1
`), 0644)
	if err != nil {
		return err
	}

	err = utils.WriteFile(fs, "users/org/user2.yaml", []byte(`
apiVersion: v1
kind: User
name: user2
spec:
  githubID: github2
`), 0644)
	if err != nil {
		return err
	}

	// Create teams
	err = fs.MkdirAll("teams/team1", 0755)
	if err != nil {
		return err
	}

	err = utils.WriteFile(fs, "teams/team1/team.yaml", []byte(`
apiVersion: v1
kind: Team
name: team1
spec:
  owners:
  - user1
  - user2
`), 0644)
	if err != nil {
		return err
	}

	// Create repositories
	err = utils.WriteFile(fs, "projects/repo1.yaml", []byte(`
apiVersion: v1
kind: Repository
name: repo1
`), 0644)
	if err != nil {
		return err
	}
	return nil
}

func TestRepository(t *testing.T) {

	// happy path
	t.Run("happy path: local directory", func(t *testing.T) {
		fs := memfs.New()
		createBasicStructure(fs)
		g := NewGoliacLocalImpl()
		errorCollector := observability.NewErrorCollection()
		g.LoadAndValidateLocal(fs, errorCollector)

		assert.False(t, errorCollector.HasErrors())
		assert.False(t, errorCollector.HasWarns())
	})

	t.Run("happy path: local repository", func(t *testing.T) {
		fs := memfs.New()
		storer := memory.NewStorage()

		// Initializes a new repository
		r, err := git.Init(storer, fs)
		assert.Nil(t, err)

		createBasicStructure(fs)
		w, err := r.Worktree()
		assert.Nil(t, err)
		_, err = w.Add(".")
		assert.Nil(t, err)

		_, err = w.Commit("initial commit", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "goliac",
				Email: "goliac@goliac-project.com",
				When:  time.Now(),
			},
		})
		assert.Nil(t, err)

		// Verify the commit
		_, err = r.Head()
		assert.Nil(t, err)

		g := &GoliacLocalImpl{
			teams:         map[string]*entity.Team{},
			repositories:  map[string]*entity.Repository{},
			users:         map[string]*entity.User{},
			externalUsers: map[string]*entity.User{},
			repo:          r,
		}

		errorCollector := observability.NewErrorCollection()
		g.LoadAndValidate(errorCollector)

		assert.False(t, errorCollector.HasErrors())
		assert.False(t, errorCollector.HasWarns())
	})
}

type ScrambleUserSync struct {
}

func (p *ScrambleUserSync) UpdateUsers(repoconfig *config.RepositoryConfig, fs billy.Filesystem, orguserdirrectorypath string, feedback observability.RemoteObservability, errorCollector *observability.ErrorCollection) map[string]*entity.User {
	users := make(map[string]*entity.User)

	// added
	foobar := &entity.User{}
	foobar.ApiVersion = "v1"
	foobar.Kind = "User"
	foobar.Name = "foobar"
	foobar.Spec.GithubID = "foobar"
	users["foobar"] = foobar

	// updated
	user1 := &entity.User{}
	user1.ApiVersion = "v1"
	user1.Kind = "User"
	user1.Name = "user1"
	user1.Spec.GithubID = "user1"
	users["user1"] = foobar

	return users
}

type ErroreUserSync struct {
}

func (p *ErroreUserSync) UpdateUsers(repoconfig *config.RepositoryConfig, fs billy.Filesystem, orguserdirrectorypath string, feedback observability.RemoteObservability, errorCollector *observability.ErrorCollection) map[string]*entity.User {
	errorCollector.AddError(fmt.Errorf("unknown error"))
	return nil
}

type UserSyncPluginNoop struct{}

func NewUserSyncPluginNoop() UserSyncPlugin {
	return &UserSyncPluginNoop{}
}

func (p *UserSyncPluginNoop) UpdateUsers(repoconfig *config.RepositoryConfig, fs billy.Filesystem, orguserdirrectorypath string, feedback observability.RemoteObservability, errorCollector *observability.ErrorCollection) map[string]*entity.User {
	users := entity.ReadUserDirectory(fs, orguserdirrectorypath, errorCollector)
	return users
}

func TestSyncUsersViaUserPlugin(t *testing.T) {

	t.Run("happy path: noop", func(t *testing.T) {
		fs := memfs.New()
		createBasicStructure(fs)

		errorCollector := observability.NewErrorCollection()
		removed, added := syncUsersViaUserPlugin(&config.RepositoryConfig{}, fs, &UserSyncPluginNoop{}, nil, errorCollector)

		assert.False(t, errorCollector.HasErrors())
		assert.Equal(t, 0, len(removed))
		assert.Equal(t, 0, len(added))
	})

	t.Run("happy path: replcae with foobar", func(t *testing.T) {
		fs := memfs.New()
		createBasicStructure(fs)

		errorCollector := observability.NewErrorCollection()
		removed, added := syncUsersViaUserPlugin(&config.RepositoryConfig{}, fs, &ScrambleUserSync{}, nil, errorCollector)

		assert.False(t, errorCollector.HasErrors())
		assert.Equal(t, 1, len(removed))
		assert.Equal(t, 2, len(added))
		assert.Equal(t, "users/org/user1.yaml", added[0])
		assert.Equal(t, "users/org/foobar.yaml", added[1])
	})
	t.Run("not happy path: dealing with usersync error", func(t *testing.T) {
		fs := memfs.New()
		createBasicStructure(fs)

		errorCollector := observability.NewErrorCollection()
		syncUsersViaUserPlugin(&config.RepositoryConfig{}, fs, &ErroreUserSync{}, nil, errorCollector)

		assert.True(t, errorCollector.HasErrors())
	})
}

func createEmptyTeamRepo(src billy.Filesystem) (*git.Repository, error) {
	masterStorer := filesystem.NewStorage(src, cache.NewObjectLRUDefault())

	// Create a fake bare repository
	repo, err := git.Init(masterStorer, src)
	if err != nil {
		return nil, err
	}

	//
	// Create a new file in the working directory
	//

	// goliac.yaml
	err = util.WriteFile(src, "goliac.yaml", []byte(`
admin_team: github-admins

rulesets:
  - pattern: .*
    ruleset: default

max_changesets: 50
archive_on_delete: true

destructive_operations:
  repositories: false
  teams: false
  users: false
  rulesets: false

usersync:
  plugin: noop
`), 0644)
	if err != nil {
		return nil, err
	}

	// Create users
	err = src.MkdirAll("users/org", 0755)
	if err != nil {
		return nil, err
	}
	err = util.WriteFile(src, "users/org/admin.yaml", []byte(`
apiVersion: v1
kind: User
name: admin
spec:
  githubID: admin
`), 0644)
	if err != nil {
		return nil, err
	}

	// Create teams
	err = src.MkdirAll("teams/github-admins", 0755)
	if err != nil {
		return nil, err
	}
	err = util.WriteFile(src, "teams/github-admins/team.yaml", []byte(`
apiVersion: v1
kind: Team
name: github-admins
spec:
  owners:
  - admin
`), 0644)
	if err != nil {
		return nil, err
	}

	// Create repositories
	err = util.WriteFile(src, "teams/github-admins/repo1.yaml", []byte(`
apiVersion: v1
kind: Repository
name: repo1
`), 0644)
	if err != nil {
		return nil, err
	}

	// commit
	worktree, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	_, err = worktree.Add(".")
	if err != nil {
		return nil, err
	}
	hash, err := worktree.Commit("Initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Goliac",
			Email: "goliac@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		return nil, err
	}

	// tag as v0.1.0
	_, err = repo.CreateTag("v0.1.0", hash, nil)
	if err != nil {
		return nil, err
	}

	// let's add a new commit after
	err = util.WriteFile(src, "teams/github-admins/repo2.yaml", []byte(`
apiVersion: v1
kind: Repository
name: repo2
`), 0644)
	if err != nil {
		return nil, err
	}

	// commit
	worktree, err = repo.Worktree()
	if err != nil {
		return nil, err
	}
	_, err = worktree.Add(".")
	if err != nil {
		return nil, err
	}
	_, err = worktree.Commit("add another repo", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Goliac",
			Email: "goliac@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		return nil, err
	}

	return repo, nil
}

func helperCreateAndClone(root billy.Filesystem, src billy.Filesystem, target billy.Filesystem) (*git.Repository, *git.Repository, error) {
	repo, err := createEmptyTeamRepo(src)
	if err != nil {
		return nil, nil, err
	}

	//
	// trying to clone it
	//

	loader := server.NewFilesystemLoader(root)
	client.InstallProtocol("inmemory", server.NewClient(loader))

	dotGit, err := target.Chroot(".git")
	if err != nil {
		return nil, nil, err
	}
	storer := filesystem.NewStorage(dotGit, cache.NewObjectLRUDefault())

	clonedRepo, err := git.Clone(storer, target, &git.CloneOptions{
		URL:      "inmemory:///src",
		Progress: nil,
	})
	if err != nil {
		return nil, nil, err
	}
	return repo, clonedRepo, nil
}

func TestPushTag(t *testing.T) {
	t.Run("push a tag into an upstream git repository", func(t *testing.T) {
		rootfs := memfs.New()
		src, _ := rootfs.Chroot("/src")
		target, _ := src.Chroot("/target")

		repo, clonedRepo, err := helperCreateAndClone(rootfs, src, target)
		assert.Nil(t, err)
		assert.NotNil(t, repo)
		assert.NotNil(t, clonedRepo)

		//
		// push tag
		//
		g := GoliacLocalImpl{
			teams:         map[string]*entity.Team{},
			repositories:  map[string]*entity.Repository{},
			users:         map[string]*entity.User{},
			externalUsers: map[string]*entity.User{},
			rulesets:      map[string]*entity.RuleSet{},
			repo:          clonedRepo,
		}

		// create a commit
		utils.WriteFile(target, "test.txt", []byte(`test`), 0644)
		w, err := clonedRepo.Worktree()
		assert.Nil(t, err)
		_, err = w.Add(".")
		assert.Nil(t, err)

		hash, err := w.Commit("new commit", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Goliac",
				Email: config.Config.GoliacEmail,
				When:  time.Now(),
			},
		})
		assert.Nil(t, err)

		err = g.PushTag("v1.0.0", hash, "none")
		assert.Nil(t, err)

		// check the tag
		tag, err := clonedRepo.Tag("v1.0.0")
		assert.Nil(t, err)
		assert.NotNil(t, tag)

		// read tag on the master repository
		tag, err = repo.Tag("v1.0.0")
		assert.Nil(t, err)
		assert.NotNil(t, tag)
	})
}

func TestBasicGitops(t *testing.T) {
	t.Run("clone", func(t *testing.T) {
		rootfs := memfs.New()
		src, _ := rootfs.Chroot("/src")
		target, _ := src.Chroot("/target")

		repo, clonedRepo, err := helperCreateAndClone(rootfs, src, target)
		assert.Nil(t, err)
		assert.NotNil(t, repo)
		assert.NotNil(t, clonedRepo)

	})

	t.Run("CheckoutCommit", func(t *testing.T) {
		rootfs := memfs.New()
		src, _ := rootfs.Chroot("/src")
		target, _ := src.Chroot("/target")

		repo, clonedRepo, err := helperCreateAndClone(rootfs, src, target)
		assert.Nil(t, err)
		assert.NotNil(t, repo)
		assert.NotNil(t, clonedRepo)

		// get commits
		g := GoliacLocalImpl{
			teams:         map[string]*entity.Team{},
			repositories:  map[string]*entity.Repository{},
			users:         map[string]*entity.User{},
			externalUsers: map[string]*entity.User{},
			rulesets:      map[string]*entity.RuleSet{},
			repo:          clonedRepo,
		}

		commits, err := g.ListCommitsFromTag("v0.1.0")
		assert.Nil(t, err)
		assert.Equal(t, 1, len(commits))

		//
		// checkout the commit
		//
		err = g.CheckoutCommit(commits[0])
		assert.Nil(t, err)

		// check the number of files in the 'teams/github-admins' directory
		files, err := target.ReadDir("teams/github-admins")
		assert.Nil(t, err)
		assert.Equal(t, 3, len(files))

		//
		// checkout v0.1.0
		//
		tagRef, err := clonedRepo.Tag("v0.1.0")
		assert.Nil(t, err)
		tagObject, err := repo.TagObject(tagRef.Hash())
		var commit *object.Commit
		if err == plumbing.ErrObjectNotFound {
			// If the tag is lightweight, the reference points directly to the commit
			commit1, err := repo.CommitObject(tagRef.Hash())
			assert.Nil(t, err)
			commit = commit1
		} else {
			// If the tag is annotated, resolve the commit it points to
			commit2, err := tagObject.Commit()
			assert.Nil(t, err)
			commit = commit2
		}

		err = g.CheckoutCommit(commit)
		assert.Nil(t, err)
		files, err = target.ReadDir("teams/github-admins")
		assert.Nil(t, err)
		assert.Equal(t, 2, len(files))

		//
		// checkout again the latest commit
		//
		err = g.CheckoutCommit(commits[0])
		assert.Nil(t, err)

		// check the number of files in the 'teams/github-admins' directory
		files, err = target.ReadDir("teams/github-admins")
		assert.Nil(t, err)
		assert.Equal(t, 3, len(files))
	})

	t.Run("CheckoutCommit", func(t *testing.T) {
		rootfs := memfs.New()
		src, _ := rootfs.Chroot("/src")
		target, _ := src.Chroot("/target")

		repo, clonedRepo, err := helperCreateAndClone(rootfs, src, target)
		assert.Nil(t, err)
		assert.NotNil(t, repo)
		assert.NotNil(t, clonedRepo)

		// get commits
		g := GoliacLocalImpl{
			teams:         map[string]*entity.Team{},
			repositories:  map[string]*entity.Repository{},
			users:         map[string]*entity.User{},
			externalUsers: map[string]*entity.User{},
			rulesets:      map[string]*entity.RuleSet{},
			repo:          clonedRepo,
		}

		head, err := g.GetHeadCommit()
		assert.Nil(t, err)

		err = g.CheckoutCommit(head)
		assert.Nil(t, err)
		files, err := target.ReadDir("teams/github-admins")
		assert.Nil(t, err)
		assert.Equal(t, 3, len(files)) // it should be 3 because we have 3 files in the 'teams/github-admins' directory
	})

	t.Run("LoadRepoConfig", func(t *testing.T) {
		rootfs := memfs.New()
		src, _ := rootfs.Chroot("/src")
		target, _ := src.Chroot("/target")

		repo, clonedRepo, err := helperCreateAndClone(rootfs, src, target)
		assert.Nil(t, err)
		assert.NotNil(t, repo)
		assert.NotNil(t, clonedRepo)

		// get commits
		g := GoliacLocalImpl{
			teams:         map[string]*entity.Team{},
			repositories:  map[string]*entity.Repository{},
			users:         map[string]*entity.User{},
			externalUsers: map[string]*entity.User{},
			rulesets:      map[string]*entity.RuleSet{},
			repo:          clonedRepo,
		}

		goliacConfig, err := g.LoadRepoConfig()
		assert.Nil(t, err)
		assert.NotNil(t, goliacConfig)
		assert.Equal(t, "github-admins", goliacConfig.AdminTeam)
		assert.Equal(t, 50, goliacConfig.MaxChangesets)
		assert.Equal(t, true, goliacConfig.ArchiveOnDelete)
		assert.Equal(t, false, goliacConfig.DestructiveOperations.AllowDestructiveRepositories)
		assert.Equal(t, false, goliacConfig.DestructiveOperations.AllowDestructiveTeams)
		assert.Equal(t, false, goliacConfig.DestructiveOperations.AllowDestructiveUsers)
		assert.Equal(t, false, goliacConfig.DestructiveOperations.AllowDestructiveRulesets)
		assert.Equal(t, "noop", goliacConfig.UserSync.Plugin)
	})

	t.Run("codeowners_regenerate", func(t *testing.T) {
		rootfs := memfs.New()
		src, _ := rootfs.Chroot("/src")
		target, _ := src.Chroot("/target")

		repo, clonedRepo, err := helperCreateAndClone(rootfs, src, target)
		assert.Nil(t, err)
		assert.NotNil(t, repo)
		assert.NotNil(t, clonedRepo)

		// get commits
		adminTeam := entity.Team{}
		adminTeam.ApiVersion = "v1"
		adminTeam.Kind = "Team"
		adminTeam.Name = "github-admins"
		adminTeam.Spec.Owners = []string{"admin"}

		g := GoliacLocalImpl{
			teams: map[string]*entity.Team{
				"github-admins": &adminTeam,
			},
			repositories:  map[string]*entity.Repository{},
			users:         map[string]*entity.User{},
			externalUsers: map[string]*entity.User{},
			rulesets:      map[string]*entity.RuleSet{},
			repo:          clonedRepo,
		}

		content := g.codeowners_regenerate("github-admins", "goliac-project")

		// check the content of the CODEOWNERS file
		assert.Equal(t, "# DO NOT MODIFY THIS FILE MANUALLY\n* @goliac-project/github-admins\n/teams/github-admins/* @goliac-project/github-admins"+config.Config.GoliacTeamOwnerSuffix+" @goliac-project/github-admins\n", content)
	})

	t.Run("codeowners_regenerate with a team path with a space", func(t *testing.T) {
		rootfs := memfs.New()
		src, _ := rootfs.Chroot("/src")
		target, _ := src.Chroot("/target")

		repo, clonedRepo, err := helperCreateAndClone(rootfs, src, target)
		assert.Nil(t, err)
		assert.NotNil(t, repo)
		assert.NotNil(t, clonedRepo)

		// get commits
		adminTeam := entity.Team{}
		adminTeam.ApiVersion = "v1"
		adminTeam.Kind = "Team"
		adminTeam.Name = "github admins"
		adminTeam.Spec.Owners = []string{"admin"}

		g := GoliacLocalImpl{
			teams: map[string]*entity.Team{
				"github admins": &adminTeam,
			},
			repositories:  map[string]*entity.Repository{},
			users:         map[string]*entity.User{},
			externalUsers: map[string]*entity.User{},
			rulesets:      map[string]*entity.RuleSet{},
			repo:          clonedRepo,
		}

		content := g.codeowners_regenerate("github admins", "goliac-project")

		// check the content of the CODEOWNERS file
		assert.Equal(t, "# DO NOT MODIFY THIS FILE MANUALLY\n* @goliac-project/github-admins\n/teams/github\\ admins/* @goliac-project/github-admins"+config.Config.GoliacTeamOwnerSuffix+" @goliac-project/github-admins\n", content)
	})

	t.Run("codeowners_regenerate with a parent", func(t *testing.T) {
		rootfs := memfs.New()
		src, _ := rootfs.Chroot("/src")
		target, _ := src.Chroot("/target")

		repo, clonedRepo, err := helperCreateAndClone(rootfs, src, target)
		assert.Nil(t, err)
		assert.NotNil(t, repo)
		assert.NotNil(t, clonedRepo)

		// get commits
		adminTeam := entity.Team{}
		adminTeam.ApiVersion = "v1"
		adminTeam.Kind = "Team"
		adminTeam.Name = "github-admins"
		adminTeam.Spec.Owners = []string{"admin"}

		subTeam := entity.Team{}
		subTeam.ApiVersion = "v1"
		subTeam.Kind = "Team"
		subTeam.Name = "subteam"
		subTeam.Spec.Owners = []string{"admin"}
		parentTeam := "github-admins"
		subTeam.ParentTeam = &parentTeam

		g := GoliacLocalImpl{
			teams: map[string]*entity.Team{
				"github-admins": &adminTeam,
				"subteam":       &subTeam,
			},
			repositories:  map[string]*entity.Repository{},
			users:         map[string]*entity.User{},
			externalUsers: map[string]*entity.User{},
			rulesets:      map[string]*entity.RuleSet{},
			repo:          clonedRepo,
		}

		content := g.codeowners_regenerate("github-admins", "goliac-project")

		// check the content of the CODEOWNERS file
		assert.Equal(t, "# DO NOT MODIFY THIS FILE MANUALLY\n* @goliac-project/github-admins\n/teams/github-admins/* @goliac-project/github-admins-goliac-owners @goliac-project/github-admins\n/teams/github-admins/subteam/* @goliac-project/subteam-goliac-owners @goliac-project/github-admins\n", content)
	})
}

func TestGoliacLocalImpl(t *testing.T) {
	t.Run("ArchiveRepos", func(t *testing.T) {
		rootfs := memfs.New()
		src, _ := rootfs.Chroot("/src")
		target, _ := src.Chroot("/target")

		repo, clonedRepo, err := helperCreateAndClone(rootfs, src, target)
		assert.Nil(t, err)
		assert.NotNil(t, repo)
		assert.NotNil(t, clonedRepo)

		// get commits
		g := GoliacLocalImpl{
			teams:         map[string]*entity.Team{},
			repositories:  map[string]*entity.Repository{},
			users:         map[string]*entity.User{},
			externalUsers: map[string]*entity.User{},
			rulesets:      map[string]*entity.RuleSet{},
			repo:          clonedRepo,
		}

		// archive the repository 'repo4' (that is not part of the cloned repository)
		err = g.UpdateRepos([]string{"repo4"}, map[string]*entity.Repository{}, "none", "master", "foobar")
		assert.Nil(t, err)

		// check the content of the 'archived/repo1.yaml' file
		content, err := utils.ReadFile(target, "archived/repo4.yaml")
		assert.Nil(t, err)
		assert.Equal(t, "apiVersion: v1\nkind: Repository\nname: repo4\n", string(content))
	})

	// t.Run("CreateRepos", func(t *testing.T) {
	// 	rootfs := memfs.New()
	// 	src, _ := rootfs.Chroot("/src")
	// 	target, _ := src.Chroot("/target")

	// 	repo, clonedRepo, err := helperCreateAndClone(rootfs, src, target)
	// 	assert.Nil(t, err)
	// 	assert.NotNil(t, repo)
	// 	assert.NotNil(t, clonedRepo)

	// 	// get commits
	// 	g := GoliacLocalImpl{
	// 		teams:         map[string]*entity.Team{},
	// 		repositories:  map[string]*entity.Repository{},
	// 		users:         map[string]*entity.User{},
	// 		externalUsers: map[string]*entity.User{},
	// 		rulesets:      map[string]*entity.RuleSet{},
	// 		repo:          clonedRepo,
	// 	}

	// 	// create a new repository
	// 	newrepo := entity.Repository{}
	// 	newrepo.ApiVersion = "v1"
	// 	newrepo.Kind = "Repository"
	// 	newrepo.Name = "newrepo"
	// 	err = g.UpdateRepos([]string{}, map[string]*entity.Repository{}, map[string]*entity.Repository{"teams/github-admins": &newrepo}, "none", "master", "foobar")
	// 	assert.Nil(t, err)

	// 	// check the content of the 'archived/repo1.yaml' file
	// 	content, err := utils.ReadFile(target, "teams/github-admins/newrepo.yaml")
	// 	assert.Nil(t, err)
	// 	assert.Equal(t, "apiVersion: v1\nkind: Repository\nname: newrepo\n", string(content))
	// })

	t.Run("UpdateAndCommitCodeOwners", func(t *testing.T) {
		rootfs := memfs.New()
		src, _ := rootfs.Chroot("/src")
		target, _ := src.Chroot("/target")

		repo, clonedRepo, err := helperCreateAndClone(rootfs, src, target)
		assert.Nil(t, err)
		assert.NotNil(t, repo)
		assert.NotNil(t, clonedRepo)

		// get commits
		adminTeam := entity.Team{}
		adminTeam.ApiVersion = "v1"
		adminTeam.Kind = "Team"
		adminTeam.Name = "github-admins"
		adminTeam.Spec.Owners = []string{"admin"}

		g := GoliacLocalImpl{
			teams: map[string]*entity.Team{
				"github-admins": &adminTeam,
			},
			repositories:  map[string]*entity.Repository{},
			users:         map[string]*entity.User{},
			externalUsers: map[string]*entity.User{},
			rulesets:      map[string]*entity.RuleSet{},
			repo:          clonedRepo,
		}

		goliacConfig, err := g.LoadRepoConfig()
		assert.Nil(t, err)
		assert.NotNil(t, goliacConfig)

		// update and commit the CODEOWNERS file
		err = g.UpdateAndCommitCodeOwners(context.TODO(), goliacConfig, false, "none", "master", "foobar", "goliac-project")
		assert.Nil(t, err)

		// check the content of the CODEOWNERS file
		content, err := utils.ReadFile(target, ".github/CODEOWNERS")
		assert.Nil(t, err)
		assert.Equal(t, "# DO NOT MODIFY THIS FILE MANUALLY\n* @goliac-project/github-admins\n/teams/github-admins/* @goliac-project/github-admins"+config.Config.GoliacTeamOwnerSuffix+" @goliac-project/github-admins\n", string(content))
	})

	t.Run("SyncUsersAndTeams", func(t *testing.T) {
		rootfs := memfs.New()
		src, _ := rootfs.Chroot("/src")
		target, _ := src.Chroot("/target")

		repo, clonedRepo, err := helperCreateAndClone(rootfs, src, target)
		assert.Nil(t, err)
		assert.NotNil(t, repo)
		assert.NotNil(t, clonedRepo)

		// get commits
		g := GoliacLocalImpl{
			teams:         map[string]*entity.Team{},
			repositories:  map[string]*entity.Repository{},
			users:         map[string]*entity.User{},
			externalUsers: map[string]*entity.User{},
			rulesets:      map[string]*entity.RuleSet{},
			repo:          clonedRepo,
		}

		goliacConfig, err := g.LoadRepoConfig()
		assert.Nil(t, err)
		assert.NotNil(t, goliacConfig)

		mockUserPlugin := &UserSyncPluginMock{}

		// sync users and teams
		errorCollector := observability.NewErrorCollection()
		change := g.SyncUsersAndTeams(context.TODO(), goliacConfig, mockUserPlugin, "none", false, false, nil, errorCollector)
		assert.False(t, errorCollector.HasErrors())
		assert.True(t, change)

		// there should be a new user: foobar
		// check the content of the 'users/org/foobar.yaml' file
		content, err := utils.ReadFile(target, "users/org/foobar.yaml")
		assert.Nil(t, err)
		assert.Equal(t, "apiVersion: v1\nkind: User\nname: foobar\nspec:\n  githubID: foobar\n", string(content))
	})
}

type UserSyncPluginMock struct {
}

func (us *UserSyncPluginMock) UpdateUsers(repoconfig *config.RepositoryConfig, fs billy.Filesystem, orguserdirrectorypath string, feedback observability.RemoteObservability, errorCollector *observability.ErrorCollection) map[string]*entity.User {
	// let's return the current one (admin) + a new one
	users := make(map[string]*entity.User)
	users["admin"] = &entity.User{}
	users["admin"].ApiVersion = "v1"
	users["admin"].Kind = "User"
	users["admin"].Name = "admin"
	users["admin"].Spec.GithubID = "admin"

	users["foobar"] = &entity.User{}
	users["foobar"].ApiVersion = "v1"
	users["foobar"].Kind = "User"
	users["foobar"].Name = "foobar"
	users["foobar"].Spec.GithubID = "foobar"

	return users
}
