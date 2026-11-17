package workflow

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const repoPath = "/repos/myorg/goliac"

func stack4(native bool) map[int]*fakePR {
	if native {
		return fixtureStack()
	}
	return fixtureStackPlain()
}

func dismissed(a *advGH) []string {
	res := []string{}
	for _, c := range a.callsTo("PUT", "/dismissals") {
		res = append(res, strings.SplitN(strings.TrimPrefix(c.Endpoint, repoPath+"/pulls/"), "/", 2)[0])
	}
	return res
}

func assertNoSideEffect(t *testing.T, a *advGH, msg string) {
	t.Helper()
	assert.Empty(t, a.mutating(), "%s: nothing approved, merged, retargeted, dismissed or commented", msg)
	for n, p := range a.inner.prs {
		assert.False(t, p.Merged, "%s: PR %d merged", msg, n)
	}
}

// ---------------------------------------------------------------------------------------------------------
// 1. pagination
// ---------------------------------------------------------------------------------------------------------

func TestAdvPaginationGithubStackChildOnPage2(t *testing.T) {
	// native stack 1 > 2 > 3 > 4 > main, plus 33 newer plain PRs based on b4 (the head of the bottom PR): GitHub lists
	// 30 PRs per page, newest first, so PR 3 is on page 2 of "the open PRs based on b4"
	prs := fixtureStack()
	for n := 100; n < 133; n++ {
		prs[n] = &fakePR{Number: n, Title: "noise", BaseRef: "b4", HeadRef: fmt.Sprintf("noise%d", n), HeadSha: fmt.Sprintf("n%d", n), State: "open"}
	}
	a := newAdv(t, prs)
	fc, _ := newAdvFC(t, a)
	summary, err := mergePR(fc, 4, "because", MergeModeStack)
	assert.Nil(t, err, "a valid native stack is merged even when a branch has more than 30 PRs based on it")
	assert.Contains(t, summary, "Force merged (stack) into `main`")
	assert.Equal(t, map[int]string{1: "main", 2: "main", 3: "main", 4: "main"}, a.mergedInto())
}

func TestAdvPaginationInferredStackRefusedWhenManyPRsBasedOnABranch(t *testing.T) {
	prs := fixtureStackPlain()
	for n := 100; n < 133; n++ {
		prs[n] = &fakePR{Number: n, Title: "noise", BaseRef: "b4", HeadRef: fmt.Sprintf("noise%d", n), HeadSha: fmt.Sprintf("n%d", n), State: "open"}
	}
	a := newAdv(t, prs)
	fc, _ := newAdvFC(t, a)
	_, err := mergePR(fc, 3, "because", "")
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "forks")
	assertNoSideEffect(t, a, "fork of the chain")
}

