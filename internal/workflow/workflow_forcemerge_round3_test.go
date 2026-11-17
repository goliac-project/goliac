package workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/goliac-project/goliac/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestForcemergeR3ApprovalWithAnUnknownOutcome(t *testing.T) {
	t.Run("the review was created but the answer was lost: it is found and dismissed when the merge fails", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR()).allMethods()
		gh.reviewResponseLost = map[int]bool{32: true}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "error approving the PR #32")
		dismissals := gh.find("PUT", "/dismissals")
		assert.Equal(t, 1, len(dismissals))
		assert.Contains(t, firstCall(dismissals).Endpoint, "/reviews/1001/dismissals")
		assert.Contains(t, err.Error(), "were dismissed")
		assert.Empty(t, gh.find("PUT", "/pulls/32/merge"))
	})

	t.Run("a 503 on the approval: no review exists, the PR is reported as 'approval may exist'", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR()).allMethods()
		gh.reviewFails = map[int]int{32: 1}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "the approval may exist, its review id is unknown")
		assert.Empty(t, gh.find("PUT", "/dismissals"))
	})

	t.Run("the reviews can't be listed: the PR is reported as 'approval may exist'", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR()).allMethods()
		gh.reviewResponseLost = map[int]bool{32: true}
		inner := gh.onCall
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && strings.HasSuffix(call.Endpoint, "/reviews") {
				return httpError("500 Internal Server Error", `{}`)
			}
			if inner != nil {
				return inner(f, call)
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "the approval may exist, its review id is unknown")
	})

	t.Run("stack: the approval of PR 2 is lost, the one found is dismissed with the earlier ones", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.reviewResponseLost = map[int]bool{2: true}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", MergeModeStack)
		assert.NotNil(t, err)
		assert.Equal(t, 2, len(gh.find("PUT", "/dismissals")), "PR 1's approval and PR 2's (found by its body)")
		assert.Empty(t, gh.find("PUT", "/merge-async"))
	})

	t.Run("direct: the approval is lost, the found one is dismissed", func(t *testing.T) {
		gh := newFake(t, fixtureStackPlain()).allMethods()
		gh.reviewResponseLost = map[int]bool{3: true}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err)
		assert.Equal(t, 1, len(gh.find("PUT", "/dismissals")))
		assert.Empty(t, gh.find("POST", "/merges"))
	})

	t.Run("a review that GitHub refuses (stale commit) is a definite error", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR()).allMethods()
		_, err := gh.CallRestAPI(context.Background(), "/repos/myorg/goliac/pulls/32/reviews", "", "POST", map[string]interface{}{"event": "APPROVE", "commit_id": "not-in-the-pr"}, nil)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "422")
		_, err = gh.CallRestAPI(context.Background(), "/repos/myorg/goliac/pulls/99/reviews", "", "POST", map[string]interface{}{"event": "APPROVE"}, nil)
		assert.Contains(t, err.Error(), "404")
		_, err = gh.CallRestAPI(context.Background(), "/repos/myorg/goliac/pulls/32/reviews/777/dismissals", "", "PUT", map[string]interface{}{"message": "x", "event": "DISMISS"}, nil)
		assert.Contains(t, err.Error(), "404", "an unknown review can't be dismissed")
	})
}

func TestForcemergeR5Deadlines(t *testing.T) {
	t.Run("every GitHub call of a force merge has a deadline (the whole force merge and each request)", func(t *testing.T) {
		previous := config.Config.WorkflowForcemergeTimeoutSeconds
		defer func() { config.Config.WorkflowForcemergeTimeoutSeconds = previous }()
		config.Config.WorkflowForcemergeTimeoutSeconds = 5
		gh := plainFake(t)
		fc := executeFixture(t, gh, &recordingPlugin{})
		_, err := exec(fc, 32, "", false)
		assert.Nil(t, err)
		assert.NotEmpty(t, gh.ctxDeadlines)
		for _, d := range gh.ctxDeadlines {
			assert.LessOrEqual(t, d, 5*time.Second, "the force merge deadline (5s) is the nearest")
			assert.Greater(t, d, 0*time.Second)
		}
	})

	t.Run("the per-request timeout applies: a merge request that never answers is an unknown outcome", func(t *testing.T) {
		gh := plainFake(t)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PUT" && strings.HasSuffix(call.Endpoint, "/pulls/32/merge") {
				time.Sleep(200 * time.Millisecond)
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		fc.requestTimeout = 50 * time.Millisecond
		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "the outcome is unknown, GitHub may have merged it")
		assert.Empty(t, gh.find("PUT", "/dismissals"))
	})

	t.Run("the default request timeout is 60s", func(t *testing.T) {
		gh := plainFake(t)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		for _, d := range gh.ctxDeadlines {
			assert.LessOrEqual(t, d, 60*time.Second)
			assert.Greater(t, d, 50*time.Second)
		}
		assert.Equal(t, 10*time.Minute, forcemergeTimeout())
	})

	t.Run("a cancelled context: nothing is merged", func(t *testing.T) {
		gh := plainFake(t)
		fc := executeFixture(t, gh, &recordingPlugin{})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := fc.ExecuteWorkflow(ctx, []string{"fmtest"}, "test-user", "fmtest", "because", map[string]string{"pr_url": prURL(32)}, false)
		assert.NotNil(t, err)
		assert.Empty(t, gh.find("PUT", "/pulls/32/merge"))
	})

	t.Run("a 429 on a merge request is a definite refusal (nothing was processed)", func(t *testing.T) {
		gh := plainFake(t)
		gh.syncMerge = func(call restCall) ([]byte, error) {
			return httpError("429 Too Many Requests", `{"message":"You have exceeded a secondary rate limit."}`)
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.NotContains(t, err.Error(), "outcome is unknown")
		assert.Equal(t, 1, len(gh.find("PUT", "/dismissals")))
	})
}

