package workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestForcemergeR32CleanupAfterTheDeadline(t *testing.T) {
	t.Run("the deadline hits the asynchronous merge of PR 3 in a 4-stack: #1 and #2 are still dismissed, the report is truthful", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		gh.asyncMerge = okJSON(asyncMerged)
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PUT" && strings.HasSuffix(call.Endpoint, "/pulls/3/merge-async") {
				cancel() // the overall deadline expires while PR 3 is being merged
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		plan, err := fc.planMerge(ctx, "test-user", "goliac", "3", prURL(3), "because", MergeModeStack)
		assert.Nil(t, err)
		_, err = fc.executePlan(ctx, plan)
		assert.NotNil(t, err)
		// PR 4 merged, PR 3 unknown, PRs 2 and 1 never tried
		assert.Contains(t, err.Error(), "already merged into `main`: #4; outcome unknown (it may be merged): #3; not merged: #2, #1")
		dismissals := gh.find("PUT", "/dismissals")
		assert.Equal(t, 2, len(dismissals), "the approvals of PRs 1 and 2 are dismissed although the context is cancelled")
		assert.Contains(t, err.Error(), "the approval of PR #3 is left in place")
		assert.True(t, gh.pr(4).Merged)
		assert.False(t, gh.pr(2).Merged)
	})

	t.Run("a plain PR: the merge request times out with the cancelled context, the approval is kept (unknown) and nothing else runs", func(t *testing.T) {
		gh := plainFake(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "PUT" && strings.HasSuffix(call.Endpoint, "/pulls/32/merge") {
				cancel()
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		plan, err := fc.planMerge(ctx, "test-user", "goliac", "32", prURL(32), "because", "")
		assert.Nil(t, err)
		_, err = fc.executePlan(ctx, plan)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "left in place")
	})

	t.Run("a definite failure after the deadline: the approval is dismissed on a detached context", func(t *testing.T) {
		gh := plainFake(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		gh.syncMerge = func(call restCall) ([]byte, error) {
			cancel() // the deadline passes during the (refused) merge
			return httpError("403 Forbidden", `{"message":"no"}`)
		}
		fc, _ := newForcemergeForTest(t, gh)
		plan, err := fc.planMerge(ctx, "test-user", "goliac", "32", prURL(32), "because", "")
		assert.Nil(t, err)
		_, err = fc.executePlan(ctx, plan)
		assert.NotNil(t, err)
		assert.Equal(t, 1, len(gh.find("PUT", "/dismissals")))
		assert.Contains(t, err.Error(), "were dismissed")
	})
}

func TestForcemergeR33FindApprovalIdentity(t *testing.T) {
	body := "Force merge via Goliac on behalf of u"
	newFC := func(reviews ...fakeReview) *ForcemergeImpl {
		gh := newFake(t, fixturePlainPR()).allMethods()
		gh.reviews = reviews
		fc, _ := newForcemergeForTest(t, gh)
		return fc
	}
	pr := &pullRequestInfo{Number: 32}
	pr.Head.Sha = "aaa111"
	good := fakeReview{ID: 7, PR: 32, State: "APPROVED", Body: body, CommitID: "aaa111", Login: "goliac-app[bot]"}

	t.Run("a matching review is bound", func(t *testing.T) {
		a := newFC(good).findApproval(context.Background(), "goliac", pr, body)
		assert.Equal(t, int64(7), a.review)
		assert.False(t, a.maybe)
		assert.Empty(t, a.also)
	})
	mutate := map[string]func(r *fakeReview){
		"not approving (dismissed)":  func(r *fakeReview) { r.State = "DISMISSED" },
		"a comment":                  func(r *fakeReview) { r.State = "COMMENTED" },
		"another body":               func(r *fakeReview) { r.Body = "something else" },
		"id 0":                       func(r *fakeReview) { r.ID = 0 },
		"another commit":             func(r *fakeReview) { r.CommitID = "other" },
		"a human with the same body": func(r *fakeReview) { r.Login = "charles" },
		"another bot":                func(r *fakeReview) { r.Login = "dependabot[bot]" },
	}
	for name, m := range mutate {
		t.Run("not matched: "+name, func(t *testing.T) {
			r := good
			m(&r)
			a := newFC(r).findApproval(context.Background(), "goliac", pr, body)
			assert.Equal(t, int64(0), a.review)
			assert.True(t, a.maybe)
		})
	}
	t.Run("the login is compared without case", func(t *testing.T) {
		r := good
		r.Login = "Goliac-App[bot]"
		assert.Equal(t, int64(7), newFC(r).findApproval(context.Background(), "goliac", pr, body).review)
	})
	t.Run("two identical Goliac approvals are both bound and both dismissed", func(t *testing.T) {
		second := good
		second.ID = 8
		human := good
		human.ID = 9
		human.Login = "charles"
		gh := newFake(t, fixturePlainPR()).allMethods()
		gh.reviews = []fakeReview{good, human, second}
		fc, _ := newForcemergeForTest(t, gh)
		a := fc.findApproval(context.Background(), "goliac", pr, body)
		assert.Equal(t, int64(7), a.review)
		assert.Equal(t, []int64{8}, a.also)
		err := fc.failAfterApprovals(context.Background(), "goliac", []*approval{a}, fmt.Errorf("boom"))
		assert.Contains(t, err.Error(), "were dismissed")
		var ids []string
		for _, c := range gh.find("PUT", "/dismissals") {
			ids = append(ids, c.Endpoint[strings.LastIndex(strings.TrimSuffix(c.Endpoint, "/dismissals"), "/")+1:len(c.Endpoint)-len("/dismissals")])
		}
		assert.ElementsMatch(t, []string{"7", "8"}, ids, "the human review (9) is never dismissed")
		assert.Equal(t, "PR #32 (review 7, 8)", describeApprovals([]*approval{a}))
	})
	t.Run("the app slug is lower-cased to build the login", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR()).allMethods()
		gh.reviews = []fakeReview{good}
		fc, _ := newForcemergeForTest(t, gh)
		fc.ws = &slugService{fc.ws, "Goliac-App"}
		assert.Equal(t, int64(7), fc.findApproval(context.Background(), "goliac", pr, body).review)
	})
	t.Run("a Goliac whose identity is unknown matches nothing", func(t *testing.T) {
		gh := newFake(t, fixturePlainPR()).allMethods()
		gh.reviews = []fakeReview{good}
		fc, _ := newForcemergeForTest(t, gh)
		fc.ws = &noSlugService{fc.ws}
		a := fc.findApproval(context.Background(), "goliac", pr, body)
		assert.True(t, a.maybe)
		assert.Empty(t, gh.find("GET", "/reviews"), "no need to read the reviews")
	})
	t.Run("a page of 100 reviews: the next page is read", func(t *testing.T) {
		reviews := []fakeReview{}
		for i := 0; i < 100; i++ {
			reviews = append(reviews, fakeReview{ID: int64(100 + i), PR: 32, State: "COMMENTED", Body: "x", CommitID: "aaa111", Login: "charles"})
		}
		reviews = append(reviews, good)
		a := newFC(reviews...).findApproval(context.Background(), "goliac", pr, body)
		assert.Equal(t, int64(7), a.review)
	})
}

