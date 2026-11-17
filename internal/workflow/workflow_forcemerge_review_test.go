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

	t.Run("pushed between the validation and the approval: nothing merged, approval dismissed", func(t *testing.T) {
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
		{"a sha mismatch", `{"message":"expected_head_sha does not match the head sha"}`, false},
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
