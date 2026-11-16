package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goliac-project/goliac/internal/config"
	"github.com/stretchr/testify/assert"
)

type restCall struct {
	Method     string
	Endpoint   string
	Parameters string
	Body       map[string]interface{}
}

// httpError mimics GitHubClientImpl.CallRestAPI on a non-2xx answer
func httpError(status string, body string) ([]byte, error) {
	return []byte(body), fmt.Errorf("unexpected status: %s", status)
}

type fakePR struct {
	Number  int
	Title   string
	Author  string
	BaseRef string
	HeadRef string
	HeadSha string
	State   string // open, closed
	Merged  bool
	Draft   bool
	// head repository: "" = myorg/goliac, "-" = null (deleted fork)
	HeadRepo string
	// stack info (StackSize 0 when the PR is not stacked)
	StackPosition int
	StackSize     int
	StackBase     string
	// raw stack payload overriding the above
	RawStack interface{}
	// the branch the PR landed in when GitHub merged it ("" while not merged by the fake)
	MergedInto string
}

// githubStacked is true when GitHub itself considers the PR part of a stack (non null stack payload)
func (p *fakePR) githubStacked() bool {
	return p.StackSize > 0 || p.RawStack != nil
}

func (p *fakePR) payload() map[string]interface{} {
	var headRepo interface{} = map[string]interface{}{"full_name": "myorg/goliac"}
	switch p.HeadRepo {
	case "":
	case "-":
		headRepo = nil
	default:
		headRepo = map[string]interface{}{"full_name": p.HeadRepo}
	}
	m := map[string]interface{}{
		"number":   p.Number,
		"title":    p.Title,
		"html_url": fmt.Sprintf("https://github.com/myorg/goliac/pull/%d", p.Number),
		"state":    p.State,
		"merged":   p.Merged,
		"draft":    p.Draft,
		"user":     map[string]interface{}{"login": p.Author},
		"base":     map[string]interface{}{"ref": p.BaseRef, "repo": map[string]interface{}{"full_name": "myorg/goliac"}},
		"head":     map[string]interface{}{"ref": p.HeadRef, "sha": p.HeadSha, "repo": headRepo},
		"stack":    nil,
	}
	if p.StackSize > 0 {
		m["stack"] = map[string]interface{}{
			"position": p.StackPosition,
			"size":     p.StackSize,
			"base":     map[string]interface{}{"ref": p.StackBase},
		}
	}
	if p.RawStack != nil {
		m["stack"] = p.RawStack
	}
	return m
}

// headLabel is the owner:ref GitHub matches the head filter of GET pulls against
func (p *fakePR) headLabel() string {
	switch p.HeadRepo {
	case "":
		return "myorg:" + p.HeadRef
	case "-":
		return "" // deleted fork: never matched
	}
	return strings.SplitN(p.HeadRepo, "/", 2)[0] + ":" + p.HeadRef
}

// fakeClock is a manual clock: Sleep advances the time, nothing really sleeps
type fakeClock struct {
	mu     sync.Mutex
	t      time.Time
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
	c.sleeps = append(c.sleeps, d)
	return nil
}

func (c *fakeClock) slept() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration{}, c.sleeps...)
}

/*
 * fakeGitHub is a small, goroutine-safe, strict in-memory GitHub REST API for the force merge.
 * Every request body and query is validated against the documented REST schemas
 * (https://docs.github.com/en/rest/pulls): an unknown field, a wrong type or a bad enum answers 422
 * and is recorded as a violation that fails the test.
 * Merge endpoints are answered by the test hooks (default: unexpected call, 500).
 */
