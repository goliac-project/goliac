# Breaking Glass workflow

If you enable the Breaking Glass workflow, you will be able to force merge PRs in specific repositories and for specific teams. This is useful when you need to merge a PR in an emergency situation.

## First way: use the wizard

![PR breaking glass - wizard](images/breakingglass.png)

![PR breaking glass - wizard 2](images/forcemerge_workflow.png)

You have the possibliity to enable a Breaking Glass workflow that allows to force merge PRs in specific repositories and for specific teams. This is useful when you need to merge a PR in an emergency situation.

![PR breaking glass - merged](images/forcemerge_pr_merged.png)


See [Workflows](resource_workflow) to see how to enable it

## Second way: use PR comments

You can also use PR comments to force merge a PR.

```
/forcemerge:<workflow_name>: <explanation>
```

Example:

```
/forcemerge:standard: need to merge this PR out of regular office hours to fix the production issue
```

This will trigger the `standard` workflow to merge the PR.

## Squash merge

By default, the PR will be merged with the `commit` merge type.


If you want to squash merge the PR, you can add the `/squash` string in the explanation.

Example:

```
/forcemerge:standard: need to merge this PR out of regular office hours to fix the production issue /squash
```

This will trigger the `standard` workflow to merge the PR with the `squash` merge type.

## Stacked PRs

