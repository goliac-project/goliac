package entity

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v5"
	"github.com/goliac-project/goliac/internal/config"
	"github.com/goliac-project/goliac/internal/observability"
	"github.com/goliac-project/goliac/internal/utils"
	"gopkg.in/yaml.v3"
)

type Team struct {
	Entity `yaml:",inline"`
	Spec   struct {
		ExternallyManaged bool     `yaml:"externallyManaged,omitempty"`
		Owners            []string `yaml:"owners,omitempty"`
		Members           []string `yaml:"members,omitempty"`
	} `yaml:"spec"`
	ParentTeam *string `yaml:"-"`
}

/*
 * NewTeam reads a file and returns a Team object
 * The next step is to validate the Team object using the Validate method
 */
func NewTeam(fs billy.Filesystem, filename string, parent *string) (*Team, error) {
	filecontent, err := utils.ReadFile(fs, filename)
	if err != nil {
		return nil, err
	}

	team := &Team{}
	err = yaml.Unmarshal(filecontent, team)
	if err != nil {
		return nil, err
	}

	if parent != nil {
		team.ParentTeam = parent
	}

	return team, nil
}

/**
 * ReadTeamDirectory reads all the files in the dirname directory and returns
 * - a map of Team objects
 * - a slice of errors that must stop the validation process
 * - a slice of warning that must not stop the validation process
 */
func ReadTeamDirectory(fs billy.Filesystem, dirname string, users map[string]*User, errorCollector *observability.ErrorCollection) map[string]*Team {
	teams := make(map[string]*Team)

	exist, err := utils.Exists(fs, dirname)
	if err != nil {
		errorCollector.AddError(err)
		return teams
	}
	if !exist {
		return teams
	}
	// Parse all the teams in the dirname directory
	entries, err := fs.ReadDir(dirname)
	if err != nil {
		errorCollector.AddError(err)
		return teams
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// skipping files starting with '.'
		if e.Name()[0] == '.' {
			continue
		}

		recursiveReadTeamDirectory(fs, filepath.Join(dirname, e.Name()), nil, users, teams, errorCollector)
	}
	return teams
}

func recursiveReadTeamDirectory(fs billy.Filesystem, dirname string, parentTeam *string, users map[string]*User, teams map[string]*Team, errorCollector *observability.ErrorCollection) {

	team, err := NewTeam(fs, filepath.Join(dirname, "team.yaml"), parentTeam)
	if err != nil {
		errorCollector.AddError(fmt.Errorf("team not found in %s: %v", dirname, err))
		return
	} else {
		team.Validate(dirname, users, errorCollector)
		if errorCollector.HasErrors() {
			return
		} else {
			teams[team.Name] = team
		}
	}

	parent := team.Name

	// Parse all the subteams in the dirname directory
	entries, err := fs.ReadDir(dirname)
	if err != nil {
		errorCollector.AddError(err)
		return
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// skipping files starting with '.'
		if e.Name()[0] == '.' {
			continue
		}
		if _, ok := teams[e.Name()]; ok {
			errorCollector.AddError(fmt.Errorf("team %s already exists in %s", e.Name(), dirname))
			continue
		}

		recursiveReadTeamDirectory(fs, filepath.Join(dirname, e.Name()), &parent, users, teams, errorCollector)
	}
}