func TestAdvPaginationMoreThan100UnrelatedPRs(t *testing.T) {
	for _, mode := range []string{"", MergeModeStack} {
		prs := fixtureStackPlain()
		for n := 100; n < 260; n++ {
			prs[n] = &fakePR{Number: n, Title: "noise", BaseRef: "main", HeadRef: fmt.Sprintf("noise%d", n), HeadSha: fmt.Sprintf("n%d", n), State: "open"}
		}
		a := newAdv(t, prs)
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 3, "because", mode)
		assert.Nil(t, err, mode)
		assert.LessOrEqual(t, a.maxPage, 100)
		if mode == "" {
			assert.Equal(t, map[int]string{3: "b4"}, a.mergedInto())
			assert.Equal(t, "b4", a.bases()[2])
		} else {
			assert.Equal(t, map[int]string{1: "main", 2: "main", 3: "main", 4: "main"}, a.mergedInto())
		}
		for n := 100; n < 260; n++ {
			assert.False(t, a.snap(n).Merged)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------
// 2. GitHub's own retarget arriving after 0, 1, 2, N reads, or never
// ---------------------------------------------------------------------------------------------------------

func TestAdvAutoRetargetArrivalInferred(t *testing.T) {
	type sc struct {
		name   string
		from   int
		mode   string
		merged map[int]string
		bases  map[int]string
	}
	scenarios := []sc{
		{"direct mid (3)", 3, "", map[int]string{3: "b4"}, map[int]string{1: "b2", 2: "b4", 4: "main"}},
		{"direct bottom (4)", 4, "", map[int]string{4: "main"}, map[int]string{1: "b2", 2: "b3", 3: "main"}},
		{"direct top (1)", 1, "", map[int]string{1: "b2"}, map[int]string{2: "b3", 3: "b4", 4: "main"}},
		{"stack from 2", 2, MergeModeStack, map[int]string{1: "main", 2: "main", 3: "main", 4: "main"}, nil},
	}
	for _, s := range scenarios {
		for _, after := range []int{0, 1, 2, 3, 5, -1} {
			t.Run(fmt.Sprintf("%s, retarget after %d reads", s.name, after), func(t *testing.T) {
				a := newAdv(t, fixtureStackPlain())
				a.inner.deleteBranchOnMerge = true
				a.arriveAfter = after
				fc, _ := newAdvFC(t, a)
				_, err := mergePR(fc, s.from, "because", s.mode)
				assert.Nil(t, err)
				assert.Equal(t, s.merged, a.mergedInto())
				for n, b := range s.bases {
					assert.Equal(t, b, a.bases()[n], "base of PR %d", n)
				}
				assert.Empty(t, dismissed(a))
				// Goliac never PATCHes more than once per PR to retarget
				for n := 1; n <= 4; n++ {
					assert.LessOrEqual(t, a.count("PATCH", fmt.Sprintf("/pulls/%d", n)), 1, "PATCH of PR %d", n)
				}
			})
		}
	}
}

func TestAdvAutoRetargetArrivalNativeStack(t *testing.T) {
	for _, after := range []int{0, 1, 2, 3, 4, 5, -1} {
		ok := after >= 0 // GitHub's retarget is waited for up to 60s (F10): a few reads later is fine, never is not
		t.Run(fmt.Sprintf("direct bottom, retarget after %d reads", after), func(t *testing.T) {
			a := newAdv(t, fixtureStack())
			a.arriveAfter = after
			fc, clock := newAdvFC(t, a)
			_, err := mergePR(fc, 4, "because", "")
			assert.Equal(t, map[int]string{4: "main"}, a.mergedInto())
			assert.Equal(t, 0, a.count("PATCH", "/pulls/3"), "Goliac never patches the base of a PR of a native stack")
			assert.Empty(t, dismissed(a), "the PR is merged: nothing to dismiss")
			if ok {
				assert.Nil(t, err)
				assert.Equal(t, "main", a.bases()[3])
			} else {
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "PR #4 is merged, but GitHub did not retarget")
				var total time.Duration
				for _, d := range clock.slept() {
					total += d
				}
				assert.LessOrEqual(t, total, 60*time.Second)
				assert.GreaterOrEqual(t, total, 50*time.Second)
			}
		})
		t.Run(fmt.Sprintf("stack from 2, retarget after %d reads", after), func(t *testing.T) {
			a := newAdv(t, fixtureStack())
			a.arriveAfter = after
			fc, _ := newAdvFC(t, a)
			_, err := mergePR(fc, 2, "because", MergeModeStack)
			assert.Equal(t, 0, a.count("PATCH", "/pulls/3")+a.count("PATCH", "/pulls/2")+a.count("PATCH", "/pulls/1"))
			if ok {
				assert.Nil(t, err)
				assert.Equal(t, map[int]string{1: "main", 2: "main", 3: "main", 4: "main"}, a.mergedInto())
				assert.Empty(t, dismissed(a))
			} else {
				assert.NotNil(t, err)
				assert.Equal(t, map[int]string{4: "main"}, a.mergedInto())
				assert.Contains(t, err.Error(), "already merged into `main`: #4; not merged: #3, #2, #1")
				assert.ElementsMatch(t, []string{"1", "2", "3"}, dismissed(a), "the approvals of the PRs not merged are dismissed, not the merged one")
			}
		})
	}
}

// ---------------------------------------------------------------------------------------------------------
// 3. asynchronous merge lifecycle
// ---------------------------------------------------------------------------------------------------------

func TestAdvAsyncLifecycle(t *testing.T) {
	pending := `{"status":"pending","details":{"message":"merge requested","uuid":"u-1"}}`
	failed := `{"status":"failed","details":{"message":"Pull request has merge conflicts","uuid":"u-1"}}`

	t.Run("bottom stays queued 3 polls then fails: nothing merged, nothing retargeted, all approvals dismissed", func(t *testing.T) {
		a := newAdv(t, fixtureStackPlain())
		polls := 0
		a.inner.asyncMerge = okJSON(pending)
		a.inner.asyncPoll = func(c restCall) ([]byte, error) {
			polls++
			if polls < 3 {
				return []byte(pending), nil
			}
			return []byte(failed), nil
		}
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 3, "because", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "already merged into `main`: none; not merged: #4, #3, #2, #1")
		assert.Equal(t, []string{"POST /pulls/1/reviews", "POST /pulls/2/reviews", "POST /pulls/3/reviews", "POST /pulls/4/reviews", "PUT /pulls/4/merge-async", "PUT /pulls/1/reviews/1001/dismissals", "PUT /pulls/2/reviews/1002/dismissals", "PUT /pulls/3/reviews/1003/dismissals", "PUT /pulls/4/reviews/1004/dismissals"}, a.mutating())
		assert.Empty(t, a.mergedInto())
	})

	t.Run("second PR queued then fails: the bottom stays merged, the others are dismissed, the retarget of PR 3 was done", func(t *testing.T) {
		a := newAdv(t, fixtureStackPlain())
		a.inner.asyncMerge = func(c restCall) ([]byte, error) {
			if strings.Contains(c.Endpoint, "/pulls/4/") {
				return []byte(asyncMerged), nil
			}
			return []byte(pending), nil
		}
		a.inner.asyncPoll = okJSON(failed)
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 2, "because", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "already merged into `main`: #4; not merged: #3, #2, #1")
		assert.Equal(t, map[int]string{4: "main"}, a.mergedInto())
		assert.ElementsMatch(t, []string{"1", "2", "3"}, dismissed(a))
		assert.Equal(t, "main", a.bases()[3])
		assert.Equal(t, "b3", a.bases()[2])
	})

	t.Run("a retry of the force merge after the partial failure finishes the stack", func(t *testing.T) {
		for _, native := range []bool{false, true} {
			a := newAdv(t, stack4(native))
			fail := true
			a.inner.asyncMerge = func(c restCall) ([]byte, error) {
				if fail && strings.Contains(c.Endpoint, "/pulls/3/") {
					return []byte(failed), nil
				}
				return []byte(asyncMerged), nil
			}
			fc, _ := newAdvFC(t, a)
			_, err := mergePR(fc, 2, "because", MergeModeStack)
			assert.NotNil(t, err, native)
			assert.Equal(t, map[int]string{4: "main"}, a.mergedInto())
			fail = false
			if native {
				// GitHub shrinks the stack: 1 > 2 > 3 > main
				for n, pos := range map[int]int{3: 1, 2: 2, 1: 3} {
					a.mutate(n, func(p *fakePR) { p.StackPosition, p.StackSize = pos, 3 })
				}
			}
			_, err = mergePR(fc, 2, "because", MergeModeStack)
			assert.Nil(t, err, native)
			assert.Equal(t, map[int]string{1: "main", 2: "main", 3: "main", 4: "main"}, a.mergedInto(), native)
		}
	})

	t.Run("stays pending until the timeout: unknown outcome, approvals left, nothing retargeted", func(t *testing.T) {
		a := newAdv(t, fixtureStackPlain())
		a.inner.asyncMerge = okJSON(pending)
		a.inner.asyncPoll = okJSON(pending)
		fc, clock := newAdvFC(t, a)
		_, err := mergePR(fc, 4, "because", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "timeout after 10s")
		// R2: PR 4's outcome is unknown (its approval is left), the PRs after it were never tried (dismissed)
		assert.ElementsMatch(t, []string{"1", "2", "3"}, dismissed(a))
		assert.Equal(t, 0, a.count("PATCH", "/pulls/3"))
		assert.Equal(t, 1, a.count("PUT", "/merge-async"))
		total := time.Duration(0)
		for _, d := range clock.slept() {
			total += d
		}
		assert.LessOrEqual(t, total, 10*time.Second)
	})

	t.Run("enqueued in a merge queue: not a merge, approvals left, the next PR is not touched", func(t *testing.T) {
		a := newAdv(t, fixtureStackPlain())
		a.inner.asyncMerge = okJSON(`{"status":"enqueued","details":{"message":"queued","uuid":"u-1"}}`)
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 4, "because", MergeModeStack)
		assert.NotNil(t, err)
		assert.ElementsMatch(t, []string{"1", "2", "3"}, dismissed(a), "PR 4's approval is left (it is queued), the others were never tried")
		assert.Empty(t, a.mergedInto())
		assert.Equal(t, 0, a.count("PATCH", ""))
	})

	t.Run("GitHub says merged but the PR never shows merged: nothing is retargeted", func(t *testing.T) {
		a := newAdv(t, fixtureStackPlain())
		a.inner.neverMarkMerged = true
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 4, "because", MergeModeStack)
		assert.NotNil(t, err)
		assert.Equal(t, 0, a.count("PATCH", ""))
		assert.Equal(t, 1, a.count("PUT", "/merge-async"))
		assert.ElementsMatch(t, []string{"1", "2", "3"}, dismissed(a))
	})
}

