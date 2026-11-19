package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func withSeconds(t *testing.T, wait, timeout int) {
	previousWait, previousTimeout := Config.WorkflowForcemergeWaitSeconds, Config.WorkflowForcemergeTimeoutSeconds
	t.Cleanup(func() {
		Config.WorkflowForcemergeWaitSeconds, Config.WorkflowForcemergeTimeoutSeconds = previousWait, previousTimeout
	})
	Config.WorkflowForcemergeWaitSeconds, Config.WorkflowForcemergeTimeoutSeconds = wait, timeout
}

func TestValidateForcemerge(t *testing.T) {
	withSeconds(t, 60, 600)
	assert.Nil(t, ValidateForcemerge())

	withSeconds(t, 1, 1)
	assert.Nil(t, ValidateForcemerge())

	for _, wait := range []int{0, -1} {
		withSeconds(t, wait, 600)
		err := ValidateForcemerge()
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "GOLIAC_WORKFLOW_FORCEMERGE_WAIT_SECONDS must be a positive number of seconds")
	}
	for _, timeout := range []int{0, -5} {
		withSeconds(t, 60, timeout)
		err := ValidateForcemerge()
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "GOLIAC_WORKFLOW_FORCEMERGE_TIMEOUT_SECONDS must be a positive number of seconds")
	}
}

func TestValidateForcemergeReportsTheRawValue(t *testing.T) {
	withSeconds(t, 60, 600)
	for _, raw := range []string{"abc", "", "12s", "1.5"} {
		t.Setenv("GOLIAC_WORKFLOW_FORCEMERGE_WAIT_SECONDS", raw)
		err := ValidateForcemerge()
		assert.NotNil(t, err, raw)
		assert.Contains(t, err.Error(), "GOLIAC_WORKFLOW_FORCEMERGE_WAIT_SECONDS=")
		assert.Contains(t, err.Error(), "is not a whole number of seconds")
		assert.NotContains(t, err.Error(), "got 0")
	}
	t.Setenv("GOLIAC_WORKFLOW_FORCEMERGE_WAIT_SECONDS", "60")
	t.Setenv("GOLIAC_WORKFLOW_FORCEMERGE_TIMEOUT_SECONDS", "ten")
	err := ValidateForcemerge()
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), `GOLIAC_WORKFLOW_FORCEMERGE_TIMEOUT_SECONDS="ten" is not a whole number of seconds`)

	// a numeric value that is not positive is reported as such
	t.Setenv("GOLIAC_WORKFLOW_FORCEMERGE_TIMEOUT_SECONDS", " 0 ")
	err = ValidateForcemerge()
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "must be a positive number of seconds (got 0)")

	t.Setenv("GOLIAC_WORKFLOW_FORCEMERGE_TIMEOUT_SECONDS", " 30 ")
	assert.Nil(t, ValidateForcemerge())
}

func TestForcemergeDefaults(t *testing.T) {
	assert.Equal(t, 60, Config.WorkflowForcemergeWaitSeconds)
	assert.Equal(t, 600, Config.WorkflowForcemergeTimeoutSeconds)
}
