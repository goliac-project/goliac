package workflow

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// getHook runs mutate on the n-th GET of a PR (1 is the plan, 2 the re-validation after the locks...)
func getHook(prNumber, nth int, mutate func(f *fakeGitHub)) func(f *fakeGitHub, call restCall) ([]byte, error) {
	var mu sync.Mutex
	count := 0
	return func(f *fakeGitHub, call restCall) ([]byte, error) {
		if call.Method == "GET" && call.Endpoint == fmt.Sprintf("/repos/myorg/goliac/pulls/%d", prNumber) {
			mu.Lock()
			count++
			hit := count == nth
			mu.Unlock()
			if hit {
				mutate(f)
			}
		}
		return nil, nil
	}
}

func TestForcemergeRevalidationRunsThroughExecuteWorkflowBeforeTheSteps(t *testing.T) {
	cases := []struct {
		name   string
		prs    func() map[int]*fakePR
		from   int
		mode   string
		target int
		mutate func(p *fakePR)
		refuse bool
	}{
		{"the requested PR was pushed to", fixturePlainPRMap, 32, "", 32, func(p *fakePR) { p.HeadSha = "pushed" }, true},
		{"the requested PR was retargeted", fixturePlainPRMap, 32, "", 32, func(p *fakePR) { p.BaseRef = "elsewhere" }, true},
		{"the requested PR became a draft", fixturePlainPRMap, 32, "", 32, func(p *fakePR) { p.Draft = true }, true},
		{"the parent PR was pushed to", fixtureStackPlain, 3, "", 4, func(p *fakePR) { p.HeadSha = "pushed" }, true},
		{"the parent PR was retargeted", fixtureStackPlain, 3, "", 4, func(p *fakePR) { p.BaseRef = "elsewhere" }, true},
		{"the parent PR was closed", fixtureStackPlain, 3, "", 4, func(p *fakePR) { p.State = "closed" }, true},
		{"the parent PR became a draft", fixtureStackPlain, 3, "", 4, func(p *fakePR) { p.Draft = true }, true},
		{"a PR above was retargeted", fixtureStackPlain, 3, "", 2, func(p *fakePR) { p.BaseRef = "elsewhere" }, true},
		{"a PR above was closed", fixtureStackPlain, 3, "", 2, func(p *fakePR) { p.State = "closed" }, true},
		{"a PR above was merged", fixtureStackPlain, 3, "", 2, func(p *fakePR) { p.Merged, p.State = true, "closed" }, true},
		{"a PR above was pushed to: fine, only its base matters", fixtureStackPlain, 3, "", 2, func(p *fakePR) { p.HeadSha = "pushed" }, false},
		{"a PR above became a draft: fine, it is left alone later", fixtureStackPlain, 3, "", 2, func(p *fakePR) { p.Draft = true }, false},
		{"a stack member was pushed to", fixtureStackPlain, 3, MergeModeStack, 1, func(p *fakePR) { p.HeadSha = "pushed" }, true},
		{"a stack member was retargeted", fixtureStackPlain, 3, MergeModeStack, 2, func(p *fakePR) { p.BaseRef = "elsewhere" }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gh := newFake(t, tc.prs()).allMethods()
			gh.asyncMerge = okJSON(asyncMerged)
			gh.syncMerge = okJSON(`{"merged":true}`)
			gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
			gh.markMergedOnBranchMerge = true
			// the requested PR is read by the plan first (the re-validation is its 2nd read); the other PRs are listed by
			// the plan, their first individual read is the re-validation
			nth := 1
			if tc.target == tc.from {
				nth = 2
			}
			gh.onCall = getHook(tc.target, nth, func(f *fakeGitHub) { f.update(tc.target, tc.mutate) })
			plugin := &recordingPlugin{}
			fc := executeFixture(t, gh, plugin)
			_, err := exec(fc, tc.from, tc.mode, false)
			if tc.refuse {
				assert.NotNil(t, err)
				assert.Empty(t, plugin.urls, "no step ran: the re-validation is before the steps")
				assert.Empty(t, gh.sideEffects(), "nothing changed")
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func fixturePlainPRMap() map[int]*fakePR { return fixturePlainPR() }

func TestForcemergeParentUnchanged(t *testing.T) {
	newFC := func(mutate func(p *fakePR)) (*ForcemergeImpl, *pullRequestInfo) {
		gh := newFake(t, fixtureStackPlain()).allMethods()
		fc, _ := newForcemergeForTest(t, gh)
		parent, err := fc.fetchPullRequest(context.Background(), "goliac", "4")
		assert.Nil(t, err)
		if mutate != nil {
			gh.update(4, mutate)
		}
		return fc, parent
	}
	fc, parent := newFC(nil)
	assert.Nil(t, fc.parentUnchanged(context.Background(), "goliac", parent))
	assert.Nil(t, fc.parentUnchanged(context.Background(), "goliac", nil))
	for name, tc := range map[string]struct {
		mutate  func(p *fakePR)
		message string
	}{
		"pushed":     {func(p *fakePR) { p.HeadSha = "new" }, "was pushed to after the validation"},
		"retarget":   {func(p *fakePR) { p.BaseRef = "elsewhere" }, "was retargeted after the validation"},
		"draft":      {func(p *fakePR) { p.Draft = true }, "became a draft"},
		"closed":     {func(p *fakePR) { p.State = "closed" }, "is no longer open"},
		"merged":     {func(p *fakePR) { p.Merged, p.State = true, "closed" }, "is no longer open"},
		"unreadable": {nil, "unable to re-check"},
	} {
		t.Run(name, func(t *testing.T) {
			fc, parent := newFC(tc.mutate)
			if tc.mutate == nil {
				parent.Number = 99
			}
			err := fc.parentUnchanged(context.Background(), "goliac", parent)
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), tc.message)
		})
	}
}

func TestForcemergeSquashRulesEdges(t *testing.T) {
	t.Run("a stack of exactly 2 PRs on a squash-only repository is refused", func(t *testing.T) {
		prs := fixtureStackPlain()
		delete(prs, 1)
		delete(prs, 2)
		gh := newFake(t, prs).squashOnly()
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "the stack of 2 PRs")
		assert.Empty(t, gh.sideEffects())
	})

	t.Run("a stack of 2 PRs on a rebase-only repository is refused too", func(t *testing.T) {
		prs := fixtureStackPlain()
		delete(prs, 1)
		delete(prs, 2)
		gh := newFake(t, prs).allMethods()
		no := false
		gh.settings = &struct{ Merge, Squash, Rebase *bool }{&no, &no, boolp(true)}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), `"rebase" method`)
	})

	t.Run("a GitHub stack bottom PR with no open PR above it is squashed on a squash-only repository", func(t *testing.T) {
		prs := fixtureStack()
		for _, n := range []int{1, 2, 3} {
			prs[n].State = "closed" // the PRs above are closed: nothing is open above the bottom PR
		}
		gh := newFake(t, prs).squashOnly()
		gh.asyncMerge = okJSON(asyncMerged)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 4, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, "squash", firstCall(gh.find("PUT", "/pulls/4/merge-async")).Body["merge_method"])
	})
}

