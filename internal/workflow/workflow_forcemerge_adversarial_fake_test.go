package workflow

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

/*
 * advGH wraps the existing fakeGitHub and makes it stricter, closer to real GitHub:
 *  - GET pulls paginates (default 30, max 100, page=N) and sorts created desc by default
 *  - the retarget GitHub does after a merge (delete_branch_on_merge, native stacks) lands after N reads of the
 *    child PR (arriveAfter: 0 = at once, -1 = never), or just before a PATCH of that PR
 *  - PATCH base: always 422 on the current base (not only with delete_branch_on_merge), 422 on a closed PR, on
 *    base == head, on a deleted/unknown branch
 *  - merge / merge-async: 404 unknown PR, 405/400 when the PR is closed, merged or a draft, the pinned sha must be
 *    the current head, the merge method must be allowed by the repository settings (405)
 *  - POST /merges: the base branch must exist (a branch deleted by delete_branch_on_merge answers 404)
 *  - a branch is deleted when its PR merged and delete_branch_on_merge is set
 * Assumptions made about GitHub that the live facts do not cover are marked "assumed".
 */
type advGH struct {
	t     *testing.T
	inner *fakeGitHub

	mu  sync.Mutex
	log []restCall

	// before runs before every call is processed (no lock held): it simulates something happening on GitHub
	// between two requests of the force merge
	before func(a *advGH, c restCall)

	arriveAfter int // reads of the child PR before GitHub's own retarget lands (0 = at once, -1 = never)
	pending     map[int]*advPending
	branches    map[string]bool
	seenMerged  map[int]bool

	maxPage int // largest page size served (check)
}

type advPending struct {
	to    string
	reads int
}

func newAdv(t *testing.T, prs map[int]*fakePR) *advGH {
	inner := newFake(t, prs).allMethods()
	inner.autoRetargetLate = true
	inner.asyncMerge = okJSON(asyncMerged)
	inner.syncMerge = okJSON(`{"merged":true}`)
	inner.branchMerge = okJSON(`{"sha":"mergecommit"}`)
	a := &advGH{t: t, inner: inner, pending: map[int]*advPending{}, branches: map[string]bool{"main": true}, seenMerged: map[int]bool{}}
	for _, p := range prs {
		a.branches[p.HeadRef] = true
		a.branches[p.BaseRef] = true
	}
	return a
}

func newAdvFC(t *testing.T, a *advGH) (*ForcemergeImpl, *fakeClock) {
	fc, clock := newForcemergeForTest(t, a.inner)
	fc.ws = NewWorkflowService("myorg", nil, nil, a)
	return fc, clock
}

// ---- inspection (the wrapper's own log: it sees every call, with the exact parameters of the code) ----

func short(endpoint string) string { return strings.TrimPrefix(endpoint, "/repos/myorg/goliac") }

// mutating is the exact sequence of the calls that change something: "METHOD path"
func (a *advGH) mutating() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	res := []string{}
	for _, c := range a.log {
		if c.Method != "GET" {
			res = append(res, c.Method+" "+short(c.Endpoint))
		}
	}
	return res
}

func (a *advGH) callsTo(method, suffix string) []restCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	res := []restCall{}
	for _, c := range a.log {
		if c.Method == method && strings.HasSuffix(c.Endpoint, suffix) {
			res = append(res, c)
		}
	}
	return res
}

func (a *advGH) count(method, suffix string) int { return len(a.callsTo(method, suffix)) }

func (a *advGH) snap(n int) *fakePR {
	a.inner.mu.Lock()
	defer a.inner.mu.Unlock()
	p := a.inner.prs[n]
	if p == nil {
		return nil
	}
	c := *p
	return &c
}

func (a *advGH) mutate(n int, fn func(p *fakePR)) {
	a.inner.mu.Lock()
	defer a.inner.mu.Unlock()
	fn(a.inner.prs[n])
}

func (a *advGH) addBranch(b string) {
	a.inner.mu.Lock()
	defer a.inner.mu.Unlock()
	a.branches[b] = true
}

func (a *advGH) mergedInto() map[int]string {
	a.inner.mu.Lock()
	defer a.inner.mu.Unlock()
	res := map[int]string{}
	for n, p := range a.inner.prs {
		if p.Merged {
			res[n] = p.MergedInto
		}
	}
	return res
}

func (a *advGH) bases() map[int]string {
	a.inner.mu.Lock()
	defer a.inner.mu.Unlock()
	res := map[int]string{}
	for n, p := range a.inner.prs {
		res[n] = p.BaseRef
	}
	return res
}

