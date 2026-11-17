package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

/*
 * fixturePlainChain returns a stack built by chaining base branches, without GitHub's stack
 * metadata (stack: null), like the phoenix-tooling smoke test T3 (PRs #57 to #59):
 * 59 (t3, based on t2) > 58 (t2, based on t1) > 57 (t1, based on main).
 */
func fixturePlainChain() map[int]*fakePR {
	return map[int]*fakePR{
		57: {Number: 57, Title: "t1", Author: "a57", BaseRef: "main", HeadRef: "t1", HeadSha: "sha57", State: "open"},
		58: {Number: 58, Title: "t2", Author: "a58", BaseRef: "t1", HeadRef: "t2", HeadSha: "sha58", State: "open"},
		59: {Number: 59, Title: "t3", Author: "a59", BaseRef: "t2", HeadRef: "t3", HeadSha: "sha59", State: "open"},
	}
}

// callIndex returns the position of the first call matching method and endpoint (-1 if none)
func (f *fakeGitHub) callIndex(method, endpoint string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.calls {
		if c.Method == method && c.Endpoint == endpoint {
			return i
		}
	}
	return -1
}

// firstCall returns the first call (an empty one when there is none, so a failing test does not panic)
func firstCall(calls []restCall) restCall {
	if len(calls) == 0 {
		return restCall{Body: map[string]interface{}{}}
	}
	return calls[0]
}

func assertNothingChanged(t *testing.T, gh *fakeGitHub) {
	t.Helper()
	assert.Empty(t, gh.sideEffects(), "nothing approved, merged, retargeted or commented")
	for _, n := range gh.sortedNumbers() {
		p := gh.pr(n)
		assert.False(t, p.Merged, "PR #%d merged", n)
	}
}