func TestForcemergeValidateStackOpen(t *testing.T) {
	gh := newFake(t, fixtureStack()).allMethods()
	fc, _ := newForcemergeForTest(t, gh)
	pr, err := fc.fetchPullRequest(context.Background(), "goliac", "3")
	assert.Nil(t, err)
	assert.Nil(t, pr.validate("goliac"))
	pr.State = ""
	err = pr.validate("goliac")
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "of a stack is not open")
	pr.State = "open"
	pr.Stack.Size = 0
	assert.Contains(t, pr.validate("goliac").Error(), "malformed stack payload")
}

func TestForcemergeInferenceEdges(t *testing.T) {
	t.Run("a lone PR is merged even when the default branch can't be read", func(t *testing.T) {
		prs := fixturePlainPR()
		prs[32].BaseRef = "release/1.0"
		gh := newFake(t, prs) // no settings
		gh.branchTips = map[string]string{}
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, 1, len(gh.find("PUT", "/pulls/32/merge")))
	})

	t.Run("a PR with a PR below and an unreadable default branch is refused", func(t *testing.T) {
		gh := newFake(t, fixtureStackPlain()) // no settings
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "unable to read the default branch")
		assert.Empty(t, gh.sideEffects())
	})

	t.Run("a PR above the chain with an unreadable default branch is refused", func(t *testing.T) {
		gh := newFake(t, fixtureStackPlain()) // no settings
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 4, "because", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "unable to read the default branch")
		assert.Empty(t, gh.sideEffects())
	})

	t.Run("a member of the chain that GitHub lists with an unknown state is refused", func(t *testing.T) {
		gh := newFake(t, fixtureStackPlain()).allMethods()
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && call.Endpoint == "/repos/myorg/goliac/pulls" && strings.Contains(call.Parameters, "head=myorg%3Ab4") {
				return []byte(`[{"number":4,"title":"PR 4","state":"","draft":false,"base":{"ref":"main"},"head":{"ref":"b4","sha":"sha4","repo":{"full_name":"myorg/goliac"}},"stack":null}]`), nil
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "is not open (state")
		assert.Empty(t, gh.sideEffects())
	})

	t.Run("the walk up reaching the same PR twice is a cycle", func(t *testing.T) {
		gh := newFake(t, fixtureStackPlain()).allMethods()
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && call.Endpoint == "/repos/myorg/goliac/pulls" && strings.Contains(call.Parameters, "base=b3") {
				// the PR based on 3's branch is... PR 4 again
				return []byte(`[{"number":4,"title":"PR 4","state":"open","draft":false,"base":{"ref":"b3"},"head":{"ref":"b4","sha":"sha4","repo":{"full_name":"myorg/goliac"}},"stack":null}]`), nil
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 4, "because", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "cycle")
	})

	t.Run("the walk down limit: 50 PRs below are fine, 51 are refused (direct from the top)", func(t *testing.T) {
		for _, tc := range []struct {
			size    int
			refused bool
		}{{50, false}, {51, true}} {
			prs := map[int]*fakePR{}
			for n := 1; n <= tc.size; n++ {
				base := fmt.Sprintf("h%d", n-1)
				if n == 1 {
					base = "main"
				}
				prs[n] = &fakePR{Number: n, Title: fmt.Sprint(n), BaseRef: base, HeadRef: fmt.Sprintf("h%d", n), HeadSha: fmt.Sprintf("s%d", n), State: "open"}
			}
			gh := newFake(t, prs).allMethods()
			gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
			gh.markMergedOnBranchMerge = true
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, tc.size, "because", "")
			if tc.refused {
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), "more than 50 PRs")
			} else {
				assert.Nil(t, err)
			}
		}
	})

	t.Run("GitHub stack walks are bounded by the size of the stack (up from the bottom, down from the top)", func(t *testing.T) {
		for from, direction := range map[int]string{4: "up", 1: "down"} {
			prs := fixtureStack()
			for _, p := range prs {
				p.StackSize = 2 // GitHub says 2 PRs but 4 are chained
			}
			gh := newFake(t, prs).allMethods()
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, from, "because", MergeModeStack)
			assert.NotNil(t, err, direction)
			assert.Contains(t, err.Error(), "more than 2 PRs found", direction)
			assert.Empty(t, gh.sideEffects())
		}
	})

	t.Run("planDirect does not take a parent with GitHub stack metadata for the same inferred stack", func(t *testing.T) {
		prs := fixturePlainChain()
		prs[57].StackSize, prs[57].StackPosition, prs[57].StackBase = 3, 1, "main"
		gh := newFake(t, prs).allMethods()
		fc, _ := newForcemergeForTest(t, gh)
		info, err := fc.fetchPullRequest(context.Background(), "goliac", "58")
		assert.Nil(t, err)
		info.Stack = &prStack{Position: 2, Size: 3}
		info.Stack.Base.Ref = "main"
		info.stackInferred = true
		err = fc.planDirect(context.Background(), "goliac", &mergePlan{pr: info})
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "no open PR of the same stack has this head branch")
	})
}

