package workflow

import (
	"fmt"
	"testing"

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
