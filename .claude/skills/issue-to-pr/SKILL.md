---
name: issue-to-pr
description: >-
  End-to-end implement a GitHub issue and open a ready pull request. Use this
  whenever the user wants you to actually FIX or BUILD something from an issue —
  phrasings like "fix issue 123", "implement issue #45", "work on issue 200",
  "take issue 88 to a PR", "start a worktree and resolve this issue", or pastes
  an issue URL and says "do this". It creates an isolated git worktree off the
  latest main, finds the root cause (bug) or scopes the feature, implements the
  smallest correct change following repo conventions, verifies it (build/lint/
  tests), runs a Codex adversarial review and addresses the findings, then
  commits and opens a ready PR. Do NOT use this for pure triage (use
  triage-issue), pure code review of an existing PR (use review-pr-or-mr /
  code-review), or when the user only wants analysis without code changes.
---

# Issue → PR

Turn a single GitHub issue into a verified, reviewed, ready-to-merge pull
request — autonomously — while staying honest about uncertainty and stopping
when a human decision is genuinely required.

The goal is not "produce a diff." It is: **understand the real problem, make the
smallest change that correctly solves it, prove it works, have an adversary try
to break it, and only then ship a PR.** Speed comes from doing these in order,
not from skipping them.

## Inputs

- An issue number (`123`), a `#123` reference, or a full issue URL.
- The repo is the current git repository; PRs target `origin` (`Silo-Server/silo-server`) and base `main`.

If no issue is given, ask for one. Do not guess.

## Operating principles

- **Root cause over symptom.** For bugs, reproduce first and find *why* it
  happens before touching code. A fix you can't explain is a fix you can't trust.
- **Smallest correct change.** Match surrounding code's idioms, comment density,
  and naming. Put code in the package that owns the behavior (see CLAUDE.md);
  don't create catch-all helpers or duplicate logic — extract shared logic when
  you find yourself copying.
- **Verify before you believe.** Build, lint, and run the relevant tests. Report
  failures honestly; never claim something passes that you didn't run.
- **Let the adversary win sometimes.** The Codex adversarial review exists to
  break your confidence. Take its material findings seriously and fix them; loop
  until it's clean or you can defend why a finding doesn't apply.