func TestForcemergeRestackStatusEdges(t *testing.T) {
	run := func(t *testing.T, status string, mutate func(f *fakeGitHub, call restCall)) (error, *fakeGitHub, *fakeClock) {
		gh := newFake(t, fixtureStackPlain()).allMethods()
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PATCH" && strings.HasSuffix(call.Endpoint, "/pulls/2") && status != "" {
				return httpError(status, `{"message":"x"}`)
			}
			if mutate != nil {
				mutate(f, call)
			}
			return nil, nil
		}
		fc, clock := newForcemergeForTest(t, gh)
		merged := &pullRequestInfo{Number: 3}
		merged.Base.Ref, merged.Head.Ref = "b4", "b3"
		_, err := fc.restack(context.Background(), "goliac", merged, []*pullRequestInfo{{Number: 2}})
		return err, gh, clock
	}
	for _, status := range []string{"408 Request Timeout", "429 Too Many Requests"} {
		t.Run(status+" is retried (the request was not processed)", func(t *testing.T) {
			err, gh, clock := run(t, status, nil)
			assert.NotNil(t, err)
			assert.Equal(t, 4, len(gh.find("PATCH", "/pulls/2")))
			assert.Equal(t, 3, len(clock.slept()))
			assert.NotContains(t, err.Error(), "not retried")
		})
	}
	for _, status := range []string{"400 Bad Request", "403 Forbidden", "404 Not Found"} {
		t.Run(status+" is not retried", func(t *testing.T) {
			err, gh, clock := run(t, status, nil)
			assert.NotNil(t, err)
			assert.Equal(t, 1, len(gh.find("PATCH", "/pulls/2")))
			assert.Empty(t, clock.slept())
			assert.Contains(t, err.Error(), "not retried")
		})
	}
	t.Run("a child closed or merged meanwhile is skipped, not patched", func(t *testing.T) {
		for _, mutate := range []func(p *fakePR){func(p *fakePR) { p.State = "closed" }, func(p *fakePR) { p.Merged, p.State = true, "closed" }} {
			gh := newFake(t, fixtureStackPlain()).allMethods()
			gh.update(2, mutate)
			fc, _ := newForcemergeForTest(t, gh)
			merged := &pullRequestInfo{Number: 3}
			merged.Base.Ref, merged.Head.Ref = "b4", "b3"
			done, err := fc.restack(context.Background(), "goliac", merged, []*pullRequestInfo{{Number: 2}})
			assert.Nil(t, err)
			assert.Empty(t, done)
			assert.Empty(t, gh.find("PATCH", "/pulls/2"))
		}
	})
	t.Run("a child based on another branch than the merged one is reported, not patched", func(t *testing.T) {
		gh := newFake(t, fixtureStackPlain()).allMethods()
		gh.update(2, func(p *fakePR) { p.BaseRef = "elsewhere" })
		fc, _ := newForcemergeForTest(t, gh)
		merged := &pullRequestInfo{Number: 3}
		merged.Base.Ref, merged.Head.Ref = "b4", "b3"
		_, err := fc.restack(context.Background(), "goliac", merged, []*pullRequestInfo{{Number: 2}})
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "it is now based on elsewhere")
		assert.Empty(t, gh.find("PATCH", "/pulls/2"))
	})
}

