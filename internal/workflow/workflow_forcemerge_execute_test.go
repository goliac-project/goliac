package workflow

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/goliac-project/goliac/internal/engine"
	"github.com/goliac-project/goliac/internal/entity"
	"github.com/stretchr/testify/assert"
)

type recordingPlugin struct {
	mu   sync.Mutex
	urls []string
	err  error
	// empty: the step has nothing to report (an audit step, for instance)
	empty bool
}

func (p *recordingPlugin) Execute(ctx context.Context, username, workflowDescription, explanation string, u *url.URL, properties map[string]interface{}) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.urls = append(p.urls, u.String())
	if p.err != nil {
		return "", p.err
	}
	if p.empty {
		return "", nil
	}
	return "step:" + u.Path, nil
}

// executeFixture returns a Forcemerge wired to a real WorkflowService (ACL + workflow lookup) over the fake GitHub
func executeFixture(t *testing.T, gh *fakeGitHub, plugin StepPlugin) *ForcemergeImpl {
	lTeam := &entity.Team{}
	lTeam.Name = "test-team"
	lTeam.Spec.Owners = []string{"test-user"}
	luser := entity.User{}
	luser.Name = "test-user"
	luser.Spec.GithubID = "test-user"
	local := &LocalResourceMock{
		MockWorkflows: map[string]*entity.Workflow{"fmtest": fixtureForcemergeWorkflow()},
		LTeams:        map[string]*entity.Team{"test-team": lTeam},
		LUsers:        map[string]*entity.User{"test-user": &luser},
	}
	remote := &RemoteResourceMock{RTeams: map[string]*engine.GithubTeam{}}

	fc, _ := newForcemergeForTest(t, gh)
	fc.ws = NewWorkflowService("myorg", local, remote, gh)
	fc.stepPlugins = map[string]StepPlugin{"jira_ticket_creation": plugin}
	return fc
}

func exec(fc *ForcemergeImpl, n int, mode string, dryrun bool) ([]string, error) {
	props := map[string]string{"pr_url": prURL(n)}
	if mode != "" {
		props[PropertyMergeMode] = mode
	}
	return fc.ExecuteWorkflow(context.Background(), []string{"fmtest"}, "test-user", "fmtest", "because", props, dryrun)
}

func TestForcemergeExecuteWorkflow(t *testing.T) {
	t.Run("input validation", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		fc := executeFixture(t, gh, &recordingPlugin{})
		for props, expected := range map[string]string{
			"":            "prPathToMerge is empty",
			"http://[::1": "not a valid URL",
			"https://x/y": "not a valid PR URL",
		} {
			_, err := fc.ExecuteWorkflow(context.Background(), []string{"fmtest"}, "test-user", "fmtest", "because", map[string]string{"pr_url": props}, false)
			assert.NotNil(t, err, props)
			assert.Contains(t, err.Error(), expected, props)
		}
		_, err := exec(fc, 32, "sideways", false)
		assert.Contains(t, err.Error(), `unknown merge mode "sideways"`)
		assert.Equal(t, 0, len(gh.calls))
	})

	t.Run("workflow not allowed: nothing happens", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		fc := executeFixture(t, gh, &recordingPlugin{})
		_, err := fc.ExecuteWorkflow(context.Background(), []string{"fmtest"}, "test-user", "unknown", "because", map[string]string{"pr_url": prURL(32)}, false)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "unable to execute the workflow")
		assert.Equal(t, 0, len(gh.calls))
	})

	t.Run("dry run does nothing", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		plugin := &recordingPlugin{}
		fc := executeFixture(t, gh, plugin)
		res, err := exec(fc, 32, "", true)
		assert.Nil(t, err)
		assert.Nil(t, res)
		assert.Equal(t, 0, len(gh.calls))
		assert.Equal(t, 0, len(plugin.urls))
	})

	t.Run("plain PR: one step run, merged", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		gh.syncMerge = okJSON(`{"merged":true}`)
		plugin := &recordingPlugin{}
		fc := executeFixture(t, gh, plugin)
		res, err := exec(fc, 32, "", false)
		assert.Nil(t, err)
		assert.Equal(t, []string{"step:/myorg/goliac/pull/32"}, res)
		assert.Equal(t, []string{prURL(32)}, plugin.urls)
	})

	t.Run("a step with nothing to report adds no (empty) line to the answer", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		gh.syncMerge = okJSON(`{"merged":true}`)
		plugin := &recordingPlugin{empty: true}
		fc := executeFixture(t, gh, plugin)
		res, err := exec(fc, 32, "", false)
		assert.Nil(t, err)
		assert.Empty(t, res)
		assert.Equal(t, []string{prURL(32)}, plugin.urls)
	})

	t.Run("a refused PR runs no step and changes nothing", func(t *testing.T) {
		prs := fixturePlainPR()
		prs[32].Draft = true
		gh := newFake(t, prs)
		plugin := &recordingPlugin{}
		fc := executeFixture(t, gh, plugin)
		_, err := exec(fc, 32, "", false)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "error when merging the PR: nothing merged: PR #32 is a draft")
		assert.Equal(t, 0, len(plugin.urls))
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("stack merge: the steps run once per PR, with every PR URL, before any side effect", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		plugin := &recordingPlugin{}
		fc := executeFixture(t, gh, plugin)
		firstSideEffect := -1
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method != "GET" && firstSideEffect < 0 {
				plugin.mu.Lock()
				firstSideEffect = len(plugin.urls)
				plugin.mu.Unlock()
			}
			return nil, nil
		}
		res, err := exec(fc, 3, MergeModeStack, false)
		assert.Nil(t, err)
		assert.Equal(t, []string{prURL(1), prURL(2), prURL(3), prURL(4)}, plugin.urls)
		assert.Equal(t, 4, firstSideEffect, "all the steps ran before the first approval")
		assert.Equal(t, 5, len(res))
		assert.Contains(t, res[4], "Force merged (stack) into `main`: #1 PR 1 (by author1)")
	})

	t.Run("direct merge: summary returned", func(t *testing.T) {
		gh := newFake(t, fixtureStackPlain()).allMethods()
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.markMergedOnBranchMerge = true
		plugin := &recordingPlugin{}
		fc := executeFixture(t, gh, plugin)
		res, err := exec(fc, 3, "", false)
		assert.Nil(t, err)
		assert.Equal(t, []string{prURL(3)}, plugin.urls)
		assert.Equal(t, 2, len(res))
		assert.Contains(t, res[1], "Force merged (direct): #3 PR 3 (by author3) into `b4`; retargeted onto `b4`: #2")
	})

	t.Run("a stack that can't be validated runs no step", func(t *testing.T) {
		prs := fixtureStack()
		delete(prs, 1)
		gh := newFake(t, prs).allMethods()
		plugin := &recordingPlugin{}
		fc := executeFixture(t, gh, plugin)
		_, err := exec(fc, 3, MergeModeStack, false)
		assert.NotNil(t, err)
		assert.Equal(t, 0, len(plugin.urls))
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("a step error stops before any side effect", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		fc := executeFixture(t, gh, &recordingPlugin{err: fmt.Errorf("jira down")})
		_, err := exec(fc, 32, "", false)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "error when executing step jira_ticket_creation: jira down")
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("a missing step plugin stops before any side effect", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		fc := executeFixture(t, gh, &recordingPlugin{})
		fc.stepPlugins = map[string]StepPlugin{}
		_, err := exec(fc, 32, "", false)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "plugin jira_ticket_creation not found")
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("a merge error is reported", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		gh.syncMerge = func(call restCall) ([]byte, error) { return httpError("409 Conflict", `{"message":"conflict"}`) }
		fc := executeFixture(t, gh, &recordingPlugin{})
		_, err := exec(fc, 32, "", false)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "error when merging the PR: error merging the PR")
	})
}