// ---------------------------------------------------------------------------------------------------------
// 4. mergeability
// ---------------------------------------------------------------------------------------------------------

func TestAdvMergeabilityTransient405MustNotDowngradeTheMergeMethod(t *testing.T) {
	// GitHub computes mergeability lazily: PUT merge can answer 405 "Pull Request is not mergeable" while the
	// repository allows merge commits. The merge method is then not the problem and must not change to squash.
	a := newAdv(t, fixturePlainPR())
	calls := 0
	a.inner.syncMerge = func(c restCall) ([]byte, error) {
		calls++
		if calls == 1 {
			return httpError("405 Method Not Allowed", `{"message":"Pull Request is not mergeable"}`)
		}
		return []byte(`{"merged":true}`), nil
	}
	fc, _ := newAdvFC(t, a)
	_, err := mergePR(fc, 32, "because", "")
	for _, c := range a.callsTo("PUT", "/merge") {
		assert.Equal(t, "merge", c.Body["merge_method"], "the repository allows merge commits: never downgraded to squash")
	}
	_ = err
}

func TestAdvMergeabilityFalseIsReportedAndDismissed(t *testing.T) {
	a := newAdv(t, fixturePlainPR())
	a.inner.syncMerge = okJSON("")
	a.inner.syncMerge = func(c restCall) ([]byte, error) {
		return httpError("405 Method Not Allowed", `{"message":"Pull Request is not mergeable"}`)
	}
	fc, _ := newAdvFC(t, a)
	_, err := mergePR(fc, 32, "because", "")
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "not mergeable")
	assert.Equal(t, []string{"32"}, dismissed(a))
	assert.Empty(t, a.mergedInto())
}

// ---------------------------------------------------------------------------------------------------------
// 5. things that happen between the approval and the merge
// ---------------------------------------------------------------------------------------------------------

func TestAdvHeadPushedOnEveryPROfAStack(t *testing.T) {
	for _, native := range []bool{false, true} {
		for k := 1; k <= 4; k++ {
			t.Run(fmt.Sprintf("native=%v push on PR %d before its review, and before the heads re-check", native, k), func(t *testing.T) {
				for _, phase := range []string{"before-review", "before-recheck"} {
					a := newAdv(t, stack4(native))
					pushed := false
					a.before = func(a *advGH, c restCall) {
						trigger := false
						switch phase {
						case "before-review":
							trigger = c.Method == "POST" && c.Endpoint == fmt.Sprintf("%s/pulls/%d/reviews", repoPath, k)
						case "before-recheck":
							trigger = c.Method == "GET" && c.Endpoint == fmt.Sprintf("%s/pulls/%d", repoPath, k) && len(a.callsTo("POST", "/reviews")) == 4
						}
						if trigger && !pushed {
							pushed = true
							a.mutate(k, func(p *fakePR) { p.HeadSha = "pushed" })
						}
					}
					fc, _ := newAdvFC(t, a)
					_, err := mergePR(fc, 2, "because", MergeModeStack)
					assert.NotNil(t, err, phase)
					assert.True(t, pushed, phase)
					if phase == "before-review" {
						// GitHub refuses to approve a commit that is not the head of the PR (422): the approvals created before are dismissed
						assert.Contains(t, err.Error(), fmt.Sprintf("error approving the PR #%d", k), phase)
						expected := []string{}
						for j := 1; j < k; j++ {
							expected = append(expected, fmt.Sprint(j))
						}
						assert.ElementsMatch(t, expected, dismissed(a), phase)
					} else {
						assert.Contains(t, err.Error(), fmt.Sprintf("PR #%d was pushed to after the approval", k), phase)
						assert.ElementsMatch(t, []string{"1", "2", "3", "4"}, dismissed(a), phase)
					}
					assert.Equal(t, 0, a.count("PUT", "/merge-async"), phase)
					assert.Equal(t, 0, a.count("PATCH", ""), phase)
					assert.Empty(t, a.mergedInto(), phase)
				}
			})
		}
	}
}

func TestAdvHeadPushedJustBeforeEachMerge(t *testing.T) {
	for _, native := range []bool{false, true} {
		for k := 1; k <= 4; k++ {
			t.Run(fmt.Sprintf("native=%v push on PR %d right before its asynchronous merge", native, k), func(t *testing.T) {
				a := newAdv(t, stack4(native))
				a.before = func(a *advGH, c restCall) {
					if c.Method == "PUT" && c.Endpoint == fmt.Sprintf("%s/pulls/%d/merge-async", repoPath, k) {
						a.mutate(k, func(p *fakePR) { p.HeadSha = "pushed" })
					}
				}
				fc, _ := newAdvFC(t, a)
				_, err := mergePR(fc, 2, "because", MergeModeStack)
				assert.NotNil(t, err)
				merged := map[int]string{}
				for n := 4; n > k; n-- {
					merged[n] = "main"
				}
				assert.Equal(t, merged, a.mergedInto(), "only the PRs below %d are merged", k)
				notMerged := []string{}
				for n := 1; n <= k; n++ {
					notMerged = append(notMerged, fmt.Sprintf("%d", n))
				}
				assert.ElementsMatch(t, notMerged, dismissed(a), "approvals of the PRs not merged")
				assert.Contains(t, err.Error(), "Head branch was modified")
				// no PR was merged with another head than the approved one
				for _, c := range a.callsTo("PUT", "/merge-async") {
					assert.NotEmpty(t, c.Body["sha"])
				}
			})
		}
	}
}

func TestAdvPlainPRPushedBetweenApprovalAndMerge(t *testing.T) {
	// the approval is on the validated head: the synchronous merge must not merge another one
	a := newAdv(t, fixturePlainPR())
	a.before = func(a *advGH, c restCall) {
		if c.Method == "PUT" && strings.HasSuffix(c.Endpoint, "/pulls/32/merge") {
			a.mutate(32, func(p *fakePR) { p.HeadSha = "pushed-after-approval" })
		}
	}
	fc, _ := newAdvFC(t, a)
	_, err := mergePR(fc, 32, "because", "")
	merges := a.callsTo("PUT", "/merge")
	assert.Equal(t, 1, len(merges))
	pinned := merges[0].Body["sha"] == "aaa111"
	assert.True(t, pinned || err != nil, "either the merge is pinned to the approved head or refused: the unreviewed push must not be merged (err=%v)", err)
	if !pinned {
		assert.False(t, a.snap(32).Merged)
	}
}