func TestForcemergeRetryAndPollEdges(t *testing.T) {
	t.Run("retryMerge stops at once when the context is cancelled during the backoff", func(t *testing.T) {
		fc, _ := newForcemergeForTest(t, newFake(t, fixturePlainPR()))
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		body, err := fc.retryMerge(ctx, func() ([]byte, error) {
			calls++
			cancel() // the context dies after the first attempt
			return httpError("405 Method Not Allowed", `{"message":"Pull Request is not mergeable"}`)
		})
		assert.NotNil(t, err)
		assert.Equal(t, 1, calls)
		assert.Contains(t, string(body), "not mergeable")
	})

	t.Run("an unreadable poll answer is an indeterminate outcome", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = okJSON(asyncPending)
		gh.asyncPoll = okJSON(`not json`)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 4, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "error parsing the asynchronous merge status")
		assert.Contains(t, err.Error(), "left in place")
	})
}

func TestForcemergeConfirmMergedUsesTheWait(t *testing.T) {
	gh := newFake(t, fixtureStack()).allMethods()
	gh.asyncMerge = okJSON(asyncMerged)
	gh.neverMarkMerged = true
	fc, clock := newForcemergeForTest(t, gh)
	fc.mergedCheckTimeout = 0 // the default: GOLIAC_WORKFLOW_FORCEMERGE_WAIT_SECONDS
	_, err := mergePR(fc, 4, "because", "")
	assert.NotNil(t, err)
	var total time.Duration
	for _, d := range clock.slept() {
		total += d
	}
	assert.GreaterOrEqual(t, total, 55*time.Second)
	assert.LessOrEqual(t, total, 60*time.Second)

	// a configured value is used as is
	gh2 := newFake(t, fixtureStack()).allMethods()
	gh2.asyncMerge = okJSON(asyncMerged)
	gh2.neverMarkMerged = true
	fc2, clock2 := newForcemergeForTest(t, gh2)
	fc2.mergedCheckTimeout = 5 * time.Second
	_, _ = mergePR(fc2, 4, "because", "")
	var total2 time.Duration
	for _, d := range clock2.slept() {
		total2 += d
	}
	assert.LessOrEqual(t, total2, 5*time.Second)
}

