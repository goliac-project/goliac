package workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

/*
 * The live test matrix of /forcemerge (phoenix-tooling, CFG-10940) replayed on the fake GitHub:
 * a chain of 4 PRs, 64 (P1, top) > 63 (P2) > 62 (P3) > 61 (P4, based on main), on a repository that
 * deletes the merged branches (GitHub then retargets the PRs above a merged PR by itself, at once or
 * just before Goliac's own retarget).
 */
func fixturePlainChain4() map[int]*fakePR {
	return map[int]*fakePR{
		61: {Number: 61, Title: "p4", Author: "a61", BaseRef: "main", HeadRef: "p4", HeadSha: "sha61", State: "open"},
		62: {Number: 62, Title: "p3", Author: "a62", BaseRef: "p4", HeadRef: "p3", HeadSha: "sha62", State: "open"},
		63: {Number: 63, Title: "p2", Author: "a63", BaseRef: "p3", HeadRef: "p2", HeadSha: "sha63", State: "open"},
		64: {Number: 64, Title: "p1", Author: "a64", BaseRef: "p2", HeadRef: "p1", HeadSha: "sha64", State: "open"},
	}
}

func TestForcemergeLiveMatrix4Chain(t *testing.T) {
	type expectation struct {
		merged []int          // PRs merged
		into   map[int]string // branch each merged PR landed in
		bases  map[int]string // base of the PRs not merged
		noMain bool           // nothing may reach main
	}
	direct := map[string]struct {
		from int
		exp  expectation
	}{
		"1/2: default and direct on P3 (62): into P4's branch, P2 retargeted onto it, nothing in main": {
			from: 62, exp: expectation{merged: []int{62}, into: map[int]string{62: "p4"}, bases: map[int]string{61: "main", 63: "p4", 64: "p2"}, noMain: true},
		},
		"3: on P4 (61): into main, P3 retargeted onto main": {
			from: 61, exp: expectation{merged: []int{61}, into: map[int]string{61: "main"}, bases: map[int]string{62: "main", 63: "p3", 64: "p2"}},
		},
		"4: on P1 (64): into P2's branch, nothing above": {
			from: 64, exp: expectation{merged: []int{64}, into: map[int]string{64: "p2"}, bases: map[int]string{61: "main", 62: "p4", 63: "p3"}, noMain: true},
		},
		"5: on P2 (63): into P3's branch, P1 retargeted onto it": {
			from: 63, exp: expectation{merged: []int{63}, into: map[int]string{63: "p3"}, bases: map[int]string{61: "main", 62: "p4", 64: "p3"}, noMain: true},
		},
	}
	for name, tc := range direct {
		for _, mode := range []string{"", MergeModeDirect} {
			for _, late := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s (mode %q, late retarget %v)", name, mode, late), func(t *testing.T) {
					gh := newFake(t, fixturePlainChain4()).allMethods()
					gh.deleteBranchOnMerge = true
					gh.autoRetargetLate = late
					gh.asyncMerge = okJSON(asyncMerged)
					gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
					gh.markMergedOnBranchMerge = true
					fc, _ := newForcemergeForTest(t, gh)

					_, err := mergePR(fc, tc.from, "ship it", mode)
					assert.Nil(t, err)
					for _, n := range tc.exp.merged {
						assert.True(t, gh.pr(n).Merged, "PR #%d merged", n)
					}
					for n, branch := range tc.exp.into {
						assert.Equal(t, branch, gh.pr(n).MergedInto, "PR #%d landed in %s", n, branch)
					}
					for n, base := range tc.exp.bases {
						assert.Equal(t, base, gh.pr(n).BaseRef, "base of PR #%d", n)
						assert.False(t, gh.pr(n).Merged, "PR #%d not merged", n)
					}
					assert.Empty(t, gh.find("PUT", "/dismissals"))
					if tc.exp.noMain {
						assert.Empty(t, gh.find("PUT", "/merge-async"), "nothing reaches main")
					}
				})
			}
		}
	}

	for _, from := range []int{62, 64, 61} {
		for _, late := range []bool{false, true} {
			t.Run(fmt.Sprintf("6/7/8: stack mode on PR %d (late retarget %v): the 4 PRs land in main, bottom-up", from, late), func(t *testing.T) {
				gh := newFake(t, fixturePlainChain4()).allMethods()
				gh.deleteBranchOnMerge = true
				gh.autoRetargetLate = late
				gh.asyncMerge = okJSON(asyncMerged)
				fc, _ := newForcemergeForTest(t, gh)

				_, err := mergePR(fc, from, "ship it", MergeModeStack)
				assert.Nil(t, err)
				for _, n := range []int{61, 62, 63, 64} {
					assert.True(t, gh.pr(n).Merged, "PR #%d merged", n)
					assert.Equal(t, "main", gh.pr(n).MergedInto)
				}
				assert.Equal(t, []string{"64", "63", "62", "61"}, gh.reviewedPRs())
				assert.Empty(t, gh.find("PUT", "/dismissals"))
			})
		}
	}
}

