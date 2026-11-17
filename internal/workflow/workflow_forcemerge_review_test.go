package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/goliac-project/goliac/internal/config"
	"github.com/stretchr/testify/assert"
)

/*
 * Findings of the independent review of the force merge (F1 to F10), each replayed on the fake GitHub.
 */

func plainFake(t *testing.T) *fakeGitHub {
	gh := newFake(t, fixturePlainPR()).allMethods()
	gh.syncMerge = okJSON(`{"merged":true}`)
	return gh
}

func TestForcemergeF1PlainMergeIsPinned(t *testing.T) {
	t.Run("the synchronous merge sends the approved head sha", func(t *testing.T) {
		gh := plainFake(t)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, "aaa111", firstCall(gh.find("PUT", "/pulls/32/merge")).Body["sha"])
		assert.Equal(t, []string{"aaa111"}, gh.syncMergedHeads)
	})

	t.Run("pushed between the validation and the approval: GitHub refuses to approve the stale commit, nothing merged", func(t *testing.T) {
		gh := plainFake(t)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "POST" && strings.HasSuffix(call.Endpoint, "/reviews") {
				f.update(32, func(p *fakePR) { p.HeadSha = "bbb222" })
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "error approving the PR #32")
		assert.Empty(t, gh.find("PUT", "/pulls/32/merge"))
	})

	t.Run("pushed between the approval and the re-check: nothing merged, approval dismissed", func(t *testing.T) {
		gh := plainFake(t)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			f.mu.Lock()
			approved := len(f.reviews) > 0
			f.mu.Unlock()
			if call.Method == "GET" && call.Endpoint == "/repos/myorg/goliac/pulls/32" && approved {
				f.update(32, func(p *fakePR) { p.HeadSha = "bbb222" })
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "was pushed to after the approval")
		assert.Empty(t, gh.find("PUT", "/pulls/32/merge"))
		assert.Equal(t, 1, len(gh.find("PUT", "/dismissals")))
	})

	t.Run("pushed between the re-check and the merge: GitHub's 409 is a definite failure, approval dismissed", func(t *testing.T) {
		gh := plainFake(t)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PUT" && strings.HasSuffix(call.Endpoint, "/pulls/32/merge") {
				f.update(32, func(p *fakePR) { p.HeadSha = "bbb222" })
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "was pushed to after the approval (GitHub: head branch was modified): nothing merged, retry")
		assert.Empty(t, gh.syncMergedHeads, "the pushed commit was not merged")
		assert.Equal(t, 1, len(gh.find("PUT", "/dismissals")))
	})

	t.Run("a 200 answer that says merged:false is not a merge", func(t *testing.T) {
		gh := plainFake(t)
		gh.syncMerge = okJSON(`{"merged":false,"message":"Pull Request is not mergeable"}`)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "is not merged: Pull Request is not mergeable")
		assert.Equal(t, 1, len(gh.find("PUT", "/dismissals")))
	})

	t.Run("a 200 answer that is not a merge result is indeterminate: the approval is left", func(t *testing.T) {
		gh := plainFake(t)
		gh.syncMerge = okJSON(`{"hello":"world"}`)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "left in place")
		assert.Empty(t, gh.find("PUT", "/dismissals"))
	})
}