func TestAdvDirectMidPushedAfterApproval(t *testing.T) {
	a := newAdv(t, fixtureStackPlain())
	a.before = func(a *advGH, c restCall) {
		if c.Method == "GET" && c.Endpoint == fmt.Sprintf("%s/pulls/3", repoPath) && a.count("POST", "/pulls/3/reviews") == 1 {
			a.mutate(3, func(p *fakePR) { p.HeadSha = "pushed" })
		}
	}
	fc, _ := newAdvFC(t, a)
	_, err := mergePR(fc, 3, "because", "")
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "was pushed to after the approval")
	assert.Equal(t, []string{"POST /pulls/3/reviews", "PUT /pulls/3/reviews/1001/dismissals"}, a.mutating())
}

func TestAdvBaseChangedByASomeoneMidOperation(t *testing.T) {
	t.Run("direct mid: base changed between the plan and the heads re-check is refused", func(t *testing.T) {
		a := newAdv(t, fixtureStackPlain())
		a.before = func(a *advGH, c restCall) {
			if c.Method == "GET" && c.Endpoint == fmt.Sprintf("%s/pulls/3", repoPath) && a.count("POST", "/pulls/3/reviews") == 1 {
				a.mutate(3, func(p *fakePR) { p.BaseRef = "main" })
			}
		}
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err, "PR 3 no longer targets b4: merging its head into b4 is not what was planned")
		assert.Equal(t, 0, a.count("POST", "/merges"))
	})

	t.Run("stack: the bottom PR retargeted onto a release branch after the approvals is not merged into it", func(t *testing.T) {
		for _, native := range []bool{false, true} {
			a := newAdv(t, stack4(native))
			a.addBranch("release")
			a.before = func(a *advGH, c restCall) {
				if c.Method == "GET" && c.Endpoint == fmt.Sprintf("%s/pulls/4", repoPath) && a.count("POST", "/reviews") == 4 {
					a.mutate(4, func(p *fakePR) { p.BaseRef = "release" })
				}
			}
			fc, _ := newAdvFC(t, a)
			_, err := mergePR(fc, 2, "because", MergeModeStack)
			assert.NotEqual(t, "release", a.mergedInto()[4], "native=%v: the change was force merged into release instead of main", native)
			assert.NotNil(t, err, native)
		}
	})
}

func TestAdvClosedOrReopenedMidOperation(t *testing.T) {
	t.Run("stack: a PR closed after the approvals is refused before any merge", func(t *testing.T) {
		for _, native := range []bool{false, true} {
			a := newAdv(t, stack4(native))
			a.before = func(a *advGH, c restCall) {
				if c.Method == "GET" && c.Endpoint == fmt.Sprintf("%s/pulls/2", repoPath) && a.count("POST", "/reviews") == 4 {
					a.mutate(2, func(p *fakePR) { p.State = "closed" })
				}
			}
			fc, _ := newAdvFC(t, a)
			_, err := mergePR(fc, 3, "because", MergeModeStack)
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "PR #2 is no longer open")
			assert.Equal(t, 0, a.count("PUT", "/merge-async"))
			assert.ElementsMatch(t, []string{"1", "2", "3", "4"}, dismissed(a))
		}
	})

	t.Run("stack: a PR closed right before its merge: the PRs below stay merged, the others are dismissed", func(t *testing.T) {
		a := newAdv(t, fixtureStackPlain())
		a.before = func(a *advGH, c restCall) {
			if c.Method == "PUT" && c.Endpoint == fmt.Sprintf("%s/pulls/2/merge-async", repoPath) {
				a.mutate(2, func(p *fakePR) { p.State = "closed" })
			}
		}
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 3, "because", MergeModeStack)
		assert.NotNil(t, err)
		assert.Equal(t, map[int]string{4: "main", 3: "main"}, a.mergedInto())
		assert.ElementsMatch(t, []string{"1", "2"}, dismissed(a))
		assert.Equal(t, 0, a.count("PATCH", "/pulls/1"), "PR 1 is not retargeted before PR 2 merged")
	})

	t.Run("stack: closed then reopened with the same head between the plan and the merge is merged", func(t *testing.T) {
		a := newAdv(t, fixtureStackPlain())
		fc, _ := newAdvFC(t, a)
		plan, err := fc.planMerge(t.Context(), "u", "goliac", "3", prURL(3), "because", MergeModeStack)
		assert.Nil(t, err)
		a.mutate(2, func(p *fakePR) { p.State = "closed" })
		a.mutate(2, func(p *fakePR) { p.State = "open" })
		_, err = fc.executePlan(t.Context(), plan)
		assert.Nil(t, err)
		assert.Equal(t, map[int]string{1: "main", 2: "main", 3: "main", 4: "main"}, a.mergedInto())
	})

	t.Run("direct mid: the parent PR (4) closed without merge after the plan: its branch is dead, nothing may be merged into it", func(t *testing.T) {
		a := newAdv(t, fixtureStackPlain())
		fc, _ := newAdvFC(t, a)
		plan, err := fc.planMerge(t.Context(), "u", "goliac", "3", prURL(3), "because", "")
		assert.Nil(t, err)
		a.mutate(4, func(p *fakePR) { p.State = "closed" })
		_, err = fc.executePlan(t.Context(), plan)
		assert.NotNil(t, err, "the parent closed: merging PR 3 into its branch strands the change")
		assert.Equal(t, 0, a.count("POST", "/merges"))
	})

	t.Run("direct mid: the parent PR (4) merged by someone else after the plan, branch kept: nothing may be merged into it", func(t *testing.T) {
		a := newAdv(t, fixtureStackPlain())
		fc, _ := newAdvFC(t, a)
		plan, err := fc.planMerge(t.Context(), "u", "goliac", "3", prURL(3), "because", "")
		assert.Nil(t, err)
		a.mutate(4, func(p *fakePR) { p.State, p.Merged, p.MergedInto = "closed", true, "main" })
		_, err = fc.executePlan(t.Context(), plan)
		assert.NotNil(t, err)
		assert.Equal(t, 0, a.count("POST", "/merges"))
	})

	t.Run("direct mid: the parent PR merged and its branch deleted after the plan: GitHub answers 404, nothing merged, approval dismissed", func(t *testing.T) {
		a := newAdv(t, fixtureStackPlain())
		a.inner.deleteBranchOnMerge = true
		fc, _ := newAdvFC(t, a)
		plan, err := fc.planMerge(t.Context(), "u", "goliac", "3", prURL(3), "because", "")
		assert.Nil(t, err)
		a.mutate(4, func(p *fakePR) { p.State, p.Merged, p.MergedInto = "closed", true, "main" })
		a.mu.Lock()
		delete(a.branches, "b4")
		a.mu.Unlock()
		_, err = fc.executePlan(t.Context(), plan)
		assert.NotNil(t, err)
		assert.False(t, a.snap(3).Merged)
		assert.Equal(t, []string{"3"}, dismissed(a))
	})

	t.Run("direct mid: the PR above (to retarget) closed mid-operation: skipped, nothing patched", func(t *testing.T) {
		a := newAdv(t, fixtureStackPlain())
		a.before = func(a *advGH, c restCall) {
			if c.Method == "POST" && c.Endpoint == repoPath+"/merges" {
				a.mutate(2, func(p *fakePR) { p.State = "closed" })
			}
		}
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 3, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, 0, a.count("PATCH", "/pulls/2"))
		assert.Equal(t, map[int]string{3: "b4"}, a.mergedInto())
	})
}

