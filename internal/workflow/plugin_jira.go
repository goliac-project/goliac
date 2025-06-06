package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/goliac-project/goliac/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type StepPluginJira struct {
	AtlassianUrlDomain string // something like "https://mycompany.atlassian.net"
	ProjectKey         string
	Email              string
	ApiToken           string //generate a Jira API token here: https://id.atlassian.com/manage/api-tokens
	IssueType          string
}

func NewStepPluginJira() StepPlugin {
	domain := config.Config.WorkflowJiraAtlassianDomain
	if !strings.HasPrefix(domain, "https://") || !strings.HasPrefix(domain, "http://") {
		domain = "https://" + domain
	}
	domain = strings.TrimSuffix(domain, "/")

	return &StepPluginJira{
		AtlassianUrlDomain: domain,
		ProjectKey:         config.Config.WorkflowJiraProjectKey,
		Email:              config.Config.WorkflowJiraEmail,
		ApiToken:           config.Config.WorkflowJiraApiToken,
		IssueType:          config.Config.WorkflowJiraIssueType,
	}
}

type JiraText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type JiraParagraph struct {
	Type    string     `json:"type"`
	Content []JiraText `json:"content"`
}

type JiraADFDescription struct {
	Type    string          `json:"type"`
	Version int             `json:"version"`
	Content []JiraParagraph `json:"content"`
}

type JiraIssue struct {
	Fields JiraFields `json:"fields"`
}

type JiraFields struct {
	Project     JiraProject        `json:"project"`
	Summary     string             `json:"summary"`
	Description JiraADFDescription `json:"description"`
	Issuetype   IssueType          `json:"issuetype"`
}

type JiraProject struct {
	Key string `json:"key"`
}

type IssueType struct {
	Name string `json:"name"`
}

type CreateIssueResponse struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Self string `json:"self"`
}

func (f *StepPluginJira) Execute(ctx context.Context, username, workflowDescription, explanation string, url *url.URL, properties map[string]interface{}) (string, error) {
	jiraURL := fmt.Sprintf("%s/rest/api/3/issue", f.AtlassianUrlDomain)
	projectKey := f.ProjectKey
	issueType := f.IssueType
	if properties["project_key"] != nil {
		projectKey = properties["project_key"].(string)
	}
	if properties["issue_type"] != nil {
		issueType = properties["issue_type"].(string)
	}

	urlstring := ""
	if url != nil {
		urlstring = "(" + url.String() + ")"
	}
	description := JiraADFDescription{
		Type:    "doc",
		Version: 1,
		Content: []JiraParagraph{
			{
				Type: "paragraph",
				Content: []JiraText{
					{Type: "text", Text: fmt.Sprintf("Workflow %s %s was requested by user %s:", workflowDescription, urlstring, username)},
					{Type: "text", Text: explanation},
				},
			},
		},
	}
	issue := JiraIssue{
		Fields: JiraFields{
			Project:     JiraProject{Key: projectKey},
			Summary:     "Goliac workflow: " + workflowDescription,
			Description: description,
			Issuetype:   IssueType{Name: issueType}, // or "Bug", "Story", etc.
		},
	}

	jsonData, err := json.Marshal(issue)
	if err != nil {
		return "", fmt.Errorf("error marshalling JSON: %s", err)
	}

	var childSpan trace.Span
	if config.Config.OpenTelemetryEnabled {
		if config.Config.OpenTelemetryTraceAll {
			// get back the tracer from the context
			ctx, childSpan = otel.Tracer("goliac").Start(ctx, fmt.Sprintf("StepPluginJira.Execute %s", jiraURL))
			defer childSpan.End()

			childSpan.SetAttributes(
				attribute.String("method", "POST"),
				attribute.String("endpoint", jiraURL),
				attribute.String("body", string(jsonData)),
				attribute.String("auth user", f.Email),
				attribute.String("auth pass", f.ApiToken[:20]+"***"),
			)
		}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", jiraURL, bytes.NewBuffer(jsonData))
	if err != nil {
		if childSpan != nil {
			childSpan.SetStatus(codes.Error, err.Error())
		}
		return "", fmt.Errorf("error creating request: %s", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(f.Email, f.ApiToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		if childSpan != nil {
			childSpan.SetStatus(codes.Error, err.Error())
		}
		return "", fmt.Errorf("error sending request: %s", err)
	}
	defer resp.Body.Close()

	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		bodyContent, _ := io.ReadAll(resp.Body)
		if childSpan != nil {
			childSpan.SetStatus(codes.Error, string(bodyContent))
		}
		return "", fmt.Errorf("failed to create issue. Status: %s (%s)", resp.Status, bodyContent)
	}

	var issueResponse CreateIssueResponse
	err = json.NewDecoder(resp.Body).Decode(&issueResponse)
	if err != nil {
		return "", fmt.Errorf("error decoding response: %s", err)
	}
	issueURL := fmt.Sprintf("%s/browse/%s", f.AtlassianUrlDomain, issueResponse.Key)
	return issueURL, nil
}
