package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goliac-project/goliac/internal/config"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type ForcemergeImpl struct {
	ws          WorkflowService
	stepPlugins map[string]StepPlugin
	// bounds for the asynchronous merge poll (0 = defaults)
	asyncPollInterval time.Duration
	asyncPollTimeout  time.Duration
	// how long to wait for GitHub to mark a PR merged (0 = default)
	mergedCheckTimeout time.Duration
	// restack: number of attempts per PR and first backoff (0 = defaults, the backoff doubles)
	restackAttempts int
	restackBackoff  time.Duration
	// longest stack inferred from a chain of PRs when GitHub sends no stack metadata (0 = default)
	maxInferredStackSize int
	// injectable clock (nil = real time)
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error

	// PRs with a force merge in flight ("repo#number")
	mu       sync.Mutex
	inflight map[string]bool
}

const (
	// PropertyMergeMode is the workflow property holding the optional merge mode of a force merge
	// ("/forcemerge:<workflow>:<stack|direct>:<explanation>")
	PropertyMergeMode = "merge_mode"
	// MergeModeDirect merges a stacked PR alone into its base branch and restacks the PRs above it (default)
	MergeModeDirect = "direct"
	// MergeModeStack merges the whole stack the PR belongs to into the stack base branch
	MergeModeStack = "stack"
)

const (
	defaultAsyncMergePollInterval = 2 * time.Second
	defaultAsyncMergePollTimeout  = 2 * time.Minute
	defaultMergedCheckTimeout     = 30 * time.Second
	defaultRestackAttempts        = 4
	defaultRestackBackoff         = time.Second
	// longest chain of PRs walked when a stack is inferred from base branches
	defaultMaxInferredStackSize = 50
)

func NewForcemergeImpl(ws WorkflowService) Workflow {
	return &ForcemergeImpl{
		ws:          ws,
		stepPlugins: GetPlugins(),
	}
}

func (g *ForcemergeImpl) clockNow() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