// noSlugService hides the GitHub App identity of a WorkflowService
type noSlugService struct{ WorkflowService }

func (noSlugService) AppSlug() string { return "" }

// slugService gives a WorkflowService the GitHub App slug of the test
type slugService struct {
	WorkflowService
	slug string
}

func (s *slugService) AppSlug() string { return s.slug }

func TestForcemergeR35StrayWarningWithSquash(t *testing.T) {
	prs := func() map[int]*fakePR {
		return map[int]*fakePR{
			32: {Number: 32, Title: "hotfix", Author: "a32", BaseRef: "main", HeadRef: "hotfix", HeadSha: "aaa", State: "open"},
			33: {Number: 33, Title: "wip", BaseRef: "hotfix", HeadRef: "wip", HeadSha: "bbb", State: "open", Draft: true},
		}
	}
	t.Run("squash-only repository: the review and the answer warn that the untouched PRs will conflict", func(t *testing.T) {
		gh := newFake(t, prs()).squashOnly()
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)
		summary, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		review := firstCall(gh.find("POST", "/reviews")).Body["body"].(string)
		assert.Contains(t, review, "#33 (draft)")
		assert.Contains(t, review, "Warning: the PRs left untouched carry the original commits of the merged PR, and this PR is merged with squash: they will conflict with the rewritten history.")
		assert.Contains(t, summary, "#33 (draft). Warning: the PRs left untouched carry the original commits")
	})
	t.Run("explicit /squash: same warning", func(t *testing.T) {
		gh := newFake(t, prs()).allMethods()
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)
		summary, err := mergePR(fc, 32, "because /squash", "")
		assert.Nil(t, err)
		assert.Contains(t, summary, "Warning")
	})
	t.Run("merge commit: no warning", func(t *testing.T) {
		gh := newFake(t, prs()).allMethods()
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)
		summary, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		assert.NotContains(t, summary, "Warning")
		assert.NotContains(t, firstCall(gh.find("POST", "/reviews")).Body["body"], "Warning")
	})
	t.Run("no stray PR: no warning even with squash", func(t *testing.T) {
		p := prs()
		delete(p, 33)
		gh := newFake(t, p).squashOnly()
		gh.syncMerge = okJSON(`{"merged":true}`)
		fc, _ := newForcemergeForTest(t, gh)
		summary, err := mergePR(fc, 32, "because", "")
		assert.Nil(t, err)
		assert.NotContains(t, summary, "Warning")
	})
	assert.Equal(t, "", squashStrayWarning("merge"))
	assert.Equal(t, "", squashStrayWarning(""))
	assert.Equal(t, "", squashStrayNote("merge"))
	assert.Contains(t, squashStrayNote("rebase"), ". Warning:")
}