func TestForcemergeInFlightGuard(t *testing.T) {
	t.Run("a second force merge of the same PR is refused while the first runs, then allowed", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		release := make(chan struct{})
		entered := make(chan struct{})
		var once sync.Once
		gh.syncMerge = func(call restCall) ([]byte, error) {
			once.Do(func() { close(entered) })
			<-release
			return []byte(`{"merged":true}`), nil
		}
		fc := executeFixture(t, gh, &recordingPlugin{})

		done := make(chan error, 1)
		go func() { _, err := exec(fc, 32, "", false); done <- err }()
		<-entered
		_, err := exec(fc, 32, "", false)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "a force merge of PR #32 is already running")
		close(release)
		assert.Nil(t, <-done)
		assert.Equal(t, 1, len(gh.find("PUT", "/pulls/32/merge")), "merged once")

		// the lock is released
		_, err = exec(fc, 32, "", false)
		assert.Nil(t, err)
	})

	t.Run("a stack merge also guards the other PRs of the stack", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		release := make(chan struct{})
		entered := make(chan struct{})
		var once sync.Once
		gh.asyncMerge = func(call restCall) ([]byte, error) {
			once.Do(func() { close(entered) })
			<-release
			return []byte(asyncMerged), nil
		}
		fc := executeFixture(t, gh, &recordingPlugin{})
		done := make(chan error, 1)
		go func() { _, err := exec(fc, 3, MergeModeStack, false); done <- err }()
		<-entered
		_, err := exec(fc, 4, MergeModeStack, false) // PR 4 is part of the running stack merge
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "already running")
		close(release)
		assert.Nil(t, <-done)
		assert.Equal(t, 4, len(gh.find("PUT", "/merge-async")), "bottom-up, one merge per PR")
	})

	t.Run("a direct merge also guards the parent and the PRs above (adjacent PRs)", func(t *testing.T) {
		for _, adjacent := range []int{2, 4} { // 2 is retargeted by a direct merge of 3, 4 is its parent
			gh := newFake(t, fixtureStackPlain()).allMethods()
			gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
			gh.markMergedOnBranchMerge = true
			fc := executeFixture(t, gh, &recordingPlugin{})
			unlock, err := fc.lock("goliac", adjacent)
			assert.Nil(t, err)
			_, err = exec(fc, 3, "", false)
			assert.NotNil(t, err, adjacent)
			assert.Contains(t, err.Error(), fmt.Sprintf("a force merge of PR #%d is already running", adjacent))
			assert.Equal(t, 0, len(gh.sideEffects()), adjacent)
			unlock()
			// released: it goes through
			_, err = exec(fc, 3, "", false)
			assert.Nil(t, err, adjacent)
		}
	})

	t.Run("a stack merge refused when another PR of the stack is already being merged", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		fc := executeFixture(t, gh, &recordingPlugin{})
		unlock, err := fc.lock("goliac", 1)
		assert.Nil(t, err)
		_, err = exec(fc, 3, MergeModeStack, false)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "already running")
		assert.Equal(t, 0, len(gh.sideEffects()))
		unlock()
	})

	t.Run("different PRs run concurrently on the same fake without races", func(t *testing.T) {
		prs := fixturePlainPR()
		for n := 40; n < 48; n++ {
			prs[n] = &fakePR{Number: n, Title: "p", BaseRef: "main", HeadRef: fmt.Sprintf("f%d", n), HeadSha: "s", State: "open"}
		}
		gh := newFake(t, prs)
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc := executeFixture(t, gh, &recordingPlugin{})
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for n := 40; n < 48; n++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				_, err := exec(fc, n, "", false)
				errs <- err
			}(n)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			assert.Nil(t, err)
		}
		assert.Equal(t, 8, len(gh.find("PUT", "/merge")))
	})
}