func (g *ForcemergeImpl) clockSleep(ctx context.Context, d time.Duration) error {
	if g.sleep != nil {
		return g.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// lock marks the PRs as being force merged; a second force merge of one of them
// (e.g. a redelivered webhook) is refused while the first one runs
func (g *ForcemergeImpl) lock(repo string, numbers ...int) (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inflight == nil {
		g.inflight = map[string]bool{}
	}
	keys := []string{}
	for _, n := range numbers {
		k := fmt.Sprintf("%s#%d", repo, n)
		if g.inflight[k] {
			return nil, fmt.Errorf("a force merge of PR #%d is already running", n)
		}
		keys = append(keys, k)
	}
	for _, k := range keys {
		g.inflight[k] = true
	}
	return func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		for _, k := range keys {
			delete(g.inflight, k)
		}
	}, nil
}

func (g *ForcemergeImpl) ExecuteWorkflow(ctx context.Context, repoconfigForceMergeworkflows []string, username, workflowName, explanation string, properties map[string]string, dryrun bool) ([]string, error) {
	var childSpan trace.Span
	if config.Config.OpenTelemetryEnabled {
		ctx, childSpan = otel.Tracer("goliac").Start(ctx, "ExecuteWorkflow")
		defer childSpan.End()
		childSpan.SetAttributes(
			attribute.String("workflow_name", workflowName),
			attribute.String("pr_path_to_merge", properties["pr_url"]),
		)
	}

	pr_url := properties["pr_url"]
	prPathToMerge := strings.TrimSpace(pr_url)

	// check the prToMerge
	if prPathToMerge == "" {
		return nil, fmt.Errorf("prPathToMerge is empty")
	}
	url, err := url.Parse(prPathToMerge)
	if err != nil {
		return nil, fmt.Errorf("prPathToMerge is not a valid URL")
	}
	// let's extract the PR number and the repo
	prExtract := regexp.MustCompile(`.*/([^/]*)/pull/(\d+)`)
	prMatch := prExtract.FindStringSubmatch(url.Path)
	if len(prMatch) != 3 {
		return nil, fmt.Errorf("prPathToMerge is not a valid PR URL")
	}
	repo := prMatch[1]
	prNumber := prMatch[2]

	mergeMode := strings.ToLower(strings.TrimSpace(properties[PropertyMergeMode]))
	if mergeMode != "" && mergeMode != MergeModeDirect && mergeMode != MergeModeStack {
		return nil, fmt.Errorf("unknown merge mode %q (expected %q or %q)", mergeMode, MergeModeStack, MergeModeDirect)
	}

	// check workflow and acl
	w, err := g.ws.GetWorkflow(ctx, repoconfigForceMergeworkflows, workflowName, repo, username)
	if err != nil {
		return nil, fmt.Errorf("unable to execute the workflow: %v", err)
	}

	// execute the workflow
	if dryrun {
		return nil, nil
	}

	// refuse a second force merge of the same PR while one is running (redelivered webhook)
	number, _ := strconv.Atoi(prNumber)
	unlock, err := g.lock(repo, number)
	if err != nil {
		return nil, fmt.Errorf("error when merging the PR: %v", err)
	}
	defer unlock()

	// validate everything first: nothing is changed, approved or notified when the PR (or its stack) can't be merged
	plan, err := g.planMerge(ctx, username, repo, prNumber, prPathToMerge, explanation, mergeMode)
	if err != nil {
		return nil, fmt.Errorf("error when merging the PR: %v", err)
	}
	// a force merge also owns the other PRs it touches (the stack, or the parent and the PRs to retarget)
	others := []int{}
	for _, p := range plan.lockedPRs() {
		if p.Number != number {
			others = append(others, p.Number)
		}
	}
	if len(others) > 0 {
		unlockOthers, err := g.lock(repo, others...)
		if err != nil {
			return nil, fmt.Errorf("error when merging the PR: %v", err)
		}
		defer unlockOthers()
	}

	// the steps (audit, notification...) run once per merged PR, before the first side effect
	responses := []string{}
	for _, mergedPR := range plan.mergedPRs() {
		prURL, perr := url.Parse(mergedPR.HTMLURL)
		if perr != nil {
			return nil, fmt.Errorf("error when merging the PR: invalid URL %q for PR #%d", mergedPR.HTMLURL, mergedPR.Number)
		}
		for _, step := range w.Spec.Steps {
			plugin := g.stepPlugins[step.Name]
			if plugin == nil {
				return nil, fmt.Errorf("plugin %s not found", step.Name)
			}
			resp, err := plugin.Execute(ctx, username, w.Spec.Description, explanation, prURL, step.Properties)
			if err != nil {
				return nil, fmt.Errorf("error when executing step %s: %v", step.Name, err)
			}
			responses = append(responses, resp)
		}
	}

	// merge the PR
	summary, err := g.executePlan(ctx, plan)
	if err != nil {
		return nil, fmt.Errorf("error when merging the PR: %v", err)
	}
	if summary != "" {
		responses = append(responses, summary)
	}

	return responses, nil
}

// pullRequestInfo is the subset of the GitHub pull request payload the force merge needs
type pullRequestInfo struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Merged  bool   `json:"merged"`
	Draft   bool   `json:"draft"`
	User    struct {
		Login string `json:"login"`
	} `json:"user"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Head struct {
		Ref  string `json:"ref"`
		Sha  string `json:"sha"`
		Repo *struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	// Stack is set by GitHub when the PR belongs to a stack of pull requests (null otherwise).
	// When GitHub sends no stack, Goliac may infer one from the chain of PRs (see inferStack).
	Stack *prStack `json:"stack"`

	// stackInferred: Stack was inferred by Goliac from the base branches, GitHub sent none
	stackInferred bool
}

type prStack struct {
	Position int `json:"position"`
	Size     int `json:"size"`
	Base     struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (pr *pullRequestInfo) isStacked() bool {
	return pr.Stack != nil
}

// stackSource says where the stack of a stacked PR comes from (for the approval review)
func (pr *pullRequestInfo) stackSource() string {
	if pr.stackInferred {
		return "Stack source: inferred from the chain of open PRs (GitHub sent no stack metadata)."
	}
	return "Stack source: GitHub stack metadata."
}

// isStackBottom is true when the PR targets the base branch of its stack
// (i.e. no other open PR is below it)
func (pr *pullRequestInfo) isStackBottom() bool {
	return pr.Stack != nil && pr.Base.Ref == pr.Stack.Base.Ref
}

func (pr *pullRequestInfo) author() string {
	if pr.User.Login == "" {
		return "unknown"
	}
	return pr.User.Login
}

// sameStack is true when other carries stack metadata with the same stack base branch.
// For a stack inferred from the base branches, the other PRs of the chain carry no stack metadata.
func (pr *pullRequestInfo) sameStack(other *pullRequestInfo) bool {
	if pr.Stack == nil {
		return false
	}
	if pr.stackInferred {
		return other.Stack == nil || other.stackInferred
	}
	return other.Stack != nil && pr.Stack.Base.Ref == other.Stack.Base.Ref
}

// validate refuses what can't be force merged: merged, closed or draft PRs,
// PRs whose head is not in the repository (fork or deleted fork) and malformed stack payloads.
// A PR of a stack must be explicitly open; for a plain PR only an explicit closed state is refused.
func (pr *pullRequestInfo) validate(repo string) error {
	if pr.Merged {
		return fmt.Errorf("PR #%d is already merged", pr.Number)
	}
	if pr.State == "closed" {
		return fmt.Errorf("PR #%d is closed", pr.Number)
	}
	if pr.Draft {
		return fmt.Errorf("PR #%d is a draft", pr.Number)
	}
	if pr.Stack != nil {
		if pr.State != "open" {
			return fmt.Errorf("PR #%d of a stack is not open (state %q)", pr.Number, pr.State)
		}
		if pr.Stack.Size < 1 || pr.Stack.Base.Ref == "" || pr.Base.Ref == "" || pr.Head.Ref == "" {
			return fmt.Errorf("PR #%d has a malformed stack payload (size %d, stack base %q, base %q, head %q)", pr.Number, pr.Stack.Size, pr.Stack.Base.Ref, pr.Base.Ref, pr.Head.Ref)
		}
	}
	if pr.Head.Ref != "" || pr.Head.Sha != "" {
		// (a payload without any head is never produced by GitHub)
		if pr.Head.Repo == nil {
			return fmt.Errorf("PR #%d has no head repository (deleted fork?)", pr.Number)
		}
		if !strings.EqualFold(pr.Head.Repo.FullName, config.Config.GithubAppOrganization+"/"+repo) {
			return fmt.Errorf("PR #%d comes from a fork (%s): its head is not in %s/%s", pr.Number, pr.Head.Repo.FullName, config.Config.GithubAppOrganization, repo)
		}
	}
	return nil
}

func (g *ForcemergeImpl) fetchPullRequest(ctx context.Context, repo, prNumber string) (*pullRequestInfo, error) {
	body, err := g.ws.CallRestAPI(
		ctx,
		fmt.Sprintf("/repos/%s/%s/pulls/%s", config.Config.GithubAppOrganization, repo, prNumber),
		"",
		"GET",
		nil,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("error loading pull request %s/%s: %w", repo, prNumber, err)
	}
	var pr pullRequestInfo
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("error parsing pull request %s/%s: %w", repo, prNumber, err)
	}
	return &pr, nil
}

// repoSettings are the merge methods the repository allows (nil = not told by GitHub)
type repoSettings struct {
	DefaultBranch    string `json:"default_branch"`
	AllowMergeCommit *bool  `json:"allow_merge_commit"`
	AllowSquashMerge *bool  `json:"allow_squash_merge"`
	AllowRebaseMerge *bool  `json:"allow_rebase_merge"`
}

func (g *ForcemergeImpl) fetchRepoSettings(ctx context.Context, repo string) (*repoSettings, error) {
	body, err := g.ws.CallRestAPI(
		ctx,
		fmt.Sprintf("/repos/%s/%s", config.Config.GithubAppOrganization, repo),
		"",
		"GET",
		nil,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("error loading the settings of repository %s: %w", repo, err)
	}
	var s repoSettings
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("error parsing the settings of repository %s: %w", repo, err)
	}
	if s.AllowMergeCommit == nil && s.AllowSquashMerge == nil && s.AllowRebaseMerge == nil {
		return nil, fmt.Errorf("repository %s settings do not tell which merge methods are allowed", repo)
	}
	return &s, nil
}

func (s *repoSettings) allows(method string) bool {
	var v *bool
	switch method {
	case "merge":
		v = s.AllowMergeCommit
	case "squash":
		v = s.AllowSquashMerge
	case "rebase":
		v = s.AllowRebaseMerge
	}
	return v == nil || *v
}

// chooseMergeMethod returns the requested merge method if the repository allows it. Otherwise
// (the default "merge" on a squash-only repository) it falls back to squash, then rebase,
// like the synchronous path does on a "Method Not Allowed". An explicit "/squash" is never replaced.
func chooseMergeMethod(s *repoSettings, requested string, explicit bool) (string, error) {
	if s.allows(requested) {
		return requested, nil
	}
	if explicit {
		return "", fmt.Errorf("the repository does not allow the %s merge method requested with /%s", requested, requested)
	}
	for _, m := range []string{"squash", "rebase"} {
		if s.allows(m) {
			return m, nil
		}
	}
	return "", fmt.Errorf("the repository allows no merge method usable for a force merge")
}

// statusRegex extracts the status code of the error GitHubClientImpl.CallRestAPI returns on a non 2xx answer
var statusRegex = regexp.MustCompile(`unexpected status: (\d{3})`)

func httpStatus(err error) int {
	if err == nil {
		return 0
	}
	m := statusRegex.FindStringSubmatch(err.Error())
	if len(m) != 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// indeterminateError is an outcome where the merge may still happen (or have happened):
// the approvals are left in place
type indeterminateError struct{ msg string }

func (e *indeterminateError) Error() string { return e.msg }

func indeterminatef(format string, args ...interface{}) error {
	return &indeterminateError{msg: fmt.Sprintf(format, args...)}
}

// listPRs returns the PRs of the repo matching the query parameters
func (g *ForcemergeImpl) listPRs(ctx context.Context, repo, query, what string) ([]*pullRequestInfo, error) {
	body, err := g.ws.CallRestAPI(
		ctx,
		fmt.Sprintf("/repos/%s/%s/pulls", config.Config.GithubAppOrganization, repo),
		query,
		"GET",
		nil,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("error looking for %s: %v (%s)", what, err, string(body))
	}
	var prs []*pullRequestInfo
	if err := json.Unmarshal(body, &prs); err != nil {
		return nil, fmt.Errorf("error parsing %s: %w", what, err)
	}
	return prs, nil
}

// listOpenPRsBasedOn returns the open PRs whose base branch is branch
func (g *ForcemergeImpl) listOpenPRsBasedOn(ctx context.Context, repo, branch string) ([]*pullRequestInfo, error) {
	return g.listPRs(ctx, repo, fmt.Sprintf("state=open&base=%s", url.QueryEscape(branch)), fmt.Sprintf("the PRs based on branch %s", branch))
}

// listOpenPRsWithHead returns the open PRs whose head branch (in the repository itself) is branch
func (g *ForcemergeImpl) listOpenPRsWithHead(ctx context.Context, repo, branch string) ([]*pullRequestInfo, error) {
	return g.listPRs(ctx, repo, fmt.Sprintf("state=open&head=%s", url.QueryEscape(config.Config.GithubAppOrganization+":"+branch)), fmt.Sprintf("the PR of branch %s", branch))
}

// inSameStack keeps the PRs of the repository (not forks) that carry the same stack metadata as ref
func inSameStack(prs []*pullRequestInfo, repo string, ref *pullRequestInfo) []*pullRequestInfo {
	res := []*pullRequestInfo{}
	for _, p := range prs {
		if p.Head.Repo == nil || !strings.EqualFold(p.Head.Repo.FullName, config.Config.GithubAppOrganization+"/"+repo) {
			continue
		}
		if !ref.sameStack(p) {
			continue
		}
		res = append(res, p)
	}
	return res
}

// fetchDownstack returns the open PRs below pr in its stack, from the closest one
// down to the bottom of the stack, by following each PR's base branch.
func (g *ForcemergeImpl) fetchDownstack(ctx context.Context, repo string, pr *pullRequestInfo, visited map[int]bool) ([]*pullRequestInfo, error) {
	downstack := []*pullRequestInfo{}
	stackBase := pr.Stack.Base.Ref
	current := pr
	for current.Base.Ref != stackBase {
		if len(downstack) >= pr.Stack.Size {
			return nil, fmt.Errorf("unable to walk the stack of PR #%d down to %s: more than %d PRs found", pr.Number, stackBase, pr.Stack.Size)
		}
		prs, err := g.listOpenPRsWithHead(ctx, repo, current.Base.Ref)
		if err != nil {
			return nil, err
		}
		candidates := []*pullRequestInfo{}
		for _, p := range inSameStack(prs, repo, pr) {
			if p.Head.Ref == current.Base.Ref {
				candidates = append(candidates, p)
			}
		}
		if len(candidates) == 0 {
			return nil, fmt.Errorf("no open PR of the stack found for branch %s below PR #%d", current.Base.Ref, current.Number)
		}
		if len(candidates) > 1 {
			return nil, fmt.Errorf("%d open PRs of the stack have the head branch %s (below PR #%d): the stack is ambiguous", len(candidates), current.Base.Ref, current.Number)
		}
		next := candidates[0]
		if visited[next.Number] {
			return nil, fmt.Errorf("the stack of PR #%d is not linear: PR #%d is reached twice (cycle)", pr.Number, next.Number)
		}
		visited[next.Number] = true
		downstack = append(downstack, next)
		current = next
	}
	return downstack, nil
}

// fetchUpstack returns the open PRs above pr in its stack, from the closest one
// up to the top of the stack, by following the PRs based on each head branch.
func (g *ForcemergeImpl) fetchUpstack(ctx context.Context, repo string, pr *pullRequestInfo, visited map[int]bool) ([]*pullRequestInfo, error) {
	upstack := []*pullRequestInfo{}
	current := pr
	for {
		prs, err := g.listOpenPRsBasedOn(ctx, repo, current.Head.Ref)
		if err != nil {
			return nil, err
		}
		candidates := []*pullRequestInfo{}
		for _, p := range inSameStack(prs, repo, pr) {
			if p.Base.Ref == current.Head.Ref {
				candidates = append(candidates, p)
			}
		}
		if len(candidates) == 0 {
			return upstack, nil
		}
		if len(candidates) > 1 {
			return nil, fmt.Errorf("%d open PRs of the stack are based on branch %s (above PR #%d): the stack is ambiguous", len(candidates), current.Head.Ref, current.Number)
		}
		next := candidates[0]
		if visited[next.Number] {
			return nil, fmt.Errorf("the stack of PR #%d is not linear: PR #%d is reached twice (cycle)", pr.Number, next.Number)
		}
		if len(upstack) >= pr.Stack.Size {
			return nil, fmt.Errorf("unable to walk the stack of PR #%d up: more than %d PRs found", pr.Number, pr.Stack.Size)
		}
		visited[next.Number] = true
		upstack = append(upstack, next)
		current = next
	}
}

func (g *ForcemergeImpl) maxInferredStack() int {
	if g.maxInferredStackSize > 0 {
		return g.maxInferredStackSize
	}
	return defaultMaxInferredStackSize
}

func prNumbers(prs []*pullRequestInfo) string {
	numbers := []string{}
	for _, p := range prs {
		numbers = append(numbers, fmt.Sprintf("#%d", p.Number))
	}
	return strings.Join(numbers, ", ")
}

// chainPRBelow returns the open PR of the repository whose head branch is the base branch of pr (nil if none)
func (g *ForcemergeImpl) chainPRBelow(ctx context.Context, repo string, pr *pullRequestInfo) (*pullRequestInfo, error) {
	prs, err := g.listOpenPRsWithHead(ctx, repo, pr.Base.Ref)
	if err != nil {
		return nil, err
	}
	candidates := []*pullRequestInfo{}
	for _, p := range prs {
		if p.Head.Ref == pr.Base.Ref {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) > 1 {
		return nil, fmt.Errorf("%d open PRs (%s) have the head branch %s (below PR #%d): the stack is ambiguous", len(candidates), prNumbers(candidates), pr.Base.Ref, pr.Number)
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	return candidates[0], nil
}

// chainPRsAbove returns the open PRs based on the head branch of pr (forks included: they are refused later)
func (g *ForcemergeImpl) chainPRsAbove(ctx context.Context, repo string, pr *pullRequestInfo) ([]*pullRequestInfo, error) {
	prs, err := g.listOpenPRsBasedOn(ctx, repo, pr.Head.Ref)
	if err != nil {
		return nil, err
	}
	res := []*pullRequestInfo{}
	for _, p := range prs {
		if p.Base.Ref == pr.Head.Ref {
			res = append(res, p)
		}
	}
	return res, nil
}

// checkInferredMember refuses a PR that can't be part of an inferred stack
func checkInferredMember(repo string, p *pullRequestInfo) error {
	if err := p.validate(repo); err != nil {
		return fmt.Errorf("PR #%d of the inferred stack: %v", p.Number, err)
	}
	if p.State != "open" {
		return fmt.Errorf("PR #%d of the inferred stack is not open (state %q)", p.Number, p.State)
	}
	if p.Stack != nil {
		return fmt.Errorf("PR #%d of the inferred stack carries GitHub stack metadata but the PR the force merge was requested on does not: the stack is ambiguous", p.Number)
	}
	return nil
}

/*
 * inferStack is the fallback when GitHub sends no stack metadata for pr (stack: null). GitHub only
 * sets it for PRs created with its own stacked pull requests feature, not for a plain chain of PRs
 * where each PR is based on the head branch of the PR below it.
 *
 * The stack is the chain of open PRs of the repository (no fork, no draft) linked by their branches:
 * - down: the PR whose head branch is the base branch of this PR, repeated until a PR is based on
 *   the default branch of the repository (the stack base);
 * - up: the PR based on the head branch of this PR, repeated until no PR is based on it.
 * A PR with no PR below and none above is not a stack (false): it is merged as usual, whatever its base.
 * A chain that forks (two open PRs based on the same branch), loops, is longer than the walk limit,
 * does not reach the default branch, or holds a PR that can't be merged (draft, fork, ...) is refused.
 *
 * On success pr.Stack is set (base = default branch, size = length of the chain) and marked inferred:
 * the stack and direct merges then run as for a GitHub stack.
 */
func (g *ForcemergeImpl) inferStack(ctx context.Context, repo string, pr *pullRequestInfo, defaultBranch string, settingsErr error) (bool, error) {
	if pr.Base.Ref == "" || pr.Head.Ref == "" {
		// (never produced by GitHub) no branches to chain: merged as usual
		return false, nil
	}
	limit := g.maxInferredStack()
	unknownDefault := func() error {
		if settingsErr != nil {
			return fmt.Errorf("PR #%d is part of a chain of PRs but GitHub sent no stack metadata, and Goliac is unable to read the default branch of the repository to infer the stack: %v", pr.Number, settingsErr)
		}
		return fmt.Errorf("PR #%d is part of a chain of PRs but GitHub sent no stack metadata, and Goliac is unable to read the default branch of the repository to infer the stack: GitHub returned none", pr.Number)
	}

	// walk down to the PR based on the default branch
	visited := map[int]bool{pr.Number: true}
	bottom := pr
	for defaultBranch == "" || bottom.Base.Ref != defaultBranch {
		next, err := g.chainPRBelow(ctx, repo, bottom)
		if err != nil {
			return false, err
		}
		if next == nil {
			break
		}
		if defaultBranch == "" {
			return false, unknownDefault()
		}
		if visited[next.Number] {
			return false, fmt.Errorf("the chain of PR #%d is not linear: PR #%d is reached twice (cycle)", pr.Number, next.Number)
		}
		if len(visited) >= limit {
			return false, fmt.Errorf("the chain of PR #%d has more than %d PRs: too long to infer a stack", pr.Number, limit)
		}
		if err := checkInferredMember(repo, next); err != nil {
			return false, err
		}
		visited[next.Number] = true
		bottom = next
	}

	if defaultBranch == "" || bottom.Base.Ref != defaultBranch {
		if bottom == pr {
			// nothing below: a stack only if a PR is based on this one
			above, err := g.chainPRsAbove(ctx, repo, pr)
			if err != nil {
				return false, err
			}
			if len(above) == 0 {
				return false, nil
			}
		}
		if defaultBranch == "" {
			return false, unknownDefault()
		}
		return false, fmt.Errorf("the chain of PR #%d stops at PR #%d, based on %s: no open PR has this head branch and it is not the default branch %s, so the stack can't be inferred (merge or retarget that PR first)", pr.Number, bottom.Number, bottom.Base.Ref, defaultBranch)
	}

	// walk up from the bottom: the whole chain, refusing a fork in it at every level
	chain := []*pullRequestInfo{bottom}
	seen := map[int]bool{bottom.Number: true}
	current := bottom
	for {
		above, err := g.chainPRsAbove(ctx, repo, current)
		if err != nil {
			return false, err
		}
		if len(above) == 0 {
			break
		}
		if len(above) > 1 {
			return false, fmt.Errorf("%d open PRs (%s) are based on branch %s (above PR #%d): the chain forks, the stack is ambiguous", len(above), prNumbers(above), current.Head.Ref, current.Number)
		}
		next := above[0]
		if next.Number == pr.Number {
			next = pr
		}
		if seen[next.Number] {
			return false, fmt.Errorf("the chain of PR #%d is not linear: PR #%d is reached twice (cycle)", pr.Number, next.Number)
		}
		if len(chain) >= limit {
			return false, fmt.Errorf("the chain of PR #%d has more than %d PRs: too long to infer a stack", pr.Number, limit)
		}
		if next != pr {
			if err := checkInferredMember(repo, next); err != nil {
				return false, err
			}
		}
		seen[next.Number] = true
		chain = append(chain, next)
		current = next
	}
	if len(chain) == 1 {
		// based on the default branch and nothing above: not a stack
		return false, nil
	}
	position := 0
	for i, p := range chain {
		if p == pr {
			position = i + 1
		}
	}
	if position == 0 {
		return false, fmt.Errorf("PR #%d is not in the chain of PRs found from its base branch: the stack is ambiguous", pr.Number)
	}
	pr.Stack = &prStack{Position: position, Size: len(chain)}
	pr.Stack.Base.Ref = defaultBranch
	pr.stackInferred = true
	return true, nil
}

func forcemergeReviewBody(username string, prNumber int, prTitle, prURL, explanation string) string {
	return fmt.Sprintf(
		"Force merge via Goliac on behalf of %s.\n\nPR #%d: %s\n%s\n\n%s",
		username, prNumber, prTitle, prURL, explanation,
	)
}

func forcemergeCommitTitle(prTitle string, prNumber int, username string) string {
	return fmt.Sprintf("%s (force merged PR %d by %s)", prTitle, prNumber, username)
}

func forcemergeCommitMessage(username, prURL, prTitle string) string {
	return fmt.Sprintf(
		"Force merge via Goliac on behalf of %s.\n\nPR: %s\nTitle: %s",
		username, prURL, prTitle,
	)
}

// forcemergeStackCommitMessage lists every PR merged by a stack merge (top to bottom)
func forcemergeStackCommitMessage(username string, requestedOn int, stack []*pullRequestInfo) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Force merge of a stack of %d PRs via Goliac on behalf of %s (requested on PR #%d).\n\nPRs:", len(stack), username, requestedOn)
	for _, p := range stack {
		fmt.Fprintf(&sb, "\n- #%d %s (by %s) %s", p.Number, p.Title, p.author(), p.HTMLURL)
	}
	return sb.String()
}

// mergePlan is everything validated before the first side effect
type mergePlan struct {
	username    string
	repo        string
	explanation string
	pr          *pullRequestInfo // the PR the force merge was requested on
	mode        string           // resolved merge mode ("" for a PR that is not stacked)
	requested   string           // the merge mode asked for
	method      string           // merge, squash or rebase (unused when merging into a parent branch)
	explicit    bool             // the method was explicitly asked (/squash)
	stack       []*pullRequestInfo
	parent      *pullRequestInfo   // direct merge of a mid-stack PR: the PR below
	children    []*pullRequestInfo // direct merge: the PRs above (to retarget)
}

func (p *mergePlan) isStack() bool  { return len(p.stack) > 0 }
func (p *mergePlan) isDirect() bool { return p.pr.isStacked() && !p.isStack() }

// mergedPRs are the PRs the force merge merges
func (p *mergePlan) mergedPRs() []*pullRequestInfo {
	if p.isStack() {
		return p.stack
	}
	return []*pullRequestInfo{p.pr}
}

// lockedPRs are the PRs a force merge changes or relies on: the merged PRs, plus the parent
// PR and the PRs to retarget of a direct merge (two concurrent merges of adjacent PRs must not restack at once)
func (p *mergePlan) lockedPRs() []*pullRequestInfo {
	prs := append([]*pullRequestInfo{}, p.mergedPRs()...)
	if p.parent != nil {
		prs = append(prs, p.parent)
	}
	return append(prs, p.children...)
}

/*
 * planMerge reads and validates everything: nothing is approved, merged or notified
 * if the PR (or any PR of its stack) can't be merged.
 *
 * "/squash" in the explanation asks for a squash merge.
 *
 * mergeMode only applies to a PR that is part of a stack (GitHub's stack metadata, or a stack
 * inferred from the chain of PRs when GitHub sends none, see inferStack):
 * - "" or "direct": merge this PR only into its base branch, then restack the PRs above it
 * - "stack": merge the whole stack into the stack base branch
 */
func (g *ForcemergeImpl) planMerge(ctx context.Context, username, repo, prNumber, prURL, explanation, mergeMode string) (*mergePlan, error) {
	method := "merge"
	explicit := strings.Contains(explanation, "/squash")
	if explicit {
		method = "squash"
	}
	// let's remove the /squash string from the explanation
	explanation = strings.ReplaceAll(explanation, "/squash", "")

	pr, err := g.fetchPullRequest(ctx, repo, prNumber)
	if err != nil {
		return nil, err
	}
	if pr.Number == 0 {
		// the URL is authoritative when the payload has no number
		pr.Number, _ = strconv.Atoi(prNumber)
	}
	pr.HTMLURL = prURL
	if err := pr.validate(repo); err != nil {
		return nil, fmt.Errorf("nothing merged: %v", err)
	}

	plan := &mergePlan{username: username, repo: repo, explanation: explanation, pr: pr, requested: mergeMode, method: method, explicit: explicit}

	var settings *repoSettings
	var settingsErr error
	if !pr.isStacked() {
		settings, settingsErr = g.fetchRepoSettings(ctx, repo)
		defaultBranch := ""
		if settingsErr == nil {
			defaultBranch = settings.DefaultBranch
		}
		// GitHub sent no stack: infer one from the chain of PRs (a lone PR is not a stack)
		if _, err := g.inferStack(ctx, repo, pr, defaultBranch, settingsErr); err != nil {
			return nil, fmt.Errorf("nothing merged: %v", err)
		}
	}
	if !pr.isStacked() {
		// the synchronous merge: when GitHub does not tell the allowed methods, "merge" is tried first, then "squash"
		if settingsErr == nil {
			m, err := chooseMergeMethod(settings, method, explicit)
			if err != nil {
				return nil, fmt.Errorf("nothing merged: %v", err)
			}
			plan.method = m
		} else {
			logrus.Warnf("force merge of %s/%s: %v, relying on the merge method fallback", repo, prNumber, settingsErr)
		}
		return plan, nil
	}

	plan.mode = mergeMode
	if plan.mode == "" {
		plan.mode = MergeModeDirect
	}

	if plan.mode == MergeModeStack || pr.isStackBottom() {
		// the asynchronous merge fails after the approvals on a method the repository does not allow
		if settings == nil {
			settings, settingsErr = g.fetchRepoSettings(ctx, repo)
		}
		if settingsErr != nil {
			return nil, fmt.Errorf("nothing merged: %v", settingsErr)
		}
		m, err := chooseMergeMethod(settings, method, explicit)
		if err != nil {
			return nil, fmt.Errorf("nothing merged: %v", err)
		}
		plan.method = m
	}

	if plan.mode == MergeModeStack {
		if err := g.planStack(ctx, repo, plan); err != nil {
			return nil, fmt.Errorf("nothing merged: %v", err)
		}
		return plan, nil
	}
	if err := g.planDirect(ctx, repo, plan); err != nil {
		return nil, fmt.Errorf("nothing merged: %v", err)
	}
	return plan, nil
}

// planStack walks the whole stack (top to bottom) and validates every PR
func (g *ForcemergeImpl) planStack(ctx context.Context, repo string, plan *mergePlan) error {
	pr := plan.pr
	visited := map[int]bool{pr.Number: true}
	upstack, err := g.fetchUpstack(ctx, repo, pr, visited)
	if err != nil {
		return err
	}
	downstack, err := g.fetchDownstack(ctx, repo, pr, visited)
	if err != nil {
		return err
	}

	// top to bottom
	stack := []*pullRequestInfo{}
	for i := len(upstack) - 1; i >= 0; i-- {
		stack = append(stack, upstack[i])
	}
	stack = append(stack, pr)
	stack = append(stack, downstack...)

	numbers := []string{}
	for _, p := range stack {
		numbers = append(numbers, fmt.Sprintf("#%d", p.Number))
	}
	if len(stack) != pr.Stack.Size {
		source := "GitHub reports"
		if pr.stackInferred {
			source = "the chain of PRs gave" // changed between the inference and this walk
		}
		return fmt.Errorf("%s a stack of %d PRs but only %d open PRs were found (%s): a PR in the stack is missing or not open", source, pr.Stack.Size, len(stack), strings.Join(numbers, " > "))
	}
	heads := map[string]int{}
	for i, p := range stack {
		if p.Number != pr.Number {
			p.HTMLURL = g.prHTMLURL(repo, p)
		}
		if err := p.validate(repo); err != nil {
			return err
		}
		if other, dup := heads[p.Head.Ref]; dup {
			return fmt.Errorf("PRs #%d and #%d have the same head branch %s: the stack is ambiguous", other, p.Number, p.Head.Ref)
		}
		heads[p.Head.Ref] = p.Number
		// linkage: every PR is based on the head branch of the PR below it, the bottom one on the stack base
		if i+1 < len(stack) {
			if p.Base.Ref != stack[i+1].Head.Ref {
				return fmt.Errorf("PR #%d is based on %s, not on the head branch %s of PR #%d below it", p.Number, p.Base.Ref, stack[i+1].Head.Ref, stack[i+1].Number)
			}
		} else if p.Base.Ref != pr.Stack.Base.Ref {
			return fmt.Errorf("the bottom PR #%d is based on %s, not on the stack base %s", p.Number, p.Base.Ref, pr.Stack.Base.Ref)
		}
	}
	plan.stack = stack
	return nil
}

// planDirect validates a direct merge: the parent PR (for a mid-stack PR) and the PRs to retarget
func (g *ForcemergeImpl) planDirect(ctx context.Context, repo string, plan *mergePlan) error {
	pr := plan.pr
	if !pr.isStackBottom() {
		prs, err := g.listOpenPRsWithHead(ctx, repo, pr.Base.Ref)
		if err != nil {
			return err
		}
		candidates := []*pullRequestInfo{}
		for _, p := range inSameStack(prs, repo, pr) {
			if p.Head.Ref == pr.Base.Ref {
				candidates = append(candidates, p)
			}
		}
		if len(candidates) == 0 {
			return fmt.Errorf("PR #%d is based on %s but no open PR of the same stack has this head branch: it can't be merged into it", pr.Number, pr.Base.Ref)
		}
		if len(candidates) > 1 {
			return fmt.Errorf("%d open PRs of the stack have the head branch %s (below PR #%d): the stack is ambiguous", len(candidates), pr.Base.Ref, pr.Number)
		}
		parent := candidates[0]
		if err := parent.validate(repo); err != nil {
			return fmt.Errorf("the PR below: %v", err)
		}
		plan.parent = parent
	}

	// the PRs to retarget (only PRs of the same stack, in the repository)
	above, err := g.listOpenPRsBasedOn(ctx, repo, pr.Head.Ref)
	if err != nil {
		return err
	}
	for _, p := range inSameStack(above, repo, pr) {
		if p.Base.Ref == pr.Head.Ref {
			plan.children = append(plan.children, p)
		}
	}
	return nil
}

func (g *ForcemergeImpl) prHTMLURL(repo string, pr *pullRequestInfo) string {
	if pr.HTMLURL != "" {
		return pr.HTMLURL
	}
	return fmt.Sprintf("https://github.com/%s/%s/pull/%d", config.Config.GithubAppOrganization, repo, pr.Number)
}

// approval is a review created by Goliac
type approval struct {
	pr     int
	review int64
}

func (g *ForcemergeImpl) approvePR(ctx context.Context, repo string, pr *pullRequestInfo, reviewBody string) (*approval, error) {
	payload := map[string]interface{}{
		"body":  reviewBody,
		"event": "APPROVE",
	}
	if pr.Head.Sha != "" {
		// approve the head that was validated
		payload["commit_id"] = pr.Head.Sha
	}
	body, err := g.ws.CallRestAPI(
		ctx,
		fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", config.Config.GithubAppOrganization, repo, pr.Number),
		"",
		"POST",
		payload,
		nil)
	if err != nil {
		return nil, fmt.Errorf("error approving the PR #%d: %v (%s)", pr.Number, err, string(body))
	}
	a := &approval{pr: pr.Number}
	var review struct {
		ID int64 `json:"id"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &review)
	}
	a.review = review.ID
	return a, nil
}