type fakeGitHub struct {
	mu    sync.Mutex
	t     *testing.T
	prs   map[int]*fakePR
	calls []restCall

	// repository settings (nil = GET /repos/{o}/{r} fails with 500)
	settings *struct{ Merge, Squash, Rebase *bool }
	// raw body for GET /repos/{o}/{r} when set
	settingsRaw string
	// default_branch of GET /repos/{o}/{r} ("" = main)
	defaultBranch string

	syncMerge   func(call restCall) ([]byte, error)
	branchMerge func(call restCall) ([]byte, error)
	asyncMerge  func(call restCall) ([]byte, error)
	asyncPoll   func(call restCall) ([]byte, error)
	// onCall runs before a call is answered (it may change the state); a non nil error answers the call
	onCall func(f *fakeGitHub, call restCall) ([]byte, error)

	// markMergedOnBranchMerge simulates GitHub marking the PR merged once its head is in its base
	markMergedOnBranchMerge bool
	// neverMarkMerged: the asynchronous merge says merged but the PR stays open
	neverMarkMerged bool

	reviewSeq  int64
	violations []string
}

var (
	pullEndpoint   = regexp.MustCompile(`^/repos/myorg/goliac/pulls/(\d+)$`)
	reviewEndpoint = regexp.MustCompile(`^/repos/myorg/goliac/pulls/(\d+)/reviews$`)
	dismissal      = regexp.MustCompile(`^/repos/myorg/goliac/pulls/(\d+)/reviews/(\d+)/dismissals$`)
	commentPath    = regexp.MustCompile(`^/repos/myorg/goliac/issues/(\d+)/comments$`)
	asyncPath      = regexp.MustCompile(`^/repos/myorg/goliac/pulls/(\d+)/merge-async$`)
	asyncPollPath  = regexp.MustCompile(`^/repos/myorg/goliac/pulls/(\d+)/merge-async/([\w-]+)$`)
	syncPath       = regexp.MustCompile(`^/repos/myorg/goliac/pulls/(\d+)/merge$`)
)

func newFake(t *testing.T, prs map[int]*fakePR) *fakeGitHub {
	f := &fakeGitHub{t: t, prs: prs}
	t.Cleanup(func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		assert.Empty(t, f.violations, "requests violating the GitHub REST schemas")
	})
	return f
}

// allowSquashOnly makes the repository squash-only (like alaya-goliac-teams#615)
func (f *fakeGitHub) squashOnly() *fakeGitHub {
	yes, no := true, false
	f.settings = &struct{ Merge, Squash, Rebase *bool }{&no, &yes, &no}
	return f
}

func (f *fakeGitHub) allMethods() *fakeGitHub {
	yes := true
	f.settings = &struct{ Merge, Squash, Rebase *bool }{&yes, &yes, &yes}
	return f
}

// schema checks: key -> allowed type ("string", "bool", or an enum "enum:a|b")
func checkSchema(body map[string]interface{}, schema map[string]string, required ...string) []string {
	res := []string{}
	for k, v := range body {
		typ, ok := schema[k]
		if !ok {
			res = append(res, "unknown field "+k)
			continue
		}
		switch {
		case typ == "string":
			if _, ok := v.(string); !ok {
				res = append(res, k+" must be a string")
			}
		case typ == "bool":
			if _, ok := v.(bool); !ok {
				res = append(res, k+" must be a boolean")
			}
		case strings.HasPrefix(typ, "enum:"):
			s, ok := v.(string)
			if !ok || !strings.Contains("|"+strings.TrimPrefix(typ, "enum:")+"|", "|"+s+"|") {
				res = append(res, fmt.Sprintf("%s must be one of %s (got %v)", k, typ, v))
			}
		}
	}
	for _, r := range required {
		if _, ok := body[r]; !ok {
			res = append(res, "missing required field "+r)
		}
	}
	return res
}