func TestForcemergeSquashFlagIsAWord(t *testing.T) {
	cases := []struct {
		in, text string
		flag     bool
	}{
		{"fix prod /squash", "fix prod ", true},
		{"/squash fix prod", " fix prod", true},
		{"fix /squash prod", "fix  prod", true},
		{"fix prod /squash.", "fix prod .", true},
		{"fix prod (/squash)", "fix prod ()", true},
		{"/squash /squash", " ", true},
		{"fix\n/squash\nnow", "fix\n\nnow", true},
		{"the /squashed branch", "the /squashed branch", false},
		{"/squash-all the things", "/squash-all the things", false},
		{"see http://x/squash", "see http://x/squash", false},
		{"/squashing", "/squashing", false},
		{"/Squash", "/Squash", false},
		{"nothing", "nothing", false},
		{"", "", false},
	}
	for _, c := range cases {
		flag, text := extractSquashFlag(c.in)
		assert.Equal(t, c.flag, flag, c.in)
		assert.Equal(t, c.text, text, c.in)
	}

	t.Run("/squashed in an explanation does not squash", func(t *testing.T) {
		gh := plainFake(t)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "I /squashed the branch already", "")
		assert.Nil(t, err)
		assert.Equal(t, "merge", firstCall(gh.find("PUT", "/pulls/32/merge")).Body["merge_method"])
		assert.Contains(t, firstCall(gh.find("POST", "/reviews")).Body["body"], "I /squashed the branch already")
	})
	t.Run("a real /squash squashes and is removed from the review", func(t *testing.T) {
		gh := plainFake(t)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "prod is down /squash", "")
		assert.Nil(t, err)
		assert.Equal(t, "squash", firstCall(gh.find("PUT", "/pulls/32/merge")).Body["merge_method"])
		assert.NotContains(t, firstCall(gh.find("POST", "/reviews")).Body["body"], "/squash")
	})
	t.Run("/squashed alone is an explanation (it is not the flag)", func(t *testing.T) {
		gh := plainFake(t)
		fc := executeFixture(t, gh, &recordingPlugin{})
		_, err := fc.ExecuteWorkflow(context.Background(), []string{"fmtest"}, "test-user", "fmtest", "/squashed", map[string]string{"pr_url": prURL(32)}, false)
		assert.Nil(t, err)
	})
}