- **Know when to stop.** See [Stop and ask](#stop-and-ask). Autonomy is for
  execution, not for inventing product decisions or shipping unverifiable guesses.

Track the phases below with a task list (TaskCreate/TaskUpdate) so progress is
visible and nothing is skipped on long runs.

## Workflow

### 1. Understand the issue

Read everything before writing anything.

```bash
gh issue view <N> --json number,title,body,labels,state,author,url,comments
```

Determine:
- **Bug vs. feature** — from labels (`bug`, `enhancement`, `feature`…) and the
  body. This shapes branch prefix (`fix/` vs `feat/`) and commit type.
- **Acceptance criteria** — what does "done" mean? If the issue is vague,
  underspecified, or needs a product call, stop and ask (see [Stop and ask](#stop-and-ask)).
- **Linked scope** — does the issue reference an epic / sub-issue (e.g. "Part of
  #NNN")? You'll link it in the PR. Per CLAUDE.md, PRs should link the capability
  epic or sub-issue they serve.
- **Client surface** — does this touch API contracts, auth, playback, session,
  library, or metadata behavior consumed by `silo-android` / `silo-apple`? If so,
  note that coordinated client follow-up may be needed (call it out in the PR).

### 2. Create a worktree off the latest main

Work in isolation so the user's current checkout is untouched. Create the
worktree at the **main repository root**, not inside the current worktree.

```bash
# Resolve the primary repo root (works from inside any worktree)
MAIN_ROOT="$(cd "$(dirname "$(git rev-parse --git-common-dir)")" && pwd)"

# Always branch off the freshly fetched main
git -C "$MAIN_ROOT" fetch origin

SLUG=<short-kebab-slug-from-issue-title>      # e.g. preserve-metadata-on-toggle
BRANCH=fix/issue-<N>-$SLUG                      # use feat/ for features
WT="$MAIN_ROOT/.claude/worktrees/issue-<N>"

git -C "$MAIN_ROOT" worktree add -b "$BRANCH" "$WT" origin/main
cd "$WT"
```

If the branch or worktree already exists from a previous run, reuse it (cd in)
rather than failing — but make sure it's based on current `origin/main`; if it's
stale, tell the user before continuing.

From here on, **all work happens in `$WT`** (the Bash tool keeps the working
directory between calls). Use absolute paths when in doubt.

### 3. Investigate and plan

- **Bug:** reproduce it (a failing test, a script, or a precise trace through the
  code). Pin down the exact faulty code path and the invariant it violates.
  Prefer adding/adjusting a test that fails before the fix and passes after.
- **Feature:** map where it fits — the owning package, existing patterns to
  follow, the data flow, and the smallest set of files to change. For anything
  architectural or multi-file, write a brief plan and confirm direction before a
  large build-out.

### 4. Implement

Make the change. Keep it scoped to this one concern (one concern per PR). Respect
the hard rules:

- **API:** additive-only within `/api/v1` — never rename/remove a response field,
  change a field's type, or repurpose a status code. New behavior = new
  fields/endpoints; expose capability endpoints for feature detection.
- **Migrations:** new DB changes are Goose SQL migrations created with
  `make migrate-create NAME=...` (timestamped). Never `goose fix`, never paired
  `.up.sql`/`.down.sql`. See CLAUDE.md.
- **Maintainability:** extract shared logic instead of duplicating; change
  existing code rather than bolting on local workarounds.

### 5. Verify

Run the relevant checks and make them pass. The exact commands (and the worktree
build quirks — `GOWORK=off`, stubbing `web/dist`, the v2 golangci invocation, the
flaky GPU tests) are in **[references/verification.md](references/verification.md)** — read it before running anything,
because plain `go build ./...` / `make lint` fail inside `.claude/worktrees`.

At minimum, before moving on:
- Go: build + the relevant package tests pass.
- Frontend (if `web/` changed): `pnpm run lint`, `pnpm run format:check`, build.
- `make verify-local-paths` is clean.

If you can't make checks pass, stop and report — don't paper over it.

### 6. Adversarial review (Codex)

Commit your work first (so the review sees exactly what will ship), then run a
Codex adversarial review of the branch against `main`. Full invocation,
output handling, looping, and fallback are in
**[references/codex-review.md](references/codex-review.md)** — follow it exactly.

The loop in short:
1. Commit the implementation on the branch.
2. Run the adversarial review (`--base main`, foreground/`--wait`, extended timeout).
3. For each **material** finding, either fix it (then re-run the review) or write
   down a defensible reason it doesn't apply.
4. Repeat until the review returns `approve` / no material findings, or you've
   resolved/dispositioned everything and can defend the result.

Do not open the PR while a material, un-rebutted adversarial finding stands.

### 7. Commit, push, open the PR

Commits use Conventional Commit subjects scoped to the domain, e.g.
`fix(playback): guard against nil session on reconnect` or
`feat(catalog): add per-library scan toggle`. End commit messages with the
trailer:

```
Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
```

Push and open a **ready** (non-draft) PR against `main`:

```bash
git push -u origin "$BRANCH"
gh pr create --base main --head "$BRANCH" --title "<conventional subject>" --body "$(cat <<'EOF'
## Problem
<what's broken / what's missing, grounded in the issue>

## Approach
<what you changed and *why this approach* — the tradeoff, not a file list>

## Verification
<commands run and their result; tests added; manual checks>

## Adversarial review
<one line: clean, or the findings and how each was resolved/rebutted>

## Risks & follow-up
<rollout/migration/compatibility risks; any client (silo-android / silo-apple)
follow-up; anything deferred>

Closes #<N>
<add "Part of #<epic>" if the issue serves a capability epic/sub-issue>

## AI-use disclosure
Implemented by Claude Code (issue-to-pr skill) with human oversight.
EOF
)"
```

End the PR body with:

```
🤖 Generated with [Claude Code](https://claude.com/claude-code)
```

For **UI changes**, attach before/after screenshots or a recording (the repo
requires this) — capture them or, if you can't, explicitly flag in the PR that
they're still needed.

### 8. Report back

Give the user the PR URL, a 2–3 line summary of the change, the verification
result, the adversarial-review outcome, and any flagged risks or required
client-side follow-up. Mention the worktree path so they can inspect it.

## Stop and ask

Pause and ask the user (don't push a PR) when:

- The issue is ambiguous, underspecified, or requires a product/UX decision.
- The "fix" would require an API contract change that can't stay additive, or a
  risky/irreversible migration.
- The change balloons into something large or architectural — propose a plan first.
- Checks can't be made to pass, or the adversarial review keeps surfacing material
  findings you can't resolve or rebut.
- The right fix clearly belongs in a sibling repo (`silo-android`, `silo-apple`,
  a plugin/SDK repo) rather than here — see CLAUDE.md's multi-repo guidance.

In these cases, summarize what you found and what you'd do, and let the user decide.

## Notes

- This skill is authorized to open ready PRs autonomously once checks pass and the
  adversarial review is clean — that's the durable instruction from setup. It is
  not authorized to merge, force-push shared branches, or close issues by hand.
- Never invent a fix you can't verify or explain. An honest "here's what's
  uncertain" beats a confident wrong PR.
