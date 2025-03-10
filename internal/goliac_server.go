package internal

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-openapi/loads"
	"github.com/go-openapi/runtime/middleware"
	"github.com/goliac-project/goliac/internal/config"
	"github.com/goliac-project/goliac/internal/engine"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/goliac-project/goliac/internal/notification"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/goliac-project/goliac/swagger_gen/models"
	"github.com/goliac-project/goliac/swagger_gen/restapi"
	"github.com/goliac-project/goliac/swagger_gen/restapi/operations"
	"github.com/goliac-project/goliac/swagger_gen/restapi/operations/app"
	"github.com/goliac-project/goliac/swagger_gen/restapi/operations/health"
	"github.com/gosimple/slug"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

/*
 * GoliacServer is here to run as a serve that
 * - sync/reconciliate periodically
 * - provide a REST API server
 */
type GoliacServer interface {
	Serve()
	GetLiveness(health.GetLivenessParams) middleware.Responder
	GetReadiness(health.GetReadinessParams) middleware.Responder
	PostFlushCache(app.PostFlushCacheParams) middleware.Responder
	PostResync(app.PostResyncParams) middleware.Responder
	GetStatus(app.GetStatusParams) middleware.Responder

	GetUsers(app.GetUsersParams) middleware.Responder
	GetUser(app.GetUserParams) middleware.Responder
	GetCollaborators(app.GetCollaboratorsParams) middleware.Responder
	GetCollaborator(app.GetCollaboratorParams) middleware.Responder
	GetTeams(app.GetTeamsParams) middleware.Responder
	GetTeam(app.GetTeamParams) middleware.Responder
	GetRepositories(app.GetRepositoriesParams) middleware.Responder
	GetRepository(app.GetRepositoryParams) middleware.Responder
	GetStatistics(app.GetStatiticsParams) middleware.Responder
	GetUnmanaged(app.GetUnmanagedParams) middleware.Responder

	PostExternalCreateRepository(app.PostExternalCreateRepositoryParams) middleware.Responder
}

type GoliacServerImpl struct {
	goliac              Goliac
	applyLobbyMutex     sync.Mutex
	applyLobbyCond      *sync.Cond
	applyCurrent        bool
	applyLobby          bool
	ready               bool // when the server has finished to load the local configuration
	lastSyncTime        *time.Time
	lastSyncError       error
	lastSyncWarnings    string // all the warnings that happened during the last sync (sorted)
	detailedErrors      []error
	detailedWarnings    []observability.Warning
	syncInterval        int64 // in seconds time remaining between 2 sync
	notificationService notification.NotificationService
	lastStatistics      config.GoliacStatistics
	maxStatistics       config.GoliacStatistics
	lastTimeToApply     time.Duration
	maxTimeToApply      time.Duration
	lastUnmanaged       *engine.UnmanagedResources
}

func NewGoliacServer(goliac Goliac, notificationService notification.NotificationService) GoliacServer {

	server := GoliacServerImpl{
		goliac:              goliac,
		ready:               false,
		notificationService: notificationService,
	}
	server.applyLobbyCond = sync.NewCond(&server.applyLobbyMutex)

	return &server
}

func (g *GoliacServerImpl) GetUnmanaged(app.GetUnmanagedParams) middleware.Responder {
	if g.lastUnmanaged == nil {
		return app.NewGetUnmanagedOK().WithPayload(&models.Unmanaged{})
	} else {
		repos := make([]string, 0, len(g.lastUnmanaged.Repositories))
		for r := range g.lastUnmanaged.Repositories {
			repos = append(repos, r)
		}
		externallyManagedTeams := make([]string, 0, len(g.lastUnmanaged.Teams))
		for t := range g.lastUnmanaged.ExternallyManagedTeams {
			externallyManagedTeams = append(externallyManagedTeams, t)
		}
		teams := make([]string, 0, len(g.lastUnmanaged.Teams))
		for t := range g.lastUnmanaged.Teams {
			teams = append(teams, t)
		}
		users := make([]string, 0, len(g.lastUnmanaged.Users))
		for u := range g.lastUnmanaged.Users {
			users = append(users, u)
		}
		rulesets := make([]string, 0, len(g.lastUnmanaged.RuleSets))
		for r := range g.lastUnmanaged.RuleSets {
			rulesets = append(rulesets, r)
		}
		return app.NewGetUnmanagedOK().WithPayload(&models.Unmanaged{
			Repos:                  repos,
			ExternallyManagedTeams: externallyManagedTeams,
			Teams:                  teams,
			Users:                  users,
			Rulesets:               rulesets,
		})
	}
}