GitHub only merges [stacked pull requests](https://docs.github.com/en/rest/pulls/pulls#merge-a-pull-request-asynchronously) through its asynchronous merge API. A PR is part of a stack when GitHub says so (the `stack` field of the pull request, set for PRs created with GitHub's stacked pull requests) or, when GitHub sends no `stack`, when Goliac infers one from a chain of PRs based on each other's branches (see [Stacks inferred from base branches](#stacks-inferred-from-base-branches)). For a PR that is part of a stack, you can add an optional merge mode between the workflow name and the explanation:

```
/forcemerge:<workflow_name>: <explanation>
/forcemerge:<workflow_name>:direct: <explanation>
/forcemerge:<workflow_name>:stack: <explanation>
```

The merge mode is the token right after the workflow name colon (case-insensitive): `/forcemerge:standard:stack: why`, or a bare `/forcemerge:standard:stack` (which is then refused with "No explanation provided": the explanation is always required). After a space it is explanation text, so `/forcemerge:standard: stack: why` and `/forcemerge:standard: fix: prod is down` have no mode. A word immediately followed by a colon that is not `stack` or `direct` (`/forcemerge:standard:fix: x`) is refused as an unknown merge mode. The approval review always states the resolved mode (`Merge mode: none (this PR is not part of a stack).` for a PR that is not stacked), and for a stacked PR where the stack comes from (`Stack source: GitHub stack metadata.` or `Stack source: inferred from the chain of open PRs (GitHub sent no stack metadata).`). A redelivered GitHub comment (same comment id) is ignored, and a second force merge of a PR that is already being force merged is refused.

Take the stack `1 > 2 > 3 > 4 > main`: PR 1 is the top (based on PR 2's branch) and PR 4 is the bottom (based on `main`).

- **Default, or `direct`**: merge only this PR into its base branch, then restack. On a stack created with GitHub's stacked pull requests, only the bottom PR can be merged this way (see below).
  - On a stack inferred from base branches (see below), `/forcemerge:standard: <explanation>` on PR 3 merges PR 3 into PR 4's branch with a merge commit. Then every open PR based on PR 3's branch (PR 2) is retargeted onto PR 4's branch. The stack becomes `1 > 2 > 4 > main`.
  - On the bottom PR (PR 4), the PR is merged alone into `main` through the asynchronous merge API, and PR 3 is retargeted onto `main`. For a GitHub stack, GitHub retargets PR 3 onto `main` by itself and refuses any other change of the base of a stacked PR ("part of a stack"), so Goliac only waits for it and never patches the base.
  - On the top PR (PR 1), the PR is merged into PR 2's branch and there is nothing to retarget.
  - `/squash` is not available when merging into a parent branch: a merge commit is used, and the approval comment says so.
  - GitHub usually marks the PR as merged once its commits are in its base branch. If it does not, Goliac closes the PR with a comment saying where it was merged.
  - Goliac does not delete the head branch of the merged PR. When the repository deletes merged branches (`delete_branch_on_merge`), GitHub deletes it by itself right after the merge (and retargets the PRs above it, see below).
- **GitHub stacks (stacked pull requests created with GitHub)**: GitHub does not support merging a PR out of the middle (or the top) of its stack, because it retargets the PRs above onto the stack base by itself. A default or `direct` force merge of a PR that is not the bottom PR of its GitHub stack is therefore refused (nothing approved, merged or notified): merge the bottom PR first, or use `stack`. This does not apply to stacks inferred from base branches (below).
- Before anything is approved, Goliac checks the PR (and, for `stack`, every PR of the stack): open, not merged, not a draft, head in the repository (no fork), consistent stack metadata, and for a direct merge into a parent branch that the PR below exists in the same stack. Otherwise nothing is approved, merged or notified. The workflow steps (audit, Slack...) run once per merged PR after this check and before the first change.
- The merge method is chosen from the repository settings (`allow_merge_commit`, `allow_squash_merge`, `allow_rebase_merge`): the default merge commit becomes a squash (then a rebase) when the repository forbids merge commits (e.g. squash-only repositories). `/squash` is never replaced: it is refused when squash is not allowed. Merging a mid-stack PR into its parent branch always makes a merge commit (it is a branch merge, not a PR merge, so it does not depend on those settings); on a squash-only repository that commit lands on the parent PR's branch and the bottom PR is squashed into the default branch afterwards.
- A PR is retargeted only after GitHub confirms it merged (`merged: true`). `enqueued` (merge queue) is reported as "queued, not merged": nothing is retargeted. A foreign pending merge request (409) is reported, not adopted. Retargeting is retried with a backoff, and a persistent failure names the PRs and the manual fix. Retargeting is idempotent: when the repository deletes merged branches (`delete_branch_on_merge`), GitHub retargets the PRs above a merged PR by itself, so a PR whose base is already the target is left alone, and a `422` from the retarget is a success when re-reading the PR shows it is now on the target (GitHub's own retarget landed in between). Any other refusal is not retried: the PRs not merged are reported and their approvals dismissed.
- If the merge definitively fails after the approvals, the approvals Goliac created are dismissed (or listed in the error when they can't be). A refusal from GitHub (400, 403, 404, 405, 409, 422) is definitive. When the outcome is unknown, they are left in place and listed, and the error says to check the PR before retrying. That is the case for a transport error, a timeout, a cancelled request or a 5xx on a merge request (GitHub may have merged), a merge queue, an asynchronous merge that is still pending, a `409` on the asynchronous merge that reports an existing pending request (or whose body GitHub does not document), and a `200` answer that is not a merge result.
- Every merge is pinned to the head that was approved, and the PRs are re-read just before it: a push after the approval aborts the merge, and the synchronous merge of a plain PR sends the approved `sha` (GitHub answers `409 Head branch was modified` if the PR was pushed to in between: nothing is merged and the approval is dismissed). The base branch of every PR, its state and its draft status are re-checked too: a PR that was retargeted, closed, merged or turned into a draft since the validation refuses the merge. After the other PRs of the operation are locked, the PRs of the plan are read again before the first change; the parent PR of a direct merge is read again before the merge into its branch; the PRs above a direct merge are listed again after the merge, so a PR stacked on top meanwhile is retargeted too.
- The merge method is only changed when GitHub says the method is not allowed on the repository (`Merge commits are not allowed on this repository.`, a `405` or `422` about the merge method), or, for a plain PR, when the repository settings could not be read. Any other `405` or `422` (`Base branch was modified`, `Pull Request is not mergeable`, `mergeable` not computed yet just after a retarget...) keeps the method: the same request is retried with a backoff (four attempts), then refused with GitHub's message.
- The lists of PRs (chain walks, PRs above, forks) are read 100 per page, page after page, until a page is not full (up to 10 000 PRs).
- GitHub does its part asynchronously after a merge (marking a PR merged, retargeting the PR above a merged PR of a GitHub stack): Goliac waits up to 60 seconds for it by default (`GOLIAC_WORKFLOW_FORCEMERGE_WAIT_SECONDS`), with a backoff. This applies to the wait for `merged: true` after an asynchronous merge, to the wait for GitHub to mark a PR merged after a merge into a parent branch, and to the wait for the retarget of a GitHub stack. (The wait for the asynchronous merge itself is 2 minutes.)
- Goliac's lock against a second force merge of the same PR lives in the memory of the Goliac process: it assumes a single Goliac replica, and it does not survive a restart in the middle of a merge. Two replicas, or a restart, can run two force merges of the same PR at once; the pinned head and the checks above still protect the merge itself.
- On a repository that only allows squash (or rebase) merges, or when `/squash` is asked: squash and rebase rewrite the commits of the merged PR, so the PRs above it, which carry the original commits, would conflict once retargeted. `stack` on a stack of 2 or more PRs is refused before any approval, and so is `direct` on the bottom PR when PRs are open above it. Merge the PRs from the top down with `direct` instead (each PR is merged into its parent branch with a merge commit, which does not depend on the repository settings): the last one, with nothing above it, is then squashed as usual. A single PR, or a bottom PR with nothing above, is squashed as usual.
- **`stack`**: merge the whole stack into the stack base branch.
  - `/forcemerge:standard:stack: <explanation>` on any PR of the stack approves every PR from 1 to 4. It then merges the PRs one at a time from the bottom up (PR 4, 3, 2, then 1), each into `main` through the asynchronous merge API (head pinned, rules bypassed, no merge queue). GitHub only allows Goliac to bypass the rules on the bottom-most unmerged PR of a GitHub stack (merging the top PR alone with the bypass answers `400 Rules can only be bypassed when merging the bottom-most unmerged pull request in the stack`), and a plain chain can't use a single asynchronous merge either (see below).
  - For a GitHub stack, GitHub retargets the next PR onto `main` after each merge: Goliac waits for it (it never patches the base of a stacked PR). For a stack inferred from base branches, Goliac retargets the next PR itself.
  - If a merge fails or GitHub does not retarget the next PR, the PRs already merged stay merged, and the error lists the merged and not merged PRs; the approvals of the PRs not merged are dismissed (left in place when the outcome is unknown, e.g. a timeout). The review of every PR says why the PRs are merged one at a time.
  - If a PR of the stack is missing or not open, nothing is approved or merged.

On a PR that is not part of a stack, `direct` and `stack` are ignored (the approval review says `Merge mode `stack` ignored: this PR is not part of a stack.`; without a mode it says `Merge mode: none (this PR is not part of a stack).`) and the PR is merged as a plain PR (pinned to the approved head, with the merge method rules above).

### Stacks inferred from base branches

GitHub sets the `stack` field of a pull request only for PRs created with its own stacked pull requests feature. A plain chain of PRs, where each PR is based on the head branch of the PR below it (`t3` based on `t2`'s branch, `t2` based on `t1`'s branch, `t1` based on `main`), has `stack: null`. When GitHub sends no `stack`, Goliac infers the stack from that chain. When GitHub sends one, it is used and nothing is inferred.

- The chain is made of open PRs of the repository. Goliac walks **down** from the PR (the open PR whose head branch is this PR's base branch, repeated) until a PR is based on the repository default branch: that branch is the stack base. It then walks **up** from the bottom (the open PR based on each head branch) to the top.
- A PR with no open PR below it and none above it is not a stack, whatever its base branch (e.g. a release branch): it is merged (pinned to the approved head), unless its base branch is dead: the newest PR (by creation date, open, closed or merged) whose head is that branch is closed, merged or not, and the branch has not been pushed since (its tip is still that PR's head). The chain was broken (or the branch was merged and kept) and merging would strand the change, so that is refused. A long-lived branch (release, develop) that moved on since its last PR was closed or merged, or whose newest PR is open, is merged into as usual. A lone PR is merged even when the default branch of the repository can't be read (the merge method then falls back from merge to squash). If Goliac can't read the PRs or the tip of the branch, it refuses.
- Refused (nothing is approved, merged or notified, and the error says why):
  - the chain forks: two open PRs are based on the same branch, anywhere in the chain;
  - the chain loops (cycle);
  - the chain does not reach the default branch: the bottom PR found is based on a branch that is neither the default branch nor the head branch of an open PR (e.g. the PR below was closed). Merge or retarget that PR first;
  - the chain is longer than 50 PRs;
  - a PR of the chain is a draft, closed, comes from a fork, or carries GitHub stack metadata while the requested PR does not;
  - the default branch of the repository can't be read.
  These checks apply to both merge modes: a direct merge is only done on a chain that is a valid stack.
- **`direct`** (or no mode) works exactly as for a GitHub stack: a mid-stack PR is merged into its parent branch and the PRs above are retargeted; the bottom PR is merged into the default branch with the asynchronous merge API, then the PR above is retargeted onto it.
- **`stack`** can't use a single asynchronous merge of the top PR: the asynchronous merge API only takes the downstack PRs along "for a stacked pull request", i.e. a PR GitHub itself considers stacked. On a plain chain it would merge the top PR alone into its own base branch. So Goliac approves every PR of the stack, re-reads their heads, then merges them **one at a time from the bottom up**, each into the default branch through the asynchronous merge API (head pinned, no merge queue, merge method chosen as above). Each next PR is retargeted onto the default branch only after GitHub confirms the PR below it merged.
  - If a merge or a retarget fails, the PRs already merged stay merged, and the error lists the merged and not merged PRs. The approvals of the PRs not merged are dismissed (left in place when the outcome is unknown, e.g. a timeout).
  - The review of every PR says the stack was inferred and is merged bottom-up.

The asynchronous merge:
- pins the PR head that Goliac approved (a push in between cancels the merge);
- never uses a merge queue;
- bypasses the rules Goliac is allowed to bypass.

Goliac waits up to 2 minutes for the result. A timeout, a refusal (e.g. conflicts) or a PR that is not ready to merge (closed, draft) is reported as an error comment.
