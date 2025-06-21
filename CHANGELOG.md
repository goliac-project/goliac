## Goliac v1.1.0

- add repository autolink management

## Goliac v1.0.0

- add an architecture.md document

## Goliac v0.18.23

- update workflow dynamodb plugin to set timestamp as number

## Goliac v0.18.22

- bugfix: githubapp cannot fetch global ruleset actors
- slimmer validation error message

## Goliac v0.18.21

- update the verify to check all files have a .yaml extensions in the teams subdir

## Goliac v0.18.20

- explicit link to GH cli in the UI
- bugfix: force teams to not be repo admins

## Goliac v0.18.19

- allow to force merge with a squash merge

## Goliac v0.18.18

- better team validation mechanic
- implement a lazy loading for some resources to reduce the number of Github call

## Goliac v0.18.17

- bugfix: better ruleset type check
- bugfix: better support for GOLIAC_MANAGE_GITHUB_ACTIONS_VARIABLES

## Goliac v0.18.16

- enable dynamodb plugin

## Goliac v0.18.15

- enable dynamodb plugin

## Goliac v0.18.14

- fix fetching externally managed repo for workflow

## Goliac v0.18.13

- adding a dynamodb workflow plugin

## Goliac v0.18.12

- better /forcemerge interactive PR workflow documentation

## Goliac v0.18.11

- security fix: Bumps http-proxy-middleware from 2.0.7 to 2.0.9
- security fix: update go jwt library 

## Goliac v0.18.10

- better .goliac/forcemerge.approvers error handling

## Goliac v0.18.9

- allows to specify workflow approver from repo's .goliac/forcemerge.approvers

## Goliac v0.18.8

- bugfix: pass pr url to workflow when receiving a PR command

## Goliac v0.18.7

- bugfix: fixing comment creation issue

## Goliac v0.18.6

- bugfix: passing organization to create a comment in an issue

## Goliac v0.18.5

- update PR merge github command

## Goliac v0.18.4

- allow to start a workflow (like forcemerge) via an update on a PR
- bugfix: handle correctly the case where the repository is not the teams-repo

## Goliac v0.18.3

- ruleset new definition bugfix

## Goliac v0.18.2

- put back repository filter in the ruleset definition

## Goliac v0.18.1

- update documentation (resources)

## Goliac v0.18.0

- CLI UI fix: reduce progressbar width
- bugfix: update correctly team's parent when the team is externally managed
- introducing github environment, variables, and (UI) secret listing
- bugfix: update correctly team's membership when it was in ADMIN in Github

## Goliac v0.17.3

- allow to create a repository from a fork
- dont force 'main' branch by default
- various fixes (ruleset OnExclude collect team's ID at creation)

## Goliac v0.17.2

- restructure workflows to be a bit more generic (for example to open the door for a "delete repository" workflow)
- introduce a "noop" workflow type (for tests)

## Goliac v0.17.1

- oauth2 worflow fix: put redirect url as an env variable

## Goliac v0.17.0

- add a PR merge breaking glass workflow feature

## Goliac v0.16.3

- move golden_reviewers feature to global rulesets
- bugfix: fix delete_repository_branchprotection mutation
- bugfix: shutdown opentelemetry properly (for plan/apply/scaffold)

## Goliac v0.16.2

- better logging when externally creating a repository
- introduing `golden_reviewers` in the `goliac.yaml` file

## Goliac v0.16.1

- update the documentation (especially the APIs)

## Goliac v0.16.0

- introduce a CreateRepository endpoint
- bugfix: support loading more than 30 teams per repository 
- logs warnings message only once (if they dont change)
- bugfix: scaffold correctly archived repositories
- enhancement: discard from readers a team that is the repository owner

## Goliac v0.15.11

- bugfix when comparing team parent name (slug vs real name)

## Goliac v0.15.10

- bugfix: removing twice the same user

## Goliac v0.15.9

- update documentation

## Goliac v0.15.8

- default branch bugfixes: when the repo is empty, and when the Github App doesn't have content access

## Goliac v0.15.7

- allow to use a PAT (Personal Access Token) to run Goliac (in particular useful to scaffold)

## Goliac v0.15.6

- new property to specify default branch for each repository

## Goliac v0.15.5

- ruleset bugfixes: include/exclude refs/heads prefix + required_status_checks payload

## Goliac v0.15.4

- introducing opentelemetry tracing

## Goliac v0.15.3

- allow public,internal,private repository visibility
- allow to restrict public repositories via global rules

## Goliac v0.15.2

- refactor error handling
- fix ruleset repository_ids API call
- dont enforce ruleset/branch protections on archived repositories
- adding support for ruleset parameters required_linear_history, non_fast_forward

## Goliac v0.15.1

- branch protections bugfixes

## Goliac v0.15.0

- introducing branch protections

## Goliac v0.14.1

- different security updates (golang/npm)
- bugfix: wrong api path for repository rulesets

## Goliac v0.14.0

- moving the project to goliac-project Github organization

## Goliac v0.13.3

- introducing ruleset rules `creation`, `update`, `deletion` and `non_fast_forward`

## Goliac v0.13.2

- bugfix on repository rulesets (name of the repository vs of the ruleset)

## Goliac v0.13.1

- bugfix on repository rulesets (bypass doesn't work on repository rulesets)
- do not touch organization rulesets when scanning repositories rulesets

## Goliac v0.13.0

- Introducing Repository Rulesets

## Goliac v0.12.0

- Introducing renaming of repository
