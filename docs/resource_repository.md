# Repository

Under a team directory, you find the `team.yaml` definition of the team (see (Team)[resource_team]), but also one yaml file per repository owned by the team

Special case: if you want to archive a repository, just create/move the repository yaml file under the `/archived` directory (see below)

## Create a repository

On a given team subdirectory you can create a repository definition via a yaml file (like `/teams/foobar/awesome-repository.yaml`):

```yaml
apiVersion: v1
kind: Repository
name: awesome-repository
```

This will create a `awesome-repository` repository under your organization, that will be
- private by default
- writable by all owners/members of this team (in our example `foobar`)

You can of course tweak that:

```yaml
apiVersion: v1
kind: Repository
name: awesome-repository
spec:
  visibility: public
  allow_auto_merge: true
  delete_branch_on_merge: true
  allow_update_branch: true
  writers:
  - anotherteamA
  - anotherteamB
  readers:
  - anotherteamC
  - anotherteamD
```

In this last example:
- the repository is now public
- the repository allows auto merge
- the repository will delete the branch on merge
- the repository allows to update the branch
- other teams have write (`anotherteamA`, `anotherteamB`) or read (`anotherteamC`, `anotherteamD`) access

## Set default branch repository

By default the default branch is `main`.
You can specify a different default branch

```yaml
apiVersion: v1
kind: Repository
name: awesome-repository
spec:
  default_branch: master
```

## Create a repository from a fork

You can also create the repository from a fork:

```yaml
apiVersion: v1
kind: Repository
name: awesome-repository
forkFrom: another_org/another_repository
spec:
  ...
```

Note: you cannot change the visibility of a forked repository


## Rename a repository

You need to add a `renameTo` to the repository, and Goliac will rename it (and update the `goliac-teams` repository):

```yaml
apiVersion: v1
kind: Repository
name: awesome-repository
spec:
  visibility: public
  ...
renameTo: anotherName
```


## Archive a repository

You can archive a repository, by a PR that move the yaml repository file into the `/archived` directory

## Adding repository ruleset

You can add different rules on a specific repository (like branch protection) using the new Github rulesets.
Few rules are currently supported (but the software can be easily extended): `pull_request`, `required_signatures`, `required_status_checks`, `creation`, `update`, `deletion`, `non_fast_forward`, `required_linear_history`

Note: team or app bypass is not supported yet

```yaml
apiVersion: v1
kind: Repository
name: awesome-repository
spec:
  visibility: public
  ...
  rulesets:
    - name: myruleset
      enforcement: active # disabled, active, evaluate
      conditions:
        include: 
          - "~DEFAULT_BRANCH" # ~DEFAULT_BRANCH, ~ALL, branch_name, ...
      rules:
        - ruletype: required_signatures
```

`pull_request` example

```yaml
apiVersion: v1
kind: Repository
name: awesome-repository
spec:
  visibility: public
  ...
  rulesets:
    - name: myruleset
      enforcement: active # disabled, active, evaluate
      conditions:
        include: 
          - develop
      rules:
        - ruletype: pull_request
          parameters: # dismissStaleReviewsOnPush, requireCodeOwnerReview, requiredApprovingReviewCount, requiredReviewThreadResolution, requireLastPushApproval
            requiredApprovingReviewCount: 1
            requireLastPushApproval: true
            ...
```

`required_status_checks` example

```yaml
apiVersion: v1
kind: Repository
name: awesome-repository
spec:
  visibility: public
  ...
  rulesets:
    - name: myruleset
      enforcement: active # disabled, active, evaluate
      conditions:
        include: 
          - "~ALL"
      rules:
        - ruletype: required_status_checks
          parameters: # requiredStatusChecks, strictRequiredStatusChecksPolicy
            requiredStatusChecks:
              - my_check
```

## Adding repository branch protection

On top of repository ruleset, you can also you the older bbranch protection.

Most but not all branch protection features are currently supported (but the software can be easily extended): `requires_approving_reviews`, `required_approving_review_count`, `dismisses_stale_reviews`, `requires_code_owner_reviews`, `require_last_push_approval`, `requires_status_checks`, `requires_strict_status_checks`, `required_status_check_contexts`, `requires_conversation_resolution`, `requires_commit_signatures`, `requires_linear_history`, `allows_force_pushes`, `allows_deletions`


```yaml
apiVersion: v1
kind: Repository
name: awesome-repository
spec:
  visibility: public
  ...
  branch_protections:
    - pattern: master
      requires_approving_reviews: true
      required_approving_review_count: 1
      dismisses_stale_reviews: true
      requires_code_owner_reviews: true
      require_last_push_approval: true
      requires_status_checks: true
      requires_strict_status_checks: true
      required_status_check_contexts:
        - my_check
      requires_conversation_resolution: true
      requires_commit_signatures: true
      requires_linear_history: true
      allows_force_pushes: false
      allows_deletions: false
```

`pull_request` example

```yaml
apiVersion: v1
kind: Repository
name: awesome-repository
spec:
  visibility: public
  ...
  branch_protections:
    - pattern: master
      requires_approving_reviews: true
      required_approving_review_count: 1
```

## Github action variables and secrets

You can define Github action variables in the repository definition

```yaml
apiVersion: v1
kind: Repository
name: awesome-repository
spec:
  ...
  actions_variables:
    VAR1: VALUE1
    VAR2: VALUE2
```

You cannot set secrets via Goliac, but you can still use the [gh CLI](https://cli.github.com/) to set secrets like

```shell
gh secret set SECRET1 --repo <my organization>/<repository> --body "value"
```

## Environments variables and secrets


You can define Environment variables in the repository definition

```yaml
apiVersion: v1
kind: Repository
name: awesome-repository
spec:
  ...
  environments:
  - name: staging
    variables:
      VAR1: VALUE1
      VAR2: VALUE2
```

You cannot set environment secrets via Goliac, but you can still use the [gh CLI](https://cli.github.com/) to set secrets like

```shell
gh secret set SECRET1 --env staging --repo <my organization>/<repository> --body "value"
```

## Add external users to a repository

If you want to give access (read or write) to users external to your organization, you need to
- add a definition of the user in the `/users/external` directory 
- add them to the repository

To add them to the repository you can use the `externalUserReaders` and `externalUserWriters` properties, like

```yaml
apiVersion: v1
kind: Repository
name: awesome-repository
spec:
  externalUserReaders:
    - alice
    ...
  externalUserWriters:
    - bob
    ...
```

where `alice` for example has been defined in the `/users/external/alice.yaml` like:

```yaml
apiVersion: v1
kind: User
name: alice
spec:
  githubID: aliceGithubUserName
```

## Add Autolink

Github has a feature called autolinks [documentatopn](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/managing-repository-settings/configuring-autolinks-to-reference-external-resources)

You can set them in the repository like:

```yaml
apiVersion: v1
kind: Repository
name: awesome-repository
spec:
  autolinks:
    - key_prefix: TICKET-
      url_template: https://example.com/TICKET?query=<num>
      is_alphanumeric: true
```

Note: if you need to remove them, use the following convention:

```yaml
apiVersion: v1
kind: Repository
name: awesome-repository
spec:
  autolinks: []
```