func TestForcemergeF2UnknownOutcome(t *testing.T) {
	failures := map[string]func(call restCall) ([]byte, error){
		"transport error": func(call restCall) ([]byte, error) { return nil, errors.New("connection reset by peer") },
		"500": func(call restCall) ([]byte, error) {
			return httpError("500 Internal Server Error", `{"message":"boom"}`)
		},
		"502": func(call restCall) ([]byte, error) { return httpError("502 Bad Gateway", ``) },
		"503": func(call restCall) ([]byte, error) { return httpError("503 Service Unavailable", ``) },
		"504": func(call restCall) ([]byte, error) { return httpError("504 Gateway Timeout", ``) },
	}
	for name, failure := range failures {
		t.Run("synchronous merge, "+name+": outcome unknown, approval left", func(t *testing.T) {
			gh := plainFake(t)
			gh.syncMerge = failure
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, 32, "because", "")
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "the outcome is unknown, GitHub may have merged it")
			assert.Contains(t, err.Error(), "left in place")
			assert.Empty(t, gh.find("PUT", "/dismissals"))
		})
		t.Run("asynchronous merge of the bottom PR, "+name+": outcome unknown, approval left", func(t *testing.T) {
			gh := newFake(t, fixtureStack()).allMethods()
			gh.asyncMerge = failure
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, 4, "because", "")
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "the outcome is unknown")
			assert.Empty(t, gh.find("PUT", "/dismissals"))
		})
		t.Run("merge into the parent branch, "+name+": outcome unknown, approval left", func(t *testing.T) {
			gh := newFake(t, fixtureStackPlain()).allMethods()
			gh.branchMerge = failure
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, 3, "because", "")
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "the outcome is unknown")
			assert.Empty(t, gh.find("PUT", "/dismissals"))
		})
	}

	for _, status := range []string{"403 Forbidden", "404 Not Found", "409 Conflict", "422 Unprocessable Entity"} {
		t.Run("synchronous merge, "+status+": definite, approval dismissed", func(t *testing.T) {
			gh := plainFake(t)
			gh.syncMerge = func(call restCall) ([]byte, error) { return httpError(status, `{"message":"no"}`) }
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, 32, "because", "")
			assert.NotNil(t, err)
			assert.NotContains(t, err.Error(), "outcome is unknown")
			assert.Equal(t, 1, len(gh.find("PUT", "/dismissals")))
		})
	}
}

func TestForcemergeF3MergeMethodIsOnlyChangedWhenRefused(t *testing.T) {
	methods := func(gh *fakeGitHub) []string {
		res := []string{}
		for _, c := range gh.find("PUT", "/pulls/32/merge") {
			res = append(res, c.Body["merge_method"].(string))
		}
		return res
	}

	t.Run("mergeable not computed yet: the same method is retried and the merge succeeds", func(t *testing.T) {
		gh := plainFake(t)
		gh.update(32, func(p *fakePR) { p.NotMergeableYet = 2 })
		fc, clock := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, []string{"merge", "merge", "merge"}, methods(gh))
		assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, clock.slept())
	})

	t.Run("a persistent 405 that is not about the method: bounded retries, same method, GitHub's message", func(t *testing.T) {
		gh := plainFake(t)
		gh.syncMerge = func(call restCall) ([]byte, error) {
			return httpError("405 Method Not Allowed", `{"message":"Base branch was modified. Review and try the merge again."}`)
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "Base branch was modified")
		assert.Equal(t, []string{"merge", "merge", "merge", "merge"}, methods(gh))
		assert.Equal(t, 1, len(gh.find("PUT", "/dismissals")))
	})

	t.Run("a 422 that is not about the method does not change the method either", func(t *testing.T) {
		gh := plainFake(t)
		gh.syncMerge = func(call restCall) ([]byte, error) {
			return httpError("422 Unprocessable Entity", `{"message":"Pull Request is not mergeable"}`)
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Equal(t, []string{"merge", "merge", "merge", "merge"}, methods(gh))
	})

	t.Run("GitHub says merge commits are not allowed: squash, even when the settings said merge is allowed", func(t *testing.T) {
		gh := plainFake(t)
		gh.syncMerge = func(call restCall) ([]byte, error) {
			if call.Body["merge_method"] == "merge" {
				return httpError("405 Method Not Allowed", `{"message":"Merge commits are not allowed on this repository."}`)
			}
			return []byte(`{"merged":true}`), nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, []string{"merge", "squash"}, methods(gh))
	})

	t.Run("settings unreadable: any 405 on merge falls back to squash (after the retries)", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR()) // no settings: GET /repos/myorg/goliac fails
		gh.syncMerge = func(call restCall) ([]byte, error) {
			if call.Body["merge_method"] == "merge" {
				return httpError("405 Method Not Allowed", `{"message":"Whatever"}`)
			}
			return []byte(`{"merged":true}`), nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, []string{"merge", "merge", "merge", "merge", "squash"}, methods(gh))
	})

	t.Run("an explicit /squash is never replaced", func(t *testing.T) {
		gh := plainFake(t)
		gh.syncMerge = func(call restCall) ([]byte, error) {
			return httpError("405 Method Not Allowed", `{"message":"Squash merges are not allowed on this repository."}`)
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because /squash", "")
		assert.NotNil(t, err)
		assert.Equal(t, []string{"squash"}, methods(gh))
	})

	t.Run("asynchronous merge: mergeable not computed yet is retried with the same method", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		gh.update(4, func(p *fakePR) { p.NotMergeableYet = 2 })
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 4, "because", "")
		assert.Nil(t, err)
		calls := gh.find("PUT", "/pulls/4/merge-async")
		assert.Equal(t, 3, len(calls))
		for _, c := range calls {
			assert.Equal(t, "merge", c.Body["merge_method"])
		}
	})

	t.Run("asynchronous merge: a 405 that is not about the method keeps the method and says what GitHub said", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = func(call restCall) ([]byte, error) {
			return httpError("405 Method Not Allowed", `{"message":"Base branch was modified. Review and try the merge again."}`)
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 4, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "Base branch was modified")
		for _, c := range gh.find("PUT", "/pulls/4/merge-async") {
			assert.Equal(t, "merge", c.Body["merge_method"])
		}
		assert.Equal(t, 4, len(gh.find("PUT", "/pulls/4/merge-async")))
	})
}