func (t *Team) Validate(dirname string, users map[string]*User, errorCollector *observability.ErrorCollection) {
	if t.ApiVersion != "v1" {
		errorCollector.AddError(fmt.Errorf("invalid apiVersion: %s for team filename %s/team.yaml", t.ApiVersion, dirname))
		return
	}

	if t.Kind != "Team" {
		errorCollector.AddError(fmt.Errorf("invalid kind: %s for team filename %s/team.yaml", t.Kind, dirname))
		return
	}

	if t.Name == "" {
		errorCollector.AddError(fmt.Errorf("metadata.name is empty for team filename %s/team.yaml", dirname))
		return
	}

	if t.Name == "everyone" {
		errorCollector.AddError(fmt.Errorf("team name 'everyone' is reserved for team filename %s/team.yaml", dirname))
		return
	}

	if strings.HasSuffix(t.Name, config.Config.GoliacTeamOwnerSuffix) {
		errorCollector.AddError(fmt.Errorf("metadata.name cannot finish with '%s' for team filename %s/team.yaml. It is a reserved suffix", config.Config.GoliacTeamOwnerSuffix, dirname))
		return
	}

	teamname := filepath.Base(dirname)
	if t.Name != teamname {
		errorCollector.AddError(fmt.Errorf("invalid metadata.name: %s for team filename %s/team.yaml", t.Name, dirname))
		return
	}

	if t.Spec.ExternallyManaged {
		if len(t.Spec.Owners) > 0 {
			errorCollector.AddError(fmt.Errorf("externallyManaged team cannot have owners for team filename %s/team.yaml", dirname))
			return
		}
		if len(t.Spec.Members) > 0 {
			errorCollector.AddError(fmt.Errorf("externallyManaged team cannot have members for team filename %s/team.yaml", dirname))
			return
		}
	}

	for _, owner := range t.Spec.Owners {
		if _, ok := users[owner]; !ok {
			errorCollector.AddError(fmt.Errorf("invalid owner: %s doesn't exist in team filename %s/team.yaml", owner, dirname))
			return
		}
	}

	for _, member := range t.Spec.Members {
		if _, ok := users[member]; !ok {
			errorCollector.AddError(fmt.Errorf("invalid member: %s doesn't exist in team filename %s/team.yaml", member, dirname))
			return
		}
	}

	// warnings

	if len(t.Spec.Owners) < 2 && !t.Spec.ExternallyManaged {
		errorCollector.AddWarn(fmt.Errorf("not enough owners for team filename %s/team.yaml", dirname))
	}
}

/**
 * AdjustTeamDirectory adjust team's defintion depending on user availability.
 * The goal is that if a user has been removed, we must update the team definition.
 * Returns:
 * - a list of (team's) file changes (to commit to Github)
 */
func ReadAndAdjustTeamDirectory(fs billy.Filesystem, dirname string, users map[string]*User) ([]string, error) {
	teamschanged := []string{}

	exist, err := utils.Exists(fs, dirname)
	if err != nil {
		return teamschanged, err
	}
	if !exist {
		return teamschanged, nil
	}

	// Parse all the teams in the dirname directory
	entries, err := fs.ReadDir(dirname)
	if err != nil {
		return teamschanged, err
	}

	for _, e := range entries {
		if e.IsDir() {
			if e.Name()[0] == '.' {
				continue
			}
			err := recursiveReadAndAdjustTeamDirectory(fs, filepath.Join(dirname, e.Name()), nil, users, &teamschanged)
			if err != nil {
				return teamschanged, err
			}
		}
	}
	return teamschanged, nil
}

func recursiveReadAndAdjustTeamDirectory(fs billy.Filesystem, dirname string, parent *string, users map[string]*User, teamschanged *[]string) error {
	team, err := NewTeam(fs, filepath.Join(dirname, "team.yaml"), parent)
	if err != nil {
		return err
	} else {
		changed, err := team.Update(fs, filepath.Join(dirname, "team.yaml"), users)
		if err != nil {
			return err
		}
		if changed {
			*teamschanged = append(*teamschanged, filepath.Join(dirname, "team.yaml"))
		}
	}

	// Parse all the teams in the dirname directory
	entries, err := fs.ReadDir(dirname)
	if err != nil {
		return err
	}

	parentTeam := team.Name
	for _, e := range entries {
		if e.IsDir() {
			if e.Name()[0] == '.' {
				continue
			}
			err := recursiveReadAndAdjustTeamDirectory(fs, filepath.Join(dirname, e.Name()), &parentTeam, users, teamschanged)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Update is telling if the team needs to be adjust (and the team's definition was changed on disk),
// based on the list of (still) existing users
func (t *Team) Update(fs billy.Filesystem, filename string, users map[string]*User) (bool, error) {
	changed := false
	owners := make([]string, 0)
	for _, owner := range t.Spec.Owners {
		if _, ok := users[owner]; !ok {
			changed = true
		} else {
			owners = append(owners, owner)
		}
	}
	t.Spec.Owners = owners

	members := make([]string, 0)
	for _, member := range t.Spec.Members {
		if _, ok := users[member]; !ok {
			changed = true
		} else {
			members = append(members, member)
		}
	}
	t.Spec.Members = members

	file, err := fs.Create(filename)
	if err != nil {
		return changed, fmt.Errorf("not able to create file %s: %v", filename, err)
	}
	defer file.Close()

	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)
	err = encoder.Encode(t)
	if err != nil {
		return changed, fmt.Errorf("not able to write file %s: %v", filename, err)
	}
	return changed, err
}