// ---------------------------------------------------------------------------------------------------------
// 6. several force merges on one stack
// ---------------------------------------------------------------------------------------------------------

func TestAdvSequentialForceMergesOnOneStack(t *testing.T) {
	a := newAdv(t, fixtureStackPlain())
	fc, _ := newAdvFC(t, a)
	_, err := mergePR(fc, 3, "one", "")
	assert.Nil(t, err)
	_, err = mergePR(fc, 2, "two", "")
	assert.Nil(t, err)
	assert.Equal(t, "b4", a.bases()[1], "PR 1 retargeted onto b4 by the second merge")
	_, err = mergePR(fc, 4, "three", MergeModeStack)
	assert.Nil(t, err)
	assert.Equal(t, map[int]string{3: "b4", 2: "b4", 4: "main", 1: "main"}, a.mergedInto())
	// a redelivered request for a merged PR changes nothing
	before := len(a.mutating())
	_, err = mergePR(fc, 3, "again", "")
	assert.NotNil(t, err)
	assert.Equal(t, before, len(a.mutating()))
}

func TestAdvConcurrentForceMergesOnOneStackNeverStrandAPR(t *testing.T) {
	for iter := 0; iter < 25; iter++ {
		for _, native := range []bool{false} {
			a := newAdv(t, stack4(native))
			a.inner.deleteBranchOnMerge = iter%2 == 0
			fc := executeFixture(t, a.inner, &recordingPlugin{})
			fc.ws.(*WorkflowServiceImpl).ghclient = a
			var wg sync.WaitGroup
			order := [][]int{{1, 2, 3, 4}, {4, 3, 2, 1}, {2, 4, 1, 3}, {3, 1, 4, 2}}[iter%4]
			results := make([]error, 5)
			for _, n := range order {
				wg.Add(1)
				go func(n int) {
					defer wg.Done()
					_, results[n] = exec(fc, n, "", false)
				}(n)
			}
			wg.Wait()
			// every PR is merged at most once
			for n := 1; n <= 4; n++ {
				assert.LessOrEqual(t, a.count("POST", fmt.Sprintf("/pulls/%d/reviews", n)), 1, "PR %d approved once", n)
			}
			// no stranded PR: an open PR must not be based on the head branch of a merged PR (unless nothing retargeted it
			// and the force merge reported it)
			mergedPRs := a.mergedInto()
			for n, p := range a.inner.prs {
				if p.State != "open" {
					continue
				}
				for m := range mergedPRs {
					if p.BaseRef == a.inner.prs[m].HeadRef {
						assert.NotNil(t, firstErr(results), "iter %d: open PR %d is still based on b%d of merged PR %d with no error reported", iter, n, m, m)
					}
				}
			}
		}
	}
}