func TestForcemergeF4AsyncConflict(t *testing.T) {
	cases := []struct {
		name          string
		body          string
		indeterminate bool
	}{
		{"a pending request (uuid)", `{"status":"pending","details":{"message":"already requested","uuid":"abc"}}`, true},
		{"the head changed", `{"message":"Head branch was modified. Review and try the merge again."}`, false},
		{"an undocumented sha body stays indeterminate (only the documented head message is definite)", `{"message":"expected_head_sha does not match the head sha"}`, true},
		{"an unknown body: the docs only say 409 means a pending request, so it stays indeterminate", `{"message":"conflict"}`, true},
		{"no body", ``, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gh := newFake(t, fixtureStack()).allMethods()
			gh.asyncMerge = func(call restCall) ([]byte, error) { return httpError("409 Conflict", tc.body) }
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, 4, "because", "")
			assert.NotNil(t, err)
			if tc.indeterminate {
				assert.Contains(t, err.Error(), "left in place")
				assert.Empty(t, gh.find("PUT", "/dismissals"))
			} else {
				assert.Contains(t, err.Error(), "nothing merged, retry")
				assert.Equal(t, 1, len(gh.find("PUT", "/dismissals")))
			}
		})
	}
}

func TestForcemergeF6Pagination(t *testing.T) {
	t.Run("every list call reads 100 PRs per page and follows the pages until a short one", func(t *testing.T) {
		prs := fixturePlainChain()
		for n := 100; n < 350; n++ {
			prs[n] = &fakePR{Number: n, Title: "other", BaseRef: "main", HeadRef: fmt.Sprintf("o%d", n), HeadSha: fmt.Sprintf("s%d", n), State: "open"}
		}
		gh := newFake(t, prs).allMethods()
		fc, _ := newForcemergeForTest(t, gh)
		all, err := fc.listOpenPRsBasedOn(context.Background(), "goliac", "main")
		assert.Nil(t, err)
		assert.Equal(t, 251, len(all)) // 250 + PR 57
		pages := []string{}
		for _, c := range gh.find("GET", "/pulls") {
			pages = append(pages, c.Parameters)
		}
		assert.Equal(t, []string{
			"state=open&base=main&per_page=100&page=1",
			"state=open&base=main&per_page=100&page=2",
			"state=open&base=main&per_page=100&page=3",
		}, pages)
	})

	t.Run("a listing that never ends is bounded", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && call.Endpoint == "/repos/myorg/goliac/pulls" {
				item := `{"number":1}`
				return []byte("[" + strings.Repeat(item+",", 99) + item + "]"), nil
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := fc.listOpenPRsBasedOn(context.Background(), "goliac", "main")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "more than 10000 PRs")
	})
}