// ---- GitHub behaviours ----

func (a *advGH) settle() {
	f := a.inner
	f.mu.Lock()
	defer f.mu.Unlock()
	for n, to := range f.pendingRetarget {
		delete(f.pendingRetarget, n)
		switch {
		case a.arriveAfter < 0:
			// GitHub's retarget never happens
		case a.arriveAfter == 0:
			if p := f.prs[n]; p != nil && p.State == "open" {
				p.BaseRef = to
			}
		default:
			a.pending[n] = &advPending{to: to}
		}
	}
	for n, p := range f.prs {
		if p.Merged && !a.seenMerged[n] {
			a.seenMerged[n] = true
			if f.deleteBranchOnMerge {
				delete(a.branches, p.HeadRef)
			}
		}
	}
}

// landLocked applies GitHub's own retarget of n (f.mu held)
func (a *advGH) landLocked(n int) {
	if pd := a.pending[n]; pd != nil {
		if p := a.inner.prs[n]; p != nil && p.State == "open" {
			p.BaseRef = pd.to
		}
		delete(a.pending, n)
	}
}

func (a *advGH) markMerged(n int, into string) {
	a.inner.mu.Lock()
	defer a.inner.mu.Unlock()
	p := a.inner.prs[n]
	p.Merged, p.State, p.MergedInto = true, "closed", into
	a.inner.afterMerge(p, into)
}

func (a *advGH) methodRefusal(method interface{}) string {
	s := a.inner.settings
	if s == nil {
		return ""
	}
	switch method {
	case "merge":
		if s.Merge != nil && !*s.Merge {
			return "Merge commits are not allowed on this repository."
		}
	case "squash":
		if s.Squash != nil && !*s.Squash {
			return "Squash merges are not allowed on this repository."
		}
	case "rebase":
		if s.Rebase != nil && !*s.Rebase {
			return "Rebase merges are not allowed on this repository."
		}
	}
	return ""
}

func (a *advGH) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	c := restCall{Method: method, Endpoint: endpoint, Parameters: parameters, Body: body}
	a.mu.Lock()
	a.log = append(a.log, c)
	before := a.before
	a.mu.Unlock()
	if before != nil {
		before(a, c)
	}
	res, err := a.handle(ctx, c, githubToken)
	a.settle()
	return res, err
}

