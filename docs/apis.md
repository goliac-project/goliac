# External APIs

If you need to do some Github operations synchronously from another service, you can use the following APIs.

## Get a list of all repositories in an organization

```bash
curl http://127.0.0.1:18000/api/v1/repositories
```


## Get a list of all users in an organization

```bash
curl http://127.0.0.1:18000/api/v1/users
```


## Get a list of all teams in an organization

```bash
curl http://127.0.0.1:18000/api/v1/teams
```


## Create a new repository

You will need to give Goliac app some more permissions, in particular
- in `Repository permissions`:
  - `Read & write`

It is because it needs to approve the repository creation PR on your behalf.

```bash
curl -X POST -H 'Content-Type: application/json' http://localhost:18000/api/v1/external/createrepository -d '{"github_token":"ghp_111111111111111111111111111111111111", "team_name":"team_123", "repository_name": "repo_456"}'
```


## Further reading

For the exhaustive list of available APIs, please refer to the [API documentation](api_docs/index.html).

