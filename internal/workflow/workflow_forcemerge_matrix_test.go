package workflow

import (
	"context"
	"fmt"
	"net/url"
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
		var total time.Duration
		for _, d := range clock.slept() {
			total += d
		}
		assert.LessOrEqual(t, total, 60*time.Second)
		assert.GreaterOrEqual(t, total, 50*time.Second, "GitHub's retarget is waited for about 60s")
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

// branch names with slashes (release/1.0, feature/a/b) must be escaped in URL paths and queries
func fixtureSlashChain() map[int]*fakePR {
	return map[int]*fakePR{
		71: {Number: 71, Title: "p3", Author: "a71", BaseRef: "main", HeadRef: "release/1.0", HeadSha: "sha71", State: "open"},
		72: {Number: 72, Title: "p2", Author: "a72", BaseRef: "release/1.0", HeadRef: "feature/a/b", HeadSha: "sha72", State: "open"},
		73: {Number: 73, Title: "p1", Author: "a73", BaseRef: "feature/a/b", HeadRef: "feature/a/b/c", HeadSha: "sha73", State: "open"},
	}
}

func TestForcemergeSlashBranchNames(t *testing.T) {
	t.Run("stack mode: the chain is found by base and head with slash names, all merged into main", func(t *testing.T) {
		gh := newFake(t, fixtureSlashChain()).allMethods()
		gh.deleteBranchOnMerge = true
		gh.asyncMerge = okJSON(asyncMerged)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 72, "ship it", MergeModeStack)
		assert.Nil(t, err)
		for _, n := range []int{71, 72, 73} {
			assert.True(t, gh.pr(n).Merged, "PR #%d merged", n)
		}
	})

	t.Run("direct on the middle PR: merged into the slash-named parent branch, the PR above retargeted onto it", func(t *testing.T) {
		gh := newFake(t, fixtureSlashChain()).allMethods()
		gh.branchMerge = okJSON(`{"sha":"mergecommit"}`)
		gh.markMergedOnBranchMerge = true
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 72, "ship it", "")
		assert.Nil(t, err)
		merges := gh.find("POST", "/repos/myorg/goliac/merges")
		assert.Equal(t, 1, len(merges))
		assert.Equal(t, "release/1.0", merges[0].Body["base"])
		assert.Equal(t, "release/1.0", gh.pr(73).BaseRef)
	})

	t.Run("direct on the bottom PR with a slash name: PR above retargeted onto main", func(t *testing.T) {
		gh := newFake(t, fixtureSlashChain()).allMethods()
		gh.asyncMerge = okJSON(asyncMerged)
		fc, _ := newForcemergeForTest(t, gh)
		_, err := mergePR(fc, 71, "ship it", "")
		assert.Nil(t, err)
		assert.Equal(t, "main", gh.pr(72).BaseRef)
	})

	t.Run("dead-base check: the tip of release/1.0 is read with an escaped path", func(t *testing.T) {
		for _, tc := range []struct {
			base, tip string
			refused   bool
		}{{"release/1.0", "sha-closed", true}, {"feature/a/b", "moved-on", false}} {
			prs := map[int]*fakePR{
				10: {Number: 10, Title: "old", BaseRef: "main", HeadRef: tc.base, HeadSha: "sha-closed", State: "closed"},
				11: {Number: 11, Title: "x", Author: "a11", BaseRef: tc.base, HeadRef: "x", HeadSha: "sha11", State: "open"},
			}
			gh := newFake(t, prs).allMethods()
			gh.branchTips = map[string]string{tc.base: tc.tip}
			gh.syncMerge = okJSON(`{"merged":true}`)
			fc, _ := newForcemergeForTest(t, gh)
			_, err := mergePR(fc, 11, "ship it", "")
			if tc.refused {
				assert.NotNil(t, err, tc.base)
				if err != nil {
					assert.Contains(t, err.Error(), "dead branch")
				}
			} else {
				assert.Nil(t, err, tc.base)
			}
			var paths []string
			for _, c := range gh.find("GET", "") {
				if strings.Contains(c.Endpoint, "/branches/") {
					paths = append(paths, c.Endpoint)
				}
			}
			assert.Equal(t, []string{"/repos/myorg/goliac/branches/" + url.PathEscape(tc.base)}, paths)
		}
	})
}