func TestForcemergeDismissWithoutAReviewID(t *testing.T) {
	gh := newFake(t, fixturePlainPR()).allMethods()
	gh.syncMerge = func(call restCall) ([]byte, error) { return httpError("403 Forbidden", `{"message":"no"}`) }
	inner := gh.onCall
	gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
		if call.Method == "POST" && strings.HasSuffix(call.Endpoint, "/reviews") {
			return []byte(`{"state":"APPROVED"}`), nil // an answer without a review id
		}
		if inner != nil {
			return inner(f, call)
		}
		return nil, nil
	}
	fc, _ := newForcemergeForTest(t, gh)
	_, err := mergePR(fc, 32, "because", "")
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "could not all be dismissed, dismiss them manually: PR #32 (the approval may exist, its review id is unknown)")
	assert.Empty(t, gh.find("PUT", "/dismissals"))
}

func TestForcemergeDeadBaseNewestPRChecks(t *testing.T) {
	newest := func(item string) *fakeGitHub {
		prs := map[int]*fakePR{11: {Number: 11, Title: "hotfix", Author: "a11", BaseRef: "release/1.0", HeadRef: "hotfix", HeadSha: "sha11", State: "open"}}
		gh := newFake(t, prs).allMethods()
		gh.branchTips = map[string]string{"release/1.0": "tip"}
		gh.syncMerge = okJSON(`{"merged":true}`)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && call.Endpoint == "/repos/myorg/goliac/pulls" && strings.Contains(call.Parameters, "state=all") {
				return []byte("[" + item + "]"), nil
			}
			return nil, nil
		}
		return gh
	}
	closed := `{"number":10,"state":"closed","merged_at":null,"draft":false,"base":{"ref":"main"},"head":{"ref":"release/1.0","sha":"tip","repo":{"full_name":"myorg/goliac"}},"stack":null}`

	t.Run("control: the newest PR is closed unmerged with the tip unchanged: refused", func(t *testing.T) {
		gh := newest(closed)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 11, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "dead branch")
	})
	t.Run("the newest PR is open (state not closed): not dead", func(t *testing.T) {
		gh := newest(strings.Replace(closed, `"state":"closed"`, `"state":"open"`, 1))
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 11, "because", "")
		assert.Nil(t, err)
	})
	t.Run("the newest PR listed has another head branch than the base (a filter mismatch): not dead", func(t *testing.T) {
		gh := newest(strings.Replace(closed, `"ref":"release/1.0"`, `"ref":"another"`, 1))
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 11, "because", "")
		assert.Nil(t, err)
	})
}