func TestForcemergeR5MutantGaps(t *testing.T) {
	t.Run("a PR above listed with an empty state counts as a valid child (GitHub always sends a state)", func(t *testing.T) {
		gh := newFake(t, fixturePlainChain()).allMethods()
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.markMergedOnBranchMerge = true
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			if call.Method == "GET" && call.Endpoint == "/repos/myorg/goliac/pulls" && strings.Contains(call.Parameters, "base=t2") && strings.Contains(call.Parameters, "page=1") {
				return []byte(`[{"number":59,"title":"t3","state":"","draft":false,"base":{"ref":"t2"},"head":{"ref":"t3","sha":"sha59","repo":{"full_name":"myorg/goliac"}},"stack":null}]`), nil
			}
			return nil, nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		summary, err := mergePR(fc, 58, "because", "")
		assert.Nil(t, err)
		assert.Contains(t, summary, "retargeted onto `t1`: #59")
	})

	t.Run("a lone PR on a release branch with a VALID PR above it is not lone: refused (the chain does not reach the default branch)", func(t *testing.T) {
		prs := map[int]*fakePR{
			32: {Number: 32, Title: "a", BaseRef: "release/1.0", HeadRef: "a", HeadSha: "aaa", State: "open"},
			33: {Number: 33, Title: "b", BaseRef: "a", HeadRef: "b", HeadSha: "bbb", State: "open"},
		}
		gh := newFake(t, prs).allMethods()
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 32, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "stops at PR #32")
		assert.Empty(t, gh.sideEffects())
	})

	t.Run("planStack counts the open PRs from the position of the bottom open PR (boundaries)", func(t *testing.T) {
		// only PR 1 (position 4 of 4) is left: the three below are merged
		prs := fixtureStack()
		for _, n := range []int{2, 3, 4} {
			prs[n].Merged, prs[n].State = true, "closed"
		}
		prs[1].BaseRef = "main"
		gh := newFake(t, prs).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 1, "because", MergeModeStack)
		assert.Nil(t, err, "position == size: one PR left")
		assert.True(t, gh.pr(1).Merged)

		// a bottom PR at position 1 (nothing merged below): the whole size is expected
		gh = newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		fc, _ = newForcemergeForTest(t, gh)
		_, err = mergePR(fc, 3, "because", MergeModeStack)
		assert.Nil(t, err)

		// a position that says one PR is merged below although it is still open: the count does not match, refused
		prs = fixtureStack()
		prs[4].StackPosition = 2
		gh = newFake(t, prs).allMethods()
		fc, _ = newForcemergeForTest(t, gh)
		_, err = mergePR(fc, 3, "because", MergeModeStack)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "(3 open)")
		assert.Empty(t, gh.sideEffects())
	})

	t.Run("direct on the bottom PR of a GitHub stack: the merge method is not downgraded while PRs remain above", func(t *testing.T) {
		gh := newFake(t, fixtureStack()).allMethods()
		gh.asyncMerge = func(call restCall) ([]byte, error) {
			return httpError("405 Method Not Allowed", `{"message":"Merge commits are not allowed on this repository."}`)
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 4, "because", "")
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "not downgraded to a squash merge")
		assert.Equal(t, 1, len(gh.find("PUT", "/merge-async")))
	})

	t.Run("direct on a bottom PR with nothing open above it: the downgrade to squash is allowed", func(t *testing.T) {
		prs := fixtureStack()
		for _, n := range []int{1, 2, 3} {
			prs[n].State = "closed"
		}
		gh := newFake(t, prs).allMethods()
		gh.asyncMerge = func(call restCall) ([]byte, error) {
			if call.Body["merge_method"] == "merge" {
				return httpError("405 Method Not Allowed", `{"message":"Merge commits are not allowed on this repository."}`)
			}
			return []byte(asyncMerged), nil
		}
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 4, "because", "")
		assert.Nil(t, err)
		calls := gh.find("PUT", "/merge-async")
		assert.Equal(t, 2, len(calls))
		assert.Equal(t, "squash", calls[1].Body["merge_method"])
	})

	t.Run("the failure cleanup keeps its own timeout", func(t *testing.T) {
		ctx, cancel := cleanupContext(context.Background())
		defer cancel()
		dl, ok := ctx.Deadline()
		assert.True(t, ok)
		assert.LessOrEqual(t, time.Until(dl), cleanupTimeout)
		parent, pcancel := context.WithCancel(context.Background())
		cctx, ccancel := cleanupContext(parent)
		defer ccancel()
		pcancel()
		assert.Nil(t, cctx.Err(), "a cancelled parent does not cancel the cleanup")
	})
}