func (f *fakeGitHub) validateRequest(call restCall) []string {
	switch {
	case call.Method == "POST" && reviewEndpoint.MatchString(call.Endpoint):
		return checkSchema(call.Body, map[string]string{"commit_id": "string", "body": "string", "event": "enum:APPROVE|REQUEST_CHANGES|COMMENT"}, "event")
	case call.Method == "PUT" && dismissal.MatchString(call.Endpoint):
		return checkSchema(call.Body, map[string]string{"message": "string", "event": "enum:DISMISS"}, "message")
	case call.Method == "PUT" && syncPath.MatchString(call.Endpoint):
		return checkSchema(call.Body, map[string]string{"commit_title": "string", "commit_message": "string", "sha": "string", "merge_method": "enum:merge|squash|rebase"})
	case call.Method == "PUT" && asyncPath.MatchString(call.Endpoint):
		return checkSchema(call.Body, map[string]string{"commit_title": "string", "commit_message": "string", "sha": "string", "merge_method": "enum:merge|squash|rebase", "merge_action": "enum:default|direct_merge|merge_queue", "bypass_rules": "bool"})
	case call.Method == "POST" && call.Endpoint == "/repos/myorg/goliac/merges":
		return checkSchema(call.Body, map[string]string{"base": "string", "head": "string", "commit_message": "string"}, "base", "head")
	case call.Method == "PATCH" && pullEndpoint.MatchString(call.Endpoint):
		return checkSchema(call.Body, map[string]string{"title": "string", "body": "string", "state": "enum:open|closed", "base": "string", "maintainer_can_modify": "bool"})
	case call.Method == "POST" && commentPath.MatchString(call.Endpoint):
		return checkSchema(call.Body, map[string]string{"body": "string"}, "body")
	case call.Method == "GET":
		if call.Body != nil {
			return []string{"GET with a body"}
		}
		if call.Endpoint == "/repos/myorg/goliac/pulls" {
			q, err := url.ParseQuery(call.Parameters)
			if err != nil {
				return []string{"bad query"}
			}
			res := []string{}
			for k, v := range q {
				switch k {
				case "state":
					if v[0] != "open" && v[0] != "closed" && v[0] != "all" {
						res = append(res, "bad state")
					}
				case "head":
					if !strings.Contains(v[0], ":") {
						res = append(res, "head must be user:ref-name")
					}
				case "base", "sort", "direction", "per_page", "page":
				default:
					res = append(res, "unknown query "+k)
				}
			}
			return res
		}
	}
	return nil
}