func (g *GoliacServerImpl) GetStatistics(app.GetStatiticsParams) middleware.Responder {
	return app.NewGetStatiticsOK().WithPayload(&models.Statistics{
		LastTimeToApply:     g.lastTimeToApply.Truncate(time.Second).String(),
		LastGithubAPICalls:  int64(g.lastStatistics.GithubApiCalls),
		LastGithubThrottled: int64(g.lastStatistics.GithubThrottled),
		MaxTimeToApply:      g.maxTimeToApply.Truncate(time.Second).String(),
		MaxGithubAPICalls:   int64(g.maxStatistics.GithubApiCalls),
		MaxGithubThrottled:  int64(g.maxStatistics.GithubThrottled),
	})
}

func (g *GoliacServerImpl) GetRepositories(app.GetRepositoriesParams) middleware.Responder {
	local := g.goliac.GetLocal()
	repositories := make(models.Repositories, 0, len(local.Repositories()))

	for _, r := range local.Repositories() {
		repo := models.Repository{
			Name:       r.Name,
			Visibility: r.Spec.Visibility,
			Archived:   r.Archived,
		}
		repositories = append(repositories, &repo)
	}

	return app.NewGetRepositoriesOK().WithPayload(repositories)
}

func (g *GoliacServerImpl) GetRepository(params app.GetRepositoryParams) middleware.Responder {
	local := g.goliac.GetLocal()

	repository, found := local.Repositories()[params.RepositoryID]
	if !found {
		message := fmt.Sprintf("Repository %s not found", params.RepositoryID)
		return app.NewGetRepositoryDefault(404).WithPayload(&models.Error{Message: &message})
	}

	teams := make([]*models.RepositoryDetailsTeamsItems0, 0)
	collaborators := make([]*models.RepositoryDetailsCollaboratorsItems0, 0)

	for _, r := range repository.Spec.Readers {
		team := models.RepositoryDetailsTeamsItems0{
			Name:   r,
			Access: "read",
		}
		teams = append(teams, &team)
	}

	if repository.Owner != nil {
		team := models.RepositoryDetailsTeamsItems0{
			Name:   *repository.Owner,
			Access: "write",
		}
		teams = append(teams, &team)
	}

	for _, w := range repository.Spec.Writers {
		team := models.RepositoryDetailsTeamsItems0{
			Name:   w,
			Access: "write",
		}
		teams = append(teams, &team)
	}

	for _, r := range repository.Spec.ExternalUserReaders {
		collaborator := models.RepositoryDetailsCollaboratorsItems0{
			Name:   r,
			Access: "read",
		}
		collaborators = append(collaborators, &collaborator)
	}

	for _, r := range repository.Spec.ExternalUserWriters {
		collaborator := models.RepositoryDetailsCollaboratorsItems0{
			Name:   r,
			Access: "write",
		}
		collaborators = append(collaborators, &collaborator)
	}

	repositoryDetails := models.RepositoryDetails{
		Name:                repository.Name,
		Visibility:          repository.Spec.Visibility,
		AutoMergeAllowed:    repository.Spec.AllowAutoMerge,
		DeleteBranchOnMerge: repository.Spec.DeleteBranchOnMerge,
		AllowUpdateBranch:   repository.Spec.AllowUpdateBranch,
		Archived:            repository.Archived,
		Teams:               teams,
		Collaborators:       collaborators,
	}

	return app.NewGetRepositoryOK().WithPayload(&repositoryDetails)
}