// failAfterApprovals dismisses the approvals Goliac created when the merge definitively failed;
// when the outcome is unknown (the merge may still happen) or a dismissal fails, the approvals are listed in the error
func (g *ForcemergeImpl) failAfterApprovals(ctx context.Context, repo string, approvals []*approval, cause error) error {
	if len(approvals) == 0 {
		return cause
	}
	var indeterminate *indeterminateError
	if errors.As(cause, &indeterminate) {
		return fmt.Errorf("%w (the approvals Goliac created are left in place: %s)", cause, describeApprovals(approvals))
	}
	failed := []*approval{}
	for _, a := range approvals {
		if a.review == 0 {
			failed = append(failed, a)
			continue
		}
		_, err := g.ws.CallRestAPI(
			ctx,
			fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews/%d/dismissals", config.Config.GithubAppOrganization, repo, a.pr, a.review),
			"",
			"PUT",
			map[string]interface{}{
				"message": "Goliac force merge failed: approval dismissed",
				"event":   "DISMISS",
			},
			nil)
		if err != nil {
			failed = append(failed, a)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%w (the approvals Goliac created could not all be dismissed, dismiss them manually: %s)", cause, describeApprovals(failed))
	}
	return fmt.Errorf("%w (the approvals Goliac created were dismissed)", cause)
}

func describeApprovals(approvals []*approval) string {
	parts := []string{}
	for _, a := range approvals {
		if a.review == 0 {
			parts = append(parts, fmt.Sprintf("PR #%d", a.pr))
		} else {
			parts = append(parts, fmt.Sprintf("PR #%d (review %d)", a.pr, a.review))
		}
	}
	return strings.Join(parts, ", ")
}

// executePlan approves and merges. It returns a summary of what was merged for a stacked PR.
func (g *ForcemergeImpl) executePlan(ctx context.Context, plan *mergePlan) (string, error) {
	switch {
	case plan.isStack() && plan.pr.stackInferred:
		return g.mergeInferredStack(ctx, plan)
	case plan.isStack():
		return g.mergeWholeStack(ctx, plan)
	case plan.isDirect():
		return g.mergeDirect(ctx, plan)
	}
	return "", g.mergePlain(ctx, plan)
}

func (g *ForcemergeImpl) mergePlain(ctx context.Context, plan *mergePlan) error {
	pr := plan.pr
	prNumber := strconv.Itoa(pr.Number)
	reviewBody := forcemergeReviewBody(plan.username, pr.Number, pr.Title, pr.HTMLURL, plan.explanation)
	if plan.requested != "" {
		reviewBody += fmt.Sprintf("\n\nMerge mode `%s` ignored: this PR is not part of a stack.", plan.requested)
	} else {
		reviewBody += "\n\nMerge mode: none (this PR is not part of a stack)."
	}
	commitTitle := forcemergeCommitTitle(pr.Title, pr.Number, plan.username)
	commitMessage := forcemergeCommitMessage(plan.username, pr.HTMLURL, pr.Title)
	mergeMethod := plan.method

	a, err := g.approvePR(ctx, plan.repo, pr, reviewBody)
	if err != nil {
		return err
	}

	// https://docs.github.com/en/rest/pulls/pulls?apiVersion=2022-11-28#merge-a-pull-request
	body, err := g.mergePRSync(ctx, plan.repo, prNumber, commitTitle, commitMessage, mergeMethod)
	if err != nil && mergeMethod == "merge" {
		if httpStatus(err) == 405 || strings.Contains(err.Error(), "Method Not Allowed") {
			// in case of we want a squash merge
			body, err = g.mergePRSync(ctx, plan.repo, prNumber, commitTitle, commitMessage, "squash")
		}
	}
	if err != nil && isStackedPRRefusal(body) {
		// GitHub says the PR is stacked but its payload has no stack information:
		// don't guess what part of the stack to merge
		err = fmt.Errorf("error merging the PR: GitHub reports a stacked PR but returned no stack information for it, nothing was merged: %v (%s)", err, string(body))
	} else if err != nil {
		err = fmt.Errorf("error merging the PR: %v (%s)", err, string(body))
	}
	if err != nil {
		return g.failAfterApprovals(ctx, plan.repo, []*approval{a}, err)
	}
	return nil
}

// headsUnchanged re-reads the PRs just before the merge: a push after the approval aborts the merge
func (g *ForcemergeImpl) headsUnchanged(ctx context.Context, repo string, prs []*pullRequestInfo) error {
	for _, p := range prs {
		cur, err := g.fetchPullRequest(ctx, repo, strconv.Itoa(p.Number))
		if err != nil {
			return fmt.Errorf("unable to re-check PR #%d before merging: %w", p.Number, err)
		}
		if cur.Head.Sha != p.Head.Sha {
			return fmt.Errorf("PR #%d was pushed to after the approval (head %s, was %s): nothing merged, retry", p.Number, cur.Head.Sha, p.Head.Sha)
		}
		if cur.State == "closed" || cur.Merged {
			return fmt.Errorf("PR #%d is no longer open: nothing merged", p.Number)
		}
	}
	return nil
}

func summarize(prs []*pullRequestInfo) string {
	parts := []string{}
	for _, p := range prs {
		parts = append(parts, fmt.Sprintf("#%d %s (by %s)", p.Number, p.Title, p.author()))
	}
	return strings.Join(parts, ", ")
}

/*
 * mergeDirect merges a stacked PR alone into its base branch, then restacks:
 * with the stack 1 > 2 > 3 > 4 > main, a direct merge of 3 merges 3 into 4's branch
 * and retargets 2 onto 4's branch, giving 1 > 2 > 4 > main.
 * The bottom PR of a stack (base = stack base branch) is merged with the asynchronous merge API.
 *
 * Merging into a parent branch (POST /merges) always makes a merge commit and does not depend on the
 * merge methods the repository allows (those apply to pull request merges): on a squash-only
 * repository the merge commit lands on the parent PR's branch, and the PR at the bottom is
 * squashed into the default branch afterwards.
 */
func (g *ForcemergeImpl) mergeDirect(ctx context.Context, plan *mergePlan) (string, error) {
	pr := plan.pr
	repo := plan.repo
	bottom := pr.isStackBottom()
	commitTitle := forcemergeCommitTitle(pr.Title, pr.Number, plan.username)
	commitMessage := forcemergeCommitMessage(plan.username, pr.HTMLURL, pr.Title)

	reviewBody := forcemergeReviewBody(plan.username, pr.Number, pr.Title, pr.HTMLURL, plan.explanation)
	reviewBody += fmt.Sprintf("\n\nMerge mode: `%s`", plan.mode)
	if plan.requested == "" {
		reviewBody += " (default)"
	}
	reviewBody += fmt.Sprintf(". Stacked PR: merged alone into its base branch `%s`; the PRs based on `%s` are retargeted to `%s`.", pr.Base.Ref, pr.Head.Ref, pr.Base.Ref)
	if !bottom && plan.explicit {
		reviewBody += " /squash is not available when merging into a parent branch: a merge commit is used instead."
	}
	reviewBody += " " + pr.stackSource()

	a, err := g.approvePR(ctx, repo, pr, reviewBody)
	if err != nil {
		return "", err
	}
	approvals := []*approval{a}

	if err := g.headsUnchanged(ctx, repo, []*pullRequestInfo{pr}); err != nil {
		return "", g.failAfterApprovals(ctx, repo, approvals, err)
	}

	mergeSha := ""
	if bottom {
		// https://docs.github.com/en/rest/pulls/pulls#merge-a-pull-request-asynchronously
		// (the bottom PR has no downstack PR: only this PR is merged)
		if err := g.mergePRAsync(ctx, repo, pr.Number, pr.Head.Sha, commitTitle, commitMessage, plan.method, plan.explicit); err != nil {
			return "", g.failAfterApprovals(ctx, repo, approvals, err)
		}
		// nothing is retargeted before GitHub says merged:true
		if err := g.confirmMerged(ctx, repo, pr.Number); err != nil {
			return "", err
		}
	} else {
		sha, err := g.mergeIntoParentBranch(ctx, repo, pr, commitTitle, commitMessage)
		if err != nil {
			return "", g.failAfterApprovals(ctx, repo, approvals, err)
		}
		mergeSha = sha
	}

	// from here the PR is merged (confirmed by GitHub for the bottom PR, merge commit for the others)
	var ensureErr error
	if !bottom {
		ensureErr = g.ensureMerged(ctx, plan.username, repo, pr, mergeSha)
	}
	restacked, restackErr := g.restack(ctx, repo, pr, plan.children)

	if ensureErr != nil || restackErr != nil {
		errs := []string{}
		if ensureErr != nil {
			errs = append(errs, ensureErr.Error())
		}
		if restackErr != nil {
			errs = append(errs, restackErr.Error())
		}
		return "", errors.New(strings.Join(errs, "; "))
	}
	summary := fmt.Sprintf("Force merged (direct): %s into `%s`", summarize([]*pullRequestInfo{pr}), pr.Base.Ref)
	if len(restacked) > 0 {
		summary += fmt.Sprintf("; retargeted onto `%s`: %s", pr.Base.Ref, strings.Join(restacked, ", "))
	}
	return summary, nil
}

// restack retargets the PRs that were above the merged PR onto its base branch, with retries.
// Only the PRs of the plan that are still open and still based on the merged branch are touched.
func (g *ForcemergeImpl) restack(ctx context.Context, repo string, pr *pullRequestInfo, children []*pullRequestInfo) ([]string, error) {
	attempts := g.restackAttempts
	if attempts <= 0 {
		attempts = defaultRestackAttempts
	}
	backoff := g.restackBackoff
	if backoff <= 0 {
		backoff = defaultRestackBackoff
	}

	done := []string{}
	failed := []string{}
	for _, child := range children {
		var lastErr error
		wait := backoff
		retargeted := false
		for attempt := 1; attempt <= attempts; attempt++ {
			cur, err := g.fetchPullRequest(ctx, repo, strconv.Itoa(child.Number))
			if err == nil {
				if cur.State == "closed" || cur.Merged || cur.Base.Ref == pr.Base.Ref {
					retargeted = true // nothing to do anymore
					break
				}
				if cur.Base.Ref != pr.Head.Ref {
					lastErr = fmt.Errorf("it is now based on %s", cur.Base.Ref)
					break
				}
				var body []byte
				body, err = g.ws.CallRestAPI(
					ctx,
					fmt.Sprintf("/repos/%s/%s/pulls/%d", config.Config.GithubAppOrganization, repo, child.Number),
					"",
					"PATCH",
					map[string]interface{}{
						"base": pr.Base.Ref,
					},
					nil)
				if err == nil {
					retargeted = true
					done = append(done, fmt.Sprintf("#%d", child.Number))
					break
				}
				lastErr = fmt.Errorf("%v (%s)", err, string(body))
				if st := httpStatus(err); st >= 400 && st < 500 && st != 408 && st != 429 {
					// a refusal (e.g. 422: GitHub may refuse to change the base of a PR that is still in a stack)
					// does not go away by retrying
					lastErr = fmt.Errorf("%v (GitHub refused the change, not retried; if the PR is still in a stack, unstack it or change its base by hand)", lastErr)
					break
				}
			} else {
				lastErr = err
			}
			if attempt < attempts {
				if serr := g.clockSleep(ctx, wait); serr != nil {
					lastErr = fmt.Errorf("%v: %v", lastErr, serr)
					break
				}
				wait *= 2
			}
		}
		if !retargeted {
			failed = append(failed, fmt.Sprintf("PR #%d: %v", child.Number, lastErr))
		}
	}
	if len(failed) > 0 {
		numbers := []string{}
		for _, child := range children {
			numbers = append(numbers, fmt.Sprintf("#%d", child.Number))
		}
		return done, fmt.Errorf("PR #%d is merged, but the PRs above it were not all retargeted onto %s (%s). Fix manually: change the base branch of the PRs among %s that still target %s to %s", pr.Number, pr.Base.Ref, strings.Join(failed, "; "), strings.Join(numbers, ", "), pr.Head.Ref, pr.Base.Ref)
	}
	return done, nil
}

// mergeIntoParentBranch merges a mid-stack PR into its base branch (the branch of the PR below it)
// with a merge commit, and returns the merge commit sha ("" if there was nothing to merge: 204).
// https://docs.github.com/en/rest/branches/branches#merge-a-branch
func (g *ForcemergeImpl) mergeIntoParentBranch(ctx context.Context, repo string, pr *pullRequestInfo, commitTitle, commitMessage string) (string, error) {
	body, err := g.ws.CallRestAPI(
		ctx,
		fmt.Sprintf("/repos/%s/%s/merges", config.Config.GithubAppOrganization, repo),
		"",
		"POST",
		map[string]interface{}{
			"base":           pr.Base.Ref,
			"head":           pr.Head.Sha,
			"commit_message": commitTitle + "\n\n" + commitMessage,
		},
		nil)
	if err != nil {
		if httpStatus(err) == 409 {
			return "", fmt.Errorf("error merging the PR into its base branch %s: merge conflict, update the PR with its base branch and retry: %v (%s)", pr.Base.Ref, err, string(body))
		}
		return "", fmt.Errorf("error merging the PR into its base branch %s: %v (%s)", pr.Base.Ref, err, string(body))
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		// 204: the head is already part of the base branch, nothing to merge
		logrus.Infof("force merge of PR %d in %s: the head is already in %s (nothing to merge)", pr.Number, repo, pr.Base.Ref)
		return "", nil
	}
	var commit struct {
		Sha string `json:"sha"`
	}
	if err := json.Unmarshal(body, &commit); err != nil {
		return "", indeterminatef("error parsing the answer of the merge into %s (the merge may have happened): %v (%s)", pr.Base.Ref, err, string(body))
	}
	return commit.Sha, nil
}

// prState reads the state of a PR
func (g *ForcemergeImpl) prState(ctx context.Context, repo string, number int) (string, bool, error) {
	pr, err := g.fetchPullRequest(ctx, repo, strconv.Itoa(number))
	if err != nil {
		return "", false, err
	}
	return pr.State, pr.Merged, nil
}

// confirmMerged waits (bounded) for GET pull to report merged:true. Nothing is retargeted before.
func (g *ForcemergeImpl) confirmMerged(ctx context.Context, repo string, number int) error {
	interval := g.asyncPollInterval
	if interval <= 0 {
		interval = defaultAsyncMergePollInterval
	}
	timeout := g.mergedCheckTimeout
	if timeout <= 0 {
		timeout = defaultMergedCheckTimeout
	}
	deadline := g.clockNow().Add(timeout)
	var lastErr error
	for {
		_, merged, err := g.prState(ctx, repo, number)
		if err == nil && merged {
			return nil
		}
		lastErr = err
		if !g.clockNow().Add(interval).Before(deadline) {
			break
		}
		if serr := g.clockSleep(ctx, interval); serr != nil {
			return indeterminatef("PR #%d: merge reported by GitHub but not confirmed (%v): nothing was retargeted, check the PR", number, serr)
		}
	}
	if lastErr != nil {
		return indeterminatef("PR #%d: merge reported by GitHub but its state could not be read (%v): nothing was retargeted, check the PR", number, lastErr)
	}
	return indeterminatef("PR #%d: GitHub reported the merge but the PR is not merged after %s: nothing was retargeted, check the PR", number, timeout)
}

/*
 * ensureMerged waits for GitHub to mark the PR merged once its head is part of its base branch
 * ("A pull request can be marked as merged if its head branch commits become reachable from the
 * base branch outside that pull request", GitHub docs, "About pull request merges").
 * If GitHub does not, the PR is closed with a comment explaining it was force merged.
 * A merged PR is never closed, and a PR whose state could not be read is not closed.
 */
func (g *ForcemergeImpl) ensureMerged(ctx context.Context, username, repo string, pr *pullRequestInfo, mergeSha string) error {
	interval := g.asyncPollInterval
	if interval <= 0 {
		interval = defaultAsyncMergePollInterval
	}
	timeout := g.mergedCheckTimeout
	if timeout <= 0 {
		timeout = defaultMergedCheckTimeout
	}
	deadline := g.clockNow().Add(timeout)

	// done: merged or closed by someone else
	check := func() (done bool, err error) {
		state, merged, err := g.prState(ctx, repo, pr.Number)
		if err != nil {
			return false, err
		}
		return merged || state == "closed", nil
	}

	var lastErr error
	succeeded := false
	for {
		done, err := check()
		if err == nil {
			succeeded = true
			if done {
				return nil
			}
		} else {
			lastErr = err
		}
		if !g.clockNow().Add(interval).Before(deadline) {
			break
		}
		if serr := g.clockSleep(ctx, interval); serr != nil {
			return fmt.Errorf("PR #%d merged into %s, but checking its state failed: %v", pr.Number, pr.Base.Ref, serr)
		}
	}
	if !succeeded {
		return fmt.Errorf("PR #%d merged into %s, but its state could not be read (%v): it was not closed, check it manually", pr.Number, pr.Base.Ref, lastErr)
	}

	// last look before touching the PR
	done, err := check()
	if err != nil {
		return fmt.Errorf("PR #%d merged into %s, but its state could not be read before closing it (%v): it was not closed, check it manually", pr.Number, pr.Base.Ref, err)
	}
	if done {
		return nil
	}

	// GitHub did not mark it merged: close it, saying where it went
	comment := fmt.Sprintf("Force merged via Goliac on behalf of %s into its base branch `%s`", username, pr.Base.Ref)
	if mergeSha != "" {
		comment += fmt.Sprintf(" (merge commit %s)", mergeSha)
	}
	comment += ". GitHub did not mark this PR as merged, so Goliac closes it."
	body, err := g.ws.CallRestAPI(
		ctx,
		fmt.Sprintf("/repos/%s/%s/issues/%d/comments", config.Config.GithubAppOrganization, repo, pr.Number),
		"",
		"POST",
		map[string]interface{}{
			"body": comment,
		},
		nil)
	if err != nil {
		return fmt.Errorf("PR #%d merged into %s, but commenting before closing it failed: %v (%s)", pr.Number, pr.Base.Ref, err, string(body))
	}
	body, err = g.ws.CallRestAPI(
		ctx,
		fmt.Sprintf("/repos/%s/%s/pulls/%d", config.Config.GithubAppOrganization, repo, pr.Number),
		"",
		"PATCH",
		map[string]interface{}{
			"state": "closed",
		},
		nil)
	if err != nil {
		// a 422 (or any error) when GitHub merged it in the meantime is a success
		if _, merged, gerr := g.prState(ctx, repo, pr.Number); gerr == nil && merged {
			return nil
		}
		return fmt.Errorf("PR #%d merged into %s, but closing it failed: %v (%s)", pr.Number, pr.Base.Ref, err, string(body))
	}
	return nil
}

/*
 * mergeWholeStack merges every PR of the stack into the stack base branch:
 * with the stack 1 > 2 > 3 > 4 > main, a stack merge requested on any of them merges 1 to 4 into main.
 * Every PR of the stack is approved, then the top PR is merged with the asynchronous merge API,
 * which includes all its open downstack PRs.
 */
func (g *ForcemergeImpl) mergeWholeStack(ctx context.Context, plan *mergePlan) (string, error) {
	stack := plan.stack
	pr := plan.pr
	repo := plan.repo
	top := stack[0]
	bottom := stack[len(stack)-1]

	approvals := []*approval{}
	for _, p := range stack {
		review := forcemergeReviewBody(plan.username, p.Number, p.Title, p.HTMLURL, plan.explanation) +
			fmt.Sprintf("\n\nMerge mode: `%s`. Merging the whole stack (%d PRs, #%d to #%d) into `%s`, requested on PR #%d. %s", plan.mode, len(stack), top.Number, bottom.Number, pr.Stack.Base.Ref, pr.Number, pr.stackSource())
		a, err := g.approvePR(ctx, repo, p, review)
		if err != nil {
			return "", g.failAfterApprovals(ctx, repo, approvals, err)
		}
		approvals = append(approvals, a)
	}

	if err := g.headsUnchanged(ctx, repo, stack); err != nil {
		return "", g.failAfterApprovals(ctx, repo, approvals, err)
	}

	// https://docs.github.com/en/rest/pulls/pulls#merge-a-pull-request-asynchronously
	// "For a stacked pull request, the operation includes all open downstack pull requests."
	err := g.mergePRAsync(
		ctx,
		repo,
		top.Number,
		top.Head.Sha,
		forcemergeCommitTitle(top.Title, top.Number, plan.username),
		forcemergeStackCommitMessage(plan.username, pr.Number, stack),
		plan.method,
		plan.explicit,
	)
	if err != nil {
		return "", g.failAfterApprovals(ctx, repo, approvals, err)
	}
	notMerged := []string{}
	for _, p := range stack {
		if err := g.confirmMerged(ctx, repo, p.Number); err != nil {
			notMerged = append(notMerged, err.Error())
		}
	}
	if len(notMerged) > 0 {
		return "", fmt.Errorf("the asynchronous merge of the stack succeeded but not every PR is confirmed merged: %s", strings.Join(notMerged, "; "))
	}
	return fmt.Sprintf("Force merged (stack) into `%s`: %s", pr.Stack.Base.Ref, summarize(stack)), nil
}

/*
 * mergeInferredStack merges a stack inferred from the chain of PRs into the stack base branch (the default branch).
 * The asynchronous merge API only takes the downstack PRs along "for a stacked pull request", i.e. a PR GitHub
 * itself considers stacked (non null stack payload). Merging the top PR of a plain chain would merge it alone
 * into its own base branch. So every PR of the stack is approved, then the PRs are merged one at a time from the
 * bottom up, each with the asynchronous merge API into the default branch: the next PR is retargeted onto the
 * default branch only once GitHub confirms the PR below it merged.
 * On a failure, the PRs already merged stay merged (and listed), and the approvals of the others are dismissed
 * (left in place when the outcome is unknown).
 */
func (g *ForcemergeImpl) mergeInferredStack(ctx context.Context, plan *mergePlan) (string, error) {
	stack := plan.stack // top to bottom
	pr := plan.pr
	repo := plan.repo
	top := stack[0]
	bottom := stack[len(stack)-1]
	base := pr.Stack.Base.Ref

	approvals := map[int]*approval{}
	created := []*approval{}
	for _, p := range stack {
		review := forcemergeReviewBody(plan.username, p.Number, p.Title, p.HTMLURL, plan.explanation) +
			fmt.Sprintf("\n\nMerge mode: `%s`. Merging the whole stack (%d PRs, #%d to #%d) into `%s`, requested on PR #%d. %s GitHub merges a whole stack in one operation only for its own stacked PRs, so the PRs are merged one at a time from the bottom (#%d) up, each PR being retargeted onto `%s` once the PR below it is merged.", plan.mode, len(stack), top.Number, bottom.Number, base, pr.Number, pr.stackSource(), bottom.Number, base)
		a, err := g.approvePR(ctx, repo, p, review)
		if err != nil {
			return "", g.failAfterApprovals(ctx, repo, created, err)
		}
		approvals[p.Number] = a
		created = append(created, a)
	}

	if err := g.headsUnchanged(ctx, repo, stack); err != nil {
		return "", g.failAfterApprovals(ctx, repo, created, err)
	}

	merged := []*pullRequestInfo{}
	fail := func(i int, cause error) error {
		// the PRs not merged yet: stack[0..i]
		pending := []*approval{}
		notMerged := []*pullRequestInfo{}
		for j := 0; j <= i; j++ {
			pending = append(pending, approvals[stack[j].Number])
			notMerged = append(notMerged, stack[j])
		}
		done := "none"
		if len(merged) > 0 {
			done = prNumbers(merged)
		}
		// not merged: bottom-up order, like the merges
		for l, r := 0, len(notMerged)-1; l < r; l, r = l+1, r-1 {
			notMerged[l], notMerged[r] = notMerged[r], notMerged[l]
		}
		return g.failAfterApprovals(ctx, repo, pending, fmt.Errorf("%w; already merged into `%s`: %s; not merged: %s", cause, base, done, prNumbers(notMerged)))
	}

	for i := len(stack) - 1; i >= 0; i-- {
		p := stack[i]
		if len(merged) > 0 {
			// the PR below is merged (and based on the stack base): retarget this one onto the stack base
			below := *merged[len(merged)-1]
			below.Base.Ref = base
			if _, err := g.restack(ctx, repo, &below, []*pullRequestInfo{p}); err != nil {
				return "", fail(i, err)
			}
		}
		err := g.mergePRAsync(
			ctx,
			repo,
			p.Number,
			p.Head.Sha,
			forcemergeCommitTitle(p.Title, p.Number, plan.username),
			forcemergeCommitMessage(plan.username, p.HTMLURL, p.Title)+fmt.Sprintf("\n\n%s", forcemergeStackCommitMessage(plan.username, pr.Number, stack)),
			plan.method,
			plan.explicit,
		)
		if err == nil {
			// nothing is retargeted before GitHub says merged:true
			err = g.confirmMerged(ctx, repo, p.Number)
		}
		if err != nil {
			return "", fail(i, err)
		}
		merged = append(merged, p)
	}
	return fmt.Sprintf("Force merged (stack) into `%s`: %s", base, summarize(stack)), nil
}

func (g *ForcemergeImpl) mergePRSync(ctx context.Context, repo, prNumber, commitTitle, commitMessage, mergeMethod string) ([]byte, error) {
	return g.ws.CallRestAPI(
		ctx,
		fmt.Sprintf("/repos/%s/%s/pulls/%s/merge", config.Config.GithubAppOrganization, repo, prNumber),
		"",
		"PUT",
		map[string]interface{}{
			"commit_title":   commitTitle,
			"commit_message": commitMessage,
			"merge_method":   mergeMethod, // can be "merge", "squash", or "rebase"
		},
		nil)
}

// isStackedPRRefusal detects the 403 GitHub returns when a stacked PR is merged
// through the synchronous merge endpoint:
// "Merging stacked PRs via this endpoint is not supported. Use the asynchronous merge endpoint instead."
func isStackedPRRefusal(body []byte) bool {
	msg := strings.ToLower(string(body))
	return strings.Contains(msg, "stacked") && strings.Contains(msg, "asynchronous merge endpoint")
}

// asyncMergeResult is the payload of
// PUT /repos/{owner}/{repo}/pulls/{pull_number}/merge-async and
// GET /repos/{owner}/{repo}/pulls/{pull_number}/merge-async/{uuid}
type asyncMergeResult struct {
	Status  string `json:"status"` // pending, merged, enqueued, failed
	Details struct {
		Message string `json:"message"`
		UUID    string `json:"uuid"`
		Sha     string `json:"sha"`
	} `json:"details"`
}

func (r *asyncMergeResult) describe() string {
	if r.Details.Message != "" {
		return r.Details.Message
	}
	return "no message from GitHub"
}

// methodRefusedError: GitHub refuses the merge method (405, or 422 about the merge method)
type methodRefusedError struct{ msg string }

func (e *methodRefusedError) Error() string { return e.msg }

// mergePRAsync merges a PR through the asynchronous merge endpoint and waits
// (bounded) for the outcome. When the default "merge" method is refused as not allowed,
// it falls back to "squash" like the synchronous path does.
// https://docs.github.com/en/rest/pulls/pulls#merge-a-pull-request-asynchronously
func (g *ForcemergeImpl) mergePRAsync(ctx context.Context, repo string, prNumber int, headSha, commitTitle, commitMessage, mergeMethod string, explicit bool) error {
	err := g.mergePRAsyncWith(ctx, repo, prNumber, headSha, commitTitle, commitMessage, mergeMethod)
	var refused *methodRefusedError
	if errors.As(err, &refused) && mergeMethod == "merge" && !explicit {
		logrus.Infof("force merge of PR %d in %s: merge commits are refused, retrying with a squash merge", prNumber, repo)
		return g.mergePRAsyncWith(ctx, repo, prNumber, headSha, commitTitle, commitMessage, "squash")
	}
	return err
}

func (g *ForcemergeImpl) mergePRAsyncWith(ctx context.Context, repo string, prNumber int, headSha, commitTitle, commitMessage, mergeMethod string) error {
	payload := map[string]interface{}{
		"commit_title":   commitTitle,
		"commit_message": commitMessage,
		"merge_method":   mergeMethod,
		// a force merge merges now: never park the PR in a merge queue
		"merge_action": "direct_merge",
		// same as the synchronous endpoint: let Goliac use its bypass permissions
		"bypass_rules": true,
	}
	if headSha != "" {
		// only merge the head that was approved; GitHub cancels the merge if the PR is pushed meanwhile
		payload["sha"] = headSha
	}

	body, err := g.ws.CallRestAPI(
		ctx,
		fmt.Sprintf("/repos/%s/%s/pulls/%d/merge-async", config.Config.GithubAppOrganization, repo, prNumber),
		"",
		"PUT",
		payload,
		nil)
	if err != nil {
		lower := strings.ToLower(string(body))
		switch httpStatus(err) {
		case 409:
			var pending asyncMergeResult
			uuid := ""
			if jerr := json.Unmarshal(body, &pending); jerr == nil {
				uuid = pending.Details.UUID
			}
			// not ours: adopting it would report a merge we did not request (other method, other message)
			return indeterminatef("another asynchronous merge is already pending for PR #%d (request %q): Goliac does not adopt it. Wait for it or cancel it, then retry: %v (%s)", prNumber, uuid, err, string(body))
		case 400:
			return fmt.Errorf("error merging the PR asynchronously: the PR is not ready to be merged (closed or draft?): %v (%s)", err, string(body))
		case 403:
			return fmt.Errorf("error merging the PR asynchronously: refused (permissions or rules): %v (%s)", err, string(body))
		case 404:
			return fmt.Errorf("error merging the PR asynchronously: PR #%d not found: %v (%s)", prNumber, err, string(body))
		case 405:
			return &methodRefusedError{msg: fmt.Sprintf("error merging the PR asynchronously: merge method %s not allowed: %v (%s)", mergeMethod, err, string(body))}
		case 422:
			if strings.Contains(lower, "merge method") || strings.Contains(lower, "not allowed") {
				return &methodRefusedError{msg: fmt.Sprintf("error merging the PR asynchronously: merge method %s refused: %v (%s)", mergeMethod, err, string(body))}
			}
			return fmt.Errorf("error merging the PR asynchronously: validation failed: %v (%s)", err, string(body))
		}
		return fmt.Errorf("error merging the PR asynchronously: %v (%s)", err, string(body))
	}

	var result asyncMergeResult
	if err := json.Unmarshal(body, &result); err != nil {
		return indeterminatef("error parsing the asynchronous merge response (the merge may have been accepted): %v (%s)", err, string(body))
	}
	return g.waitAsyncMerge(ctx, repo, prNumber, &result)
}

// waitAsyncMerge polls the asynchronous merge request until it is final or the timeout is reached.
// "merged" is the only success: "enqueued" (merge queue) is not a merge.
func (g *ForcemergeImpl) waitAsyncMerge(ctx context.Context, repo string, prNumber int, result *asyncMergeResult) error {
	interval := g.asyncPollInterval
	if interval <= 0 {
		interval = defaultAsyncMergePollInterval
	}
	timeout := g.asyncPollTimeout
	if timeout <= 0 {
		timeout = defaultAsyncMergePollTimeout
	}
	deadline := g.clockNow().Add(timeout)
	uuid := result.Details.UUID

	for {
		switch result.Status {
		case "merged":
			return nil
		case "enqueued":
			return indeterminatef("PR #%d was queued in a merge queue, not merged (request %s): nothing was retargeted. It merges when the queue gets to it: %s", prNumber, uuid, result.describe())
		case "failed":
			return fmt.Errorf("GitHub refused the asynchronous merge (request %s): %s", uuid, result.describe())
		case "pending":
			if uuid == "" {
				return indeterminatef("asynchronous merge is pending but GitHub returned no request id: %s", result.describe())
			}
		default:
			return indeterminatef("unexpected asynchronous merge status %q (request %s): %s", result.Status, uuid, result.describe())
		}

		if !g.clockNow().Add(interval).Before(deadline) {
			return indeterminatef("timeout after %s waiting for the asynchronous merge (request %s) to complete: still pending, check the PR before retrying", timeout, uuid)
		}
		if err := g.clockSleep(ctx, interval); err != nil {
			return indeterminatef("asynchronous merge (request %s) still pending: %v", uuid, err)
		}

		body, err := g.ws.CallRestAPI(
			ctx,
			fmt.Sprintf("/repos/%s/%s/pulls/%d/merge-async/%s", config.Config.GithubAppOrganization, repo, prNumber, uuid),
			"",
			"GET",
			nil,
			nil)
		if err != nil {
			if httpStatus(err) == 404 {
				return indeterminatef("asynchronous merge request %s not found (expired?): %v (%s)", uuid, err, string(body))
			}
			// transient error: keep polling until the deadline
			logrus.Warnf("error polling asynchronous merge %s of PR %d in %s: %v (%s)", uuid, prNumber, repo, err, string(body))
			continue
		}
		var next asyncMergeResult
		if err := json.Unmarshal(body, &next); err != nil {
			return indeterminatef("error parsing the asynchronous merge status: %v (%s)", err, string(body))
		}
		if next.Details.UUID == "" {
			next.Details.UUID = uuid
		}
		result = &next
	}
}