func firstErr(errs []error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// ---------------------------------------------------------------------------------------------------------
// 7. branch names
// ---------------------------------------------------------------------------------------------------------

func TestAdvSpecialBranchNames(t *testing.T) {
	names := []string{"feat/ünï#1%41+x", "a%2Fb", "x+y/z#", "release/2026.11/ü", "café/v+1"}
	for _, nm := range names {
		heads := []string{nm + "-t1", nm + "-t2", nm + "-t3"}
		mk := func() map[int]*fakePR {
			return map[int]*fakePR{
				1: {Number: 1, Title: "t1", BaseRef: "main", HeadRef: heads[0], HeadSha: "s1", State: "open"},
				2: {Number: 2, Title: "t2", BaseRef: heads[0], HeadRef: heads[1], HeadSha: "s2", State: "open"},
				3: {Number: 3, Title: "t3", BaseRef: heads[1], HeadRef: heads[2], HeadSha: "s3", State: "open"},
			}
		}
		t.Run("direct mid "+nm, func(t *testing.T) {
			a := newAdv(t, mk())
			a.inner.deleteBranchOnMerge = true
			fc, _ := newAdvFC(t, a)
			_, err := mergePR(fc, 2, "because", "")
			assert.Nil(t, err)
			assert.Equal(t, map[int]string{2: heads[0]}, a.mergedInto())
			assert.Equal(t, heads[0], a.bases()[3])
			assert.Equal(t, heads[0], a.callsTo("POST", "/merges")[0].Body["base"])
		})
		t.Run("stack "+nm, func(t *testing.T) {
			a := newAdv(t, mk())
			fc, _ := newAdvFC(t, a)
			_, err := mergePR(fc, 3, "because", MergeModeStack)
			assert.Nil(t, err)
			assert.Equal(t, map[int]string{1: "main", 2: "main", 3: "main"}, a.mergedInto())
		})
		t.Run("dead base lookup "+nm, func(t *testing.T) {
			prs := map[int]*fakePR{
				9: {Number: 9, Title: "lone", BaseRef: nm, HeadRef: "lone", HeadSha: "l", State: "open"},
				8: {Number: 8, Title: "old", BaseRef: "main", HeadRef: nm, HeadSha: "tip", State: "closed"},
			}
			a := newAdv(t, prs)
			a.inner.branchTips = map[string]string{nm: "tip"}
			fc, _ := newAdvFC(t, a)
			_, err := mergePR(fc, 9, "because", "")
			assert.NotNil(t, err, "the newest PR of the base is closed unmerged and the tip did not move: dead base")
			assert.Contains(t, err.Error(), "dead branch")
			assertNoSideEffect(t, a, "dead base")

			// the branch moved on: alive
			a2 := newAdv(t, map[int]*fakePR{
				9: {Number: 9, Title: "lone", BaseRef: nm, HeadRef: "lone", HeadSha: "l", State: "open"},
				8: {Number: 8, Title: "old", BaseRef: "main", HeadRef: nm, HeadSha: "tip", State: "closed"},
			})
			a2.inner.branchTips = map[string]string{nm: "other"}
			fc2, _ := newAdvFC(t, a2)
			_, err = mergePR(fc2, 9, "because", "")
			assert.Nil(t, err)
			assert.True(t, a2.snap(9).Merged)
		})
	}
}

// ---------------------------------------------------------------------------------------------------------
// 8. repository merge methods
// ---------------------------------------------------------------------------------------------------------

func TestAdvRepositoryMergeMethods(t *testing.T) {
	yes, no := true, false
	type settings struct{ m, s, r *bool }
	set := func(a *advGH, st settings) {
		a.inner.settings = &struct{ Merge, Squash, Rebase *bool }{st.m, st.s, st.r}
	}
	cases := map[string]struct {
		st     settings
		method string
	}{
		"squash only": {settings{&no, &yes, &no}, "squash"},
		"rebase only": {settings{&no, &no, &yes}, "rebase"},
		"merge only":  {settings{&yes, &no, &no}, "merge"},
	}
	for name, tc := range cases {
		t.Run(name+": plain PR", func(t *testing.T) {
			a := newAdv(t, fixturePlainPR())
			set(a, tc.st)
			fc, _ := newAdvFC(t, a)
			_, err := mergePR(fc, 32, "because", "")
			assert.Nil(t, err)
			merges := a.callsTo("PUT", "/merge")
			assert.Equal(t, 1, len(merges), "no wasted attempt with a method the repository forbids")
			assert.Equal(t, tc.method, merges[0].Body["merge_method"])
		})
		t.Run(name+": inferred stack mode is refused up front (squash and rebase rewrite the commits the PRs above carry, F5)", func(t *testing.T) {
			a := newAdv(t, fixtureStackPlain())
			set(a, tc.st)
			fc, _ := newAdvFC(t, a)
			_, err := mergePR(fc, 2, "because", MergeModeStack)
			if tc.method == "merge" {
				assert.Nil(t, err)
				ms := a.callsTo("PUT", "/merge-async")
				assert.Equal(t, 4, len(ms))
				return
			}
			assert.NotNil(t, err)
			if err != nil {
				assert.Contains(t, err.Error(), "rewrite the commits of each PR")
			}
			assertNoSideEffect(t, a, "refused before any approval")
		})
		t.Run(name+": native bottom direct with PRs above is refused up front (F5)", func(t *testing.T) {
			a := newAdv(t, fixtureStack())
			set(a, tc.st)
			fc, _ := newAdvFC(t, a)
			_, err := mergePR(fc, 4, "because", "")
			if tc.method == "merge" {
				assert.Nil(t, err)
				assert.Equal(t, 1, a.count("PUT", "/merge-async"))
				return
			}
			assert.NotNil(t, err)
			if err != nil {
				assert.Contains(t, err.Error(), "rewrites the commits they carry")
			}
			assertNoSideEffect(t, a, "refused before any approval")
		})
		t.Run(name+": direct mid on an inferred stack merges into the parent branch whatever the method", func(t *testing.T) {
			a := newAdv(t, fixtureStackPlain())
			set(a, tc.st)
			fc, _ := newAdvFC(t, a)
			_, err := mergePR(fc, 3, "because", "")
			assert.Nil(t, err)
			assert.Equal(t, 1, a.count("POST", "/merges"))
			assert.Equal(t, 0, a.count("PUT", "/merge"))
		})
	}
	t.Run("/squash on a repository without squash: refused, nothing done", func(t *testing.T) {
		for _, from := range []struct {
			prs  map[int]*fakePR
			n    int
			mode string
		}{{fixturePlainPR(), 32, ""}, {fixtureStackPlain(), 4, ""}, {fixtureStack(), 4, ""}, {fixtureStackPlain(), 2, MergeModeStack}, {fixtureStack(), 2, MergeModeStack}} {
			for _, st := range []settings{{&yes, &no, &no}, {&no, &no, &yes}} {
				a := newAdv(t, from.prs)
				set(a, st)
				fc, _ := newAdvFC(t, a)
				_, err := mergePR(fc, from.n, "ship it /squash", from.mode)
				assert.NotNil(t, err)
				assertNoSideEffect(t, a, "squash refused")
				// rebuild the fixtures (mergePR may have changed them)
				from.prs = map[int]*fakePR{}
			}
		}
	})
	t.Run("no merge method allowed: refused, nothing done", func(t *testing.T) {
		a := newAdv(t, fixtureStackPlain())
		set(a, settings{&no, &no, &no})
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 4, "because", "")
		assert.NotNil(t, err)
		assertNoSideEffect(t, a, "no method")
	})
}

// ---------------------------------------------------------------------------------------------------------
// 9. chain length: exactly 50 and 51
// ---------------------------------------------------------------------------------------------------------

func TestAdvChainLength(t *testing.T) {
	t.Run("a chain of exactly 50 is a stack, whatever the PR the request is on", func(t *testing.T) {
		for _, from := range []int{1, 2, 25, 49, 50} {
			a := newAdv(t, chainFixture(50, "main", "h%d"))
			fc, _ := newAdvFC(t, a)
			_, err := mergePR(fc, from, "because", "")
			assert.Nil(t, err, "direct on %d of 50", from)
			if from == 1 {
				assert.Equal(t, map[int]string{1: "main"}, a.mergedInto())
				assert.Equal(t, "main", a.bases()[2])
			} else {
				assert.Equal(t, map[int]string{from: fmt.Sprintf("h%d", from-1)}, a.mergedInto())
			}
		}
		a := newAdv(t, chainFixture(50, "main", "h%d"))
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 17, "because", MergeModeStack)
		assert.Nil(t, err)
		order := []string{}
		for _, c := range a.callsTo("PUT", "/merge-async") {
			order = append(order, short(c.Endpoint))
		}
		expected := []string{}
		for n := 1; n <= 50; n++ {
			expected = append(expected, fmt.Sprintf("/pulls/%d/merge-async", n))
		}
		assert.Equal(t, expected, order, "bottom-up, one asynchronous merge per PR")
		assert.Equal(t, 50, len(a.mergedInto()))
		for n := 1; n <= 50; n++ {
			assert.Equal(t, "main", a.mergedInto()[n])
		}
	})
	t.Run("a chain of 51 is refused in stack mode whatever the PR, and in direct mode from the top PR (the walk down), with no side effect", func(t *testing.T) {
		for _, from := range []int{1, 2, 25, 50, 51} {
			for _, mode := range []string{"", MergeModeStack} {
				a := newAdv(t, chainFixture(51, "main", "h%d"))
				fc, _ := newAdvFC(t, a)
				_, err := mergePR(fc, from, "because", mode)
				if mode == "" && from != 51 {
					// default/direct mode only walks down from the PR (what is above it is not part of the question)
					assert.Nil(t, err, "PR %d of 51, mode %q", from, mode)
					continue
				}
				assert.NotNil(t, err, "PR %d of 51, mode %q", from, mode)
				if err != nil {
					assert.Contains(t, err.Error(), "more than 50 PRs")
				}
				assertNoSideEffect(t, a, fmt.Sprintf("51-chain from %d", from))
			}
		}
	})
}

