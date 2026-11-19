package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

/*
 * Repositories with delete_branch_on_merge=true: once a PR is merged GitHub deletes its head branch
 * and retargets the open PRs based on it by itself (AutomaticBaseChangeSucceededEvent), so Goliac's own
 * PATCH base arrives when the base is already the target and GitHub answers 422
 * ("A pull request already exists for base branch ...").
 * Smoke test: phoenix-tooling #64 <- #65 <- #66, /forcemerge:phoenix:stack: on #65.
 */

func TestForcemergeAutoRetarget(t *testing.T) {
	for _, late := range []bool{false, true} {
		for _, from := range []int{58, 59, 57} {
			t.Run(fmt.Sprintf("stack mode on PR %d, delete_branch_on_merge (late=%v): the 3 PRs land in main", from, late), func(t *testing.T) {
				gh := newFake(t, fixturePlainChain()).allMethods()
				gh.deleteBranchOnMerge = true
				gh.autoRetargetLate = late
				gh.asyncMerge = okJSON(asyncMerged)
				fc, _ := newForcemergeForTest(t, gh)

				summary, err := mergePR(fc, from, "ship it", MergeModeStack)
				assert.Nil(t, err)
				assert.Equal(t, "Force merged (stack) into `main`: #59 t3 (by a59), #58 t2 (by a58), #57 t1 (by a57)", summary)
				for _, n := range []int{57, 58, 59} {
					assert.True(t, gh.pr(n).Merged, "PR #%d merged", n)
					assert.Equal(t, "main", gh.pr(n).MergedInto, "PR #%d landed in main", n)
				}
				assert.Equal(t, 0, len(gh.find("PUT", "/dismissals")))
				if !late {
					// GitHub already retargeted: no PATCH needed
					assert.Equal(t, 0, len(gh.find("PATCH", "/pulls/58")))
				}
			})
		}
	}

	t.Run("direct mode on the bottom PR 57, delete_branch_on_merge: success, #58 is on main", func(t *testing.T) {
		for _, late := range []bool{false, true} {
			gh := newFake(t, fixturePlainChain()).allMethods()
			gh.deleteBranchOnMerge = true
			gh.autoRetargetLate = late
			gh.asyncMerge = okJSON(asyncMerged)
			fc, _ := newForcemergeForTest(t, gh)

			summary, err := mergePR(fc, 57, "because", "")
			assert.Nil(t, err, "late=%v", late)
			assert.Contains(t, summary, "Force merged (direct): #57 t1 (by a57) into `main`")
			assert.Equal(t, "main", gh.pr(58).BaseRef)
			assert.Equal(t, "t2", gh.pr(59).BaseRef)
			assert.False(t, gh.pr(58).Merged)
			assert.Equal(t, 0, len(gh.find("PUT", "/dismissals")))
		}
	})

	t.Run("direct mode on the middle PR 58, delete_branch_on_merge: #59 auto-retargeted onto t1 (not the default branch)", func(t *testing.T) {
		for _, late := range []bool{false, true} {
			gh := newFake(t, fixturePlainChain()).allMethods()
			gh.deleteBranchOnMerge = true
			gh.autoRetargetLate = late
			gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
			gh.markMergedOnBranchMerge = true
			fc, _ := newForcemergeForTest(t, gh)

			_, err := mergePR(fc, 58, "because", "")
			assert.Nil(t, err, "late=%v", late)
			assert.Equal(t, "t1", gh.pr(59).BaseRef)
			assert.Equal(t, 0, len(gh.find("PUT", "/dismissals")))
		}
	})

	t.Run("race: the read shows the old base, the PATCH gets 422, the re-read shows the target: success", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		gh.deleteBranchOnMerge = true
		gh.autoRetargetLate = true
		gh.asyncMerge = okJSON(asyncMerged)
		fc, clock := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 58, "ship it", MergeModeStack)
		assert.Nil(t, err)
		// the PATCH was really sent and refused, and not retried
		assert.Equal(t, 1, len(gh.find("PATCH", "/pulls/58")))
		assert.Equal(t, 1, len(gh.find("PATCH", "/pulls/59")))
		assert.Equal(t, 0, len(clock.slept()))
		assert.Equal(t, 0, len(gh.find("PUT", "/dismissals")))
	})

	t.Run("a 422 for another reason still refuses: not retried, manual fix message, approvals dismissed", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PATCH" && call.Endpoint == "/repos/myorg/goliac/pulls/58" {
				return httpError("422 Unprocessable Entity", `{"message":"Validation Failed","errors":[{"message":"something else"}]}`)
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 58, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		if err != nil {
			assert.Contains(t, err.Error(), "PR #57 is merged, but the PRs above it were not all retargeted onto main")
			assert.Contains(t, err.Error(), "GitHub refused the change, not retried")
			assert.Contains(t, err.Error(), "Fix manually")
			assert.Contains(t, err.Error(), "were dismissed")
		}
		assert.Equal(t, 1, len(gh.find("PATCH", "/pulls/58")))
		assert.Equal(t, 2, len(gh.find("PUT", "/dismissals")))
		assert.False(t, gh.pr(58).Merged)
		assert.Equal(t, "t1", gh.pr(58).BaseRef)
	})

	t.Run("a 422 and a re-read showing another base than the target still refuses", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PATCH" && call.Endpoint == "/repos/myorg/goliac/pulls/58" {
				f.update(58, func(p *fakePR) { p.BaseRef = "elsewhere" })
				return httpError("422 Unprocessable Entity", `{"message":"Validation Failed"}`)
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 58, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		if err != nil {
			assert.True(t, strings.Contains(err.Error(), "not all retargeted") || strings.Contains(err.Error(), "retargeted onto main"), err.Error())
		}
		assert.Equal(t, 2, len(gh.find("PUT", "/dismissals")))
	})

	t.Run("a 422 and the re-read failing still refuses", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		patched := false
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PATCH" && call.Endpoint == "/repos/myorg/goliac/pulls/58" {
				patched = true
				return httpError("422 Unprocessable Entity", `{"message":"Validation Failed"}`)
			}
			if patched && call.Method == "GET" && call.Endpoint == "/repos/myorg/goliac/pulls/58" {
				return httpError("500 Internal Server Error", `{"message":"boom"}`)
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 58, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Equal(t, 2, len(gh.find("PUT", "/dismissals")))
	})

	t.Run("delete_branch_on_merge off: the PATCH is sent and works as before", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 58, "ship it", MergeModeStack)
		assert.Nil(t, err)
		assert.Equal(t, 1, len(gh.find("PATCH", "/pulls/58")))
		assert.Equal(t, 1, len(gh.find("PATCH", "/pulls/59")))
		for _, n := range []int{57, 58, 59} {
			assert.Equal(t, "main", gh.pr(n).MergedInto)
		}
	})
}