func (g *GoliacServerImpl) GetTeams(app.GetTeamsParams) middleware.Responder {
	teams := make(models.Teams, 0)

	local := g.goliac.GetLocal()
	remote := g.goliac.GetRemote()

	githubidToUser := make(map[string]string)
	for _, u := range local.Users() {
		githubidToUser[u.Spec.GithubID] = u.Name
	}

	for teamname, team := range local.Teams() {
		t := models.Team{
			Name:    teamname,
			Members: team.Spec.Members,
			Owners:  team.Spec.Owners,
			Path:    teamname,
		}

		// if the team is externally managed, we dont have the info locally
		// we need to get the members from the remote
		if team.Spec.ExternallyManaged {
			rteams := remote.Teams(context.TODO(), true)
			if rteams != nil {
				teamSlug := slug.Make(team.Name)
				if team, ok := rteams[teamSlug]; ok {
					for _, u := range team.Members {
						// u is the githubid
						if user, ok := githubidToUser[u]; ok {
							t.Owners = append(t.Owners, user)
						} else {
							t.Owners = append(t.Owners, "githubid:"+u)
						}
					}
				}
			}
		}

		// prevent any issue, but it shoudn't happen
		maxRec := 100
		for team.ParentTeam != nil && maxRec > 0 {
			parentName := *team.ParentTeam
			team = local.Teams()[parentName]
			t.Path = parentName + "/" + t.Path
			maxRec--
		}
		teams = append(teams, &t)

	}
	return app.NewGetTeamsOK().WithPayload(teams)
}

func (g *GoliacServerImpl) GetTeam(params app.GetTeamParams) middleware.Responder {
	local := g.goliac.GetLocal()

	team, found := local.Teams()[params.TeamID]
	if !found {
		message := fmt.Sprintf("Team %s not found", params.TeamID)
		return app.NewGetTeamDefault(404).WithPayload(&models.Error{Message: &message})
	}

	repos := make(map[string]*entity.Repository)
	for reponame, repo := range local.Repositories() {
		if repo.Owner != nil && *repo.Owner == params.TeamID {
			repos[reponame] = repo
		}
		for _, r := range repo.Spec.Readers {
			if r == params.TeamID {
				repos[reponame] = repo
				break
			}
		}
		for _, r := range repo.Spec.Writers {
			if r == params.TeamID {
				repos[reponame] = repo
				break
			}
		}
	}

	repositories := make([]*models.Repository, 0, len(repos))
	for reponame, repo := range repos {
		r := models.Repository{
			Name:                reponame,
			Archived:            repo.Archived,
			Visibility:          repo.Spec.Visibility,
			AutoMergeAllowed:    repo.Spec.AllowAutoMerge,
			DeleteBranchOnMerge: repo.Spec.DeleteBranchOnMerge,
			AllowUpdateBranch:   repo.Spec.AllowUpdateBranch,
		}
		repositories = append(repositories, &r)
	}

	teamDetails := models.TeamDetails{
		Owners:       make([]*models.TeamDetailsOwnersItems0, len(team.Spec.Owners)),
		Members:      make([]*models.TeamDetailsMembersItems0, len(team.Spec.Members)),
		Name:         team.Name,
		Repositories: repositories,
		Path:         team.Name,
	}

	recTeam := team
	// prevent any issue, but it shoudn't happen
	maxRec := 100
	for recTeam.ParentTeam != nil && maxRec > 0 {
		parentName := *recTeam.ParentTeam
		recTeam = local.Teams()[parentName]
		teamDetails.Path = parentName + "/" + teamDetails.Path
		maxRec--
	}

	for i, u := range team.Spec.Owners {
		if orgUser, ok := local.Users()[u]; ok {
			teamDetails.Owners[i] = &models.TeamDetailsOwnersItems0{
				Name:     u,
				Githubid: orgUser.Spec.GithubID,
				External: false,
			}
		} else {
			extUser := local.ExternalUsers()[u]
			teamDetails.Owners[i] = &models.TeamDetailsOwnersItems0{
				Name:     u,
				Githubid: extUser.Spec.GithubID,
				External: false,
			}
		}
	}

	for i, u := range team.Spec.Members {
		if orgUser, ok := local.Users()[u]; ok {
			teamDetails.Members[i] = &models.TeamDetailsMembersItems0{
				Name:     u,
				Githubid: orgUser.Spec.GithubID,
				External: false,
			}
		} else {
			extUser := local.ExternalUsers()[u]
			teamDetails.Members[i] = &models.TeamDetailsMembersItems0{
				Name:     u,
				Githubid: extUser.Spec.GithubID,
				External: false,
			}
		}
	}

	remote := g.goliac.GetRemote()
	// if the team is externally managed, we dont have the info locally
	// we need to get the members from the remote
	if team.Spec.ExternallyManaged {
		teams := remote.Teams(context.TODO(), true)
		if teams != nil {
			teamSlug := slug.Make(team.Name)
			if t, ok := teams[teamSlug]; ok {
				for _, t := range t.Members {
					// t is the githubid
					githubidToUser := make(map[string]string)
					for _, u := range local.Users() {
						githubidToUser[u.Spec.GithubID] = u.Name
					}

					if user, ok := githubidToUser[t]; ok {
						tDetail := models.TeamDetailsOwnersItems0{
							Name:     user,
							Githubid: t,
							External: false,
						}
						teamDetails.Owners = append(teamDetails.Owners, &tDetail)
					} else {
						tDetail := models.TeamDetailsOwnersItems0{
							Name:     "unknown",
							Githubid: t,
							External: false,
						}
						teamDetails.Owners = append(teamDetails.Owners, &tDetail)
					}
				}
			}
		}
	}

	return app.NewGetTeamOK().WithPayload(&teamDetails)
}