// ---------------------------------------------------------------------------------------------------------
// 10. default branch not called main
// ---------------------------------------------------------------------------------------------------------

func TestAdvDefaultBranchNotMain(t *testing.T) {
	for _, def := range []string{"master", "trunk", "develop/v2"} {
		t.Run(def, func(t *testing.T) {
			prs := fixtureStackPlain()
			prs[4].BaseRef = def
			a := newAdv(t, prs)
			a.inner.defaultBranch = def
			a.addBranch(def)
			fc, _ := newAdvFC(t, a)
			_, err := mergePR(fc, 3, "because", MergeModeStack)
			assert.Nil(t, err)
			assert.Equal(t, map[int]string{1: def, 2: def, 3: def, 4: def}, a.mergedInto())

			// a chain based on main while the default branch is another one: the chain does not reach it
			prs2 := fixtureStackPlain()
			a2 := newAdv(t, prs2)
			a2.inner.defaultBranch = def
			fc2, _ := newAdvFC(t, a2)
			_, err = mergePR(fc2, 3, "because", "")
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "stops at PR #4")
			assertNoSideEffect(t, a2, "chain not on the default branch")
		})
	}
	t.Run("a native stack on trunk", func(t *testing.T) {
		prs := fixtureStack()
		prs[4].BaseRef = "trunk"
		for _, p := range prs {
			p.StackBase = "trunk"
		}
		a := newAdv(t, prs)
		a.addBranch("trunk")
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 1, "because", MergeModeStack)
		assert.Nil(t, err)
		assert.Equal(t, map[int]string{1: "trunk", 2: "trunk", 3: "trunk", 4: "trunk"}, a.mergedInto())
	})
}

// ---------------------------------------------------------------------------------------------------------
// 11. redelivered webhook (ExecuteWorkflow)
// ---------------------------------------------------------------------------------------------------------

func TestAdvRedeliveredWebhook(t *testing.T) {
	a := newAdv(t, fixtureStackPlain())
	plugin := &recordingPlugin{}
	fc := executeFixture(t, a.inner, plugin)
	fc.ws.(*WorkflowServiceImpl).ghclient = a
	_, err := exec(fc, 3, MergeModeStack, false)
	assert.Nil(t, err)
	n := len(a.mutating())
	steps := len(plugin.urls)
	assert.Equal(t, 4, steps, "the steps ran once per merged PR")
	for i := 0; i < 3; i++ {
		_, err = exec(fc, 3, MergeModeStack, false)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "already merged")
		_, err = exec(fc, 1, "", false)
		assert.NotNil(t, err)
	}
	assert.Equal(t, n, len(a.mutating()), "redeliveries changed nothing")
	assert.Equal(t, steps, len(plugin.urls), "no step re-run")
}

// ---------------------------------------------------------------------------------------------------------
// 12. refusals have no side effect
// ---------------------------------------------------------------------------------------------------------

func TestAdvRefusalsHaveNoSideEffects(t *testing.T) {
	type tc struct {
		name string
		prs  func() map[int]*fakePR
		from int
		mode string
		want string
	}
	setPR := func(f func(map[int]*fakePR)) func() map[int]*fakePR {
		return func() map[int]*fakePR { p := fixtureStackPlain(); f(p); return p }
	}
	setNative := func(f func(map[int]*fakePR)) func() map[int]*fakePR {
		return func() map[int]*fakePR { p := fixtureStack(); f(p); return p }
	}
	cases := []tc{
		{"inferred: draft in the chain (direct mid)", setPR(func(p map[int]*fakePR) { p[4].Draft = true }), 3, "", "draft"},
		{"inferred: draft in the chain (stack)", setPR(func(p map[int]*fakePR) { p[1].Draft = true }), 3, MergeModeStack, "draft"},
		{"inferred: the PR itself is a draft", setPR(func(p map[int]*fakePR) { p[3].Draft = true }), 3, "", "draft"},
		{"inferred: fork above the PR, stack mode", setPR(func(p map[int]*fakePR) { p[2].HeadRepo = "someone/goliac" }), 3, MergeModeStack, "fork"},
		{"inferred: fork below the PR, direct mode", setPR(func(p map[int]*fakePR) { p[4].HeadRepo = "someone/goliac" }), 3, "", "stops at PR #3"},
		{"inferred: deleted fork in the chain", setPR(func(p map[int]*fakePR) { p[1].HeadRepo = "-" }), 3, MergeModeStack, "head repository"},
		{"inferred: the chain forks", setPR(func(p map[int]*fakePR) {
			p[9] = &fakePR{Number: 9, BaseRef: "b3", HeadRef: "other", HeadSha: "o", State: "open"}
		}), 4, MergeModeStack, "forks"},
		{"inferred: the chain does not reach the default branch", setPR(func(p map[int]*fakePR) { p[4].BaseRef = "release" }), 3, "", "stops at PR #4"},
		{"inferred: a PR below carries native stack metadata", setPR(func(p map[int]*fakePR) { p[4].StackSize, p[4].StackPosition, p[4].StackBase = 4, 1, "main" }), 3, "", "ambiguous"},
		{"inferred: cycle", func() map[int]*fakePR {
			return map[int]*fakePR{
				1: {Number: 1, BaseRef: "b2", HeadRef: "b1", HeadSha: "s", State: "open"},
				2: {Number: 2, BaseRef: "b1", HeadRef: "b2", HeadSha: "s", State: "open"},
			}
		}, 1, "", "cycle"},
		{"inferred: already merged", setPR(func(p map[int]*fakePR) { p[3].Merged, p[3].State = true, "closed" }), 3, "", "already merged"},
		{"inferred: closed", setPR(func(p map[int]*fakePR) { p[3].State = "closed" }), 3, "", "closed"},
		{"inferred: stack mode, the same head branch twice", setPR(func(p map[int]*fakePR) {
			p[9] = &fakePR{Number: 9, BaseRef: "main", HeadRef: "b4", HeadSha: "x", State: "open"}
		}), 3, MergeModeStack, ""},
		{"native: direct on a mid PR", setNative(func(p map[int]*fakePR) {}), 3, "", "not the bottom PR"},
		{"native: direct on the top PR", setNative(func(p map[int]*fakePR) {}), 1, "", "not the bottom PR"},
		{"native: draft in the stack", setNative(func(p map[int]*fakePR) { p[2].Draft = true }), 3, MergeModeStack, "draft"},
		{"native: fork in the stack", setNative(func(p map[int]*fakePR) { p[2].HeadRepo = "someone/goliac" }), 3, MergeModeStack, ""},
		{"native: a PR of the stack is missing", setNative(func(p map[int]*fakePR) { delete(p, 3) }), 4, MergeModeStack, ""},
		{"native: a PR of the stack is closed", setNative(func(p map[int]*fakePR) { p[3].State = "closed" }), 4, MergeModeStack, ""},
		{"native: stack size lies", setNative(func(p map[int]*fakePR) {
			for _, pr := range p {
				pr.StackSize = 5
			}
		}), 4, MergeModeStack, "missing"},
		{"native: malformed stack payload", setNative(func(p map[int]*fakePR) { p[2].RawStack = map[string]interface{}{"size": 0} }), 2, MergeModeStack, "malformed"},
		{"unknown merge mode", func() map[int]*fakePR { return fixtureStack() }, 4, "weird", "unknown merge mode"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newAdv(t, c.prs())
			fc := executeFixture(t, a.inner, &recordingPlugin{})
			fc.ws.(*WorkflowServiceImpl).ghclient = a
			plugin := fc.stepPlugins["jira_ticket_creation"].(*recordingPlugin)
			_, err := exec(fc, c.from, c.mode, false)
			assert.NotNil(t, err)
			if err != nil && c.want != "" {
				assert.Contains(t, err.Error(), c.want)
			}
			if strings.Contains(c.name, "already merged") {
				assert.Empty(t, a.mutating())
			} else {
				assertNoSideEffect(t, a, c.name)
			}
			assert.Empty(t, plugin.urls, "no notification / audit step")
		})
	}
}

