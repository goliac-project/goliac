# Team

Under the /teams directory, you can create one subdirectory per team.


## Create a new team

If you want to create a new team (like `foobar`), you need to create a PR with a `/teams/foobar/team.yaml` file:

```yaml
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
```

The users defined there are in 2 different categories
- members: are part of the team (and will be writer on all repositories of the team)
- owners: are part of the team (and will be writer on all repositories of the team) AND can approve PR in the `foobar` teams repository (when you want to change a team definition, or when you want to create/update a repository definition)

The users name used are the one defined in the `/users` sub directories (like `alice`)


## Externally managed team

If the definition of a team is externally managed (your IT team is responsible to push the definition of a team via a tool/script), you can set a specific property to tell Goliac to not own/enforce the definition of a team:


```yaml
apiVersion: v1
kind: Team
name: AnotherTeam
spec:
  externallyManaged: true
```

It means that Goliac will not try to enforce the team definition, but will take it as it is in Github. It will only use the team to manage the repositories.

## Slack channels

A team can record the Slack channels where it wants to be reached. Goliac doesn't use these values itself; other tools read them from the teams repository (for example, the GitHub App that posts review requests to the team's channel).

```yaml
apiVersion: v1
kind: Team
name: foobar
spec:
  owners:
    - user1
    - user2
  slack:
    teamChannel: C0123ABCD
    reviewsChannel: C0456EFGH
```

- `teamChannel`: the team's main channel.
- `reviewsChannel`: the channel for review requests. Tools fall back to `teamChannel` when it isn't set.

Both are optional and work on externally managed teams too. The values are Slack channel IDs (in Slack: open the channel, click its name, the ID is at the bottom of the About tab), not channel names, so they keep working when a channel is renamed. Goliac rejects a value that isn't shaped like a channel ID.

Comments in a `team.yaml` are not kept: Goliac rewrites the file when it removes a user who left the organization.