func (g *GoliacServerImpl) GetCollaborators(app.GetCollaboratorsParams) middleware.Responder {
	users := make(models.Users, 0)

	local := g.goliac.GetLocal()
	for username, user := range local.ExternalUsers() {
		u := models.User{
			Name:     username,
			Githubid: user.Spec.GithubID,
		}
		users = append(users, &u)
	}
	return app.NewGetCollaboratorsOK().WithPayload(users)

}

func (g *GoliacServerImpl) GetCollaborator(params app.GetCollaboratorParams) middleware.Responder {
	local := g.goliac.GetLocal()

	user, found := local.ExternalUsers()[params.CollaboratorID]
	if !found {
		message := fmt.Sprintf("Collaborator %s not found", params.CollaboratorID)
		return app.NewGetCollaboratorDefault(404).WithPayload(&models.Error{Message: &message})
	}

	collaboratordetails := models.CollaboratorDetails{
		Githubid:     user.Spec.GithubID,
		Repositories: make([]*models.Repository, 0),
	}

	githubidToExternal := make(map[string]string)
	for _, e := range local.ExternalUsers() {
		githubidToExternal[e.Spec.GithubID] = e.Name
	}

	// let's sort repo per team
	for _, repo := range local.Repositories() {
		for _, r := range repo.Spec.ExternalUserReaders {
			if r == params.CollaboratorID {
				collaboratordetails.Repositories = append(collaboratordetails.Repositories, &models.Repository{
					Name:       repo.Name,
					Visibility: repo.Spec.Visibility,
					Archived:   repo.Archived,
				})
			}
		}
		for _, r := range repo.Spec.ExternalUserWriters {
			if r == params.CollaboratorID {
				collaboratordetails.Repositories = append(collaboratordetails.Repositories, &models.Repository{
					Name:       repo.Name,
					Visibility: repo.Spec.Visibility,
					Archived:   repo.Archived,
				})
			}
		}
	}

	return app.NewGetCollaboratorOK().WithPayload(&collaboratordetails)
}

func (g *GoliacServerImpl) GetUsers(app.GetUsersParams) middleware.Responder {
	users := make(models.Users, 0)

	local := g.goliac.GetLocal()
	for username, user := range local.Users() {
		u := models.User{
			Name:     username,
			Githubid: user.Spec.GithubID,
		}
		users = append(users, &u)
	}
	return app.NewGetUsersOK().WithPayload(users)
}

