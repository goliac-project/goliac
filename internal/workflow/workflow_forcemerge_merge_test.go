package workflow

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func mergePR(fc *ForcemergeImpl, n int, explanation, mode string) (string, error) {
	plan, err := fc.planMerge(context.Background(), "test-user", "goliac", strconv.Itoa(n), prURL(n), explanation, mode)
	if err != nil {
		return "", err
	}
	return fc.executePlan(context.Background(), plan)
}

func TestForcemergeMergePRNotStacked(t *testing.T) {
	stackedRefusal := `{"message":"Merging stacked PRs via this endpoint is not supported. Use the asynchronous merge endpoint instead.","status":"403"}`

	for _, mode := range []string{"", MergeModeDirect, MergeModeStack} {
		t.Run(fmt.Sprintf("synchronous merge, mode %q", mode), func(t *testing.T) {
			gh := newFake(t, fixturePlainPR())
			gh.syncMerge = okJSON(`{"merged":true}`)
			fc, _ := newForcemergeForTest(t, gh)

			_, err := mergePR(fc, 32, "because", mode)
			assert.Nil(t, err)

			reviews := gh.find("POST", "/pulls/32/reviews")
			assert.Equal(t, 1, len(reviews))
			assert.Equal(t, "APPROVE", reviews[0].Body["event"])
			assert.Equal(t, "aaa111", reviews[0].Body["commit_id"])
			expectedReview := "Force merge via Goliac on behalf of test-user.\n\nPR #32: Plain PR\nhttps://github.com/myorg/goliac/pull/32\n\nbecause"
			if mode != "" {
				expectedReview += fmt.Sprintf("\n\nMerge mode `%s` ignored: this PR is not part of a stack.", mode)
			} else {
				expectedReview += "\n\nMerge mode: none (this PR is not part of a stack)."
			}
			assert.Equal(t, expectedReview, reviews[0].Body["body"])

			merges := gh.find("PUT", "/pulls/32/merge")
			assert.Equal(t, 1, len(merges))
			assert.Equal(t, "merge", merges[0].Body["merge_method"])
			assert.Equal(t, "Plain PR (force merged PR 32 by test-user)", merges[0].Body["commit_title"])
			assert.Equal(t, "Force merge via Goliac on behalf of test-user.\n\nPR: https://github.com/myorg/goliac/pull/32\nTitle: Plain PR", merges[0].Body["commit_message"])
			assert.Equal(t, 0, len(gh.find("PUT", "/merge-async")))
			assert.Equal(t, 0, len(gh.find("POST", "/merges")))
			assert.Equal(t, 0, len(gh.find("PATCH", "")))
		})
	}

	t.Run("/squash", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 32, "because /squash", "")
		assert.Nil(t, err)
		merges := gh.find("PUT", "/pulls/32/merge")
		assert.Equal(t, 1, len(merges))
		assert.Equal(t, "squash", merges[0].Body["merge_method"])
		assert.NotContains(t, gh.find("POST", "/reviews")[0].Body["body"], "/squash")
	})

	t.Run("squash fallback on Method Not Allowed", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		gh.syncMerge = func(call restCall) ([]byte, error) {
			if call.Body["merge_method"] == "merge" {
				return httpError("405 Method Not Allowed", `{"message":"Merge commits are not allowed on this repository."}`)
			}
			return []byte(`{"merged":true}`), nil
		}
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		merges := gh.find("PUT", "/pulls/32/merge")
		assert.Equal(t, 2, len(merges))
		assert.Equal(t, "merge", merges[0].Body["merge_method"])
		assert.Equal(t, "squash", merges[1].Body["merge_method"])
	})

	t.Run("squash-only repository: squash chosen up front", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR()).squashOnly()
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		merges := gh.find("PUT", "/pulls/32/merge")
		assert.Equal(t, 1, len(merges))
		assert.Equal(t, "squash", merges[0].Body["merge_method"])
	})

	t.Run("stacked refusal without stack info merges nothing, approval dismissed", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		gh.syncMerge = func(call restCall) ([]byte, error) { return httpError("403 Forbidden", stackedRefusal) }
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "no stack information")
		assert.Contains(t, err.Error(), "were dismissed")
		assert.Equal(t, 1, len(gh.find("PUT", "/pulls/32/reviews/1001/dismissals")))
		assert.Equal(t, 0, len(gh.find("PUT", "/merge-async")))
		assert.Equal(t, 0, len(gh.find("POST", "/merges")))
	})

	t.Run("merge failure: approval dismissal fails, the approval is listed", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		gh.syncMerge = func(call restCall) ([]byte, error) {
			return httpError("409 Conflict", `{"message":"Head branch was modified"}`)
		}
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PUT" && strings.HasSuffix(call.Endpoint, "/dismissals") {
				return httpError("403 Forbidden", `{"message":"no"}`)
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "dismiss them manually: PR #32 (review 1001)")
	})

	t.Run("GET failure on a plain PR fails closed: no approval", func(t *testing.T) {
		gh := newFake(t, map[int]*fakePR{})
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "error loading pull request")
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("bad JSON for a plain PR fails closed", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && strings.HasSuffix(call.Endpoint, "/pulls/32") {
				return []byte(`not json`), nil
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "error parsing pull request")
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	refusals := map[string]func(p *fakePR){
		"closed":                 func(p *fakePR) { p.State = "closed" },
		"merged":                 func(p *fakePR) { p.Merged = true },
		"draft":                  func(p *fakePR) { p.Draft = true },
		"fork":                   func(p *fakePR) { p.HeadRepo = "someone/goliac" },
		"deleted fork (no head)": func(p *fakePR) { p.HeadRepo = "-" },
	}
	for name, mutate := range refusals {
		t.Run("refused before any side effect: "+name, func(t *testing.T) {
			prs := fixturePlainPR()
			mutate(prs[32])
			gh := newFake(t, prs)
			gh.syncMerge = okJSON(`{"merged":true}`)
			fc, _ := newForcemergeForTest(t, gh)

			_, err := mergePR(fc, 32, "because", "")
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "nothing merged")
			assert.Equal(t, 0, len(gh.sideEffects()))
		})
	}

	t.Run("explicit /squash on a repository that forbids squash: nothing approved", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		gh.settings = &struct{ Merge, Squash, Rebase *bool }{boolp(true), boolp(false), boolp(true)}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because /squash", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "does not allow the squash merge method")
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("repository allowing nothing usable: nothing approved", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		gh.settings = &struct{ Merge, Squash, Rebase *bool }{boolp(false), boolp(false), boolp(false)}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "allows no merge method")
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("rebase-only repository: rebase chosen", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		gh.settings = &struct{ Merge, Squash, Rebase *bool }{boolp(false), boolp(false), boolp(true)}
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, "rebase", gh.find("PUT", "/pulls/32/merge")[0].Body["merge_method"])
	})

	t.Run("settings without any allow_* field are treated as unknown", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		gh.settingsRaw = `{"name":"goliac"}`
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, "merge", gh.find("PUT", "/pulls/32/merge")[0].Body["merge_method"])
	})

	t.Run("settings with bad JSON are treated as unknown", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		gh.settingsRaw = `nope`
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
	})

	t.Run("the URL number is used when the payload has none", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR())
		gh.syncMerge = okJSON(`{"merged":true}`)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && strings.HasSuffix(call.Endpoint, "/pulls/32") {
				return []byte(`{"title":"t","state":"open"}`), nil
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, 1, len(gh.find("POST", "/pulls/32/reviews")))
	})
}