// ---------------------------------------------------------------------------------------------------------
// 13. exact calls of the success paths
// ---------------------------------------------------------------------------------------------------------

func TestAdvExactCallsOnSuccess(t *testing.T) {
	check := func(t *testing.T, a *advGH, expected []string) {
		t.Helper()
		assert.Equal(t, expected, a.mutating())
	}
	t.Run("inferred, stack mode from PR 2", func(t *testing.T) {
		a := newAdv(t, fixtureStackPlain())
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 2, "because", MergeModeStack)
		assert.Nil(t, err)
		check(t, a, []string{
			"POST /pulls/1/reviews", "POST /pulls/2/reviews", "POST /pulls/3/reviews", "POST /pulls/4/reviews",
			"PUT /pulls/4/merge-async",
			"PATCH /pulls/3", "PUT /pulls/3/merge-async",
			"PATCH /pulls/2", "PUT /pulls/2/merge-async",
			"PATCH /pulls/1", "PUT /pulls/1/merge-async",
		})
		for _, c := range a.callsTo("PATCH", "/pulls/3") {
			assert.Equal(t, map[string]interface{}{"base": "main"}, c.Body)
		}
		for n, c := range a.callsTo("PUT", "/merge-async") {
			assert.Equal(t, fmt.Sprintf("sha%d", 4-n), c.Body["sha"])
			assert.Equal(t, "direct_merge", c.Body["merge_action"])
			assert.Equal(t, true, c.Body["bypass_rules"])
			assert.Equal(t, "merge", c.Body["merge_method"])
		}
		for _, c := range a.callsTo("POST", "/reviews") {
			assert.Equal(t, "APPROVE", c.Body["event"])
		}
	})
	t.Run("native, stack mode from PR 3: no PATCH ever", func(t *testing.T) {
		a := newAdv(t, fixtureStack())
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 3, "because", MergeModeStack)
		assert.Nil(t, err)
		check(t, a, []string{
			"POST /pulls/1/reviews", "POST /pulls/2/reviews", "POST /pulls/3/reviews", "POST /pulls/4/reviews",
			"PUT /pulls/4/merge-async", "PUT /pulls/3/merge-async", "PUT /pulls/2/merge-async", "PUT /pulls/1/merge-async",
		})
	})
	t.Run("inferred, direct mid (3)", func(t *testing.T) {
		a := newAdv(t, fixtureStackPlain())
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 3, "because", "")
		assert.Nil(t, err)
		check(t, a, []string{"POST /pulls/3/reviews", "POST /merges", "PATCH /pulls/2"})
		m := a.callsTo("POST", "/merges")[0]
		assert.Equal(t, "b4", m.Body["base"])
		assert.Equal(t, "sha3", m.Body["head"])
		assert.Equal(t, map[int]string{3: "b4"}, a.mergedInto())
		assert.Equal(t, "b4", a.bases()[2])
		assert.Equal(t, "b2", a.bases()[1])
	})
	t.Run("inferred, direct bottom (4) with delete_branch_on_merge and an arrival after 2 reads", func(t *testing.T) {
		a := newAdv(t, fixtureStackPlain())
		a.inner.deleteBranchOnMerge = true
		a.arriveAfter = 2
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 4, "because", "")
		assert.Nil(t, err)
		// GitHub's retarget lands just before the PATCH: 422, re-read, success
		check(t, a, []string{"POST /pulls/4/reviews", "PUT /pulls/4/merge-async", "PATCH /pulls/3"})
		assert.Equal(t, "main", a.bases()[3])
	})
	t.Run("native, direct bottom (4)", func(t *testing.T) {
		a := newAdv(t, fixtureStack())
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 4, "because", "")
		assert.Nil(t, err)
		check(t, a, []string{"POST /pulls/4/reviews", "PUT /pulls/4/merge-async"})
		assert.Equal(t, "main", a.bases()[3])
	})
	t.Run("plain PR", func(t *testing.T) {
		a := newAdv(t, fixturePlainPR())
		fc, _ := newAdvFC(t, a)
		_, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		check(t, a, []string{"POST /pulls/32/reviews", "PUT /pulls/32/merge"})
		assert.Equal(t, map[int]string{32: "main"}, a.mergedInto())
	})
}
