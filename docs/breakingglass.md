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

The merge mode is the token right after the workflow name colon (case-insensitive): `/forcemerge:standard:stack: why`, or a bare `/forcemerge:standard:stack`. After a space it is explanation text, so `/forcemerge:standard: stack: why` and `/forcemerge:standard: fix: prod is down` have no mode. A word immediately followed by a colon that is not `stack` or `direct` (`/forcemerge:standard:fix: x`) is refused as an unknown merge mode. The approval review always states the resolved mode (`Merge mode: none (this PR is not part of a stack).` for a PR that is not stacked), and for a stacked PR where the stack comes from (`Stack source: GitHub stack metadata.` or `Stack source: inferred from the chain of open PRs (GitHub sent no stack metadata).`). A redelivered GitHub comment (same comment id) is ignored, and a second force merge of a PR that is already being force merged is refused.

Take the stack `1 > 2 > 3 > 4 > main`: PR 1 is the top (based on PR 2's branch) and PR 4 is the bottom (based on `main`).

- **Default, or `direct`**: merge only this PR into its base branch, then restack.
  - `/forcemerge:standard: <explanation>` on PR 3 merges PR 3 into PR 4's branch with a merge commit. Then every open PR based on PR 3's branch (PR 2) is retargeted onto PR 4's branch. The stack becomes `1 > 2 > 4 > main`.
  - On the bottom PR (PR 4), the PR is merged alone into `main` through the asynchronous merge API, and PR 3 is retargeted onto `main`.
  - On the top PR (PR 1), the PR is merged into PR 2's branch and there is nothing to retarget.
  - `/squash` is not available when merging into a parent branch: a merge commit is used, and the approval comment says so.
  - GitHub usually marks the PR as merged once its commits are in its base branch. If it does not, Goliac closes the PR with a comment saying where it was merged.
  - The PR's head branch is not deleted.
- Before anything is approved, Goliac checks the PR (and, for `stack`, every PR of the stack): open, not merged, not a draft, head in the repository (no fork), consistent stack metadata, and for a direct merge into a parent branch that the PR below exists in the same stack. Otherwise nothing is approved, merged or notified. The workflow steps (audit, Slack...) run once per merged PR after this check and before the first change.
- The merge method is chosen from the repository settings (`allow_merge_commit`, `allow_squash_merge`, `allow_rebase_merge`): the default merge commit becomes a squash (then a rebase) when the repository forbids merge commits (e.g. squash-only repositories). `/squash` is never replaced: it is refused when squash is not allowed. Merging a mid-stack PR into its parent branch always makes a merge commit (it is a branch merge, not a PR merge, so it does not depend on those settings); on a squash-only repository that commit lands on the parent PR's branch and the bottom PR is squashed into the default branch afterwards.
- A PR is retargeted only after GitHub confirms it merged (`merged: true`). `enqueued` (merge queue) is reported as "queued, not merged": nothing is retargeted. A foreign pending merge request (409) is reported, not adopted. Retargeting is retried with a backoff, and a persistent failure names the PRs and the manual fix.
- If the merge definitively fails after the approvals, the approvals Goliac created are dismissed (or listed in the error when they can't be). When the outcome is unknown (timeout, queue), they are left in place and listed.
- Just before the asynchronous merge the heads are re-read: a push after the approval aborts the merge.
- **`stack`**: merge the whole stack into the stack base branch.
  - `/forcemerge:standard:stack: <explanation>` on any PR of the stack approves every PR from 1 to 4. It then merges the top PR (PR 1) through the asynchronous merge API, which merges PR 1 to PR 4 into `main`.
  - For a stack inferred from base branches, the PRs are merged one at a time from the bottom up instead (see below).
  - If a PR of the stack is missing or not open, nothing is approved or merged.

On a PR that is not part of a stack, `direct` and `stack` are ignored (the approval comment says so) and the PR is merged as usual.

### Stacks inferred from base branches

GitHub sets the `stack` field of a pull request only for PRs created with its own stacked pull requests feature. A plain chain of PRs, where each PR is based on the head branch of the PR below it (`t3` based on `t2`'s branch, `t2` based on `t1`'s branch, `t1` based on `main`), has `stack: null`. When GitHub sends no `stack`, Goliac infers the stack from that chain. When GitHub sends one, it is used and nothing is inferred.

- The chain is made of open PRs of the repository. Goliac walks **down** from the PR (the open PR whose head branch is this PR's base branch, repeated) until a PR is based on the repository default branch: that branch is the stack base. It then walks **up** from the bottom (the open PR based on each head branch) to the top.
- A PR with no open PR below it and none above it is not a stack, whatever its base branch (e.g. a release branch): it is merged as usual.
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
