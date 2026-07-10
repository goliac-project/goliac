## Goliac v1.9.8

- bugfix: add Goliac app to branch protection and ruleset bypass when a repository defines `codeowners` or `codeowners_raw` and branch protection requires approving reviews (`required_approving_review_count` or `requires_code_owner_reviews`), so `UpdateRepositoryCodeowners` can commit without a 409 from repository rules
- improvement: when `UpdateRepositoryCodeowners` fails, include the GitHub API `message` field (e.g. repository rule violations) in the error output

## Goliac v1.9.7

- build: `make build_ui` now uses Yarn via Corepack to avoid npm optional-dependency issues with Rolldown

## Goliac v1.9.6

- update UI dependencies and build tooling (security updates mainly)
- bugfix: GitHub Pages `visibility: public` can now be reconciled even when GitHub rejects `public: true` (retries Pages API call without `public`)
- UI: migrate `browser/goliac-ui` from Vue CLI to Vite 8 (includes `VUE_APP_API_URL` -> `VITE_API_URL`)

## Goliac v1.9.5

- bugfix: when a GitHub repository name matches the teams definition only case-insensitively, reconcile by renaming the repository on GitHub to the exact name from the teams repo (avoids treating it as delete + create)
- bugfix: during `goliac plan` (dry-run), `RenameRepository` now rekeys the in-memory GitHub cache so subsequent steps resolve repositories by the new name (fixes false `repository … not found` after case alignment or YAML `renameTo`)

## Goliac v1.9.4

- Github page code fix for workflow

## Goliac v1.9.3

- Github page code fix when using visibility

## Goliac v1.9.2

- Github page code fix when not using custom_domain

## Goliac v1.9.1

- Github page code race condition fix

## Goliac v1.9.0

- add support for Github pages

## Goliac v1.8.11

- change the way commit title is generated when doing a forcemerge

## Goliac v1.8.10

- bugfix: resolve GitHub app slug to GraphQL node ID when creating branch protection rules with app bypass actors (create path now matches the update path)

## Goliac v1.8.9

- various library updates (security updates)
- improvement: force-merge workflow loads the pull request title from the GitHub API and appends it to the merge commit title
- improvement: force-merge approval comment and merge commit message include the original PR URL and title for traceability

## Goliac v1.8.8

- bugfix: nil pointer fix

## Goliac v1.8.7

- bugfix: add Goliac app on repositories branch protection bypass when the repository is part of a workflow and if a CODEOWNER review is required

## Goliac v1.8.6

- bugfix: retry branch protection mutation with PAT when the integration error is returned
- validation improvement: validate when the required status checks are defined, at least one is required

## Goliac v1.8.5

- improvement: add Goliac app on repositories branch protection bypass when the repository is part of a workflow

## Goliac v1.8.4

- validation improvement: validate ruleset repositories presence
- bugfix: do not reorder CODEOWNERS

## Goliac v1.8.3

- bugfix: validate correctly rulesets when the repository is not defined in the ruleset

## Goliac v1.8.2

- bugfix: repository topics update no longer returns HTTP 422 when `topics` is omitted in the repository definition (send an empty JSON array instead of `null` for GitHub’s replace-topics API)

## Goliac v1.8.1

- manage_org_custom_properties bugfix (it was not fully taken into account)

## Goliac v1.8.0

- configure GitHub Actions environments/variables, repository autolinks, and organization custom properties via `goliac.yaml` under `features` instead of environment variables
- **Breaking / migration:** remove `GOLIAC_MANAGE_GITHUB_ACTIONS_VARIABLES`, `GOLIAC_MANAGE_GITHUB_AUTOLINKS`, and `GOLIAC_MANAGE_ORG_CUSTOM_PROPERTIES` from your deployment; set `features.manage_github_env_and_variables`, `features.manage_github_autolinks`, and `features.manage_org_custom_properties` in `goliac.yaml`. If the `features` block is omitted, all three still default to `true` (same as the former env defaults)
- plan/apply loads `goliac.yaml` before warming the GitHub remote cache so those flags apply to the first load
- update the documentation regarding Github ruleset integration issues

## Goliac v1.7.2

- validate that teams in codeowners are writer of the repo
- fix 409 Conflict on codeowners update

## Goliac v1.7.1

- remove deleted teams from managed codeowner
- fix loading of codeowners file

## Goliac v1.7.0

- add codeowner support

## Goliac v1.6.10

- bugfix: dont panic on nil ruleset

## Goliac v1.6.9

- bugfix: treat custom properties load failure as loadRepositories failure

## Goliac v1.6.8

- bugfix: treat custom properties load failure as loadRepositories failure

## Goliac v1.6.7

- couple of security updates
- add extra 2xx test in GraphQL call
- fix team membership removal mutation

## Goliac v1.6.6

- bugfix: lowercase allowed_merge_methods for merge settings

## Goliac v1.6.5

- add only non-archived repositories to the organization ruleset

## Goliac v1.6.4

- bugfix: validate mergeMethod compatibility with repository merge settings
- bugfix: validate groupingStrategy validity
- security update: node-forge library update to 1.3.2

## Goliac v1.6.3

- bugfix: set default values for ruleset rules

## Goliac v1.6.2

- add support for merge queue in branch protections ruleset
- add also support for allowed_merge_methods in pull request ruleset

## Goliac v1.6.1

- bugfix: fix Github rate limiting headers case
- add branch protection bypass user validation

## Goliac v1.6.0

- add support for topics
- better Github rate limiting support

## Goliac v1.5.2

- custom properties scan can be disabled
- fix branch protection bugs: when requiredStatusCheckContexts is not defined and ignore dismissesStaleReviews/requiresCodeOwnerReviews/requireLastPushApproval when requiresApprovingReviews is false

## Goliac v1.5.1

- bugfix with Custom Properties, when the value is ''

## Goliac v1.5.0

- introducing repository Custom Properties

## Goliac v1.4.2

- bugfix: bad "Pull request and description" label

## Goliac v1.4.1

- bugfix: allow_squash_merge/allow_rebase_merge/allow_merge_commit validation enhancement and fix

## Goliac v1.4.0

- add support for allow_squash_merge/allow_rebase_merge/allow_merge_commit

## Goliac v1.3.8

- deal correctly when a team is added as reader and a writer at the same time as the repository's owner

## Goliac v1.3.7

- small improvement: deal correctly when a team is added as reader and a writer at the same time on a repository

## Goliac v1.3.6

- bugfix for branch_name_pattern/tag_name_pattern ruleset

## Goliac v1.3.5

- bugfix for branch_name_pattern/tag_name_pattern ruleset

## Goliac v1.3.4

- bugfix for branch_name_pattern/tag_name_pattern ruleset

## Goliac v1.3.3

- security update (npm axios library)
- support for branch_name_pattern/tag_name_pattern ruleset type

## Goliac v1.3.2

- fix app id for graphql call
- fix autolink updates

## Goliac v1.3.1

- support of apps in branch protection bypass and global ruleset

## Goliac v1.3.0

- introducing branch protection bypass (users/teams)

## Goliac v1.2.2

- bugfix: support disabled ruleset

## Goliac v1.2.1

- security update: nodejs form-data update to 4.0.4
- enhance error message (especially naming explicitely the repository or team involved)
- Github client bugfix: when PAT is provided, dont try to read the (nonprovided) private key

## Goliac v1.2.0

- internal refactor to make the code more modular (and introduce reconciliator "datasource")
- refactor UX CLI, in particular the progressbar (and the lazy loading)
- vue/cli-service security update

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
