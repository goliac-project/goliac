package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateForcemerge(t *testing.T) {
	assert.Nil(t, ValidateForcemerge(60, 600))
	assert.Nil(t, ValidateForcemerge(1, 1))
	for _, wait := range []int{0, -1} {
		err := ValidateForcemerge(wait, 600)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "GOLIAC_WORKFLOW_FORCEMERGE_WAIT_SECONDS must be a positive number of seconds")
	}
	for _, timeout := range []int{0, -5} {
		err := ValidateForcemerge(60, timeout)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "GOLIAC_WORKFLOW_FORCEMERGE_TIMEOUT_SECONDS must be a positive number of seconds")
	}
}

func TestForcemergeDefaults(t *testing.T) {
	assert.Equal(t, 60, Config.WorkflowForcemergeWaitSeconds)
	assert.Equal(t, 600, Config.WorkflowForcemergeTimeoutSeconds)
}