func (f *fakeGitHub) CallRestAPI(ctx context.Context, endpoint, parameters, method string, body map[string]interface{}, githubToken *string) ([]byte, error) {
	f.mu.Lock()
	call := restCall{Method: method, Endpoint: endpoint, Parameters: parameters, Body: body}
	f.calls = append(f.calls, call)
	if v := f.validateRequest(call); len(v) > 0 {
		f.violations = append(f.violations, fmt.Sprintf("%s %s: %s", method, endpoint, strings.Join(v, ", ")))
		f.mu.Unlock()
		return httpError("422 Unprocessable Entity", `{"message":"Validation Failed"}`)
	}
	onCall := f.onCall
	f.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if onCall != nil {
		if res, err := onCall(f, call); err != nil || res != nil {
			return res, err
		}
	}

	f.mu.Lock()
	// hooks run unlocked (they may call back into the fake state through the helpers)
	syncMerge, branchMerge, asyncMerge, asyncPoll := f.syncMerge, f.branchMerge, f.asyncMerge, f.asyncPoll
	f.mu.Unlock()

	unexpected := func() ([]byte, error) {
		return httpError("500 Internal Server Error", fmt.Sprintf(`{"message":"unexpected call %s %s"}`, method, endpoint))
	}
	hook := func(h func(call restCall) ([]byte, error)) ([]byte, error) {
		if h == nil {
			return unexpected()
		}
		return h(call)
	}

	switch {
	case method == "GET" && endpoint == "/repos/myorg/goliac":
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.settingsRaw != "" {
			return []byte(f.settingsRaw), nil
		}
		if f.settings == nil {
			return httpError("500 Internal Server Error", `{"message":"no settings"}`)
		}
		m := map[string]interface{}{"default_branch": "main"}
		if f.defaultBranch != "" {
			m["default_branch"] = f.defaultBranch
		}
		if f.settings.Merge != nil {
			m["allow_merge_commit"] = *f.settings.Merge
		}
		if f.settings.Squash != nil {
			m["allow_squash_merge"] = *f.settings.Squash
		}
		if f.settings.Rebase != nil {
			m["allow_rebase_merge"] = *f.settings.Rebase
		}
		return json.Marshal(m)

	case method == "GET" && pullEndpoint.MatchString(endpoint):
		f.mu.Lock()
		defer f.mu.Unlock()
		n, _ := strconv.Atoi(pullEndpoint.FindStringSubmatch(endpoint)[1])
		pr, ok := f.prs[n]
		if !ok {
			return httpError("404 Not Found", `{"message":"Not Found"}`)
		}
		return json.Marshal(pr.payload())

	case method == "GET" && endpoint == "/repos/myorg/goliac/pulls":
		f.mu.Lock()
		defer f.mu.Unlock()
		q, _ := url.ParseQuery(parameters)
		res := []map[string]interface{}{}
		for _, n := range f.sortedNumbers() {
			pr := f.prs[n]
			if q.Get("state") == "open" && pr.State != "open" {
				continue
			}
			if b := q.Get("base"); b != "" && pr.BaseRef != b {
				continue
			}
			if h := q.Get("head"); h != "" && pr.headLabel() != h {
				continue
			}
			res = append(res, pr.payload())
		}
		return json.Marshal(res)

	case method == "POST" && reviewEndpoint.MatchString(endpoint):
		f.mu.Lock()
		defer f.mu.Unlock()
		f.reviewSeq++
		return []byte(fmt.Sprintf(`{"id":%d,"state":"APPROVED"}`, 1000+f.reviewSeq)), nil

	case method == "PUT" && dismissal.MatchString(endpoint):
		return []byte(`{}`), nil

	case method == "POST" && commentPath.MatchString(endpoint):
		return []byte(`{}`), nil

	case method == "PATCH" && pullEndpoint.MatchString(endpoint):
		f.mu.Lock()
		defer f.mu.Unlock()
		n, _ := strconv.Atoi(pullEndpoint.FindStringSubmatch(endpoint)[1])
		pr := f.prs[n]
		if pr == nil {
			return httpError("404 Not Found", `{"message":"Not Found"}`)
		}
		if base, ok := body["base"].(string); ok {
			pr.BaseRef = base
		}
		if state, ok := body["state"].(string); ok {
			pr.State = state
		}
		return json.Marshal(pr.payload())

	case method == "PUT" && syncPath.MatchString(endpoint):
		return hook(syncMerge)

	case method == "POST" && endpoint == "/repos/myorg/goliac/merges":
		res, err := hook(branchMerge)
		if err == nil && f.markMergedOnBranchMerge {
			f.mu.Lock()
			for _, pr := range f.prs {
				if pr.HeadSha == body["head"] {
					pr.Merged = true
					pr.State = "closed"
					pr.MergedInto, _ = body["base"].(string)
				}
			}
			f.mu.Unlock()
		}
		return res, err

	case method == "PUT" && asyncPath.MatchString(endpoint):
		res, err := hook(asyncMerge)
		f.noteAsync(endpoint, res, err)
		return res, err

	case method == "GET" && asyncPollPath.MatchString(endpoint):
		res, err := hook(asyncPoll)
		f.noteAsync(strings.Replace(endpoint, "/merge-async/"+asyncPollPath.FindStringSubmatch(endpoint)[2], "/merge-async", 1), res, err)
		return res, err
	}
	return unexpected()
}

/*
 * noteAsync applies an asynchronous merge that GitHub says is merged, like GitHub does
 * (https://docs.github.com/en/rest/pulls/pulls#merge-a-pull-request-asynchronously):
 * "For a stacked pull request, the operation includes all open downstack pull requests."
 * Only a PR GitHub itself considers stacked (non null stack payload) takes its open downstack
 * PRs along, and all of them land in the base branch of the bottom one. Any other PR is
 * merged alone into its own base branch, even when it is based on another PR's branch.
 */
func (f *fakeGitHub) noteAsync(endpoint string, res []byte, err error) {
	if err != nil || f.neverMarkMerged || !strings.Contains(string(res), `"status":"merged"`) {
		return
	}
	n, _ := strconv.Atoi(asyncPath.FindStringSubmatch(endpoint)[1])
	f.mu.Lock()
	defer f.mu.Unlock()
	top := f.prs[n]
	if top == nil {
		return
	}
	merged := []*fakePR{top}
	if top.githubStacked() {
		cur := top
		for {
			var next *fakePR
			for _, p := range f.prs {
				if p.HeadRef == cur.BaseRef && p.State == "open" && p.githubStacked() {
					next = p
				}
			}
			if next == nil {
				break
			}
			merged = append(merged, next)
			cur = next
		}
	}
	into := merged[len(merged)-1].BaseRef
	for _, p := range merged {
		p.Merged = true
		p.State = "closed"
		p.MergedInto = into
	}
}

