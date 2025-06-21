package config

// Config is the whole configuration of the app
var Config = struct {

	// LogrusLevel sets the logrus logging level
	LogrusLevel string `env:"GOLIAC_LOGRUS_LEVEL" envDefault:"info"`
	// LogrusFormat sets the logrus logging formatter
	// Possible values: text, json
	LogrusFormat string `env:"GOLIAC_LOGRUS_FORMAT" envDefault:"text"`

	GithubServer              string `env:"GOLIAC_GITHUB_SERVER" envDefault:"https://api.github.com"`
	GithubAppOrganization     string `env:"GOLIAC_GITHUB_APP_ORGANIZATION" envDefault:""`
	GithubAppID               int64  `env:"GOLIAC_GITHUB_APP_ID"`
	GithubAppPrivateKeyFile   string `env:"GOLIAC_GITHUB_APP_PRIVATE_KEY_FILE" envDefault:"github-app-private-key.pem"`
	GithubAppClientSecret     string `env:"GOLIAC_GITHUB_APP_CLIENT_SECRET"`
	GithubAppCallbackURL      string `env:"GOLIAC_GITHUB_APP_CALLBACK_URL"`
	GithubPersonalAccessToken string `env:"GOLIAC_GITHUB_PERSONAL_ACCESS_TOKEN"`
	GoliacEmail               string `env:"GOLIAC_EMAIL" envDefault:"goliac@goliac-project.com"`
	GoliacTeamOwnerSuffix     string `env:"GOLIAC_TEAM_OWNER_SUFFIX" envDefault:"-goliac-owners"`

	GithubConcurrentThreads int64 `env:"GOLIAC_GITHUB_CONCURRENT_THREADS" envDefault:"5"`
	GithubCacheTTL          int64 `env:"GOLIAC_GITHUB_CACHE_TTL" envDefault:"86400"`

	// ManageGithubActionsVariables - to manage Github Actions repository variables
	ManageGithubActionsVariables bool `env:"GOLIAC_MANAGE_GITHUB_ACTIONS_VARIABLES" envDefault:"true"`
	// ManageGithubAutolinks - to manage Github repositoryAutolinks
	ManageGithubAutolinks bool `env:"GOLIAC_MANAGE_GITHUB_AUTOLINKS" envDefault:"true"`

	ServerApplyInterval int64  `env:"GOLIAC_SERVER_APPLY_INTERVAL" envDefault:"600"`
	ServerGitRepository string `env:"GOLIAC_SERVER_GIT_REPOSITORY" envDefault:""`
	ServerGitBranch     string `env:"GOLIAC_SERVER_GIT_BRANCH" envDefault:"main"`
	// the name of the CI validating each PR on the teams repsotiry. See scaffold.go for the Github action
	ServerGitBranchProtectionRequiredCheck string `env:"GOLIAC_SERVER_PR_REQUIRED_CHECK" envDefault:"validate"`

	// MaxChangesetsOverride - override the max changesets limitation from the repository config
	MaxChangesetsOverride bool `env:"GOLIAC_MAX_CHANGESETS_OVERRIDE" envDefault:"false"`

	// SyncUsersBeforeApply - to sync users before applying the commits
	SyncUsersBeforeApply bool `env:"GOLIAC_SYNC_USERS_BEFORE_APPLY" envDefault:"true"`

	// Host - golang-skeleton server host
	SwaggerHost string `env:"GOLIAC_SERVER_HOST" envDefault:"localhost"`
	// Port - golang-skeleton server port
	SwaggerPort int `env:"GOLIAC_SERVER_PORT" envDefault:"18000"`

	// MiddlewareVerboseLoggerEnabled - to enable the negroni-logrus logger for all the endpoints useful for debugging
	MiddlewareVerboseLoggerEnabled bool `env:"GOLIAC_MIDDLEWARE_VERBOSE_LOGGER_ENABLED" envDefault:"true"`
	// MiddlewareVerboseLoggerExcludeURLs - to exclude urls from the verbose logger via comma separated list
	MiddlewareVerboseLoggerExcludeURLs []string `env:"GOLIAC_MIDDLEWARE_VERBOSE_LOGGER_EXCLUDE_URLS" envDefault:"" envSeparator:","`
	// MiddlewareGzipEnabled - to enable gzip middleware
	MiddlewareGzipEnabled bool `env:"GOLIAC_MIDDLEWARE_GZIP_ENABLED" envDefault:"true"`

	// CORSEnabled - enable CORS
	CORSEnabled          bool     `env:"GOLIAC_CORS_ENABLED" envDefault:"false"`
	CORSAllowCredentials bool     `env:"GOLIAC_CORS_ALLOW_CREDENTIALS" envDefault:"true"`
	CORSAllowedHeaders   []string `env:"GOLIAC_CORS_ALLOWED_HEADERS" envDefault:"Origin,Accept,Content-Type,X-Requested-With,Authorization,Time_Zone" envSeparator:","`
	CORSAllowedMethods   []string `env:"GOLIAC_CORS_ALLOWED_METHODS" envDefault:"GET,POST,PUT,DELETE,PATCH" envSeparator:","`
	CORSAllowedOrigins   []string `env:"GOLIAC_CORS_ALLOWED_ORIGINS" envDefault:"*" envSeparator:","`
	CORSExposedHeaders   []string `env:"GOLIAC_CORS_EXPOSED_HEADERS" envDefault:"WWW-Authenticate" envSeparator:","`

	// WebPrefix - base path for web and API
	// e.g. GOLANG_SKELETON_WEB_PREFIX=/foo
	// UI path  => localhost:18000/foo"
	// API path => localhost:18000/foo/api/v1"
	WebPrefix string `env:"GOLIAC_WEB_PREFIX" envDefault:""`

	// to receive slack notifications on errors
	SlackToken   string `env:"GOLIAC_SLACK_TOKEN" envDefault:""`
	SlackChannel string `env:"GOLIAC_SLACK_CHANNEL" envDefault:""`

	// to receive Github main branch merge webhook events on the /webhook endpoint
	GithubWebhookSecret        string `env:"GOLIAC_GITHUB_WEBHOOK_SECRET" envDefault:""`
	GithubWebhookDedicatedHost string `env:"GOLIAC_GITHUB_WEBHOOK_HOST" envDefault:"localhost"`
	GithubWebhookDedicatedPort int    `env:"GOLIAC_GITHUB_WEBHOOK_PORT" envDefault:"18001"`
	GithubWebhookPath          string `env:"GOLIAC_GITHUB_WEBHOOK_PATH" envDefault:"/webhook"`

	OpenTelemetryEnabled      bool   `env:"GOLIAC_OPENTELEMETRY_ENABLED" envDefault:"false"`
	OpenTelemetryGrpcEndpoint string `env:"GOLIAC_OPENTELEMETRY_GRPC_ENDPOINT" envDefault:"localhost:4317"`
	OpenTelemetryTraceAll     bool   `env:"GOLIAC_OPENTELEMETRY_TRACE_ALL" envDefault:"true"`

	// ForcemergeWorkflow specific configuration
	WorkflowJiraAtlassianDomain string `env:"GOLIAC_WORKFLOW_JIRA_ATLASSIAN_DOMAIN" envDefault:""`
	WorkflowJiraProjectKey      string `env:"GOLIAC_WORKFLOW_JIRA_PROJECT_KEY" envDefault:""`
	WorkflowJiraEmail           string `env:"GOLIAC_WORKFLOW_JIRA_EMAIL" envDefault:""`
	WorkflowJiraApiToken        string `env:"GOLIAC_WORKFLOW_JIRA_API_TOKEN" envDefault:""`
	WorkflowJiraIssueType       string `env:"GOLIAC_WORKFLOW_JIRA_ISSUE_TYPE" envDefault:"Task"`
	WorkflowDynamoDBTableName   string `env:"GOLIAC_WORKFLOW_DYNAMODB_TABLE_NAME" envDefault:"goliac-workflows"`
}{}

// to be overrided at build time with
// go build -ldflags "-X github.com/goliac-project/goliac/internal/config.GoliacBuildVersion=...
var GoliacBuildVersion = "unknown"