func TestForcemergeAsyncExplicitSquashIsNeverReplaced(t *testing.T) {
	gh := newFake(t, fixtureStack()).allMethods()
	gh.asyncMerge = func(call restCall) ([]byte, error) {
		return httpError("405 Method Not Allowed", `{"message":"Merge commits are not allowed on this repository."}`)
	}
	fc, _ := newForcemergeForTest(t, gh)
	err := fc.mergePRAsync(context.Background(), "goliac", 4, "sha4", "t", "m", "merge", true, true)
	assert.NotNil(t, err)
	assert.Equal(t, 1, len(gh.find("PUT", "/merge-async")), "explicit: no squash attempt")
}

func TestForcemergeModeIsCaseInsensitive(t *testing.T) {
	for _, mode := range []string{"STACK", "Stack", " stack "} {
		gh := newFake(t, fixtureStackPlain()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		fc := executeFixture(t, gh, &recordingPlugin{})
		_, err := exec(fc, 3, mode, false)
		assert.Nil(t, err, mode)
		assert.Equal(t, 4, len(gh.find("PUT", "/merge-async")), mode)
	}
	for _, mode := range []string{"DIRECT", "Direct"} {
		gh := newFake(t, fixtureStackPlain()).allMethods()
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.markMergedOnBranchMerge = true
		fc := executeFixture(t, gh, &recordingPlugin{})
		_, err := exec(fc, 3, mode, false)
		assert.Nil(t, err, mode)
	}
}

func TestForcemergeStrayClassification(t *testing.T) {
	t.Run("a PR above with GitHub stack metadata is left untouched and named", func(t *testing.T) {
		prs := fixturePlainChain()
		prs[60] = &fakePR{Number: 60, Title: "native", BaseRef: "t2", HeadRef: "native", HeadSha: "sha60", State: "open", StackSize: 2, StackPosition: 1, StackBase: "main"}
		gh := newFake(t, prs).allMethods()
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.markMergedOnBranchMerge = true
		fc, _ := newForcemergeForTest(t, gh)
		summary, err := mergePR(fc, 58, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, "t2", gh.pr(60).BaseRef)
		assert.Equal(t, "t1", gh.pr(59).BaseRef)
		assert.Contains(t, summary, "#60 (has GitHub stack metadata)")
	})

	t.Run("a PR above that is not open any more (race) is neither retargeted nor named", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.markMergedOnBranchMerge = true
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && call.Endpoint == "/repos/myorg/goliac/pulls" && strings.Contains(call.Parameters, "base=t2") && strings.Contains(call.Parameters, "page=1") {
				return []byte(`[{"number":59,"title":"t3","state":"open","draft":false,"base":{"ref":"t2"},"head":{"ref":"t3","sha":"sha59","repo":{"full_name":"myorg/goliac"}},"stack":null},` +
					`{"number":77,"title":"gone","state":"closed","draft":false,"base":{"ref":"t2"},"head":{"ref":"gone","sha":"sha77","repo":{"full_name":"myorg/goliac"}},"stack":null}]`), nil
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		summary, err := mergePR(fc, 58, "because", "")
		assert.Nil(t, err)
		assert.NotContains(t, summary, "#77")
		assert.Empty(t, gh.find("PATCH", "/pulls/77"))
	})

	t.Run("a lone PR on a release branch with only a draft above is merged as a plain PR and the draft is named", func(t *testing.T) {
		prs := map[int]*fakePR{
			32: {Number: 32, Title: "hotfix", Author: "a32", BaseRef: "release/1.0", HeadRef: "hotfix", HeadSha: "aaa", State: "open"},
			33: {Number: 33, Title: "wip", BaseRef: "hotfix", HeadRef: "wip", HeadSha: "bbb", State: "open", Draft: true},
		}
		gh := newFake(t, prs).allMethods()
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)
		summary, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, 1, len(gh.find("PUT", "/pulls/32/merge")))
		assert.Contains(t, summary, "#33 (draft)")
	})
}