func TestForcemergeInferredStack(t *testing.T) {
	for _, from := range []int{58, 59, 57} {
		t.Run(fmt.Sprintf("T3: stack mode on PR %d of a plain base chain: every PR approved, the whole chain lands in main", from), func(t *testing.T) {
			gh := newFake(t, fixturePlainChain()).allMethods()
			gh.asyncMerge = okJSON(asyncMerged)
			fc, _ := newForcemergeForTest(t, gh)

			summary, err := mergePR(fc, from, "ship it", MergeModeStack)
			assert.Nil(t, err)
			assert.Equal(t, "Force merged (stack) into `main`: #59 t3 (by a59), #58 t2 (by a58), #57 t1 (by a57)", summary)

			// every PR approved, on the validated head, saying the mode and where the stack comes from
			assert.Equal(t, []string{"59", "58", "57"}, gh.reviewedPRs())
			for _, r := range gh.find("POST", "/reviews") {
				n := strings.TrimSuffix(strings.TrimPrefix(r.Endpoint, "/repos/myorg/goliac/pulls/"), "/reviews")
				assert.Equal(t, "sha"+n, r.Body["commit_id"])
				body := r.Body["body"].(string)
				assert.Contains(t, body, fmt.Sprintf("Merge mode: `stack`. Merging the whole stack (3 PRs, #59 to #57) into `main`, requested on PR #%d.", from))
				assert.Contains(t, body, "Stack source: inferred from the chain of open PRs (GitHub sent no stack metadata).")
				assert.Contains(t, body, "merged one at a time from the bottom (#57) up")
			}

			// bottom-up, one asynchronous merge per PR, each pinned to its approved head
			asyncCalls := gh.find("PUT", "/merge-async")
			assert.Equal(t, 3, len(asyncCalls))
			for i, n := range []int{57, 58, 59} {
				if i >= len(asyncCalls) {
					break
				}
				assert.Equal(t, fmt.Sprintf("/repos/myorg/goliac/pulls/%d/merge-async", n), asyncCalls[i].Endpoint)
				assert.Equal(t, fmt.Sprintf("sha%d", n), asyncCalls[i].Body["sha"])
				assert.Equal(t, "merge", asyncCalls[i].Body["merge_method"])
				assert.Equal(t, "direct_merge", asyncCalls[i].Body["merge_action"])
			}

			// each PR is retargeted onto main only after the PR below it is merged, and before its own merge
			assert.True(t, gh.callIndex("PUT", "/repos/myorg/goliac/pulls/57/merge-async") < gh.callIndex("PATCH", "/repos/myorg/goliac/pulls/58"))
			assert.True(t, gh.callIndex("PATCH", "/repos/myorg/goliac/pulls/58") < gh.callIndex("PUT", "/repos/myorg/goliac/pulls/58/merge-async"))
			assert.True(t, gh.callIndex("PUT", "/repos/myorg/goliac/pulls/58/merge-async") < gh.callIndex("PATCH", "/repos/myorg/goliac/pulls/59"))
			assert.True(t, gh.callIndex("PATCH", "/repos/myorg/goliac/pulls/59") < gh.callIndex("PUT", "/repos/myorg/goliac/pulls/59/merge-async"))

			for _, n := range []int{57, 58, 59} {
				p := gh.pr(n)
				assert.True(t, p.Merged, "PR #%d merged", n)
				assert.Equal(t, "main", p.MergedInto, "PR #%d landed in main", n)
			}
			assert.Equal(t, 0, len(gh.find("POST", "/merges")))
			assert.Equal(t, 0, len(gh.find("PUT", "/merge")))
			assert.Equal(t, 0, len(gh.find("PUT", "/dismissals")))
		})
	}

	t.Run("stack mode, squash-only repository: refused up front, nothing approved (F5)", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).squashOnly()
		gh.asyncMerge = okJSON(asyncMerged)
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 58, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		if err != nil {
			assert.Contains(t, err.Error(), `nothing merged: the stack of 3 PRs would be merged one at a time with the "squash" method`)
			assert.Contains(t, err.Error(), "rewrite the commits of each PR")
		}
		assertNothingChanged(t, gh)
	})

	t.Run("stack mode with /squash is refused too (squash rewrites the commits of the PRs above)", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 58, "ship it /squash", MergeModeStack)
		assert.NotNil(t, err)
		assertNothingChanged(t, gh)
	})

	t.Run("direct on the bottom PR with PRs above, squash-only repository: refused; on a mid or top PR (merge commit): fine", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).squashOnly()
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 57, "ship it", "")
		assert.NotNil(t, err)
		if err != nil {
			assert.Contains(t, err.Error(), "rewrites the commits they carry")
		}
		assertNothingChanged(t, gh)

		for _, n := range []int{59, 58} { // top first, then the middle one: top down
			gh := newFake(t, fixturePlainChain()).squashOnly()
			gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
			gh.markMergedOnBranchMerge = true
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, n, "ship it", "")
			assert.Nil(t, err, "PR %d", n)
		}
	})

	t.Run("squash-only repository, bottom PR with nothing above: squashed (a single PR is not a stack)", func(t *testing.T) {
		prs := fixturePlainChain()
		delete(prs, 58)
		delete(prs, 59)
		gh := newFake(t, prs).squashOnly()
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 57, "ship it", MergeModeStack)
		assert.Nil(t, err)
		assert.Equal(t, "squash", gh.find("PUT", "/pulls/57/merge")[0].Body["merge_method"])
	})

	t.Run("stack mode, the second merge fails: #57 is reported merged, the approvals of #58 and #59 are dismissed", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		gh.asyncMerge = func(call restCall) ([]byte, error) {
			if strings.Contains(call.Endpoint, "/58/") {
				return httpError("403 Forbidden", `{"message":"rules"}`)
			}
			return []byte(asyncMerged), nil
		}
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 58, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "already merged into `main`: #57")
		assert.Contains(t, err.Error(), "not merged: #58, #59")
		dismissals := gh.find("PUT", "/dismissals")
		assert.Equal(t, 2, len(dismissals))
		for _, d := range dismissals {
			assert.False(t, strings.Contains(d.Endpoint, "/pulls/57/"), "the approval of the merged PR is kept")
		}
		assert.True(t, gh.pr(57).Merged)
		assert.False(t, gh.pr(58).Merged)
		assert.False(t, gh.pr(59).Merged)
		// #58 was retargeted onto main (its parent is merged); #59 untouched
		assert.Equal(t, "main", gh.pr(58).BaseRef)
		assert.Equal(t, "t2", gh.pr(59).BaseRef)
		assert.Equal(t, 2, len(gh.find("PUT", "/merge-async")), "#59 is never tried")
	})

	t.Run("stack mode, a merge times out: indeterminate, the approval of the PR with an unknown outcome is left in place", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		gh.asyncMerge = func(call restCall) ([]byte, error) {
			if strings.Contains(call.Endpoint, "/57/") {
				return []byte(asyncPending), nil
			}
			return []byte(asyncMerged), nil
		}
		gh.asyncPoll = okJSON(asyncPending)
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 58, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "left in place")
		assert.Contains(t, err.Error(), "outcome unknown (it may be merged): #57; not merged: #58, #59")
		assert.Equal(t, 2, len(gh.find("PUT", "/dismissals")), "#58 and #59 were never tried: dismissed; #57 (unknown) is kept")
		assert.Equal(t, 1, len(gh.find("PUT", "/merge-async")))
		assert.Equal(t, 0, len(gh.find("PATCH", "")))
	})

	t.Run("stack mode, a push after the approval: nothing merged, approvals dismissed", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "POST" && strings.HasSuffix(call.Endpoint, "/57/reviews") {
				f.update(59, func(p *fakePR) { p.HeadSha = "pushed" })
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 58, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "PR #59 was pushed to after the approval")
		assert.Equal(t, 0, len(gh.find("PUT", "/merge-async")))
		assert.Equal(t, 3, len(gh.find("PUT", "/dismissals")))
	})

	for _, mode := range []string{"", MergeModeDirect} {
		t.Run(fmt.Sprintf("direct on the middle PR 58 (mode %q): merged into t1, PR 59 retargeted onto t1", mode), func(t *testing.T) {
			gh := newFake(t, fixturePlainChain()).allMethods() // the default branch is needed to infer the stack
			gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
			gh.markMergedOnBranchMerge = true
			fc, _ := newForcemergeForTest(t, gh)

			summary, err := mergePR(fc, 58, "because", mode)
			assert.Nil(t, err)
			assert.Equal(t, "Force merged (direct): #58 t2 (by a58) into `t1`; retargeted onto `t1`: #59", summary)

			assert.Equal(t, []string{"58"}, gh.reviewedPRs())
			body := fmt.Sprint(firstCall(gh.find("POST", "/reviews")).Body["body"])
			assert.Contains(t, body, "Merge mode: `direct`")
			assert.Contains(t, body, "Stack source: inferred from the chain of open PRs (GitHub sent no stack metadata).")

			merges := gh.find("POST", "/repos/myorg/goliac/merges")
			assert.Equal(t, 1, len(merges))
			assert.Equal(t, "t1", firstCall(merges).Body["base"])
			assert.Equal(t, "sha58", firstCall(merges).Body["head"])
			assert.Equal(t, "t1", gh.pr(58).MergedInto)
			assert.Equal(t, "t1", gh.pr(59).BaseRef)
			assert.False(t, gh.pr(57).Merged)
			assert.Equal(t, 0, len(gh.find("PUT", "/merge-async")))
			assert.Equal(t, 0, len(gh.find("PUT", "/merge")))
		})
	}

	t.Run("direct on the bottom PR 57: merged into main, PR 58 retargeted onto main", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		fc, _ := newForcemergeForTest(t, gh)

		summary, err := mergePR(fc, 57, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, "Force merged (direct): #57 t1 (by a57) into `main`; retargeted onto `main`: #58", summary)
		assert.Equal(t, "main", gh.pr(57).MergedInto)
		assert.Equal(t, "main", gh.pr(58).BaseRef)
		assert.Equal(t, "t2", gh.pr(59).BaseRef)
		assert.False(t, gh.pr(58).Merged)
	})

	t.Run("direct on the top PR 59: merged into t2, nothing to retarget", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.markMergedOnBranchMerge = true
		fc, _ := newForcemergeForTest(t, gh)

		summary, err := mergePR(fc, 59, "because", MergeModeDirect)
		assert.Nil(t, err)
		assert.Equal(t, "Force merged (direct): #59 t3 (by a59) into `t2`", summary)
		assert.Equal(t, "t2", gh.pr(59).MergedInto)
		assert.Equal(t, 0, len(gh.find("PATCH", "")))
	})

	t.Run("a single PR (no PR below, none above): the plain merge, the review says so", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)

		summary, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, "", summary)
		assert.Equal(t, 1, len(gh.find("PUT", "/pulls/32/merge")))
		body := fmt.Sprint(firstCall(gh.find("POST", "/reviews")).Body["body"])
		assert.True(t, strings.HasSuffix(body, "\n\nMerge mode: none (this PR is not part of a stack)."), body)
	})

	t.Run("a single PR based on another branch with no PR (e.g. a release branch): the plain merge into it", func(t *testing.T) {
		prs := fixturePlainPR()
		prs[32].BaseRef = "release/1.0"
		gh := newFake(t, prs)
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 32, "because", MergeModeStack)
		assert.Nil(t, err)
		assert.Equal(t, 1, len(gh.find("PUT", "/pulls/32/merge")))
		assert.Contains(t, firstCall(gh.find("POST", "/reviews")).Body["body"], "Merge mode `stack` ignored: this PR is not part of a stack.")
	})

	// live 15a: a PR whose base branch is the head of a PR closed without being merged (nothing above it)
	for _, mode := range []string{"", MergeModeStack, MergeModeDirect} {
		t.Run(fmt.Sprintf("a lone PR based on the branch of a closed, unmerged PR is refused (mode %q)", mode), func(t *testing.T) {
			prs := fixturePlainChain()
			delete(prs, 59)
			prs[58].State = "closed" // t2 closed without being merged; PR 59 would be on t2
			prs[60] = &fakePR{Number: 60, Title: "t3", Author: "a60", BaseRef: "t2", HeadRef: "t3", HeadSha: "sha60", State: "open"}
			gh := newFake(t, prs).allMethods()
			gh.branchTips = map[string]string{"t2": "sha58"} // not pushed since the PR was closed
			gh.syncMerge = okJSON(`{"merged":true}`)
			fc, _ := newForcemergeForTest(t, gh)

			_, err := mergePR(fc, 60, "ship it", mode)
			assert.NotNil(t, err)
			if err != nil {
				assert.Contains(t, err.Error(), "nothing merged: PR #60 is based on branch t2, the head branch of PR #58 which is closed without being merged and whose branch was not pushed since: merging into it would strand the change in a dead branch")
			}
			assertNothingChanged(t, gh)
		})
	}

	t.Run("a lone PR based on the branch of a merged PR (closed and merged) is merged as usual", func(t *testing.T) {
		prs := map[int]*fakePR{
			58: {Number: 58, Title: "t2", BaseRef: "main", HeadRef: "t2", HeadSha: "sha58", State: "closed", Merged: true},
			60: {Number: 60, Title: "t3", Author: "a60", BaseRef: "t2", HeadRef: "t3", HeadSha: "sha60", State: "open"},
		}
		gh := newFake(t, prs).allMethods()
		gh.branchTips = map[string]string{"t2": "moved-on-since-the-merge"}
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 60, "ship it", "")
		assert.Nil(t, err)
		assert.Equal(t, 1, len(gh.find("PUT", "/pulls/60/merge")))

		// F7: the branch of the merged PR was kept and not pushed since the merge: nothing may be merged into it
		prs = map[int]*fakePR{
			58: {Number: 58, Title: "t2", BaseRef: "main", HeadRef: "t2", HeadSha: "sha58", State: "closed", Merged: true},
			60: {Number: 60, Title: "t3", Author: "a60", BaseRef: "t2", HeadRef: "t3", HeadSha: "sha60", State: "open"},
		}
		gh = newFake(t, prs).allMethods()
		gh.branchTips = map[string]string{"t2": "sha58"}
		fc, _ = newForcemergeForTest(t, gh)
		_, err = mergePR(fc, 60, "ship it", "")
		assert.NotNil(t, err)
		if err != nil {
			assert.Contains(t, err.Error(), "which is already merged (the branch was kept) and whose branch was not pushed since")
		}
		assert.Empty(t, gh.sideEffects())
	})

	// the base branch is only dead when the newest PR of the branch is closed unmerged AND the tip is still its head
	liveBranch := func(t *testing.T, prs map[int]*fakePR, tips map[string]string) error {
		prs[60] = &fakePR{Number: 60, Title: "x", Author: "a60", BaseRef: "release/1.0", HeadRef: "x", HeadSha: "sha60", State: "open"}
		gh := newFake(t, prs).allMethods()
		gh.branchTips = tips
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 60, "ship it", "")
		if err == nil {
			assert.Equal(t, 1, len(gh.find("PUT", "/pulls/60/merge")))
		}
		return err
	}
	oldClosed := func() *fakePR {
		return &fakePR{Number: 10, Title: "old", BaseRef: "main", HeadRef: "release/1.0", HeadSha: "old-tip", State: "closed"}
	}

	t.Run("a release branch with an old closed PR and later commits: merged as usual", func(t *testing.T) {
		err := liveBranch(t, map[int]*fakePR{10: oldClosed()}, map[string]string{"release/1.0": "newer-tip"})
		assert.Nil(t, err)
	})

	t.Run("a branch whose newest PR is merged, with an older closed one: merged as usual", func(t *testing.T) {
		newer := &fakePR{Number: 20, Title: "newer", BaseRef: "main", HeadRef: "release/1.0", HeadSha: "old-tip", State: "closed", Merged: true}
		err := liveBranch(t, map[int]*fakePR{10: oldClosed(), 20: newer}, map[string]string{"release/1.0": "moved-on"})
		assert.Nil(t, err)
		// F7: the same branch with the tip still on the merged PR's head is dead
		err = liveBranch(t, map[int]*fakePR{10: oldClosed(), 20: newer}, map[string]string{"release/1.0": "old-tip"})
		assert.NotNil(t, err)
	})

	t.Run("a branch whose newest PR is closed unmerged but an older one was merged, tip unchanged: refused", func(t *testing.T) {
		older := &fakePR{Number: 5, Title: "older", BaseRef: "main", HeadRef: "release/1.0", HeadSha: "x", State: "closed", Merged: true}
		err := liveBranch(t, map[int]*fakePR{5: older, 10: oldClosed()}, map[string]string{"release/1.0": "old-tip"})
		assert.NotNil(t, err)
	})

	t.Run("a branch with an open PR from it that is newer than the closed one: not dead", func(t *testing.T) {
		open := &fakePR{Number: 20, Title: "open", BaseRef: "main", HeadRef: "release/1.0", HeadSha: "old-tip", State: "open"}
		// (the open PR below is found by the chain walk: this is no lone PR, so the chain is refused for another reason)
		err := liveBranch(t, map[int]*fakePR{10: oldClosed(), 20: open}, map[string]string{"release/1.0": "old-tip"})
		if err != nil {
			assert.NotContains(t, err.Error(), "dead branch")
		}
	})

	t.Run("a lone PR: reading the tip of its base branch fails or is unreadable: refused", func(t *testing.T) {
		assert.NotNil(t, liveBranch(t, map[int]*fakePR{10: oldClosed()}, map[string]string{})) // 404
		prs := map[int]*fakePR{10: oldClosed()}
		prs[60] = &fakePR{Number: 60, Title: "x", BaseRef: "release/1.0", HeadRef: "x", HeadSha: "sha60", State: "open"}
		gh := newFake(t, prs).allMethods()
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if strings.HasPrefix(call.Endpoint, "/repos/myorg/goliac/branches/") {
				return []byte(`not json`), nil
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 60, "ship it", "")
		assert.NotNil(t, err)
		assertNothingChanged(t, gh)
	})

	t.Run("a lone PR: listing the closed PRs of its base branch fails: refused before any side effect", func(t *testing.T) {
		prs := map[int]*fakePR{60: {Number: 60, Title: "t3", BaseRef: "t2", HeadRef: "t3", HeadSha: "sha60", State: "open"}}
		gh := newFake(t, prs).allMethods()
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && call.Endpoint == "/repos/myorg/goliac/pulls" && strings.Contains(call.Parameters, "state=all") {
				return httpError("500 Internal Server Error", `{}`)
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 60, "ship it", "")
		assert.NotNil(t, err)
		assertNothingChanged(t, gh)
	})

	refusals := map[string]struct {
		prs     func() map[int]*fakePR
		from    int
		mode    string
		message string
	}{
		"a fork in the chain (two open PRs based on t2)": {
			prs: func() map[int]*fakePR {
				prs := fixturePlainChain()
				prs[60] = &fakePR{Number: 60, Title: "t3b", BaseRef: "t2", HeadRef: "t3b", HeadSha: "sha60", State: "open"}
				return prs
			},
			from: 58, mode: MergeModeStack,
			message: "2 open PRs (#59, #60) are based on branch t2 (above PR #58): the chain forks, the stack is ambiguous",
		},
		"a fork in the chain below the requested PR": {
			prs: func() map[int]*fakePR {
				prs := fixturePlainChain()
				prs[60] = &fakePR{Number: 60, Title: "t2b", BaseRef: "t1", HeadRef: "t2b", HeadSha: "sha60", State: "open"}
				return prs
			},
			from: 59, mode: MergeModeStack,
			message: "2 open PRs (#58, #60) are based on branch t1 (above PR #57): the chain forks, the stack is ambiguous",
		},
		"a fork in the chain below the requested PR, direct mode": {
			prs: func() map[int]*fakePR {
				prs := fixturePlainChain()
				prs[60] = &fakePR{Number: 60, Title: "t2b", BaseRef: "t1", HeadRef: "t2b", HeadSha: "sha60", State: "open"}
				return prs
			},
			from: 59, mode: "",
			message: "the chain forks",
		},
		"a cycle": {
			prs: func() map[int]*fakePR {
				return map[int]*fakePR{
					70: {Number: 70, Title: "c1", BaseRef: "cA", HeadRef: "cB", HeadSha: "sha70", State: "open"},
					71: {Number: 71, Title: "c2", BaseRef: "cB", HeadRef: "cA", HeadSha: "sha71", State: "open"},
				}
			},
			from: 70, mode: MergeModeStack,
			message: "is not linear: PR #70 is reached twice (cycle)",
		},
		"a chain that never reaches the default branch (no PR for t1)": {
			prs: func() map[int]*fakePR {
				prs := fixturePlainChain()
				delete(prs, 57)
				return prs
			},
			from: 59, mode: MergeModeStack,
			message: "the chain of PR #59 stops at PR #58, based on t1: no open PR has this head branch and it is not the default branch main",
		},
		"a chain that never reaches the default branch, direct mode": {
			prs: func() map[int]*fakePR {
				prs := fixturePlainChain()
				delete(prs, 57)
				return prs
			},
			from: 58, mode: MergeModeDirect,
			message: "it is not the default branch main",
		},
		"a draft in the chain": {
			prs: func() map[int]*fakePR {
				prs := fixturePlainChain()
				prs[57].Draft = true
				return prs
			},
			from: 58, mode: MergeModeStack,
			message: "PR #57 of the inferred stack: PR #57 is a draft",
		},
		"a draft above the requested PR, stack mode": {
			prs: func() map[int]*fakePR {
				prs := fixturePlainChain()
				prs[59].Draft = true
				return prs
			},
			from: 58, mode: MergeModeStack,
			message: "PR #59 of the inferred stack: PR #59 is a draft",
		},
		"a fork-repository head in the chain": {
			prs: func() map[int]*fakePR {
				prs := fixturePlainChain()
				prs[59].HeadRepo = "someone/goliac"
				return prs
			},
			from: 58, mode: MergeModeStack,
			message: "PR #59 of the inferred stack: PR #59 comes from a fork (someone/goliac)",
		},
	}
	for name, tc := range refusals {
		t.Run("refused: "+name, func(t *testing.T) {
			gh := newFake(t, tc.prs()).allMethods()
			gh.asyncMerge = okJSON(asyncMerged)
			gh.syncMerge = okJSON(`{"merged":true}`)
			gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
			fc, _ := newForcemergeForTest(t, gh)

			_, err := mergePR(fc, tc.from, "ship it", tc.mode)
			assert.NotNil(t, err)
			if err != nil {
				assert.Contains(t, err.Error(), "nothing merged")
				assert.Contains(t, err.Error(), tc.message)
			}
			assertNothingChanged(t, gh)
		})
	}

	t.Run("refused: a chain mixing a GitHub-stacked PR and plain PRs", func(t *testing.T) {
		prs := fixturePlainChain()
		prs[57].StackSize, prs[57].StackPosition, prs[57].StackBase = 1, 1, "main"
		gh := newFake(t, prs).allMethods()
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 58, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		if err != nil {
			assert.Contains(t, err.Error(), "PR #57 of the inferred stack carries GitHub stack metadata")
		}
		assertNothingChanged(t, gh)
	})

	t.Run("refused: listing the PRs of the chain fails", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && call.Endpoint == "/repos/myorg/goliac/pulls" {
				return httpError("502 Bad Gateway", `{}`)
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 58, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		if err != nil {
			assert.Contains(t, err.Error(), "nothing merged: error looking for")
		}
		assertNothingChanged(t, gh)
	})

	t.Run("stack mode, retargeting the next PR is refused: #57 merged, #58 and #59 not merged, their approvals dismissed", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PATCH" && call.Endpoint == "/repos/myorg/goliac/pulls/58" {
				return httpError("422 Unprocessable Entity", `{"message":"cannot change the base"}`)
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 58, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		if err != nil {
			assert.Contains(t, err.Error(), "PR #57 is merged, but the PRs above it were not all retargeted onto main")
			assert.Contains(t, err.Error(), "already merged into `main`: #57; not merged: #58, #59")
			assert.Contains(t, err.Error(), "were dismissed")
		}
		assert.Equal(t, 1, len(gh.find("PUT", "/merge-async")))
		assert.Equal(t, 2, len(gh.find("PUT", "/dismissals")))
		assert.Equal(t, "main", gh.pr(57).MergedInto)
		assert.False(t, gh.pr(58).Merged)
	})

	t.Run("refused: a chain longer than the walk limit", func(t *testing.T) {
		prs := map[int]*fakePR{}
		for n := 1; n <= 6; n++ {
			base := fmt.Sprintf("l%d", n-1)
			if n == 1 {
				base = "main"
			}
			prs[n] = &fakePR{Number: n, Title: fmt.Sprintf("l%d", n), BaseRef: base, HeadRef: fmt.Sprintf("l%d", n), HeadSha: fmt.Sprintf("sha%d", n), State: "open"}
		}
		gh := newFake(t, prs).allMethods()
		fc, _ := newForcemergeForTest(t, gh)
		fc.maxInferredStackSize = 5

		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		if err != nil {
			assert.Contains(t, err.Error(), "more than 5 PRs")
		}
		assertNothingChanged(t, gh)
	})

	t.Run("refused: a chain when the default branch can't be read", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()) // no settings: GET /repos/myorg/goliac fails
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 58, "ship it", MergeModeDirect)
		assert.NotNil(t, err)
		if err != nil {
			assert.Contains(t, err.Error(), "unable to read the default branch")
		}
		assertNothingChanged(t, gh)
	})

	t.Run("the GitHub stack payload is used when present: merged bottom-up (GitHub refuses bypass on a PR that is not the bottom one), review says so", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.Nil(t, err)
		asyncCalls := gh.find("PUT", "/merge-async")
		assert.Equal(t, 4, len(asyncCalls))
		assert.Equal(t, "/repos/myorg/goliac/pulls/4/merge-async", asyncCalls[0].Endpoint)
		assert.Equal(t, "/repos/myorg/goliac/pulls/1/merge-async", asyncCalls[3].Endpoint)
		for n := 1; n <= 4; n++ {
			assert.Equal(t, "main", gh.pr(n).MergedInto)
		}
		for _, r := range gh.find("POST", "/reviews") {
			assert.Contains(t, r.Body["body"], "Stack source: GitHub stack metadata.")
		}
		assert.Equal(t, 0, len(gh.find("PATCH", "")))
	})
}