func (g *GoliacServerImpl) GetUser(params app.GetUserParams) middleware.Responder {
	local := g.goliac.GetLocal()

	user, found := local.Users()[params.UserID]
	if !found {
		message := fmt.Sprintf("User %s not found", params.UserID)
		return app.NewGetUserDefault(404).WithPayload(&models.Error{Message: &message})
	}

	userdetails := models.UserDetails{
		Githubid:     user.Spec.GithubID,
		Teams:        make([]*models.Team, 0),
		Repositories: make([]*models.Repository, 0),
	}

	// [teamname]team
	userTeams := make(map[string]*models.Team)
	for teamname, team := range local.Teams() {
		for _, owner := range team.Spec.Owners {
			if owner == params.UserID {
				team := models.Team{
					Name:    teamname,
					Members: team.Spec.Members,
					Owners:  team.Spec.Owners,
				}
				userTeams[teamname] = &team
				break
			}
		}
		for _, member := range team.Spec.Members {
			if member == params.UserID {
				team := models.Team{
					Name:    teamname,
					Members: team.Spec.Members,
					Owners:  team.Spec.Owners,
				}
				userTeams[teamname] = &team
				break
			}
		}
	}

	for _, t := range userTeams {
		userdetails.Teams = append(userdetails.Teams, t)
	}

	// let's sort repo per team
	teamRepo := make(map[string]map[string]*entity.Repository)
	for _, repo := range local.Repositories() {
		if repo.Owner != nil {
			if _, ok := teamRepo[*repo.Owner]; !ok {
				teamRepo[*repo.Owner] = make(map[string]*entity.Repository)
			}
			teamRepo[*repo.Owner][repo.Name] = repo
		}
		for _, r := range repo.Spec.Readers {
			if _, ok := teamRepo[r]; !ok {
				teamRepo[r] = make(map[string]*entity.Repository)
			}
			teamRepo[r][repo.Name] = repo
		}
		for _, w := range repo.Spec.Writers {
			if _, ok := teamRepo[w]; !ok {
				teamRepo[w] = make(map[string]*entity.Repository)
			}
			teamRepo[w][repo.Name] = repo
		}
	}

	// [reponame]repo
	userRepos := make(map[string]*entity.Repository)
	for _, team := range userdetails.Teams {
		if repositories, ok := teamRepo[team.Name]; ok {
			for n, r := range repositories {
				userRepos[n] = r
			}
		}
	}

	for _, r := range userRepos {
		repo := models.Repository{
			Name:       r.Name,
			Visibility: r.Spec.Visibility,
			Archived:   r.Archived,
		}
		userdetails.Repositories = append(userdetails.Repositories, &repo)
	}

	return app.NewGetUserOK().WithPayload(&userdetails)
}

func (g *GoliacServerImpl) GetStatus(app.GetStatusParams) middleware.Responder {
	s := models.Status{
		LastSyncError:    "",
		LastSyncTime:     "N/A",
		NbRepos:          int64(len(g.goliac.GetLocal().Repositories())),
		NbTeams:          int64(len(g.goliac.GetLocal().Teams())),
		NbUsers:          int64(len(g.goliac.GetLocal().Users())),
		NbUsersExternal:  int64(len(g.goliac.GetLocal().ExternalUsers())),
		Version:          config.GoliacBuildVersion,
		DetailedErrors:   make([]string, 0),
		DetailedWarnings: make([]string, 0),
	}
	if g.lastSyncError != nil {
		s.LastSyncError = g.lastSyncError.Error()
	}
	if g.detailedErrors != nil {
		for _, err := range g.detailedErrors {
			s.DetailedErrors = append(s.DetailedErrors, err.Error())
		}
	}
	if g.detailedWarnings != nil {
		for _, warn := range g.detailedWarnings {
			s.DetailedWarnings = append(s.DetailedWarnings, warn.Error())
		}
	}
	if g.lastSyncTime != nil {
		s.LastSyncTime = g.lastSyncTime.UTC().Format("2006-01-02T15:04:05")
	}
	return app.NewGetStatusOK().WithPayload(&s)
}