func (a *advGH) handle(ctx context.Context, c restCall, tok *string) ([]byte, error) {
	endpoint, parameters, method, body := c.Endpoint, c.Parameters, c.Method, c.Body
	pass := func() ([]byte, error) { return a.inner.CallRestAPI(ctx, endpoint, parameters, method, body, tok) }

	switch {
	case method == "GET" && endpoint == "/repos/myorg/goliac/pulls":
		q, _ := url.ParseQuery(parameters)
		perPage, page := 30, 1
		if v := q.Get("per_page"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return httpError("422 Unprocessable Entity", `{"message":"Validation Failed"}`)
			}
			perPage = n
			if perPage > 100 {
				perPage = 100
			}
		}
		if v := q.Get("page"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return httpError("422 Unprocessable Entity", `{"message":"Validation Failed"}`)
			}
			page = n
		}
		_ = page
		if q.Get("sort") == "" {
			// GitHub default: sort=created, direction=desc
			q.Set("sort", "created")
			q.Set("direction", "desc")
		}
		a.mu.Lock()
		if perPage > a.maxPage {
			a.maxPage = perPage
		}
		a.mu.Unlock()
		// (the inner fake paginates: 30 per page by default, 100 at most)
		return a.inner.CallRestAPI(ctx, endpoint, q.Encode(), "GET", nil, tok)

	case method == "GET" && pullEndpoint.MatchString(endpoint):
		n, _ := strconv.Atoi(pullEndpoint.FindStringSubmatch(endpoint)[1])
		a.inner.mu.Lock()
		if pd := a.pending[n]; pd != nil {
			pd.reads++
			if pd.reads > a.arriveAfter {
				a.landLocked(n)
			}
		}
		a.inner.mu.Unlock()
		return pass()

	case method == "PATCH" && pullEndpoint.MatchString(endpoint):
		n, _ := strconv.Atoi(pullEndpoint.FindStringSubmatch(endpoint)[1])
		a.inner.mu.Lock()
		a.landLocked(n) // GitHub's retarget lands just before the PATCH
		a.inner.mu.Unlock()
		p := a.snap(n)
		if p == nil {
			return httpError("404 Not Found", `{"message":"Not Found"}`)
		}
		if base, ok := body["base"].(string); ok {
			switch {
			case p.State != "open" || p.Merged:
				return httpError("422 Unprocessable Entity", `{"message":"Validation Failed","errors":[{"message":"Cannot change the base branch of a closed pull request.","field":"base"}]}`) // assumed
			case base == p.BaseRef:
				return httpError("422 Unprocessable Entity", fmt.Sprintf(`{"message":"Validation Failed","errors":[{"message":"A pull request already exists for base branch '%s' and head branch '%s'","resource":"PullRequest","field":"base","code":"invalid"}]}`, base, p.HeadRef))
			case base == p.HeadRef:
				return httpError("422 Unprocessable Entity", `{"message":"Validation Failed"}`)
			}
			a.inner.mu.Lock()
			exists := a.branches[base]
			a.inner.mu.Unlock()
			if !exists {
				return httpError("422 Unprocessable Entity", fmt.Sprintf(`{"message":"Validation Failed","errors":[{"message":"Proposed base branch '%s' was not found","field":"base"}]}`, base))
			}
		}
		return pass()

	case method == "PUT" && syncPath.MatchString(endpoint):
		n, _ := strconv.Atoi(syncPath.FindStringSubmatch(endpoint)[1])
		p := a.snap(n)
		if p == nil {
			return httpError("404 Not Found", `{"message":"Not Found"}`)
		}
		if p.Merged || p.State != "open" || p.Draft {
			return httpError("405 Method Not Allowed", `{"message":"Pull Request is not mergeable"}`)
		}
		if sha, ok := body["sha"].(string); ok && sha != p.HeadSha {
			return httpError("409 Conflict", `{"message":"Head branch was modified. Review and try the merge again."}`)
		}
		if m := a.methodRefusal(body["merge_method"]); m != "" {
			return httpError("405 Method Not Allowed", fmt.Sprintf(`{"message":%q}`, m))
		}
		res, err := pass()
		if err == nil {
			a.markMerged(n, p.BaseRef)
		}
		return res, err

	case method == "PUT" && asyncPath.MatchString(endpoint):
		n, _ := strconv.Atoi(asyncPath.FindStringSubmatch(endpoint)[1])
		p := a.snap(n)
		if p == nil {
			return httpError("404 Not Found", `{"message":"Not Found"}`)
		}
		if p.Merged || p.State != "open" || p.Draft {
			return httpError("400 Bad Request", `{"status":"failed","details":{"message":"Pull request is closed, merged or a draft."}}`) // assumed shape
		}
		if m := a.methodRefusal(body["merge_method"]); m != "" {
			return httpError("405 Method Not Allowed", fmt.Sprintf(`{"message":%q}`, m)) // assumed
		}
		if sha, ok := body["sha"].(string); ok && sha != p.HeadSha {
			return []byte(`{"status":"failed","details":{"message":"Head branch was modified. Review and try the merge again."}}`), nil // assumed
		}
		return pass()

	case method == "POST" && endpoint == "/repos/myorg/goliac/merges":
		a.inner.mu.Lock()
		exists := a.branches[body["base"].(string)]
		a.inner.mu.Unlock()
		if !exists {
			return httpError("404 Not Found", `{"message":"Base does not exist"}`)
		}
		res, err := pass()
		if err == nil && len(strings.TrimSpace(string(res))) > 0 {
			base, head := body["base"].(string), body["head"].(string)
			a.inner.mu.Lock()
			target := 0
			for _, n := range a.inner.sortedNumbers() {
				p := a.inner.prs[n]
				if p.State == "open" && p.HeadSha == head && p.BaseRef == base {
					target = n
				}
			}
			a.inner.mu.Unlock()
			if target != 0 {
				a.markMerged(target, base)
			}
		}
		return res, err
	}
	return pass()
}

// ---- fixtures ----

// chainFixture returns the plain (stack: null) chain n > n-1 > ... > 1 > base: PR i has head h<i> and sha s<i>
func chainFixture(n int, base string, headFmt string) map[int]*fakePR {
	prs := map[int]*fakePR{}
	for i := 1; i <= n; i++ {
		b := base
		if i > 1 {
			b = fmt.Sprintf(headFmt, i-1)
		}
		prs[i] = &fakePR{Number: i, Title: fmt.Sprintf("PR %d", i), Author: fmt.Sprintf("a%d", i), BaseRef: b, HeadRef: fmt.Sprintf(headFmt, i), HeadSha: fmt.Sprintf("sha%d", i), State: "open"}
	}
	return prs
}

