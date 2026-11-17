package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/goliac-project/goliac/internal/config"
)

type StepPluginSlack struct {
	SlackUrl   string
	SlackToken string
	Channel    string
}

func NewStepPluginSlack() StepPlugin {
	return &StepPluginSlack{
		SlackUrl:   "https://slack.com/api/chat.postMessage",
		SlackToken: config.Config.SlackToken,
		Channel:    config.Config.SlackChannel,
	}
}

type SlackMessage struct {
	Channel string `json:"channel"`
	Text    string `json:"text"`
}

// slackEscape escapes the characters Slack interprets in a message (https://api.slack.com/reference/surfaces/formatting#escaping):
// "&", "<" and ">" (a "<!channel>" or "<https://evil|link>" written in an explanation must not become a mention or a link)
func slackEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return strings.ReplaceAll(s, ">", "&gt;")
}

func (f *StepPluginSlack) Execute(ctx context.Context, username, workflowDescription, explanation string, url *url.URL, properties map[string]interface{}) (string, error) {
	channel := f.Channel
	if properties["channel"] != nil {
		channel = properties["channel"].(string)
	}

	urlpath := ""
	if url != nil {
		urlpath = "(" + url.Path + ")"
	}
	message := fmt.Sprintf("Workflow %s %s was submited by %s with explanation: %s", slackEscape(workflowDescription), slackEscape(urlpath), slackEscape(username), slackEscape(explanation))
	// Prepare the message payload
	msg := SlackMessage{
		Channel: channel,
		Text:    message,
	}

	// Convert the payload to JSON
	jsonPayload, err := json.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %v", err)
	}

	// Create a new HTTP POST request
	req, err := http.NewRequest("POST", f.SlackUrl, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", fmt.Errorf("failed to create new request: %v", err)
	}

	// Set the required headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.SlackToken)

	// Make the HTTP request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// Check the response from Slack API
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("received non-200 response: %v", resp.Status)
	}

	return "", nil
}