func TestNewForcemergeImplDefaults(t *testing.T) {
	ws := NewWorkflowService("myorg", nil, nil, &GithubClientMock{})
	w := NewForcemergeImpl(ws)
	fc, ok := w.(*ForcemergeImpl)
	assert.True(t, ok)
	assert.Equal(t, ws, fc.ws)
	assert.NotNil(t, fc.stepPlugins["jira_ticket_creation"])
	assert.NotNil(t, fc.stepPlugins["slack_notification"])
	assert.NotNil(t, fc.stepPlugins["dynamodb"])
	assert.Zero(t, fc.asyncPollInterval)
	assert.Zero(t, fc.asyncPollTimeout)
	assert.Zero(t, fc.mergedCheckTimeout)
	// real clock by default
	assert.WithinDuration(t, time.Now(), fc.clockNow(), time.Second)
	assert.Nil(t, fc.clockSleep(context.Background(), time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.NotNil(t, fc.clockSleep(ctx, time.Hour))
}

func TestChooseMergeMethod(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name      string
		s         *repoSettings
		requested string
		explicit  bool
		want      string
		wantErr   bool
	}{
		{"merge allowed", &repoSettings{AllowMergeCommit: &yes, AllowSquashMerge: &yes}, "merge", false, "merge", false},
		{"squash only, default", &repoSettings{AllowMergeCommit: &no, AllowSquashMerge: &yes, AllowRebaseMerge: &no}, "merge", false, "squash", false},
		{"squash only, explicit squash", &repoSettings{AllowMergeCommit: &no, AllowSquashMerge: &yes, AllowRebaseMerge: &no}, "squash", true, "squash", false},
		{"rebase only", &repoSettings{AllowMergeCommit: &no, AllowSquashMerge: &no, AllowRebaseMerge: &yes}, "merge", false, "rebase", false},
		{"explicit squash refused", &repoSettings{AllowMergeCommit: &yes, AllowSquashMerge: &no}, "squash", true, "", true},
		{"nothing", &repoSettings{AllowMergeCommit: &no, AllowSquashMerge: &no, AllowRebaseMerge: &no}, "merge", false, "", true},
		{"unknown fields allow", &repoSettings{AllowSquashMerge: &no}, "merge", false, "merge", false},
	}
	for _, c := range cases {
		got, err := chooseMergeMethod(c.s, c.requested, c.explicit)
		assert.Equal(t, c.wantErr, err != nil, c.name)
		assert.Equal(t, c.want, got, c.name)
	}
	assert.True(t, (&repoSettings{}).allows("merge"))
	assert.True(t, (&repoSettings{}).allows("whatever"))
	assert.Equal(t, 0, httpStatus(nil))
	assert.Equal(t, 0, httpStatus(fmt.Errorf("boom")))
	assert.Equal(t, 404, httpStatus(fmt.Errorf("unexpected status: 404 Not Found")))
}

func TestPullRequestInfoHelpers(t *testing.T) {
	pr := &pullRequestInfo{}
	assert.Equal(t, "unknown", pr.author())
	assert.False(t, pr.isStackBottom())
	assert.False(t, pr.sameStack(pr))
	assert.Equal(t, "https://github.com/x/goliac/pull/7", (&ForcemergeImpl{}).prHTMLURL("goliac", &pullRequestInfo{Number: 7, HTMLURL: "https://github.com/x/goliac/pull/7"}))
	assert.Contains(t, (&ForcemergeImpl{}).prHTMLURL("goliac", &pullRequestInfo{Number: 7}), "/goliac/pull/7")
}