func TestForcemergeMergePRDirect(t *testing.T) {
	for _, mode := range []string{"", MergeModeDirect} {
		t.Run(fmt.Sprintf("mid-stack PR 3, mode %q: merged into 4's branch, PR 2 restacked onto it", mode), func(t *testing.T) {
			gh := newFake(t, fixtureStack()).allMethods()
			gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
			gh.markMergedOnBranchMerge = true
			fc, _ := newForcemergeForTest(t, gh)

			summary, err := mergePR(fc, 3, "because", mode)
			assert.Nil(t, err)
			assert.Contains(t, summary, "#3 PR 3 (by author3) into `b4`")
			assert.Contains(t, summary, "retargeted onto `b4`: #2")

			merges := gh.find("POST", "/repos/myorg/goliac/merges")
			assert.Equal(t, 1, len(merges))
			assert.Equal(t, "b4", merges[0].Body["base"])
			assert.Equal(t, "sha3", merges[0].Body["head"])
			assert.Equal(t, "PR 3 (force merged PR 3 by test-user)\n\nForce merge via Goliac on behalf of test-user.\n\nPR: https://github.com/myorg/goliac/pull/3\nTitle: PR 3", merges[0].Body["commit_message"])

			assert.Equal(t, []string{"3"}, gh.reviewedPRs())
			review := gh.find("POST", "/reviews")[0]
			assert.Equal(t, "sha3", review.Body["commit_id"])
			body := review.Body["body"].(string)
			assert.True(t, strings.HasPrefix(body, "Force merge via Goliac on behalf of test-user.\n\nPR #3: PR 3\nhttps://github.com/myorg/goliac/pull/3\n\nbecause"))
			assert.Contains(t, body, "Merge mode: `direct`")
			assert.Contains(t, body, "merged alone into its base branch `b4`; the PRs based on `b3` are retargeted to `b4`.")
			if mode == "" {
				assert.Contains(t, body, "`direct` (default)")
			} else {
				assert.NotContains(t, body, "(default)")
			}

			// restack: 1 > 2 > 4 > main
			patches := gh.find("PATCH", "/pulls/2")
			assert.Equal(t, 1, len(patches))
			assert.Equal(t, "b4", patches[0].Body["base"])
			assert.Equal(t, "b4", gh.pr(2).BaseRef)
			assert.Equal(t, "b2", gh.pr(1).BaseRef)
			assert.Equal(t, "main", gh.pr(4).BaseRef)

			// GitHub marked it merged: no close, no comment
			assert.True(t, gh.pr(3).Merged)
			assert.Equal(t, 0, len(gh.find("PATCH", "/pulls/3")))
			assert.Equal(t, 0, len(gh.find("POST", "/comments")))
			assert.Equal(t, 0, len(gh.find("PUT", "/merge-async")))
			assert.Equal(t, 0, len(gh.find("PUT", "/merge")))
		})
	}

	t.Run("mid-stack PR 3 on a squash-only repository: a merge commit into the parent branch, no method needed", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).squashOnly()
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.markMergedOnBranchMerge = true
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 3, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, 1, len(gh.find("POST", "/repos/myorg/goliac/merges")))
		assert.Equal(t, "b4", gh.pr(2).BaseRef)
	})

	t.Run("mid-stack PR 3 does not even read the repository settings", func(t *testing.T) {
		gh := newFake(t, fixtureStack()) // no settings: GET /repos/myorg/goliac would fail
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.markMergedOnBranchMerge = true
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", "")
		assert.Nil(t, err)
	})

	t.Run("mid-stack PR 3 not marked merged by GitHub: closed with a comment", func(t *testing.T) {
		gh := newFake(t, fixtureStack())
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		fc, clock := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 3, "because", MergeModeDirect)
		assert.Nil(t, err)
		assert.NotEmpty(t, clock.slept(), "polled with the fake clock")

		comments := gh.find("POST", "/issues/3/comments")
		assert.Equal(t, 1, len(comments))
		assert.Equal(t, "Force merged via Goliac on behalf of test-user into its base branch `b4` (merge commit mergecommit). GitHub did not mark this PR as merged, so Goliac closes it.", comments[0].Body["body"])
		closes := gh.find("PATCH", "/pulls/3")
		assert.Equal(t, 1, len(closes))
		assert.Equal(t, "closed", closes[0].Body["state"])
		assert.Equal(t, "closed", gh.pr(3).State)
		// restack still done
		assert.Equal(t, "b4", gh.pr(2).BaseRef)
	})

	t.Run("mid-stack PR 3 with /squash: merge commit, noted in the review", func(t *testing.T) {
		gh := newFake(t, fixtureStack())
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.markMergedOnBranchMerge = true
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 3, "because /squash", "")
		assert.Nil(t, err)
		assert.Equal(t, 1, len(gh.find("POST", "/repos/myorg/goliac/merges")))
		review := gh.find("POST", "/reviews")[0].Body["body"].(string)
		assert.Contains(t, review, "/squash is not available when merging into a parent branch: a merge commit is used instead.")
		assert.NotContains(t, review, "because /squash")
	})

	t.Run("mid-stack PR 3: 204 nothing to merge is handled", func(t *testing.T) {
		gh := newFake(t, fixtureStack())
		gh.branchMerge = func(call restCall) ([]byte, error) { return []byte{}, nil }
		gh.markMergedOnBranchMerge = true
		fc, _ := newForcemergeForTest(t, gh)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "POST" && call.Endpoint == "/repos/myorg/goliac/merges" {
				f.update(3, func(p *fakePR) { p.Merged, p.State = true, "closed" })
			}
			return nil, nil
		}
		_, err := mergePR(fc, 3, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, "b4", gh.pr(2).BaseRef)
		assert.Equal(t, 0, len(gh.find("POST", "/comments")))
	})

	t.Run("mid-stack PR 3: merge answer that is not JSON is indeterminate, approvals kept", func(t *testing.T) {
		gh := newFake(t, fixtureStack())
		gh.branchMerge = okJSON(`<html>`)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "the merge may have happened")
		assert.Contains(t, err.Error(), "left in place: PR #3 (review 1001)")
		assert.Equal(t, 0, len(gh.find("PUT", "/dismissals")))
		assert.Equal(t, 0, len(gh.find("PATCH", "")))
	})

	t.Run("bottom PR 4: merged alone with merge-async, PR 3 restacked onto main only after merged:true", func(t *testing.T) {
		polls := 0
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = okJSON(asyncPending)
		gh.asyncPoll = func(call restCall) ([]byte, error) {
			polls++
			if polls < 3 {
				return []byte(asyncPending), nil
			}
			return []byte(asyncMerged), nil
		}
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 4, "because /squash", MergeModeDirect)
		assert.Nil(t, err)
		assert.Equal(t, 3, polls)

		asyncCalls := gh.find("PUT", "/pulls/4/merge-async")
		assert.Equal(t, 1, len(asyncCalls))
		assert.Equal(t, "squash", asyncCalls[0].Body["merge_method"])
		assert.Equal(t, "direct_merge", asyncCalls[0].Body["merge_action"])
		assert.Equal(t, true, asyncCalls[0].Body["bypass_rules"])
		assert.Equal(t, "sha4", asyncCalls[0].Body["sha"])
		assert.Equal(t, "PR 4 (force merged PR 4 by test-user)", asyncCalls[0].Body["commit_title"])
		assert.Equal(t, "Force merge via Goliac on behalf of test-user.\n\nPR: https://github.com/myorg/goliac/pull/4\nTitle: PR 4", asyncCalls[0].Body["commit_message"])

		assert.Equal(t, []string{"4"}, gh.reviewedPRs())
		assert.NotContains(t, gh.find("POST", "/reviews")[0].Body["body"], "merge commit is used instead")
		assert.Equal(t, "main", gh.pr(3).BaseRef)
		assert.Equal(t, 1, len(gh.find("PATCH", "/pulls/3")))
		assert.Equal(t, 0, len(gh.find("POST", "/merges")))
		assert.Equal(t, 0, len(gh.find("PUT", "/pulls/4/merge")))
		assert.Equal(t, 0, len(gh.find("POST", "/comments")))
	})

	t.Run("bottom PR 4 on a squash-only repository: squash used (no merge commit asked)", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).squashOnly()
		gh.asyncMerge = okJSON(asyncMerged)
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 4, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, "squash", gh.find("PUT", "/merge-async")[0].Body["merge_method"])
		assert.Equal(t, "main", gh.pr(3).BaseRef)
	})

	t.Run("bottom PR 4: settings unreadable fails closed before the approval", func(t *testing.T) {
		gh := newFake(t, fixtureStack())
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 4, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "nothing merged")
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("bottom PR 4: merge-async says enqueued: not success, nothing retargeted, approval kept", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = okJSON(`{"status":"enqueued","details":{"message":"added to the merge queue","uuid":"uuid-9"}}`)
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 4, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "queued in a merge queue, not merged")
		assert.Contains(t, err.Error(), "nothing was retargeted")
		assert.Equal(t, 0, len(gh.find("PATCH", "")))
		assert.Equal(t, "b4", gh.pr(3).BaseRef)
		assert.Equal(t, 0, len(gh.find("PUT", "/dismissals")))
	})

	t.Run("bottom PR 4: merge-async says merged but GET says not merged: nothing retargeted", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		gh.neverMarkMerged = true
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 4, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "is not merged after")
		assert.Contains(t, err.Error(), "nothing was retargeted")
		assert.Equal(t, 0, len(gh.find("PATCH", "")))
	})

	t.Run("bottom PR 4: merged but the confirmation GET fails: nothing retargeted", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		calls := 0
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && strings.HasSuffix(call.Endpoint, "/pulls/4") {
				calls++
				if calls > 2 { // 1st GET: the plan, 2nd: the head re-check
					return httpError("502 Bad Gateway", `{}`)
				}
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 4, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "could not be read")
		assert.Equal(t, 0, len(gh.find("PATCH", "")))
	})

	t.Run("top PR 1: merged into 2's branch, nothing to restack", func(t *testing.T) {
		gh := newFake(t, fixtureStack())
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.markMergedOnBranchMerge = true
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 1, "because", "")
		assert.Nil(t, err)
		merges := gh.find("POST", "/repos/myorg/goliac/merges")
		assert.Equal(t, 1, len(merges))
		assert.Equal(t, "b2", merges[0].Body["base"])
		assert.Equal(t, "sha1", merges[0].Body["head"])
		assert.Equal(t, 0, len(gh.find("PATCH", "")))
		assert.Equal(t, "b3", gh.pr(2).BaseRef)
	})

	t.Run("mid-stack PR 3: merge conflict with its base branch, nothing restacked, approval dismissed", func(t *testing.T) {
		gh := newFake(t, fixtureStack())
		gh.branchMerge = func(call restCall) ([]byte, error) { return httpError("409 Conflict", `{"message":"Merge conflict"}`) }
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "merge conflict")
		assert.Contains(t, err.Error(), "b4")
		assert.Contains(t, err.Error(), "were dismissed")
		assert.Equal(t, 1, len(gh.find("PUT", "/pulls/3/reviews/1001/dismissals")))
		assert.Equal(t, 0, len(gh.find("PATCH", "")))
		assert.Equal(t, "b3", gh.pr(2).BaseRef)
	})

	t.Run("mid-stack PR 3: other branch merge error", func(t *testing.T) {
		gh := newFake(t, fixtureStack())
		gh.branchMerge = func(call restCall) ([]byte, error) {
			return httpError("404 Not Found", `{"message":"Base does not exist"}`)
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err)
		assert.NotContains(t, err.Error(), "merge conflict")
		assert.Contains(t, err.Error(), "error merging the PR into its base branch b4")
	})

	t.Run("bottom PR 4: asynchronous merge fails, nothing restacked", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = okJSON(asyncPending)
		gh.asyncPoll = okJSON(`{"status":"failed","details":{"message":"Merge conflict between base and head"}}`)
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 4, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "GitHub refused the asynchronous merge (request uuid-1): Merge conflict between base and head")
		assert.Equal(t, 0, len(gh.find("PATCH", "")))
		assert.Equal(t, 1, len(gh.find("PUT", "/dismissals")))
	})

	t.Run("direct merge: the PR below is not in the stack: refused before any side effect", func(t *testing.T) {
		prs := fixtureStack()
		delete(prs, 4)
		gh := newFake(t, prs)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "no open PR of the same stack has this head branch")
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("direct merge: the PR below is a draft or a fork or has no stack metadata", func(t *testing.T) {
		for name, mutate := range map[string]func(p *fakePR){
			"draft":       func(p *fakePR) { p.Draft = true },
			"fork":        func(p *fakePR) { p.HeadRepo = "other/goliac" },
			"no stack":    func(p *fakePR) { p.StackSize = 0 },
			"other stack": func(p *fakePR) { p.StackBase = "develop" },
		} {
			prs := fixtureStack()
			mutate(prs[4])
			gh := newFake(t, prs)
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, 3, "because", "")
			assert.NotNil(t, err, name)
			assert.Equal(t, 0, len(gh.sideEffects()), name)
		}
	})

	t.Run("direct merge: two PRs below with the same head: ambiguous", func(t *testing.T) {
		prs := fixtureStack()
		prs[9] = &fakePR{Number: 9, Title: "dup", BaseRef: "main", HeadRef: "b4", HeadSha: "sha9", State: "open", StackPosition: 1, StackSize: 4, StackBase: "main"}
		gh := newFake(t, prs)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "ambiguous")
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("direct merge: a listing failure refuses before any side effect", func(t *testing.T) {
		gh := newFake(t, fixtureStack())
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && call.Endpoint == "/repos/myorg/goliac/pulls" {
				return httpError("500 Internal Server Error", `{}`)
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err)
		_, err = mergePR(fc, 4, "because", "")
		assert.NotNil(t, err)
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("direct merge: a stack PR that is closed or draft or malformed is refused", func(t *testing.T) {
		for name, mutate := range map[string]func(p *fakePR){
			"closed": func(p *fakePR) { p.State = "closed" },
			"merged": func(p *fakePR) { p.Merged = true },
			"draft":  func(p *fakePR) { p.Draft = true },
			"fork":   func(p *fakePR) { p.HeadRepo = "other/goliac" },
			"size 0": func(p *fakePR) {
				p.StackSize = 0
				p.RawStack = map[string]interface{}{"size": 0, "base": map[string]interface{}{"ref": "main"}}
			},
			"no base": func(p *fakePR) {
				p.RawStack = map[string]interface{}{"size": 4, "position": 2, "base": map[string]interface{}{}}
			},
			"state odd": func(p *fakePR) { p.State = "" },
		} {
			prs := fixtureStack()
			mutate(prs[3])
			gh := newFake(t, prs)
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, 3, "because", "")
			assert.NotNil(t, err, name)
			assert.Equal(t, 0, len(gh.sideEffects()), name)
		}
	})

	t.Run("malformed stack payload (a string) fails closed", func(t *testing.T) {
		prs := fixtureStack()
		prs[3].RawStack = "oops"
		gh := newFake(t, prs)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "error parsing pull request")
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("head pushed after the approval: nothing merged, approval dismissed", func(t *testing.T) {
		gh := newFake(t, fixtureStack())
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "POST" && strings.HasSuffix(call.Endpoint, "/reviews") {
				f.update(3, func(p *fakePR) { p.HeadSha = "pushed" })
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "was pushed to after the approval")
		assert.Equal(t, 0, len(gh.find("POST", "/merges")))
		assert.Equal(t, 1, len(gh.find("PUT", "/dismissals")))
	})

	t.Run("PR closed or unreadable at the re-check: nothing merged", func(t *testing.T) {
		for name, fn := range map[string]func(f *fakeGitHub, call restCall) ([]byte, error){
			"closed": func(f *fakeGitHub, call restCall) ([]byte, error) {
				if call.Method == "POST" && strings.HasSuffix(call.Endpoint, "/reviews") {
					f.update(3, func(p *fakePR) { p.State = "closed" })
				}
				return nil, nil
			},
			"unreadable": func(f *fakeGitHub, call restCall) ([]byte, error) {
				if call.Method == "GET" && strings.HasSuffix(call.Endpoint, "/pulls/3") && len(f.find("POST", "/reviews")) > 0 {
					return httpError("500 Internal Server Error", `{}`)
				}
				return nil, nil
			},
		} {
			gh := newFake(t, fixtureStack())
			gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
			gh.onCall = fn
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, 3, "because", "")
			assert.NotNil(t, err, name)
			assert.Equal(t, 0, len(gh.find("POST", "/merges")), name)
		}
	})

	t.Run("an approval error stops before the merge", func(t *testing.T) {
		gh := newFake(t, fixtureStack())
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "POST" && strings.HasSuffix(call.Endpoint, "/reviews") {
				return httpError("422 Unprocessable Entity", `{"message":"cannot approve your own PR"}`)
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "error approving the PR #3")
		assert.Equal(t, 0, len(gh.find("POST", "/merges")))
	})
}

func TestForcemergeRestack(t *testing.T) {
	setup := func(t *testing.T) (*fakeGitHub, *ForcemergeImpl, *fakeClock) {
		gh := newFake(t, fixtureStack())
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.markMergedOnBranchMerge = true
		fc, clock := newForcemergeForTest(t, gh)
		return gh, fc, clock
	}

	t.Run("a transient PATCH failure is retried with a doubling backoff", func(t *testing.T) {
		gh, fc, clock := setup(t)
		failures := 2
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PATCH" && failures > 0 {
				failures--
				return httpError("502 Bad Gateway", `{}`)
			}
			return nil, nil
		}
		_, err := mergePR(fc, 3, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, "b4", gh.pr(2).BaseRef)
		assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, clock.slept())
	})

	t.Run("a persistent PATCH failure names the PRs and the manual fix", func(t *testing.T) {
		gh, fc, clock := setup(t)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PATCH" {
				return httpError("422 Unprocessable Entity", `{"message":"nope"}`)
			}
			return nil, nil
		}
		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "PR #3 is merged")
		assert.Contains(t, err.Error(), "PR #2")
		assert.Contains(t, err.Error(), "Fix manually: change the base branch")
		assert.Equal(t, "b3", gh.pr(2).BaseRef)
		// a 4xx refusal is not retried
		assert.Contains(t, err.Error(), "not retried")
		assert.Empty(t, clock.slept())
		assert.Equal(t, 1, len(gh.find("PATCH", "/pulls/2")))
	})

	t.Run("a persistent 5xx is retried with backoff then reported", func(t *testing.T) {
		gh, fc, clock := setup(t)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PATCH" {
				return httpError("503 Service Unavailable", `{}`)
			}
			return nil, nil
		}
		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "Fix manually")
		assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}, clock.slept())
		assert.Equal(t, 4, len(gh.find("PATCH", "/pulls/2")))
	})

	t.Run("partial failure: the other PRs are still retargeted", func(t *testing.T) {
		prs := fixtureStack()
		// a second PR of the stack above 3 (siblings): both are retargeted
		prs[7] = &fakePR{Number: 7, Title: "sibling", BaseRef: "b3", HeadRef: "b7", HeadSha: "sha7", State: "open", StackPosition: 2, StackSize: 4, StackBase: "main"}
		gh := newFake(t, prs)
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.markMergedOnBranchMerge = true
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PATCH" && strings.HasSuffix(call.Endpoint, "/pulls/2") {
				return httpError("500 Internal Server Error", `{}`)
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "PR #2")
		assert.NotContains(t, err.Error(), "PR #7:")
		assert.Equal(t, "b4", gh.pr(7).BaseRef)
		assert.Equal(t, "b3", gh.pr(2).BaseRef)
	})

	t.Run("only PRs with matching stack metadata are retargeted", func(t *testing.T) {
		prs := fixtureStack()
		prs[8] = &fakePR{Number: 8, Title: "unrelated", BaseRef: "b3", HeadRef: "b8", HeadSha: "sha8", State: "open"}                                                     // no stack
		prs[9] = &fakePR{Number: 9, Title: "other stack", BaseRef: "b3", HeadRef: "b9", HeadSha: "sha9", State: "open", StackSize: 2, StackPosition: 1, StackBase: "dev"} // other stack
		prs[10] = &fakePR{Number: 10, Title: "fork", BaseRef: "b3", HeadRef: "b10", HeadSha: "sha10", State: "open", HeadRepo: "x/goliac", StackSize: 4, StackPosition: 1, StackBase: "main"}
		gh := newFake(t, prs)
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.markMergedOnBranchMerge = true
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, "b4", gh.pr(2).BaseRef)
		for _, n := range []int{8, 9, 10} {
			assert.Equal(t, "b3", gh.pr(n).BaseRef)
		}
		assert.Equal(t, 1, len(gh.find("PATCH", "")))
	})

	t.Run("a PR changed meanwhile is skipped or reported", func(t *testing.T) {
		gh, fc, _ := setup(t)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "POST" && call.Endpoint == "/repos/myorg/goliac/merges" {
				f.update(2, func(p *fakePR) { p.BaseRef = "elsewhere" })
			}
			return nil, nil
		}
		_, err := mergePR(fc, 3, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "it is now based on elsewhere")
		assert.Equal(t, 0, len(gh.find("PATCH", "/pulls/2")))
	})

	t.Run("a child already retargeted or closed is fine", func(t *testing.T) {
		for name, mutate := range map[string]func(p *fakePR){
			"already": func(p *fakePR) { p.BaseRef = "b4" },
			"closed":  func(p *fakePR) { p.State = "closed" },
			"merged":  func(p *fakePR) { p.Merged, p.State = true, "closed" },
		} {
			gh, fc, _ := setup(t)
			gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
				if call.Method == "POST" && call.Endpoint == "/repos/myorg/goliac/merges" {
					f.update(2, mutate)
				}
				return nil, nil
			}
			_, err := mergePR(fc, 3, "because", "")
			assert.Nil(t, err, name)
			assert.Equal(t, 0, len(gh.find("PATCH", "/pulls/2")), name)
		}
	})

	t.Run("context cancelled while backing off", func(t *testing.T) {
		gh, fc, _ := setup(t)
		ctx, cancel := context.WithCancel(context.Background())
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PATCH" {
				cancel()
				return httpError("502 Bad Gateway", `{}`)
			}
			return nil, nil
		}
		plan, err := fc.planMerge(ctx, "test-user", "goliac", "3", prURL(3), "because", "")
		assert.Nil(t, err)
		_, err = fc.executePlan(ctx, plan)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})

	t.Run("GET failing during the restack is retried", func(t *testing.T) {
		gh, fc, _ := setup(t)
		failures := 0
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && strings.HasSuffix(call.Endpoint, "/pulls/2") && len(f.find("POST", "/merges")) > 0 && failures < 1 {
				failures++
				return httpError("502 Bad Gateway", `{}`)
			}
			return nil, nil
		}
		_, err := mergePR(fc, 3, "because", "")
		assert.Nil(t, err)
		assert.Equal(t, "b4", gh.pr(2).BaseRef)
	})
}