func (g *GoliacServerImpl) GetLiveness(params health.GetLivenessParams) middleware.Responder {
	return health.NewGetLivenessOK().WithPayload(&models.Health{Status: "OK"})
}

func (g *GoliacServerImpl) GetReadiness(params health.GetReadinessParams) middleware.Responder {
	if g.ready {
		return health.NewGetLivenessOK().WithPayload(&models.Health{Status: "OK"})
	} else {
		message := "Not yet ready, loading local state"
		return health.NewGetLivenessDefault(503).WithPayload(&models.Error{Message: &message})
	}
}

func (g *GoliacServerImpl) PostFlushCache(app.PostFlushCacheParams) middleware.Responder {
	g.goliac.FlushCache()
	return app.NewPostFlushCacheOK()
}

func (g *GoliacServerImpl) PostResync(params app.PostResyncParams) middleware.Responder {
	go func() {
		ctx := context.Background()
		if config.Config.OpenTelemetryEnabled {
			var span trace.Span
			tracer := otel.Tracer("background-post-resync")
			ctx, span = tracer.Start(ctx, "backgroundResync")
			defer span.End()
		}
		g.triggerApply(ctx)
	}()

	return app.NewPostResyncOK()
}

func (g *GoliacServerImpl) PostExternalCreateRepository(params app.PostExternalCreateRepositoryParams) middleware.Responder {
	if params.Body.Visibility == "" {
		params.Body.Visibility = "private"
	}
	if params.Body.DefaultBranch == "" {
		params.Body.DefaultBranch = "main"
	}
	if params.Body.Visibility != "private" && params.Body.Visibility != "public" && params.Body.Visibility != "internal" {
		message := fmt.Sprintf("Invalid visibility: %s", params.Body.Visibility)
		return app.NewPostExternalCreateRepositoryDefault(400).WithPayload(&models.Error{Message: &message})
	}
	errorCollector := observability.NewErrorCollection()

	g.goliac.ExternalCreateRepository(
		params.HTTPRequest.Context(),
		errorCollector,
		osfs.New("/"),
		params.Body.GithubToken,
		params.Body.RepositoryName,
		params.Body.TeamName,
		params.Body.Visibility,
		params.Body.DefaultBranch,
		config.Config.ServerGitRepository,
		config.Config.ServerGitBranch,
	)

	if errorCollector.HasErrors() {
		message := fmt.Sprintf("Error when creating repository: %s", errorCollector.Errors[0])
		return app.NewPostExternalCreateRepositoryDefault(500).WithPayload(&models.Error{Message: &message})
	}

	return app.NewPostExternalCreateRepositoryOK()
}

func (g *GoliacServerImpl) Serve() {
	var wg sync.WaitGroup
	stopCh := make(chan struct{})

	restserver, err := g.StartRESTApi()
	if err != nil {
		logrus.Fatal(err)
	}

	// start the REST server
	go func() {
		if err := restserver.Serve(); err != nil {
			logrus.Error(err)
			close(stopCh)
		}
	}()

	// start the webhook server
	if config.Config.GithubWebhookDedicatedPort == config.Config.SwaggerPort {
		logrus.Warn("Github webhook server port is the same as the Swagger port, the webhook server will not be started")
	}

	var webhookserver GithubWebhookServer
	if config.Config.GithubWebhookDedicatedHost != "" &&
		config.Config.GithubWebhookDedicatedPort != 0 &&
		config.Config.GithubWebhookPath != "" &&
		config.Config.GithubWebhookSecret != "" &&
		config.Config.GithubWebhookDedicatedPort != config.Config.SwaggerPort {
		webhookserver = NewGithubWebhookServerImpl(
			config.Config.GithubWebhookDedicatedHost,
			config.Config.GithubWebhookDedicatedPort,
			config.Config.GithubWebhookPath,
			config.Config.GithubWebhookSecret,
			config.Config.ServerGitBranch, func() {
				// when receiving a Github webhook event
				// let's start the apply process asynchronously
				go func() {
					ctx := context.Background()
					var span trace.Span
					if config.Config.OpenTelemetryEnabled {
						tracer := otel.Tracer("github-webhook-tracer")
						ctx, span = tracer.Start(ctx, "github-webhook")
					}
					g.triggerApply(ctx)
					if span != nil {
						span.End()
					}
				}()
			},
		)
		go func() {
			if err := webhookserver.Start(); err != nil {
				logrus.Fatal(err)
				close(stopCh)
			}
		}()
	}

	logrus.Info("Server started")
	// Start the goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		g.syncInterval = 0
		for {
			select {
			case <-stopCh:
				restserver.Shutdown()
				if webhookserver != nil {
					webhookserver.Shutdown()
				}
				return
			default:
				g.syncInterval--
				time.Sleep(1 * time.Second)
				if g.syncInterval <= 0 {
					// we want to forceSync.
					// because we want to reconciliate even if there
					// is no new commit
					// (and also it will populate the lastUnmanaged structure)

					ctx := context.Background()
					var span trace.Span
					if config.Config.OpenTelemetryEnabled {
						tracer := otel.Tracer("cronjob-tracer")
						ctx, span = tracer.Start(ctx, "cronjob")
					}
					g.triggerApply(ctx)
					if span != nil {
						span.End()
					}
				}
			}
		}
	}()

	// Handle OS signals
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	<-signalCh
	logrus.Info("Received OS signal, stopping Goliac...")

	close(stopCh)
	wg.Wait()
}