func TestForcemergeF9PlanIsRevalidated(t *testing.T) {
	t.Run("a PR stacked on top after the plan is retargeted too", func(t *testing.T) {
		gh := newFake(t, fixtureStackPlain()).allMethods()
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.markMergedOnBranchMerge = true
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "POST" && call.Endpoint == "/repos/myorg/goliac/merges" {
				f.mu.Lock()
				f.prs[9] = &fakePR{Number: 9, Title: "late", BaseRef: "b3", HeadRef: "late", HeadSha: "sha9", State: "open"}
				f.mu.Unlock()
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		summary, err := mergePR(fc, 3, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, "b4", gh.pr(9).BaseRef, "retargeted although it did not exist at the plan")
		assert.Equal(t, "b4", gh.pr(2).BaseRef)
		assert.Contains(t, summary, "#9")
	})

	t.Run("the planned PRs are re-read after the lock: a change refuses before any side effect", func(t *testing.T) {
		mutations := map[string]func(p *fakePR){
			"pushed":     func(p *fakePR) { p.HeadSha = "new" },
			"retarget":   func(p *fakePR) { p.BaseRef = "elsewhere" },
			"closed":     func(p *fakePR) { p.State = "closed" },
			"draft":      func(p *fakePR) { p.Draft = true },
			"merged":     func(p *fakePR) { p.Merged, p.State = true, "closed" },
			"unreadable": nil,
		}
		for name, mutate := range mutations {
			for _, n := range []int{1, 2, 3, 4} {
				t.Run(fmt.Sprintf("%s PR %d (stack)", name, n), func(t *testing.T) {
					gh := newFake(t, fixtureStackPlain()).allMethods()
					fc, _ := newForcemergeForTest(t, gh)
					plan, err := fc.planMerge(context.Background(), "test-user", "goliac", "3", prURL(3), "because", MergeModeStack)
					assert.Nil(t, err)
					if mutate == nil {
						gh.mu.Lock()
						delete(gh.prs, n)
						gh.mu.Unlock()
					} else {
						gh.update(n, mutate)
					}
					err = fc.revalidatePlan(context.Background(), plan)
					assert.NotNil(t, err)
					assert.Empty(t, gh.sideEffects())
				})
			}
		}
	})

	t.Run("a child retargeted or closed after the plan refuses (direct merge)", func(t *testing.T) {
		gh := newFake(t, fixtureStackPlain()).allMethods()
		fc, _ := newForcemergeForTest(t, gh)
		plan, err := fc.planMerge(context.Background(), "test-user", "goliac", "3", prURL(3), "because", "")
		assert.Nil(t, err)
		gh.update(2, func(p *fakePR) { p.State = "closed" })
		assert.NotNil(t, fc.revalidatePlan(context.Background(), plan))
	})

	t.Run("an unchanged plan is accepted", func(t *testing.T) {
		gh := newFake(t, fixtureStackPlain()).allMethods()
		fc, _ := newForcemergeForTest(t, gh)
		plan, err := fc.planMerge(context.Background(), "test-user", "goliac", "3", prURL(3), "because", "")
		assert.Nil(t, err)
		assert.Nil(t, fc.revalidatePlan(context.Background(), plan))
	})
}

func TestForcemergeF10WaitForGithubRetarget(t *testing.T) {
	total := func(clock *fakeClock) time.Duration {
		var d time.Duration
		for _, s := range clock.slept() {
			d += s
		}
		return d
	}
	merged := &pullRequestInfo{Number: 4}
	merged.Base.Ref, merged.Head.Ref = "main", "b4"
	requested := &pullRequestInfo{Number: 3}

	t.Run("the wait is configurable: GOLIAC_WORKFLOW_FORCEMERGE_WAIT_SECONDS", func(t *testing.T) {
		previous := config.Config.WorkflowForcemergeWaitSeconds
		defer func() { config.Config.WorkflowForcemergeWaitSeconds = previous }()
		config.Config.WorkflowForcemergeWaitSeconds = 12
		gh := newFake(t, fixtureStack())
		fc, clock := newForcemergeForTest(t, gh)
		_, err := fc.retargetAfterMerge(context.Background(), "goliac", requested, merged, []*pullRequestInfo{{Number: 3}})
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "after 12s")
		assert.LessOrEqual(t, total(clock), 12*time.Second)
		assert.GreaterOrEqual(t, total(clock), 7*time.Second)
	})

	t.Run("the default is 60s", func(t *testing.T) {
		assert.Equal(t, 60*time.Second, githubWait())
	})

	t.Run("a retarget that lands after ~45s is waited for", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.deleteBranchOnMerge = true
		gh.asyncMerge = okJSON(asyncMerged)
		gh.autoRetargetLate = true
		gh.lateReads = 8
		fc, clock := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 4, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, "main", gh.pr(3).BaseRef)
		assert.Greater(t, total(clock), 10*time.Second)
	})

	t.Run("the merged-state waits use the same total", func(t *testing.T) {
		gh := newFake(t, fixtureStackPlain()).allMethods()
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`) // GitHub never marks the PR merged
		fc, clock := newForcemergeForTest(t, gh)
		fc.mergedCheckTimeout = 0
		_, err := mergePR(fc, 3, "because", "")
		assert.Nil(t, err)
		assert.GreaterOrEqual(t, total(clock), 55*time.Second)
		assert.LessOrEqual(t, total(clock), 60*time.Second)
	})
}

func TestForcemergeSquashAdviceFitsTheStack(t *testing.T) {
	t.Run("GitHub stack on a squash-only repository: no top-down advice (it would be refused too)", func(t *testing.T) {
		for _, tc := range []struct {
			from int
			mode string
		}{{3, MergeModeStack}, {4, ""}} {
			gh := newFake(t, fixtureStack()).squashOnly()
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, tc.from, "because", tc.mode)
			assert.NotNil(t, err)
			if err != nil {
				assert.Contains(t, err.Error(), "This is a GitHub stack")
				assert.NotContains(t, err.Error(), "from the top down with the direct mode")
			}
			assertNothingChanged(t, gh)
		}
	})
	t.Run("inferred stack: the top-down advice is given", func(t *testing.T) {
		gh := newFake(t, fixtureStackPlain()).squashOnly()
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "from the top down with the direct mode")
	})
}

// live (2026-11-17, phoenix-tooling): after the bottom PR of a GitHub stack of 3 is merged, the two others still say size 3
// (positions 2 and 3), the merged PR keeps its stack
func fixtureStackBottomMerged() map[int]*fakePR {
	prs := fixtureStack()
	prs[4].Merged, prs[4].State = true, "closed"
	prs[3].BaseRef = "main"
	return prs
}

func TestForcemergeR7PartlyMergedGithubStack(t *testing.T) {
	t.Run("stack mode on a stack whose bottom PR is already merged: the size counts the merged PR", func(t *testing.T) {
		for _, from := range []int{1, 2, 3} {
			gh := newFake(t, fixtureStackBottomMerged()).allMethods()
			gh.asyncMerge = okJSON(asyncMerged)
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, from, "because", MergeModeStack)
			assert.Nil(t, err, "from PR %d", from)
			assert.Equal(t, []string{"1", "2", "3"}, gh.reviewedPRs())
			for _, n := range []int{1, 2, 3} {
				assert.True(t, gh.pr(n).Merged, "PR %d", n)
			}
		}
	})

	t.Run("two PRs merged below: the position of the bottom open PR tells", func(t *testing.T) {
		prs := fixtureStackBottomMerged()
		prs[3].Merged, prs[3].State = true, "closed"
		prs[2].BaseRef = "main"
		gh := newFake(t, prs).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 1, "because", MergeModeStack)
		assert.Nil(t, err)
		assert.Equal(t, []string{"1", "2"}, gh.reviewedPRs())
	})

	t.Run("a PR missing inside a partly merged stack is still refused", func(t *testing.T) {
		prs := fixtureStackBottomMerged()
		prs[2].State = "closed"
		gh := newFake(t, prs).allMethods()
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", MergeModeStack)
		assert.NotNil(t, err)
		assert.Empty(t, gh.sideEffects())
	})

	t.Run("direct on the new bottom PR of a partly merged stack", func(t *testing.T) {
		gh := newFake(t, fixtureStackBottomMerged()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", "")
		assert.Nil(t, err)
		assert.True(t, gh.pr(3).Merged)
	})
}

func TestForcemergeAStrayPRsAboveInDirectMode(t *testing.T) {
	chain := func() map[int]*fakePR {
		prs := fixturePlainChain() // 59 > 58 > 57 > main
		prs[60] = &fakePR{Number: 60, Title: "t3 bis", Author: "a60", BaseRef: "t2", HeadRef: "t3bis", HeadSha: "sha60", State: "open"}
		prs[61] = &fakePR{Number: 61, Title: "wip", Author: "a61", BaseRef: "t2", HeadRef: "wip", HeadSha: "sha61", State: "open", Draft: true}
		prs[62] = &fakePR{Number: 62, Title: "fork", Author: "a62", BaseRef: "t2", HeadRef: "forkbranch", HeadSha: "sha62", State: "open", HeadRepo: "someone/goliac"}
		return prs
	}

	for _, mode := range []string{"", MergeModeDirect} {
		t.Run(fmt.Sprintf("direct (mode %q) on PR 58: merged, every valid PR above (59, 60) retargeted, the draft and the fork left untouched and named", mode), func(t *testing.T) {
			gh := newFake(t, chain()).allMethods()
			gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
			gh.markMergedOnBranchMerge = true
			fc, _ := newForcemergeForTest(t, gh)
			summary, err := mergePR(fc, 58, "because", mode)
			assert.Nil(t, err)
			assert.True(t, gh.pr(58).Merged)
			assert.Equal(t, "t1", gh.pr(59).BaseRef)
			assert.Equal(t, "t1", gh.pr(60).BaseRef)
			assert.Equal(t, "t2", gh.pr(61).BaseRef, "the draft is not touched")
			assert.Equal(t, "t2", gh.pr(62).BaseRef, "the fork PR is not touched")
			assert.Contains(t, summary, "retargeted onto `t1`: #59, #60")
			assert.Contains(t, summary, "left untouched (not retargeted): #61 (draft), #62 (from a fork)")
			review := firstCall(gh.find("POST", "/reviews")).Body["body"].(string)
			assert.Contains(t, review, "PRs above that are left untouched (not retargeted): #61 (draft), #62 (from a fork)")
		})
	}

	t.Run("direct on the bottom PR 57: same", func(t *testing.T) {
		gh := newFake(t, chain()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		fc, _ := newForcemergeForTest(t, gh)
		summary, err := mergePR(fc, 57, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, "main", gh.pr(58).BaseRef)
		assert.Equal(t, "t2", gh.pr(59).BaseRef, "PRs above the PR above are not touched")
		assert.NotContains(t, summary, "#61", "the draft and the fork are above PR 58, not above PR 57")
	})

	t.Run("only stray PRs above: the PR is merged as a lone PR and they are named", func(t *testing.T) {
		prs := map[int]*fakePR{
			57: {Number: 57, Title: "t1", Author: "a57", BaseRef: "main", HeadRef: "t1", HeadSha: "sha57", State: "open"},
			61: {Number: 61, Title: "wip", BaseRef: "t1", HeadRef: "wip", HeadSha: "sha61", State: "open", Draft: true},
		}
		gh := newFake(t, prs).allMethods()
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)
		summary, err := mergePR(fc, 57, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, 1, len(gh.find("PUT", "/pulls/57/merge")))
		assert.Contains(t, summary, "#61 (draft)")
		assert.Contains(t, firstCall(gh.find("POST", "/reviews")).Body["body"], "#61 (draft)")
	})

	t.Run("stack mode keeps refusing a draft, a fork PR or several PRs above", func(t *testing.T) {
		for _, n := range []int{60, 61, 62} {
			prs := fixturePlainChain()
			prs[n] = chain()[n]
			gh := newFake(t, prs).allMethods()
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, 58, "because", MergeModeStack)
			assert.NotNil(t, err, "PR %d above", n)
			assert.Empty(t, gh.sideEffects())
		}
	})

	t.Run("an irregular PR BELOW the requested PR is still refused in direct mode", func(t *testing.T) {
		for name, mutate := range map[string]func(p *fakePR){
			"draft": func(p *fakePR) { p.Draft = true },
			"fork":  func(p *fakePR) { p.HeadRepo = "someone/goliac" },
		} {
			prs := fixturePlainChain()
			mutate(prs[57])
			gh := newFake(t, prs).allMethods()
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, 58, "because", "")
			assert.NotNil(t, err, name)
			assert.Empty(t, gh.sideEffects(), name)
		}
	})

	t.Run("a PR stacked on top between the plan and the merge: valid ones retargeted, strays named", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.markMergedOnBranchMerge = true
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "POST" && call.Endpoint == "/repos/myorg/goliac/merges" {
				f.mu.Lock()
				f.prs[70] = &fakePR{Number: 70, Title: "late", BaseRef: "t2", HeadRef: "late", HeadSha: "sha70", State: "open"}
				f.prs[71] = &fakePR{Number: 71, Title: "late draft", BaseRef: "t2", HeadRef: "late2", HeadSha: "sha71", State: "open", Draft: true}
				f.mu.Unlock()
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		summary, err := mergePR(fc, 58, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, "t1", gh.pr(70).BaseRef)
		assert.Equal(t, "t2", gh.pr(71).BaseRef)
		assert.Contains(t, summary, "#71 (draft)")
	})
}

func TestForcemergeBProtectedBranchesAreNeverDead(t *testing.T) {
	deadBranch := func() map[int]*fakePR {
		return map[int]*fakePR{
			10: {Number: 10, Title: "release", BaseRef: "main", HeadRef: "release/1.0", HeadSha: "tip", State: "closed", Merged: true},
			11: {Number: 11, Title: "hotfix", Author: "a11", BaseRef: "release/1.0", HeadRef: "hotfix", HeadSha: "sha11", State: "open"},
		}
	}
	run := func(setup func(gh *fakeGitHub)) (error, *fakeGitHub) {
		gh := newFake(t, deadBranch()).allMethods()
		gh.branchTips = map[string]string{"release/1.0": "tip"} // unchanged since the merge of PR 10
		gh.syncMerge = okJSON(`{"merged":true}`)
		setup(gh)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 11, "hotfix", "")
		return err, gh
	}

	t.Run("unprotected: dead, refused", func(t *testing.T) {
		err, gh := run(func(gh *fakeGitHub) {})
		assert.NotNil(t, err)
		assert.Empty(t, gh.sideEffects())
	})
	t.Run("covered by classic branch protection: merged", func(t *testing.T) {
		err, gh := run(func(gh *fakeGitHub) { gh.branchProtected = map[string]bool{"release/1.0": true} })
		assert.Nil(t, err)
		assert.Equal(t, 1, len(gh.find("PUT", "/pulls/11/merge")))
		assert.Empty(t, gh.find("GET", "/rules/branches/release%2F1.0"), "no need to look at the rulesets")
	})
	t.Run("covered by a ruleset only (the branch is not 'protected'): merged", func(t *testing.T) {
		err, gh := run(func(gh *fakeGitHub) { gh.branchRules = map[string]int{"release/1.0": 2} })
		assert.Nil(t, err)
		assert.Equal(t, 1, len(gh.find("PUT", "/pulls/11/merge")))
		assert.Equal(t, 1, len(gh.find("GET", "/rules/branches/release%2F1.0")))
	})
	t.Run("a failing rules lookup refuses", func(t *testing.T) {
		err, gh := run(func(gh *fakeGitHub) {
			gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
				if strings.Contains(call.Endpoint, "/rules/branches/") {
					return httpError("500 Internal Server Error", `{}`)
				}
				return nil, nil
			}
		})
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "error reading the rules that apply to the branch release/1.0")
		assert.Empty(t, gh.sideEffects())
	})
	t.Run("an unreadable rules answer refuses", func(t *testing.T) {
		err, gh := run(func(gh *fakeGitHub) {
			gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
				if strings.Contains(call.Endpoint, "/rules/branches/") {
					return []byte(`not json`), nil
				}
				return nil, nil
			}
		})
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "error parsing the rules")
		assert.Empty(t, gh.sideEffects())
	})
	t.Run("a branch that moved on is alive without any lookup of the rules", func(t *testing.T) {
		err, gh := run(func(gh *fakeGitHub) { gh.branchTips["release/1.0"] = "moved" })
		assert.Nil(t, err)
		assert.Empty(t, gh.find("GET", "/rules/branches/release%2F1.0"))
	})
}