func TestForcemergeEnsureMerged(t *testing.T) {
	// mid-stack PR 3, GitHub does not mark it merged
	run := func(t *testing.T, gh *fakeGitHub) error {
		gh.prs = fixtureStack()
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "because", "")
		return err
	}

	t.Run("every state poll fails: the PR is not closed, an error is returned", func(t *testing.T) {
		gh := newFake(t, nil)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && strings.HasSuffix(call.Endpoint, "/pulls/3") && len(f.find("POST", "/merges")) > 0 {
				return httpError("500 Internal Server Error", `{}`)
			}
			return nil, nil
		}
		err := run(t, gh)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "its state could not be read")
		assert.Contains(t, err.Error(), "it was not closed")
		assert.Equal(t, 0, len(gh.find("PATCH", "/pulls/3")))
		assert.Equal(t, 0, len(gh.find("POST", "/comments")))
		assert.Equal(t, "b4", gh.pr(2).BaseRef) // the restack still happened (the merge commit landed)
	})

	t.Run("GitHub marks it merged just before the close: never closed", func(t *testing.T) {
		gh := newFake(t, nil)
		polls := 0
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && strings.HasSuffix(call.Endpoint, "/pulls/3") && len(f.find("POST", "/merges")) > 0 {
				polls++
				if polls == 6 { // 5s timeout / 1s interval: 5 polls, then the last look
					f.update(3, func(p *fakePR) { p.Merged, p.State = true, "closed" })
				}
			}
			return nil, nil
		}
		err := run(t, gh)
		assert.Nil(t, err)
		assert.Equal(t, 0, len(gh.find("PATCH", "/pulls/3")))
		assert.Equal(t, 0, len(gh.find("POST", "/comments")))
	})

	t.Run("the last look fails: not closed", func(t *testing.T) {
		gh := newFake(t, nil)
		polls := 0
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && strings.HasSuffix(call.Endpoint, "/pulls/3") && len(f.find("POST", "/merges")) > 0 {
				polls++
				if polls > 5 { // the last look
					return httpError("500 Internal Server Error", `{}`)
				}
			}
			return nil, nil
		}
		err := run(t, gh)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "before closing it")
		assert.Equal(t, 0, len(gh.find("PATCH", "/pulls/3")))
	})

	t.Run("closing answers 422 but the PR is merged now: success", func(t *testing.T) {
		gh := newFake(t, nil)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PATCH" && strings.HasSuffix(call.Endpoint, "/pulls/3") {
				f.update(3, func(p *fakePR) { p.Merged, p.State = true, "closed" })
				return httpError("422 Unprocessable Entity", `{"message":"already merged"}`)
			}
			return nil, nil
		}
		assert.Nil(t, run(t, gh))
	})

	t.Run("closing fails and the PR is not merged: error", func(t *testing.T) {
		gh := newFake(t, nil)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PATCH" && strings.HasSuffix(call.Endpoint, "/pulls/3") {
				return httpError("422 Unprocessable Entity", `{"message":"nope"}`)
			}
			return nil, nil
		}
		err := run(t, gh)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "closing it failed")
	})

	t.Run("commenting fails: not closed", func(t *testing.T) {
		gh := newFake(t, nil)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "POST" && strings.HasSuffix(call.Endpoint, "/comments") {
				return httpError("403 Forbidden", `{}`)
			}
			return nil, nil
		}
		err := run(t, gh)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "commenting before closing it failed")
		assert.Equal(t, 0, len(gh.find("PATCH", "/pulls/3")))
	})

	t.Run("closed by someone else meanwhile: nothing more to do", func(t *testing.T) {
		gh := newFake(t, nil)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "POST" && call.Endpoint == "/repos/myorg/goliac/merges" {
				f.update(3, func(p *fakePR) { p.State = "closed" })
			}
			return nil, nil
		}
		assert.Nil(t, run(t, gh))
		assert.Equal(t, 0, len(gh.find("POST", "/comments")))
	})

	t.Run("context cancelled while waiting", func(t *testing.T) {
		gh := newFake(t, fixtureStack())
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		fc, _ := newForcemergeForTest(t, gh)
		ctx, cancel := context.WithCancel(context.Background())
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "POST" && call.Endpoint == "/repos/myorg/goliac/merges" {
				cancel()
			}
			return nil, nil
		}
		plan, err := fc.planMerge(ctx, "test-user", "goliac", "3", prURL(3), "because", "")
		assert.Nil(t, err)
		_, err = fc.executePlan(ctx, plan)
		assert.NotNil(t, err)
	})
}