/*
triggerApply will trigger the apply process (by calling serveApply())
inside serverApply, it will check if the lobby is free
- if the lobby is free, it will start the apply process
- if the lobby is busy, it will do nothing
*/
func (g *GoliacServerImpl) triggerApply(ctx context.Context) {
	errorCollector := observability.NewErrorCollection()
	applied := g.serveApply(ctx, errorCollector)
	if !applied && !errorCollector.HasErrors() {
		// the run was skipped
		g.syncInterval = config.Config.ServerApplyInterval
	} else {
		now := time.Now()
		g.lastSyncTime = &now
		previousError := g.lastSyncError
		g.lastSyncError = nil
		if errorCollector.HasErrors() {
			g.lastSyncError = errorCollector.Errors[len(errorCollector.Errors)-1]
		}

		// we want to log the warnings only if they are new
		previousSyncWarnings := g.lastSyncWarnings
		g.lastSyncWarnings = ""
		warns := []string{}
		for _, w := range errorCollector.Warns {
			warns = append(warns, w.Error())
		}
		sort.Strings(warns)
		for _, w := range warns {
			g.lastSyncWarnings += w + "\n"
		}

		if previousSyncWarnings != g.lastSyncWarnings {
			for _, w := range errorCollector.Warns {
				logrus.Warn(w)
			}
		}

		g.detailedErrors = errorCollector.Errors
		g.detailedWarnings = errorCollector.Warns
		// log the error only if it's a new one
		if g.lastSyncError != nil && (previousError == nil || g.lastSyncError.Error() != previousError.Error()) {
			logrus.Error(g.lastSyncError)
			if err := g.notificationService.SendNotification(fmt.Sprintf("Goliac error when syncing: %s", g.lastSyncError)); err != nil {
				logrus.Error(err)
			}
		}
		g.syncInterval = config.Config.ServerApplyInterval
	}
}

