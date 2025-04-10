# How to install and use

You need to:
- [1. create the Goliac GitHub App (and point it to the server)](#1-creating-the-goliac-github-app)
- [2. create the IAC github repository (with the GitHub actions associated) or clone the example repository](#2-creating-the-iac-github-repository)
- [3. deploy the server somewhere (and configure it)](#3-configure-the-goliac-server)

Optionally, you can:
- [Sync Users from an external source](#optional-syncing-users-from-an-external-source)
- [Add the Slack integration](#optional-slack-integration)
- [Configure a GitHub webhook](#optional-github-webhook)


## 1. Creating the Goliac GitHub App

In GitHub:
- Register new GitHub App
  - in your profile settings, go to `Developer settings`/`GitHub Apps`
  - Click on `New GitHub App`
- Give basic information:
  - GitHub App  name can be `<yourorg>-goliac-app` (it will be used in the rulesets later)
  - Homepage URL can be `https://github.com/goliac-project/goliac`
  - Disable the active Webhook
- Under Organization permissions
  - Give Read/Write access to `Administration`
  - Give Read/Write access to `Members`
  - Give Read/Write access to `Environments`
  - Give Read/Write access to `Actions`
  - Give Read/Write access to `Variables`
- Under Repository permissions
  - Give Read/Write access to `Administration`
  - Give Read/Write access to `Content` (it is needed to access the default branch of repositories)
- Where can this GitHub App be installed: `Only on this account`
- And Create
- then you must
  - collect the AppID
  - Generate (and collect) a private key (file)
- Go to the left tab "Install App"
  - Click on "Install"

## 2. Creating the IAC github repository

In your GitHub organization, you need to create a git repository. Usually it is called `goliac-teams`.

You have different way to initialize it.

### Manual initialization

You can check https://github.com/goliac-project/goliac-teams as an example

You need the following structure:
```
/
├─ goliac.yaml
├─ rulesets/
│  ├─ <rulesetname>.yaml
│  ...
├─ archived/
├─ users/
│ ├─ org/
│ │ ├─ <user>.yaml
│ │ ...
│ └─ protected/
│   ├─ <user>.yaml
│   ...
└─ teams/
   ├─ <teamname>/
   │ ├─ team.yaml
   │ ├─ <reponame>.yaml
   │ ├─ <reponame>.yaml
   │  ...
   ├─ <teamame>/
   │ ├─ <team.yaml
   │ ├─ <reponame>.yaml
   │ ├─ ...
   │ └─ <subteamname>/
   │   ├─ <team.yaml
   │   ├─ <reponame>.yaml
   │   ...
   ├─ <teamame>/
   │ └─ team.yaml
   │ ...
   ...
```

### Assisted initialization

You will need the goliac binary:

```shell
curl -o goliac -L https://github.com/goliac-project/goliac/releases/latest/download/goliac-`uname -s`-`uname -m` && chmod +x goliac
```

You will need as well to have created an admin team in your Github organization (in our example, the team is called `goliac-admin` ).

And now you can use the goliac application to assist you:

```shell
export GOLIAC_GITHUB_APP_ID=<appid>
export GOLIAC_GITHUB_APP_PRIVATE_KEY_FILE=<private key filename>
export GOLIAC_GITHUB_APP_ORGANIZATION=<your github organization>
./goliac scaffold <directory> <goliac-admin team you want to create>
```

So something like

```shell
mkdir goliac-teams
export GOLIAC_GITHUB_APP_ID=355525
export GOLIAC_GITHUB_APP_PRIVATE_KEY_FILE=goliac-project-app.2023-07-03.private-key.pem
export GOLIAC_GITHUB_APP_ORGANIZATION=goliac-project
./goliac scaffold goliac-teams goliac-admin
```

The application will connect to your GitHub organization and will try to guess
- your users
- your teams
- the repos associated with your teams

And it will create the corresponding structure into the "goliac-teams" directory

### the goliac.yaml configuration file

To make Goliac working you can configure the `/goliac.yaml` file

```yaml
admin_team: goliac-admin # the name of the team (in the `/teams` directory ) that can admin this repository
everyone_team_enabled: false # if you want all members to have read access to all repositories

rulesets: # if you want to have organization-wide enforced rules (see the /rulesets directory)
  - pattern: .*
    ruleset: default

max_changesets: 50 # protection measure: how many changes Goliac can do at once before considering that suspicious
archive_on_delete: true # allow to not delete directly repository, but archive them first. (only usefull if destructive_operations.repository = true. See below)

destructive_operations:
  repositories: false # can Goliac remove repositories not listed in this repository
  teams: false        # can Goliac remove teams not listed in this repository
  users: false        # can Goliac remove users not listed in this repository
  rulesets: false     # can Goliac remove rulesets not listed in this repository

usersync:
  plugin: noop # noop, fromgithubsaml, shellscript

#visibility_rules:
#  forbid_public_repositories: true # if you want to forbid public repositories
#  forbid_public_repositories_exclusions: # if you want to allow some public repositories
#    - goliac-teams
#    - repo_public.*
```

and you can configure different ruleset in the `/rulesets` directory like

```yaml
apiVersion: v1
kind: Ruleset
name: default
spec:
  enforcement: evaluate # can be disable, active or evaluate
  bypassapps:
    - appname: goliac-project-app
      mode: always # always or pull_request
  conditions:
    include:
      - "~DEFAULT_BRANCH" # it can be ~ALL,~DEFAULT_BRANCH, or branch name

  rules:
    - ruletype: pull_request # currently supported: pull_request, required_signatures,required_status_checks, creation, update, deletion, non_fast_forward, required_linear_history
      parameters:
        requiredApprovingReviewCount: 1
```

### Testing your IAC github repository

Before commiting your new structure you can use `goliac verify <path to goliac-teams repo>` to test the validity:

```
goliac verify goliac-teams/
```

### Applying manually

After merging your team IAC goliac-teams repository, you can begin to test and apply

```shell
export GOLIAC_GITHUB_APP_ID=355525
export GOLIAC_GITHUB_APP_PRIVATE_KEY_FILE=goliac-project-app.2023-07-03.private-key.pem
export GOLIAC_GITHUB_APP_ORGANIZATION=goliac-project
export GOLIAC_SERVER_GIT_REPOSITORY=https://github.com/goliac-project/goliac-teams

./goliac plan --repository https://github.com/goliac-project/goliac-teams --branch main
```

and you can apply the change "manually"

```shell
export GOLIAC_GITHUB_APP_ID=355525
export GOLIAC_GITHUB_APP_PRIVATE_KEY_FILE=goliac-project-app.2023-07-03.private-key.pem
export GOLIAC_GITHUB_APP_ORGANIZATION=goliac-project
export GOLIAC_SERVER_GIT_REPOSITORY=https://github.com/goliac-project/goliac-teams

./goliac apply --repository https://github.com/goliac-project/goliac-teams --branch main
```

If it works for you, you can put in place the goliac service to fetch and apply automatically (like every 10 minute). See below

### The goliac application

By using the standalone application, goliac comes with different commands

| Command  | Description                                                                    |
|----------|--------------------------------------------------------------------------------|
| scaffold | help you bootstrap an IAC structure, based on your current GitHub organization |
| verify   | check the validity of a local IAC structure. Used for the CI (for example)  to valiate a PR |
| plan     | download a goliac teams IAC repository, and show changes to apply              |
| apply    | download a goliac teams IAC repository, and apply it to GitHub                 |
| serve    | starts a server (and a UI) and apply automaticall every 10 minutes             |
| syncusers| get the definition of users outside and put it back to the IAC structure       |

## 3. Configure the Goliac server

You can run the goliac server as a service or a docker container. It needs several environment variables:

| Environment variable             | Default     | Description                 |
|----------------------------------|-------------|-----------------------------|
| GOLIAC_LOGRUS_LEVEL              | info        | debug,info,warning or error |
| GOLIAC_LOGRUS_FORMAT             | text        | text or json                |
| GOLIAC_GITHUB_SERVER             | https://api.github.com |                  |
| GOLIAC_GITHUB_APP_ORGANIZATION   |             | (mandatory) name of your github org     |
| GOLIAC_GITHUB_APP_ID             |             | (mandatory) app id of Goliac GitHub App |
| GOLIAC_GITHUB_APP_PRIVATE_KEY_FILE |           | (mandatory) path to private key       |
| GOLIAC_GITHUB_APP_CLIENT_SECRET  |             | (recommended) app client secret of Goliac GitHub App |
| GOLIAC_GITHUB_PERSONAL_ACCESS_TOKEN |           | (optional) personal access token to use instead of the GitHub App |
| GOLIAC_EMAIL                     | goliac@goliac-project.com | author name used by Goliac to commit (Codeowners) |
| GOLIAC_GITHUB_CONCURRENT_THREADS | 5           | You can increase, like '10' |
| GOLIAC_GITHUB_CACHE_TTL          |  86400      | GitHub remote cache seconds retention |
| GOLIAC_SERVER_APPLY_INTERVAL     | 600         | How often (seconds) Goliac try to apply |
| GOLIAC_SERVER_GIT_REPOSITORY     |             | (mandatory) goliac teams repo name in your organization |
| GOLIAC_SERVER_GIT_BRANCH         | main        | goliac teams repo default branch name to use |
| GOLIAC_SERVER_HOST               |localhost    | it is set as `0.0.0.0` in the Dockerfile |
| GOLIAC_SERVER_PORT               | 18000       |                            |
| GOLIAC_SERVER_PR_REQUIRED_CHECK  | validate    | ci check to enforce when evaluating a PR (used for CI mode) |
| GOLIAC_MAX_CHANGESETS_OVERRIDE    | false          | if you need to override the `max_changesets` setting in the `goliac.yaml` file. Useful in particular using the `goliac apply` CLI  |
| GOLIAC_SYNC_USERS_BEFORE_APPLY    | true          | to sync users before applying the changes |
| GOLIAC_SLACK_TOKEN                |               | (optional) Slack token to send notification (ususally error messages if any) |
| GOLIAC_SLACK_CHANNEL              |               | (optional) Slack channel to send notification |
| GOLIAC_GITHUB_WEBHOOK_HOST        | 0.0.0.0       | (optional) Hostname to listen to GitHub webhook |
| GOLIAC_GITHUB_WEBHOOK_PORT        | 18001         | (optional) Port to listen to GitHub webhook |
| GOLIAC_GITHUB_WEBHOOK_SECRET      |               | (optional) Secret to validate GitHub webhook |
| GOLIAC_GITHUB_WEBHOOK_PATH        | /webhook      | (optional) Path to listen to GitHub webhook |
| GOLIAC_OPENTELEMETRY_ENABLED      | false         | (optional) Enable OpenTelemetry tracing |
| GOLIAC_OPENTELEMETRY_GRPC_ENDPOINT| localhost:4317| (optional) OpenTelemetry grpc endpoint |
| GOLIAC_WORKFLOW_JIRA_ATLASSIAN_DOMAIN |      | PR Breaking glass workflow - Jira plugin: company domain  |
| GOLIAC_WORKFLOW_JIRA_EMAIL   |               | PR Breaking glass workflow - Jira plugin: email |
| GOLIAC_WORKFLOW_JIRA_API_TOKEN |             | PR Breaking glass workflow - Jira plugin: token |

then you just need to start it with

```shell
export GOLIAC_GITHUB_APP_ID=355525
export GOLIAC_GITHUB_APP_PRIVATE_KEY_FILE=goliac-project-app.2023-07-03.private-key.pem
export GOLIAC_GITHUB_APP_CLIENT_SECRET=bed08cd3f542ac3a39c8c1d142888b150d5e2880
export GOLIAC_GITHUB_APP_ORGANIZATION=goliac-project
export GOLIAC_SERVER_GIT_REPOSITORY=https://github.com/goliac-project/goliac-teams

./goliac serve
```

You can connect (eventually) to the UI for some statistic to `http://GOLIAC_SERVER_HOST:GOLIAC_SERVER_PORT`

### Using docker container

```shell
docker run -ti -v `pwd`/goliac-project-app.2023-07-03.private-key.pem:/app/private-key.pem -e GOLIAC_GITHUB_APP_ID=355525 -e GOLIAC_GITHUB_APP_PRIVATE_KEY_FILE=/app/private-key.pem -e GOLIAC_GITHUB_APP_ORGANIZATION=goliac-project -e GOLIAC_SERVER_GIT_REPOSITORY=https://github.com/goliac-project/goliac-teams -e GOLIAC_SERVER_HOST=0.0.0.0 -p 18000:18000 ghcr.io/goliac-project/goliac serve
```

### Using docker-compose

```yaml
services:
    goliac:
        volumes:
            - ./goliac-project-app.2023-07-03.private-key.pem:/app/private-key.pem"
        environment:
            - GOLIAC_GITHUB_APP_ID=355525
            - GOLIAC_GITHUB_APP_PRIVATE_KEY_FILE=/app/private-key.pem
            - GOLIAC_GITHUB_APP_CLIENT_SECRET=bed08cd3f542ac3a39c8c1d142888b150d5e2880
            - GOLIAC_GITHUB_APP_ORGANIZATION=goliac-project
            - GOLIAC_SERVER_GIT_REPOSITORY=https://github.com/goliac-project/goliac-teams
            - GOLIAC_SERVER_HOST=0.0.0.0
        ports:
            - 18000:18000
        image: ghcr.io/goliac-project/goliac
        command: serve
```

### Using kubernetes

You can deploy the goliac server in a kubernetes cluster. You can use the `k8s/goliac-deployment.yaml` file as a template.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: goliac
  namespace: goliac
spec:
  progressDeadlineSeconds: 600
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: goliac
  template:
    metadata:
      labels:
        app.kubernetes.io/name: goliac
    spec:
      containers:
        - args:
            - serve
          env:
            - name: GOLIAC_GITHUB_APP_PRIVATE_KEY_FILE
              value: /etc/goliac/github-app-private-key.pem
            - name: GOLIAC_LOGRUS_LEVEL
              value: warning
            - name: GOLIAC_SERVER_GIT_REPOSITORY
              value: 'https://github.com/goliac-project/goliac-teams'
          envFrom:
            - secretRef:
                name: goliac-secrets
          image: ghcr.io/goliac-project/goliac
          livenessProbe:
            failureThreshold: 3
            httpGet:
              path: /api/v1/liveness
              port: http
              scheme: HTTP
            initialDelaySeconds: 10
            periodSeconds: 20
            successThreshold: 1
            timeoutSeconds: 5
          name: backend
          ports:
            - containerPort: 18000
              name: http
              protocol: TCP
          readinessProbe:
            failureThreshold: 3
            httpGet:
              path: /api/v1/readiness
              port: http
              scheme: HTTP
            initialDelaySeconds: 10
            periodSeconds: 20
            successThreshold: 1
            timeoutSeconds: 5
          resources:
            limits:
              cpu: 500m
              memory: 512Mi
            requests:
              cpu: 100m
              memory: 256Mi
          volumeMounts:
            - mountPath: /etc/goliac
              name: goliac-secrets
              readOnly: true
      volumes:
        - name: goliac-secrets
          secret:
            secretName: goliac-secrets
---
apiVersion: v1
kind: Service
metadata:
  name: goliac
  namespace: goliac
spec:
  ports:
    - name: http
      port: 18000
      protocol: TCP
      targetPort: http
  selector:
    app.kubernetes.io/name: goliac
  type: ClusterIP
```

## Optional: Syncing Users from an external source

You can create/edit all your users manually in the `users/org/` directory. But often you are already managing your users from another source of thruth.

Goliac can sync users from an external source, This is the `usersync` section in the `goliac.yaml` file. There are different plugins:

| Plugin name    | Description                                                               |
|----------------|---------------------------------------------------------------------------|
| noop           | Doing nothing (if you dont want to sync from an external source of truth) |
| fromgithubsaml | If you are using GitHub Enterprise SAML integration                       |
| shellscript    | If you want an ad-hoc sync method, Goliac call the `usersync.path`        |

What you need to do:
- edit the `goliac.yaml` file to specify the right `usersync` plugin
- by default Goliac will run the sync before applying new changes

If you want to run the sync manually, you can
- set the GOLIAC_SYNC_USERS_BEFORE_APPLY to false
- run regularly the `./goliac syncusers` command (cronjob or k8s cronjob) to sync users definition

### Protected users

On top of syncing users, if you fear to loose control on users, or you want to ensure that some users are not deleted, you can copy their definition into the `org/protected` directory.

As a reminder a user is defined via a yaml file like `alice.yaml` with the content:

```
apiVersion: v1
kind: User
name: alice
spec:
  githubID: alice-myorg
```

## Optional: Slack integration

If you want to be notified of sync process issues, you can create a Slack application, and configure the `GOLIAC_SLACK_TOKEN` and `GOLIAC_SLACK_CHANNEL` environment variables.

To create a Slack application, you can go to https://api.slack.com/apps, and `Create New App`, you can use the following yaml manifest (when asked to import a manifest):

```yaml
display_information:
  name: Goliac
  description: GitHub Organization Leveraged by Infrastructure As Code
  background_color: "#616161"
  long_description: "https://github.com/goliac-project/goliac\r

    \r

    Goliac (GitHub Organization Leveraged by Infrastructure As Code), is a tool to manage your GitHub Organization (users/teams/repositories) via yaml manifests files structured in a GitHub repository\r

    this IAC GitHub repositories can be updated by teams from your organization, but only the repositories they owns\r

    all repositories rules are enforced via a central configuration that only the IT/security team can update (if you are using GitHub Enterprise)\r

    a GitHub App watching this repository and applying any changes"
features:
  bot_user:
    display_name: Goliac
    always_online: false
oauth_config:
  scopes:
    bot:
      - chat:write
settings:
  org_deploy_enabled: false
  socket_mode_enabled: false
  token_rotation_enabled: false
```

You need to
- install the application into your workspace. (You can do it by clicking on the `Install App` button)
-  to set the 2 environments variables (`GOLIAC_SLACK_TOKEN` and `GOLIAC_SLACK_CHANNEL`) with the token and the channel name.
-  to invite the bot to the channel.

## Optional: GitHub webhook

By default Goliac works by polling the state of the goliac teams GitHub repository (by default every 10 minutes).
 But you can configure a webhook to be notified of changes in your GitHub organization.

To do so, you need to update the GitHub App configuration:
- in General:
  - enable the active Webhook
  - change the Content-Type for `application/json`
  - set a webhook secret
  - set the webhook URL to be able to reach `http://GOLIAC_SERVER_HOST:GOLIAC_SERVER_PORT/webhook`
- in Subscribe to events
  - select `Push`

And you need to configure the Goliac server with
- the `GOLIAC_GITHUB_WEBHOOK_SECRET` environment variable.
- the `GOLIAC_GITHUB_WEBHOOK_HOST` environment variable (`localhost` by default, so you need to change it to something like `0.0.0.0`)
- the `GOLIAC_GITHUB_WEBHOOK_PORT` environment variable (`18001` by default)
- the `GOLIAC_GITHUB_WEBHOOK_PATH` environment variable (`/webhook` by default)