func TestForcemergePlanDirectDefensiveChecks(t *testing.T) {
	// planDirect runs after the inference or the stack metadata checks: it re-reads the neighbours and refuses
	// what changed in between
	newPlan := func(gh *fakeGitHub, number int) (*ForcemergeImpl, *mergePlan) {
		fc, _ := newForcemergeForTest(t, gh)
		info, err := fc.fetchPullRequest(context.Background(), "goliac", fmt.Sprint(number))
		assert.Nil(t, err)
		info.Stack = &prStack{Position: 2, Size: 3}
		info.Stack.Base.Ref = "main"
		info.stackInferred = true
		return fc, &mergePlan{pr: info}
	}

	t.Run("the PR below disappeared", func(t *testing.T) {
		prs := fixturePlainChain()
		delete(prs, 57)
		fc, plan := newPlan(newFake(t, prs), 58)
		err := fc.planDirect(context.Background(), "goliac", plan)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "no open PR of the same stack has this head branch")
	})

	t.Run("two PRs below", func(t *testing.T) {
		prs := fixturePlainChain()
		prs[60] = &fakePR{Number: 60, Title: "dup", BaseRef: "main", HeadRef: "t1", HeadSha: "sha60", State: "open"}
		fc, plan := newPlan(newFake(t, prs), 58)
		err := fc.planDirect(context.Background(), "goliac", plan)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "the stack is ambiguous")
	})

	t.Run("the PR below became a draft", func(t *testing.T) {
		prs := fixturePlainChain()
		prs[57].Draft = true
		fc, plan := newPlan(newFake(t, prs), 58)
		err := fc.planDirect(context.Background(), "goliac", plan)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "the PR below: PR #57 is a draft")
	})

	t.Run("listing the PR below or the PRs above fails", func(t *testing.T) {
		for _, failing := range []string{"head=", "base="} {
			gh := newFake(t, fixturePlainChain())
			fc, plan := newPlan(gh, 58)
			gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
				if call.Method == "GET" && call.Endpoint == "/repos/myorg/goliac/pulls" && strings.Contains(call.Parameters, failing) {
					return httpError("500 Internal Server Error", `{}`)
				}
				return nil, nil
			}
			assert.NotNil(t, fc.planDirect(context.Background(), "goliac", plan), failing)
		}
	})
}

func TestForcemergeGithubStackWaitsForGithubsRetarget(t *testing.T) {
	merged := &pullRequestInfo{Number: 4}
	merged.Base.Ref, merged.Head.Ref = "main", "b4"
	requested := &pullRequestInfo{Number: 3}

	t.Run("reading the PR above fails every time", func(t *testing.T) {
		gh := newFake(t, fixtureStack())
		gh.onCall = func(f *fakeGitHub, call restCall) ([]byte, error) {
			return httpError("500 Internal Server Error", `{}`)
		}
		fc, clock := newForcemergeForTest(t, gh)
		_, err := fc.retargetAfterMerge(context.Background(), "goliac", requested, merged, []*pullRequestInfo{{Number: 3}})
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "GitHub did not retarget")
		assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}, clock.slept())
	})

	t.Run("the context is cancelled while waiting", func(t *testing.T) {
		gh := newFake(t, fixtureStack())
		fc, _ := newForcemergeForTest(t, gh)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := fc.retargetAfterMerge(ctx, "goliac", requested, merged, []*pullRequestInfo{{Number: 3}})
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})

	t.Run("the PR above was closed meanwhile: nothing to wait for", func(t *testing.T) {
		prs := fixtureStack()
		prs[3].State = "closed"
		gh := newFake(t, prs)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := fc.retargetAfterMerge(context.Background(), "goliac", requested, merged, []*pullRequestInfo{{Number: 3}})
		assert.Nil(t, err)
	})
}

func TestGithubErrorDetail(t *testing.T) {
	assert.Equal(t, "boom", githubErrorDetail([]byte(`{"message":"boom"}`)))
	assert.Equal(t, "deep", githubErrorDetail([]byte(`{"message":"boom","details":{"message":"deep"}}`)))
	assert.Equal(t, "", githubErrorDetail([]byte(`not json`)))
}