func (g *GoliacServerImpl) StartRESTApi() (*restapi.Server, error) {
	swaggerSpec, err := loads.Embedded(restapi.SwaggerJSON, restapi.FlatSwaggerJSON)
	if err != nil {
		return nil, err
	}

	api := operations.NewGoliacAPI(swaggerSpec)

	// configure API

	// healthcheck
	api.HealthGetLivenessHandler = health.GetLivenessHandlerFunc(g.GetLiveness)
	api.HealthGetReadinessHandler = health.GetReadinessHandlerFunc(g.GetReadiness)

	api.AppPostFlushCacheHandler = app.PostFlushCacheHandlerFunc(g.PostFlushCache)
	api.AppPostResyncHandler = app.PostResyncHandlerFunc(g.PostResync)
	api.AppGetStatusHandler = app.GetStatusHandlerFunc(g.GetStatus)
	api.AppGetStatiticsHandler = app.GetStatiticsHandlerFunc(g.GetStatistics)
	api.AppGetUnmanagedHandler = app.GetUnmanagedHandlerFunc(g.GetUnmanaged)

	api.AppGetUsersHandler = app.GetUsersHandlerFunc(g.GetUsers)
	api.AppGetUserHandler = app.GetUserHandlerFunc(g.GetUser)
	api.AppGetCollaboratorsHandler = app.GetCollaboratorsHandlerFunc(g.GetCollaborators)
	api.AppGetCollaboratorHandler = app.GetCollaboratorHandlerFunc(g.GetCollaborator)
	api.AppGetTeamsHandler = app.GetTeamsHandlerFunc(g.GetTeams)
	api.AppGetTeamHandler = app.GetTeamHandlerFunc(g.GetTeam)
	api.AppGetRepositoriesHandler = app.GetRepositoriesHandlerFunc(g.GetRepositories)
	api.AppGetRepositoryHandler = app.GetRepositoryHandlerFunc(g.GetRepository)

	api.AppPostExternalCreateRepositoryHandler = app.PostExternalCreateRepositoryHandlerFunc(g.PostExternalCreateRepository)

	server := restapi.NewServer(api)

	server.Host = config.Config.SwaggerHost
	server.Port = config.Config.SwaggerPort

	server.ConfigureAPI()

	return server, nil
}

func (g *GoliacServerImpl) serveApply(ctx context.Context, errorCollector *observability.ErrorCollection) bool {
	// we want to run ApplyToGithub
	// and queue one new run (the lobby) if a new run is asked
	g.applyLobbyMutex.Lock()
	// we already have a current run, and another waiting in the lobby
	if g.applyLobby {
		g.applyLobbyMutex.Unlock()
		return false
	}

	if !g.applyCurrent {
		g.applyCurrent = true
	} else {
		g.applyLobby = true
		for g.applyLobby {
			g.applyLobbyCond.Wait()
		}
	}
	g.applyLobbyMutex.Unlock()

	// free the lobbdy (or just the current run) for the next run
	defer func() {
		g.applyLobbyMutex.Lock()
		if g.applyLobby {
			g.applyLobby = false
			g.applyLobbyCond.Signal()
		} else {
			g.applyCurrent = false
		}
		g.applyLobbyMutex.Unlock()
	}()

	repo := config.Config.ServerGitRepository
	branch := config.Config.ServerGitBranch

	if repo == "" {
		errorCollector.AddError(fmt.Errorf("GOLIAC_SERVER_GIT_REPOSITORY env variable not set"))
		return false
	}
	if branch == "" {
		errorCollector.AddError(fmt.Errorf("GOLIAC_SERVER_GIT_BRANCH env variable not set"))
		return false
	}

	// we are ready (to give local state, and to sync with remote)
	g.ready = true

	startTime := time.Now()
	stats := config.GoliacStatistics{}
	newctx := context.WithValue(ctx, config.ContextKeyStatistics, &stats)

	fs := osfs.New("/")
	unmanaged := g.goliac.Apply(newctx, errorCollector, fs, false, repo, branch)
	if errorCollector.HasErrors() {
		return false
	}
	endTime := time.Now()
	g.lastTimeToApply = endTime.Sub(startTime)
	g.lastStatistics.GithubApiCalls = stats.GithubApiCalls
	g.lastStatistics.GithubThrottled = stats.GithubThrottled

	if g.lastTimeToApply > g.maxTimeToApply {
		g.maxTimeToApply = g.lastTimeToApply
	}

	if stats.GithubApiCalls > g.maxStatistics.GithubApiCalls {
		g.maxStatistics.GithubApiCalls = stats.GithubApiCalls
	}

	if stats.GithubThrottled > g.maxStatistics.GithubThrottled {
		g.maxStatistics.GithubThrottled = stats.GithubThrottled
	}

	if unmanaged != nil {
		g.lastUnmanaged = unmanaged
	}

	return true
}
