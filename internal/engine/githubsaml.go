package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/goliac-project/goliac/internal/config"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/goliac-project/goliac/internal/github"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/sirupsen/logrus"
)

const listUsersFromGithubOrgSaml = `
query listSamlUsers($orgLogin: String!, $endCursor: String) {
  organization(login: $orgLogin) {
    samlIdentityProvider {
      ssoUrl
      externalIdentities(first: 100, after: $endCursor) {
        edges {
          node {
            guid
            samlIdentity {
              nameId
            }
            user {
              login
            }
          }
        }
        pageInfo {
          hasNextPage
          endCursor
        }
        totalCount
      }
    }
  }
}`

type GraplQLUsersFromGithubOrgSaml struct {
	Data struct {
		Organization struct {
			SamlIdentityProvider struct {
				ExternalIdentities struct {
					Edges []struct {
						Node struct {
							Guid         string
							SamlIdentity struct {
								NameId string
							}
							User struct {
								Login string
							}
						}
					}
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					} `json:"pageInfo"`
					TotalCount int `json:"totalCount"`
				} `json:"externalIdentities"`
			}
		}
	}
	Errors []struct {
		Path []struct {
			Query string `json:"query"`
		} `json:"path"`
		Extensions struct {
			Code         string
			ErrorMessage string
		} `json:"extensions"`
		Message string
	} `json:"errors"`
}

/*
 * This function works only for Github organization that have the Entreprise plan ANAD use SAML integration
 */
func LoadUsersFromGithubOrgSaml(ctx context.Context, client github.GitHubClient, feedback observability.RemoteObservability) (map[string]*entity.User, error) {
	users := make(map[string]*entity.User)

	variables := make(map[string]interface{})
	variables["orgLogin"] = config.Config.GithubAppOrganization
	variables["endCursor"] = nil

	hasNextPage := true
	count := 0
	for hasNextPage {
		data, err := client.QueryGraphQLAPI(ctx, listUsersFromGithubOrgSaml, variables)
		if err != nil {
			return users, err
		}
		var gResult GraplQLUsersFromGithubOrgSaml

		// parse first page
		err = json.Unmarshal(data, &gResult)
		if err != nil {
			return users, err
		}
		if len(gResult.Errors) > 0 {
			return users, fmt.Errorf("graphql error: %v", gResult.Errors[0].Message)
		}

		for _, c := range gResult.Data.Organization.SamlIdentityProvider.ExternalIdentities.Edges {
			if c.Node.SamlIdentity.NameId == "" {
				logrus.Debugf("Skipping user with empty NameId: %s", c.Node.User.Login)
				continue
			}
			if c.Node.User.Login == "" {
				logrus.Debugf("Skipping user with empty login: %s", c.Node.SamlIdentity.NameId)
				continue
			}
			user := &entity.User{}
			user.ApiVersion = "v1"
			user.Kind = "User"
			user.Name = c.Node.SamlIdentity.NameId
			user.Spec.GithubID = c.Node.User.Login

			users[c.Node.SamlIdentity.NameId] = user
		}

		if feedback != nil {
			feedback.LoadingAsset("github_saml", len(gResult.Data.Organization.SamlIdentityProvider.ExternalIdentities.Edges))
		}

		hasNextPage = gResult.Data.Organization.SamlIdentityProvider.ExternalIdentities.PageInfo.HasNextPage
		variables["endCursor"] = gResult.Data.Organization.SamlIdentityProvider.ExternalIdentities.PageInfo.EndCursor

		count++
		// sanity check to avoid loops
		if count > 100 {
			break
		}
	}

	return users, nil
}

/*
Listing only pending invitations for the organization for login (and not email)
*/
func LoadGithubLoginPendingInvitations(ctx context.Context, client github.GitHubClient) (map[string]bool, error) {

	logrus.Debug("loading pending invitations")
	type PendingInvitation struct {
		Login string
		Email string
	}
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2022-11-28#list-pending-organization-invitations
	body, err := client.CallRestAPI(ctx, fmt.Sprintf("/orgs/%s/invitations", config.Config.GithubAppOrganization),
		"",
		"GET",
		nil,
		nil)

	if err != nil {
		return nil, fmt.Errorf("not able to list github invitations: %v. %s", err, string(body))
	}

	var invitations []PendingInvitation
	err = json.Unmarshal(body, &invitations)
	if err != nil {
		return nil, fmt.Errorf("not able to list github invitations: %v", err)
	}

	users := map[string]bool{}
	for _, i := range invitations {
		if i.Login != "" {
			users[i.Login] = true
		}
	}

	return users, nil
}