func (f *fakeGitHub) sortedNumbers() []int {
	numbers := []int{}
	for n := range f.prs {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	return numbers
}

func (f *fakeGitHub) find(method, endpointSuffix string) []restCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	res := []restCall{}
	for _, c := range f.calls {
		if c.Method == method && strings.HasSuffix(c.Endpoint, endpointSuffix) {
			res = append(res, c)
		}
	}
	return res
}

// sideEffects counts the calls that change something
func (f *fakeGitHub) sideEffects() []restCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	res := []restCall{}
	for _, c := range f.calls {
		if c.Method != "GET" {
			res = append(res, c)
		}
	}
	return res
}

func (f *fakeGitHub) reviewedPRs() []string {
	res := []string{}
	for _, c := range f.find("POST", "/reviews") {
		res = append(res, strings.TrimSuffix(strings.TrimPrefix(c.Endpoint, "/repos/myorg/goliac/pulls/"), "/reviews"))
	}
	return res
}

func (f *fakeGitHub) pr(n int) fakePR {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *f.prs[n]
}

func (f *fakeGitHub) update(n int, fn func(p *fakePR)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f.prs[n])
}

/*
 * fixtureStack returns the stack 1 > 2 > 3 > 4 > main:
 * PR 1 is the top (based on 2's branch), PR 4 the bottom (based on main).
 */
func fixtureStack() map[int]*fakePR {
	prs := map[int]*fakePR{}
	for n := 1; n <= 4; n++ {
		base := fmt.Sprintf("b%d", n+1)
		if n == 4 {
			base = "main"
		}
		prs[n] = &fakePR{
			Number:        n,
			Title:         fmt.Sprintf("PR %d", n),
			Author:        fmt.Sprintf("author%d", n),
			BaseRef:       base,
			HeadRef:       fmt.Sprintf("b%d", n),
			HeadSha:       fmt.Sprintf("sha%d", n),
			State:         "open",
			StackPosition: 5 - n,
			StackSize:     4,
			StackBase:     "main",
		}
	}
	return prs
}

func fixturePlainPR() map[int]*fakePR {
	return map[int]*fakePR{
		32: {Number: 32, Title: "Plain PR", Author: "someone", BaseRef: "main", HeadRef: "feature", HeadSha: "aaa111", State: "open"},
	}
}

func newForcemergeForTest(t *testing.T, gh *fakeGitHub) (*ForcemergeImpl, *fakeClock) {
	previousOrg := config.Config.GithubAppOrganization
	config.Config.GithubAppOrganization = "myorg"
	t.Cleanup(func() { config.Config.GithubAppOrganization = previousOrg })

	clock := &fakeClock{t: time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)}
	return &ForcemergeImpl{
		ws:                 NewWorkflowService("myorg", nil, nil, gh),
		stepPlugins:        map[string]StepPlugin{},
		asyncPollInterval:  time.Second,
		asyncPollTimeout:   10 * time.Second,
		mergedCheckTimeout: 5 * time.Second,
		now:                clock.Now,
		sleep:              clock.Sleep,
	}, clock
}

func prURL(n int) string {
	return fmt.Sprintf("https://github.com/myorg/goliac/pull/%d", n)
}

const (
	asyncPending = `{"status":"pending","details":{"message":"merge requested","uuid":"uuid-1","merge_method":"merge","merge_action":"direct_merge","expected_head_sha":"sha"}}`
	asyncMerged  = `{"status":"merged","details":{"message":"merged","sha":"mergecommit"}}`
)

func okJSON(s string) func(call restCall) ([]byte, error) {
	return func(call restCall) ([]byte, error) { return []byte(s), nil }
}

func boolp(b bool) *bool { return &b }
