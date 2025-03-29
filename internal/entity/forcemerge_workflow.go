package entity

import (
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/go-git/go-billy/v5"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/goliac-project/goliac/internal/utils"
	"gopkg.in/yaml.v3"
)

type ForcemergeWorkflow struct {
	Entity `yaml:",inline"`
	Spec   struct {
		Description  string `yaml:"description"`
		Repositories struct {
			Allowed []string `yaml:"allowed"`
			Except  []string `yaml:"except"`
		} `yaml:"repositories"`
		Acls struct {
			Allowed []string `yaml:"allowed"`
			Except  []string `yaml:"except"`
		} `yaml:"acls"`
		Steps []struct {
			Name       string                 `yaml:"name"` // for now only 'jira' is supported
			Properties map[string]interface{} `yaml:"properties"`
		} `yaml:"steps"`
	} `yaml:"spec"`
}

/*
 * NewForcemergeWorkflow reads a file and returns a ForcemergeWorkflow object
 * The next step is to validate the ForcemergeWorkflow object using the Validate method
 */
func NewForcemergeWorkflow(fs billy.Filesystem, filename string) (*ForcemergeWorkflow, error) {
	filecontent, err := utils.ReadFile(fs, filename)
	if err != nil {
		return nil, err
	}

	ForcemergeWorkflow := &ForcemergeWorkflow{}
	err = yaml.Unmarshal(filecontent, ForcemergeWorkflow)
	if err != nil {
		return nil, err
	}

	return ForcemergeWorkflow, nil
}

func (w *ForcemergeWorkflow) Validate(filename string) error {

	if w.ApiVersion != "v1" {
		return fmt.Errorf("invalid apiVersion: %s for ForcemergeWorkflow filename %s", w.ApiVersion, filename)
	}

	if w.Kind != "ForcemergeWorkflow" {
		return fmt.Errorf("invalid kind: %s for ForcemergeWorkflow filename %s", w.Kind, filename)
	}

	if w.Name == "" {
		return fmt.Errorf("metadata.name is empty for ForcemergeWorkflow filename %s", filename)
	}

	filename = filepath.Base(filename)
	if w.Name != filename[:len(filename)-len(filepath.Ext(filename))] {
		return fmt.Errorf("invalid metadata.name: %s for ForcemergeWorkflow filename %s", w.Name, filename)
	}

	for _, step := range w.Spec.Steps {
		if step.Name == "" {
			return fmt.Errorf("step.name is empty for ForcemergeWorkflow filename %s", filename)
		}

		// only one step is allowed for now
		if step.Name != "jira_ticket_creation" && step.Name != "slack_notification" {
			return fmt.Errorf("invalid step.name: %s for ForcemergeWorkflow filename %s", step.Name, filename)
		}
		switch step.Name {
		case "jira_ticket_creation":
			// check if the jiraSpace is set
			jiraProjectSet := false
			for k, v := range step.Properties {
				if k == "project_key" {
					jiraProjectSet = true
					if v == "" {
						return fmt.Errorf("step.jira_ticket_creation.properties.project_key is empty for ForcemergeWorkflow filename %s", filename)
					}
				}
			}
			if !jiraProjectSet {
				return fmt.Errorf("step.jira_ticket_creation.properties.project_key is not set for ForcemergeWorkflow filename %s", filename)
			}
		case "slack_notification":
			// check if the slackChannel is set
			slackChannelSet := false
			for k, v := range step.Properties {
				if k == "channel" {
					slackChannelSet = true
					if v == "" {
						return fmt.Errorf("step.slack_notification.properties.channel is empty for ForcemergeWorkflow filename %s", filename)
					}
				}
			}
			if !slackChannelSet {
				return fmt.Errorf("step.slack_notification.properties.channel is not set for ForcemergeWorkflow filename %s", filename)
			}
		}
	}

	return nil
}

func (w *ForcemergeWorkflow) PassAcl(usernameTeams []string, repository string) bool {
	// checking the repository name
	repoMatch := false
	for _, repo := range w.Spec.Repositories.Allowed {
		if repo == "~ALL" {
			repoMatch = true
			break
		}
		repoRegex, err := regexp.Match(fmt.Sprintf("^%s$", repo), []byte(repository))
		if err != nil {
			return false
		}
		if repoRegex {
			repoMatch = true
			break
		}
	}

	for _, repo := range w.Spec.Repositories.Except {
		repoRegex, err := regexp.Match(fmt.Sprintf("^%s$", repo), []byte(repository))
		if err != nil {
			return false
		}
		if repoRegex {
			return false
		}
	}

	if !repoMatch {
		return false
	}

	// checking if (one of) the team is allowed

	teamsOwned := make(map[string]bool)
	for _, team := range usernameTeams {
		teamsOwned[team] = false
	}

	if len(w.Spec.Acls.Allowed) > 0 {
		for _, allowed := range w.Spec.Acls.Allowed {
			if allowed == "~ALL" {
				break
			}
			for _, team := range usernameTeams {
				teamRegex, err := regexp.Match(fmt.Sprintf("^%s$", allowed), []byte(team))
				if err != nil {
					return false
				}
				if teamRegex {
					teamsOwned[team] = true
				}
			}
		}
	} else {
		for _, team := range usernameTeams {
			teamsOwned[team] = true
		}
	}

	if len(w.Spec.Acls.Except) > 0 {
		for _, except := range w.Spec.Acls.Except {
			for _, team := range usernameTeams {
				teamRegex, err := regexp.Match(fmt.Sprintf("^%s$", except), []byte(team))
				if err != nil {
					return false
				}
				if teamRegex {
					teamsOwned[team] = false
				}
			}
		}
	}

	for _, v := range teamsOwned {
		if v {
			return true
		}
	}

	return false
}

func ReadForcemergeWorkflowDirectory(fs billy.Filesystem, dirname string, errorCollection *observability.ErrorCollection) map[string]*ForcemergeWorkflow {
	ForcemergeWorkflows := make(map[string]*ForcemergeWorkflow)

	exist, err := utils.Exists(fs, dirname)
	if err != nil {
		errorCollection.AddError(err)
		return ForcemergeWorkflows
	}
	if !exist {
		return ForcemergeWorkflows
	}

	// Parse all the ForcemergeWorkflows in the dirname directory
	entries, err := fs.ReadDir(dirname)
	if err != nil {
		errorCollection.AddError(err)
		return ForcemergeWorkflows
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// skipping files starting with '.'
		if e.Name()[0] == '.' {
			continue
		}
		ForcemergeWorkflow, err := NewForcemergeWorkflow(fs, filepath.Join(dirname, e.Name()))
		if err != nil {
			errorCollection.AddError(err)
		} else {
			err := ForcemergeWorkflow.Validate(filepath.Join(dirname, e.Name()))
			if err != nil {
				errorCollection.AddError(err)
			} else {
				ForcemergeWorkflows[ForcemergeWorkflow.Name] = ForcemergeWorkflow
			}

		}
	}
	return ForcemergeWorkflows
}