func TestForcemergeMergePRStack(t *testing.T) {
	for _, from := range []int{3, 1, 4} {
		t.Run(fmt.Sprintf("stack requested on PR %d: every PR approved, merge-async on the top PR 1", from), func(t *testing.T) {
			gh := newFake(t, fixtureStack()).allMethods()
			gh.asyncMerge = okJSON(asyncMerged)
			fc, _ := newForcemergeForTest(t, gh)

			summary, err := mergePR(fc, from, "ship it /squash", MergeModeStack)
			assert.Nil(t, err)
			assert.Contains(t, summary, "Force merged (stack) into `main`: #1 PR 1 (by author1), #2 PR 2 (by author2), #3 PR 3 (by author3), #4 PR 4 (by author4)")

			assert.Equal(t, []string{"1", "2", "3", "4"}, gh.reviewedPRs())
			for i, r := range gh.find("POST", "/reviews") {
				n := i + 1
				assert.Equal(t, "APPROVE", r.Body["event"])
				assert.Equal(t, fmt.Sprintf("sha%d", n), r.Body["commit_id"])
				assert.Equal(t,
					fmt.Sprintf("Force merge via Goliac on behalf of test-user.\n\nPR #%d: PR %d\nhttps://github.com/myorg/goliac/pull/%d\n\nship it \n\nMerge mode: `stack`. Merging the whole stack (4 PRs, #1 to #4) into `main`, requested on PR #%d. Stack source: GitHub stack metadata.", n, n, n, from),
					r.Body["body"])
			}

			asyncCalls := gh.find("PUT", "/merge-async")
			assert.Equal(t, 1, len(asyncCalls))
			assert.Equal(t, "/repos/myorg/goliac/pulls/1/merge-async", asyncCalls[0].Endpoint)
			assert.Equal(t, "sha1", asyncCalls[0].Body["sha"])
			assert.Equal(t, "squash", asyncCalls[0].Body["merge_method"])
			assert.Equal(t, "direct_merge", asyncCalls[0].Body["merge_action"])
			assert.Equal(t, true, asyncCalls[0].Body["bypass_rules"])
			assert.Equal(t, "PR 1 (force merged PR 1 by test-user)", asyncCalls[0].Body["commit_title"])
			msg := asyncCalls[0].Body["commit_message"].(string)
			assert.Contains(t, msg, fmt.Sprintf("requested on PR #%d", from))
			for n := 1; n <= 4; n++ {
				assert.Contains(t, msg, fmt.Sprintf("- #%d PR %d (by author%d) https://github.com/myorg/goliac/pull/%d", n, n, n, n))
			}

			assert.Equal(t, 0, len(gh.find("POST", "/merges")))
			assert.Equal(t, 0, len(gh.find("PATCH", "")))
			assert.Equal(t, 0, len(gh.find("PUT", "/merge")))
			assert.Equal(t, 0, len(gh.find("PUT", "/dismissals")))
		})
	}

	t.Run("squash-only repository: squash for the stack (default method replaced)", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).squashOnly()
		gh.asyncMerge = okJSON(asyncMerged)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.Nil(t, err)
		assert.Equal(t, "squash", gh.find("PUT", "/merge-async")[0].Body["merge_method"])
	})

	t.Run("async 'merge' refused as not allowed: squash fallback like the sync path", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods() // settings lie: the repository refuses merge commits
		gh.asyncMerge = func(call restCall) ([]byte, error) {
			if call.Body["merge_method"] == "merge" {
				return httpError("405 Method Not Allowed", `{"message":"Merge commits are not allowed on this repository."}`)
			}
			return []byte(asyncMerged), nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.Nil(t, err)
		calls := gh.find("PUT", "/merge-async")
		assert.Equal(t, 2, len(calls))
		assert.Equal(t, "squash", calls[1].Body["merge_method"])
	})

	t.Run("async 422 about the merge method: squash fallback; other 422: error", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = func(call restCall) ([]byte, error) {
			if call.Body["merge_method"] == "merge" {
				return httpError("422 Unprocessable Entity", `{"message":"Merge method merge is not allowed"}`)
			}
			return []byte(asyncMerged), nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.Nil(t, err)

		gh2 := newFake(t, fixtureStack()).allMethods()
		gh2.asyncMerge = func(call restCall) ([]byte, error) {
			return httpError("422 Unprocessable Entity", `{"message":"sha is stale"}`)
		}
		fc2, _ := newForcemergeForTest(t, gh2)
		_, err = mergePR(fc2, 3, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "validation failed")
		assert.Equal(t, 1, len(gh2.find("PUT", "/merge-async")))
	})

	t.Run("explicit /squash refused as not allowed: no fallback, error", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = func(call restCall) ([]byte, error) {
			return httpError("405 Method Not Allowed", `{"message":"Squash not allowed"}`)
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "ship it /squash", MergeModeStack)
		assert.NotNil(t, err)
		assert.Equal(t, 1, len(gh.find("PUT", "/merge-async")))
	})

	t.Run("a closed PR in the stack (above): nothing approved or merged", func(t *testing.T) {
		prs := fixtureStack()
		prs[2].State = "closed"
		gh := newFake(t, prs).allMethods()
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "nothing merged: GitHub reports a stack of 4 PRs but only 2 open PRs were found (#3 > #4)")
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("a missing PR in the stack (below): nothing approved or merged", func(t *testing.T) {
		prs := fixtureStack()
		delete(prs, 4)
		gh := newFake(t, prs).allMethods()
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 2, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "nothing merged: no open PR of the stack found for branch b4 below PR #3")
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	for name, mutate := range map[string]func(p *fakePR){
		"draft":          func(p *fakePR) { p.Draft = true },
		"fork head":      func(p *fakePR) { p.HeadRepo = "someone/goliac" },
		"state not open": func(p *fakePR) { p.State = "" },
		"merged":         func(p *fakePR) { p.Merged = true },
	} {
		for _, n := range []int{1, 2, 4} { // above, mid, below the commented PR 3
			t.Run(fmt.Sprintf("PR %d of the stack is %s: refused before any side effect", n, name), func(t *testing.T) {
				prs := fixtureStack()
				mutate(prs[n])
				gh := newFake(t, prs).allMethods()
				fc, _ := newForcemergeForTest(t, gh)
				_, err := mergePR(fc, 3, "ship it", MergeModeStack)
				assert.NotNil(t, err)
				assert.Equal(t, 0, len(gh.sideEffects()))
			})
		}
	}

	t.Run("the commented PR is a draft/closed/fork: refused", func(t *testing.T) {
		for name, mutate := range map[string]func(p *fakePR){
			"draft":  func(p *fakePR) { p.Draft = true },
			"closed": func(p *fakePR) { p.State = "closed" },
			"fork":   func(p *fakePR) { p.HeadRepo = "x/goliac" },
		} {
			prs := fixtureStack()
			mutate(prs[3])
			gh := newFake(t, prs).allMethods()
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, 3, "ship it", MergeModeStack)
			assert.NotNil(t, err, name)
			assert.Equal(t, 0, len(gh.sideEffects()), name)
		}
	})

	t.Run("two PRs above with the same base: ambiguous", func(t *testing.T) {
		prs := fixtureStack()
		prs[9] = &fakePR{Number: 9, Title: "sibling", BaseRef: "b3", HeadRef: "b9", HeadSha: "sha9", State: "open", StackPosition: 2, StackSize: 4, StackBase: "main"}
		gh := newFake(t, prs).allMethods()
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "ambiguous")
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("two PRs below with the same head branch: ambiguous", func(t *testing.T) {
		prs := fixtureStack()
		prs[9] = &fakePR{Number: 9, Title: "dup", BaseRef: "main", HeadRef: "b4", HeadSha: "sha9", State: "open", StackPosition: 1, StackSize: 4, StackBase: "main"}
		gh := newFake(t, prs).allMethods()
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "ambiguous")
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("a PR above without stack metadata, from a fork, or from another stack is not part of the stack", func(t *testing.T) {
		prs := fixtureStack()
		prs[8] = &fakePR{Number: 8, Title: "unrelated", BaseRef: "b1", HeadRef: "b8", HeadSha: "sha8", State: "open"}
		prs[9] = &fakePR{Number: 9, Title: "forked", BaseRef: "b1", HeadRef: "b9", HeadSha: "sha9", State: "open", HeadRepo: "x/goliac", StackPosition: 5, StackSize: 4, StackBase: "main"}
		prs[10] = &fakePR{Number: 10, Title: "other", BaseRef: "b1", HeadRef: "b10", HeadSha: "sha10", State: "open", StackPosition: 1, StackSize: 2, StackBase: "dev"}
		gh := newFake(t, prs).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.Nil(t, err)
		assert.Equal(t, []string{"1", "2", "3", "4"}, gh.reviewedPRs())
	})

	t.Run("cycle above: refused", func(t *testing.T) {
		prs := fixtureStack()
		// PR 1 is made the base of PR 4's branch... 4 (head b4) based on b1 (head of PR 1): 1 > 2 > 3 > 4 > 1
		prs[4].BaseRef = "b1"
		gh := newFake(t, prs).allMethods()
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("cycle below: refused", func(t *testing.T) {
		prs := fixtureStack()
		prs[4].BaseRef = "b1"
		prs[1].BaseRef = "b4"
		gh := newFake(t, prs).allMethods()
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 4, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("a walk longer than the stack size is refused", func(t *testing.T) {
		prs := fixtureStack()
		for _, p := range prs {
			p.StackSize = 2
		}
		gh := newFake(t, prs).allMethods()
		fc, _ := newForcemergeForTest(t, gh)
		for _, n := range []int{1, 3} {
			_, err := mergePR(fc, n, "ship it", MergeModeStack)
			assert.NotNil(t, err)
		}
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("malformed stack payloads are refused", func(t *testing.T) {
		for name, raw := range map[string]interface{}{
			"empty object":  map[string]interface{}{},
			"size 0":        map[string]interface{}{"size": 0, "position": 1, "base": map[string]interface{}{"ref": "main"}},
			"no base":       map[string]interface{}{"size": 4, "position": 1},
			"negative size": map[string]interface{}{"size": -1, "position": 1, "base": map[string]interface{}{"ref": "main"}},
		} {
			prs := fixtureStack()
			prs[3].RawStack = raw
			gh := newFake(t, prs).allMethods()
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, 3, "ship it", MergeModeStack)
			assert.NotNil(t, err, name)
			assert.Contains(t, err.Error(), "malformed stack payload", name)
			assert.Equal(t, 0, len(gh.sideEffects()), name)
		}
	})

	t.Run("a PR of the stack with a different stack base is not followed", func(t *testing.T) {
		prs := fixtureStack()
		prs[4].StackBase = "develop"
		gh := newFake(t, prs).allMethods()
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("listing failures refuse the stack merge", func(t *testing.T) {
		for _, fail := range []string{"base=", "head="} {
			gh := newFake(t, fixtureStack()).allMethods()
			gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
				if call.Method == "GET" && call.Endpoint == "/repos/myorg/goliac/pulls" && strings.Contains(call.Parameters, fail) {
					return httpError("500 Internal Server Error", `{}`)
				}
				return nil, nil
			}
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, 3, "ship it", MergeModeStack)
			assert.NotNil(t, err, fail)
			assert.Equal(t, 0, len(gh.sideEffects()), fail)
		}
		// bad JSON in the listing
		gh := newFake(t, fixtureStack()).allMethods()
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && call.Endpoint == "/repos/myorg/goliac/pulls" {
				return []byte(`{`), nil
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Equal(t, 0, len(gh.sideEffects()))
	})

	t.Run("a head pushed between the approvals and the merge: nothing merged, all approvals dismissed", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "POST" && strings.HasSuffix(call.Endpoint, "/pulls/4/reviews") {
				f.update(2, func(p *fakePR) { p.HeadSha = "pushed" })
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "PR #2 was pushed to after the approval")
		assert.Equal(t, 0, len(gh.find("PUT", "/merge-async")))
		assert.Equal(t, 4, len(gh.find("PUT", "/dismissals")))
	})

	t.Run("an approval failing in the middle dismisses the ones already created", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "POST" && strings.HasSuffix(call.Endpoint, "/pulls/3/reviews") {
				return httpError("422 Unprocessable Entity", `{}`)
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Equal(t, 2, len(gh.find("PUT", "/dismissals")))
		assert.Equal(t, 0, len(gh.find("PUT", "/merge-async")))
	})

	t.Run("asynchronous merge times out: approvals kept and listed", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = okJSON(asyncPending)
		gh.asyncPoll = okJSON(asyncPending)
		fc, clock := newForcemergeForTest(t, gh)
		fc.asyncPollTimeout = 20 * time.Second

		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "timeout after 20s waiting for the asynchronous merge (request uuid-1)")
		assert.Contains(t, err.Error(), "left in place: PR #1 (review 1001), PR #2")
		assert.Equal(t, 0, len(gh.find("PUT", "/dismissals")))
		assert.GreaterOrEqual(t, len(clock.slept()), 19)
	})

	t.Run("asynchronous merge fails: approvals dismissed", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = okJSON(asyncPending)
		gh.asyncPoll = okJSON(`{"status":"failed","details":{"message":"Required status check is failing"}}`)
		fc, _ := newForcemergeForTest(t, gh)

		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "GitHub refused the asynchronous merge (request uuid-1): Required status check is failing")
		assert.Contains(t, err.Error(), "were dismissed")
		assert.Equal(t, 4, len(gh.find("PUT", "/dismissals")))
	})

	t.Run("the stack merged but a PR is not confirmed merged: error", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		gh.neverMarkMerged = true
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 3, "ship it", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "not every PR is confirmed merged")
	})
}

func TestForcemergeAsyncMerge(t *testing.T) {
	// asynchronous merge of the bottom PR 4 of the stack
	run := func(t *testing.T, gh *fakeGitHub) (error, *fakeGitHub) {
		gh.prs = fixtureStack()
		gh.allMethods()
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 4, "because", "")
		return err, gh
	}
	fresh := func(t *testing.T) *fakeGitHub { return newFake(t, nil) }

	t.Run("timeout while still pending", func(t *testing.T) {
		gh := fresh(t)
		gh.asyncMerge, gh.asyncPoll = okJSON(asyncPending), okJSON(asyncPending)
		gh.allMethods()
		gh.prs = fixtureStack()
		fc, _ := newForcemergeForTest(t, gh)
		fc.asyncPollTimeout = 3 * time.Second
		_, err := mergePR(fc, 4, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "timeout after 3s waiting for the asynchronous merge (request uuid-1)")
	})

	for status, expected := range map[string]string{
		"400": "not ready to be merged",
		"403": "refused (permissions or rules)",
		"404": "not found",
		"422": "validation failed",
		"500": "error merging the PR asynchronously",
	} {
		t.Run("HTTP "+status, func(t *testing.T) {
			gh := fresh(t)
			gh.asyncMerge = func(call restCall) ([]byte, error) { return httpError(status+" Whatever", `{"message":"boom"}`) }
			err, gh := run(t, gh)
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), expected)
			assert.Equal(t, 1, len(gh.find("PUT", "/dismissals")), "definitive failure: approval dismissed")
		})
	}

	t.Run("an existing foreign request (409) is not adopted", func(t *testing.T) {
		gh := fresh(t)
		gh.asyncMerge = func(call restCall) ([]byte, error) { return httpError("409 Conflict", asyncPending) }
		err, gh := run(t, gh)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "another asynchronous merge is already pending")
		assert.Contains(t, err.Error(), "does not adopt it")
		assert.Contains(t, err.Error(), "uuid-1")
		assert.Equal(t, 0, len(gh.find("GET", "/merge-async/uuid-1")))
		assert.Equal(t, 0, len(gh.find("PATCH", "")))
		assert.Equal(t, 0, len(gh.find("PUT", "/dismissals")))
	})

	t.Run("409 with an unreadable body is still reported", func(t *testing.T) {
		gh := fresh(t)
		gh.asyncMerge = func(call restCall) ([]byte, error) { return httpError("409 Conflict", `<html>`) }
		err, _ := run(t, gh)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "another asynchronous merge is already pending")
	})

	t.Run("already merged (200 merged) needs no poll", func(t *testing.T) {
		gh := fresh(t)
		gh.asyncMerge = okJSON(asyncMerged)
		err, gh := run(t, gh)
		assert.Nil(t, err)
		assert.Equal(t, 0, len(gh.find("GET", "/merge-async/uuid-1")))
	})

	t.Run("enqueued", func(t *testing.T) {
		gh := fresh(t)
		gh.asyncMerge = okJSON(`{"status":"enqueued","details":{"message":"queued","uuid":"u"}}`)
		err, gh := run(t, gh)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "queued in a merge queue, not merged")
		assert.Equal(t, 0, len(gh.find("PATCH", "")))
	})

	t.Run("enqueued reported by the poll", func(t *testing.T) {
		gh := fresh(t)
		gh.asyncMerge = okJSON(asyncPending)
		gh.asyncPoll = okJSON(`{"status":"enqueued","details":{"message":"queued"}}`)
		err, _ := run(t, gh)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "queued in a merge queue, not merged (request uuid-1)")
	})

	t.Run("pending without a request id", func(t *testing.T) {
		gh := fresh(t)
		gh.asyncMerge = okJSON(`{"status":"pending","details":{"message":"working"}}`)
		err, gh := run(t, gh)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "no request id")
		assert.Equal(t, 0, len(gh.find("PATCH", "")))
	})

	t.Run("unknown status", func(t *testing.T) {
		gh := fresh(t)
		gh.asyncMerge = okJSON(`{"status":"exploded","details":{}}`)
		err, _ := run(t, gh)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), `unexpected asynchronous merge status "exploded"`)
		assert.Contains(t, err.Error(), "no message from GitHub")
	})

	t.Run("bad JSON from merge-async", func(t *testing.T) {
		gh := fresh(t)
		gh.asyncMerge = okJSON(`{`)
		err, _ := run(t, gh)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "error parsing the asynchronous merge response")
	})

	t.Run("bad JSON from the poll", func(t *testing.T) {
		gh := fresh(t)
		gh.asyncMerge = okJSON(asyncPending)
		gh.asyncPoll = okJSON(`{`)
		err, _ := run(t, gh)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "error parsing the asynchronous merge status")
	})

	t.Run("transient poll error then merged", func(t *testing.T) {
		polls := 0
		gh := fresh(t)
		gh.asyncMerge = okJSON(asyncPending)
		gh.asyncPoll = func(call restCall) ([]byte, error) {
			polls++
			if polls == 1 {
				return httpError("502 Bad Gateway", `{}`)
			}
			return []byte(asyncMerged), nil
		}
		err, _ := run(t, gh)
		assert.Nil(t, err)
		assert.Equal(t, 2, polls)
	})

	t.Run("expired request (404)", func(t *testing.T) {
		gh := fresh(t)
		gh.asyncMerge = okJSON(asyncPending)
		gh.asyncPoll = func(call restCall) ([]byte, error) { return httpError("404 Not Found", `{"message":"Not Found"}`) }
		err, _ := run(t, gh)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "not found (expired?)")
	})

	t.Run("context cancelled while polling", func(t *testing.T) {
		gh := fresh(t)
		gh.prs = fixtureStack()
		gh.allMethods()
		gh.asyncMerge = okJSON(asyncPending)
		fc, _ := newForcemergeForTest(t, gh)
		ctx, cancel := context.WithCancel(context.Background())
		gh.asyncPoll = func(call restCall) ([]byte, error) { cancel(); return []byte(asyncPending), nil }
		plan, err := fc.planMerge(ctx, "test-user", "goliac", "4", prURL(4), "because", "")
		assert.Nil(t, err)
		_, err = fc.executePlan(ctx, plan)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "still pending")
		assert.Equal(t, 0, len(gh.find("PUT", "/dismissals")))
	})
}