func TestForcemergeR6ExplanationRequired(t *testing.T) {
	for _, explanation := range []string{"/squash", "  /squash ", "/squash /squash", "\t/squash\n"} {
		t.Run(fmt.Sprintf("%q is not an explanation", explanation), func(t *testing.T) {
			gh := plainFake(t)
			fc := executeFixture(t, gh, &recordingPlugin{})
			_, err := fc.ExecuteWorkflow(context.Background(), []string{"fmtest"}, "test-user", "fmtest", explanation, map[string]string{"pr_url": prURL(32)}, false)
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "an explanation is required")
			assert.Empty(t, gh.sideEffects())
		})
	}
	t.Run("an explanation with /squash is fine", func(t *testing.T) {
		gh := plainFake(t)
		fc := executeFixture(t, gh, &recordingPlugin{})
		_, err := fc.ExecuteWorkflow(context.Background(), []string{"fmtest"}, "test-user", "fmtest", "prod is down /squash", map[string]string{"pr_url": prURL(32)}, false)
		assert.Nil(t, err)
	})
}

func TestForcemergeR9DirectBottomConfirmFailureListsTheApprovals(t *testing.T) {
	gh := newFake(t, fixtureStackPlain()).allMethods()
	gh.asyncMerge = okJSON(asyncMerged)
	gh.neverMarkMerged = true // GitHub says merged but the PR never shows merged
	fc, _ := newForcemergeForTest(t, gh)
	_, err := mergePR(fc, 4, "because", "")
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "GitHub reported the merge but the PR is not merged")
	assert.Contains(t, err.Error(), "left in place: PR #4 (review 1001)")
	assert.Empty(t, gh.find("PUT", "/dismissals"))
	assert.Empty(t, gh.find("PATCH", ""))
}

func TestForcemergeR11StepsRunBeforeTheMerge(t *testing.T) {
	t.Run("a step failing on the 2nd PR of a stack says the earlier steps already ran and nothing was merged", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		plugin := &recordingPlugin{failOn: 2}
		fc := executeFixture(t, gh, plugin)
		_, err := exec(fc, 3, MergeModeStack, false)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "error when executing step")
		assert.Contains(t, err.Error(), "Nothing was merged, but the steps already ran for PR #1")
		assert.Empty(t, gh.sideEffects())
	})
	t.Run("a step failing on the first PR has nothing to add", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		plugin := &recordingPlugin{failOn: 1}
		fc := executeFixture(t, gh, plugin)
		_, err := exec(fc, 3, MergeModeStack, false)
		assert.NotNil(t, err)
		assert.NotContains(t, err.Error(), "already ran")
	})
}

func TestForcemergeR12LockKeyIsCaseInsensitive(t *testing.T) {
	fc := &ForcemergeImpl{}
	unlock, err := fc.lock("My-Repo", 7)
	assert.Nil(t, err)
	_, err = fc.lock("my-repo", 7)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "already running")
	_, err = fc.lock("MY-REPO", 7, 8)
	assert.NotNil(t, err)
	unlock()
	_, err = fc.lock("my-repo", 7)
	assert.Nil(t, err)
}

func TestForcemergeWaitConfigEdges(t *testing.T) {
	previous := config.Config.WorkflowForcemergeWaitSeconds
	defer func() { config.Config.WorkflowForcemergeWaitSeconds = previous }()
	for _, v := range []int{0, -3} {
		config.Config.WorkflowForcemergeWaitSeconds = v
		assert.Equal(t, 60*time.Second, githubWait(), "a non positive value falls back to the default (it is rejected at startup)")
	}
	config.Config.WorkflowForcemergeWaitSeconds = 1
	assert.Equal(t, time.Second, githubWait())

	prevT := config.Config.WorkflowForcemergeTimeoutSeconds
	defer func() { config.Config.WorkflowForcemergeTimeoutSeconds = prevT }()
	config.Config.WorkflowForcemergeTimeoutSeconds = 0
	assert.Equal(t, 10*time.Minute, forcemergeTimeout())
	config.Config.WorkflowForcemergeTimeoutSeconds = 3
	assert.Equal(t, 3*time.Second, forcemergeTimeout())
}
